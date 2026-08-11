// 本文件装配 Gateway 下游 gRPC 客户端，并统一管理其连接资源。
package gateway

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	gresolver "google.golang.org/grpc/resolver"

	clientv3 "go.etcd.io/etcd/client/v3"
	resolver "go.etcd.io/etcd/client/v3/naming/resolver"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"

	"seckill-mall/shared/contracts"
	pb "seckill-mall/shared/gen/commerce"
	"seckill-mall/shared/platform/config"
)

type grpcClients struct {
	commerceOrder pb.CommerceOrderServiceClient
	catalog       pb.CatalogServiceClient
	inventory     pb.InventoryServiceClient
	identity      pb.IdentityServiceClient
	cart          pb.CartServiceClient
	payment       pb.PaymentServiceClient
	fulfillment   pb.FulfillmentServiceClient
	closers       []io.Closer
}

func initGRPCClients() (clients grpcClients, initErr error) {
	defer func() {
		if initErr != nil {
			initErr = errors.Join(initErr, clients.Close())
		}
	}()

	etcdAddr := config.Conf.Etcd.Addr
	var etcdResolver gresolver.Builder
	if etcdAddr != "" {
		cli, err := clientv3.New(clientv3.Config{Endpoints: []string{etcdAddr}, DialTimeout: 5 * time.Second})
		if err != nil {
			return clients, fmt.Errorf("etcd connect failed: %w", err)
		}
		clients.closers = append(clients.closers, cli)
		builtResolver, err := resolver.NewBuilder(cli)
		if err != nil {
			return clients, fmt.Errorf("etcd resolver create failed: %w", err)
		}
		etcdResolver = builtResolver
	}

	catalogName := config.Conf.Catalog.ServiceName
	if catalogName == "" {
		catalogName = contracts.ServiceCatalog
	}
	catalogConn, err := dialService(catalogName, config.Conf.Catalog.Address, etcdResolver)
	if err != nil {
		return clients, err
	}
	clients.closers = append(clients.closers, catalogConn)
	inventoryName := config.Conf.Inventory.ServiceName
	if inventoryName == "" {
		inventoryName = contracts.ServiceInventory
	}
	inventoryConn, err := dialService(inventoryName, config.Conf.Inventory.Address, etcdResolver)
	if err != nil {
		return clients, err
	}
	clients.closers = append(clients.closers, inventoryConn)
	orderServiceName := config.Conf.Order.ServiceName
	if orderServiceName == "" {
		orderServiceName = contracts.ServiceOrder
	}
	commerceOrderConn, err := dialService(orderServiceName, config.Conf.Order.Address, etcdResolver)
	if err != nil {
		return clients, err
	}
	clients.closers = append(clients.closers, commerceOrderConn)
	identityName := config.Conf.Identity.ServiceName
	if identityName == "" {
		identityName = contracts.ServiceIdentity
	}
	identityConn, err := dialService(identityName, config.Conf.Identity.Address, etcdResolver)
	if err != nil {
		return clients, err
	}
	clients.closers = append(clients.closers, identityConn)
	cartName := config.Conf.Cart.ServiceName
	if cartName == "" {
		cartName = contracts.ServiceCart
	}
	cartConn, err := dialService(cartName, config.Conf.Cart.Address, etcdResolver)
	if err != nil {
		return clients, err
	}
	clients.closers = append(clients.closers, cartConn)
	paymentName := config.Conf.Payment.ServiceName
	if paymentName == "" {
		paymentName = contracts.ServicePayment
	}
	paymentConn, err := dialService(paymentName, config.Conf.Payment.Address, etcdResolver)
	if err != nil {
		return clients, err
	}
	clients.closers = append(clients.closers, paymentConn)
	fulfillmentName := config.Conf.Fulfillment.ServiceName
	if fulfillmentName == "" {
		fulfillmentName = contracts.ServiceFulfillment
	}
	fulfillmentConn, err := dialService(fulfillmentName, config.Conf.Fulfillment.Address, etcdResolver)
	if err != nil {
		return clients, err
	}
	clients.closers = append(clients.closers, fulfillmentConn)

	clients.commerceOrder = pb.NewCommerceOrderServiceClient(commerceOrderConn)
	clients.catalog = pb.NewCatalogServiceClient(catalogConn)
	clients.inventory = pb.NewInventoryServiceClient(inventoryConn)
	clients.identity = pb.NewIdentityServiceClient(identityConn)
	clients.cart = pb.NewCartServiceClient(cartConn)
	clients.payment = pb.NewPaymentServiceClient(paymentConn)
	clients.fulfillment = pb.NewFulfillmentServiceClient(fulfillmentConn)
	return clients, nil
}

// Close 按创建的逆序关闭 gRPC 连接和 etcd 客户端，并聚合所有错误。
func (c *grpcClients) Close() error {
	var errs []error
	for index := len(c.closers) - 1; index >= 0; index-- {
		if err := c.closers[index].Close(); err != nil {
			errs = append(errs, err)
		}
	}
	c.closers = nil
	return errors.Join(errs...)
}

func dialService(serviceName, directAddress string, etcdResolver gresolver.Builder) (*grpc.ClientConn, error) {
	target := strings.TrimSpace(directAddress)
	options := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	}
	if target == "" {
		target = "etcd:///" + serviceName
		if etcdResolver == nil {
			return nil, fmt.Errorf("service discovery unavailable service=%s", serviceName)
		}
		options = append(options, grpc.WithResolvers(etcdResolver), grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`))
	}
	conn, err := grpc.NewClient(target, options...)
	if err != nil {
		return nil, fmt.Errorf("grpc client dial failed service=%s: %w", serviceName, err)
	}
	return conn, nil
}
