package main

import (
	"log"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	gresolver "google.golang.org/grpc/resolver"

	clientv3 "go.etcd.io/etcd/client/v3"
	resolver "go.etcd.io/etcd/client/v3/naming/resolver"

	"seckill-mall/common/config"
	"seckill-mall/common/contracts"
	"seckill-mall/common/pb"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
)

type grpcClients struct {
	product       pb.ProductServiceClient
	order         pb.OrderServiceClient
	commerceOrder pb.CommerceOrderServiceClient
	catalog       pb.CatalogServiceClient
	inventory     pb.InventoryServiceClient
}

func initGRPCClients() grpcClients {
	etcdAddr := config.Conf.Etcd.Addr
	var etcdResolver gresolver.Builder
	if etcdAddr != "" {
		cli, err := clientv3.New(clientv3.Config{Endpoints: []string{etcdAddr}, DialTimeout: 5 * time.Second})
		if err != nil {
			log.Fatalf("etcd connect failed: %v", err)
		}
		builtResolver, err := resolver.NewBuilder(cli)
		if err != nil {
			log.Fatalf("etcd resolver create failed: %v", err)
		}
		etcdResolver = builtResolver
	}

	productConn := dialService("product", "", etcdResolver)
	orderConn := dialService("order", "", etcdResolver)
	catalogName := config.Conf.Catalog.ServiceName
	if catalogName == "" {
		catalogName = contracts.ServiceCatalog
	}
	catalogConn := dialService(catalogName, config.Conf.Catalog.Address, etcdResolver)
	inventoryName := config.Conf.Inventory.ServiceName
	if inventoryName == "" {
		inventoryName = contracts.ServiceInventory
	}
	inventoryConn := dialService(inventoryName, config.Conf.Inventory.Address, etcdResolver)
	orderServiceName := config.Conf.Order.ServiceName
	if orderServiceName == "" {
		orderServiceName = contracts.ServiceOrder
	}
	commerceOrderConn := dialService(orderServiceName, config.Conf.Order.Address, etcdResolver)

	return grpcClients{
		product:       pb.NewProductServiceClient(productConn),
		order:         pb.NewOrderServiceClient(orderConn),
		commerceOrder: pb.NewCommerceOrderServiceClient(commerceOrderConn),
		catalog:       pb.NewCatalogServiceClient(catalogConn),
		inventory:     pb.NewInventoryServiceClient(inventoryConn),
	}
}

func dialService(serviceName, directAddress string, etcdResolver gresolver.Builder) *grpc.ClientConn {
	target := strings.TrimSpace(directAddress)
	options := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	}
	if target == "" {
		target = "etcd:///seckill/" + serviceName
		if etcdResolver == nil {
			log.Fatalf("service discovery unavailable service=%s", serviceName)
		}
		options = append(options, grpc.WithResolvers(etcdResolver), grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`))
	}
	conn, err := grpc.Dial(target, options...)
	if err != nil {
		log.Fatalf("grpc client dial failed service=%s: %v", serviceName, err)
	}
	return conn
}
