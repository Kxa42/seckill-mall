// order-service 是商城唯一订单编排 gRPC 服务。
// 未配置 Order DSN 时使用内存 Repository，便于本地和无 Docker 验收。
package main

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"seckill-mall/common/config"
	"seckill-mall/common/contracts"
	"seckill-mall/common/discovery"
	"seckill-mall/common/internalcall"
	"seckill-mall/common/messaging"
	"seckill-mall/common/pb"
	"seckill-mall/internal/order"
)

func main() {
	config.InitConfig("order")
	if strings.EqualFold(config.Conf.Server.Mode, "release") {
		if err := internalcall.ValidateSecret(os.Getenv("SECKILL_INTERNAL_CALL_SECRET")); err != nil {
			log.Fatalf("order internal call config invalid: %v", err)
		}
	}
	if err := contracts.ValidateServiceBoundaries(); err != nil {
		log.Fatalf("service contract validation failed: %v", err)
	}
	if _, ok := contracts.ServiceBoundaryFor(contracts.ServiceOrder); !ok {
		log.Fatalf("order service contract is not defined")
	}

	repository := buildRepository()
	messagingRuntime := configureMessaging(repository)
	connections := dialDependencies()
	defer func() {
		for _, connection := range connections {
			_ = connection.Close()
		}
	}()
	service, err := order.NewService(repository, order.NewGRPCCatalogClient(pb.NewCatalogServiceClient(connections[0])), order.NewGRPCIdentityClient(pb.NewIdentityServiceClient(connections[1])), order.NewGRPCInventoryClient(pb.NewInventoryServiceClient(connections[2])), orderTTL())
	if err != nil {
		log.Fatalf("order service create failed: %v", err)
	}
	server, err := order.NewGRPCServer(service)
	if err != nil {
		log.Fatalf("order grpc server create failed: %v", err)
	}
	port := config.Conf.Server.Port
	if port == "" {
		port = "51005"
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("", port))
	if err != nil {
		log.Fatalf("order listen failed: %v", err)
	}
	serviceName := config.Conf.Order.ServiceName
	if serviceName == "" {
		serviceName = contracts.ServiceOrder
	}
	address := config.Conf.Order.Address
	if address == "" {
		address = "127.0.0.1:" + port
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if messagingRuntime.publisher != nil {
		defer func() { _ = messagingRuntime.publisher.Close() }()
	}
	registration, err := discovery.Register(ctx, config.Conf.Etcd.Addr, serviceName, config.AdvertiseAddr(address))
	if err != nil {
		log.Printf("order etcd registration skipped service=%s err=%v", serviceName, err)
	} else {
		defer func() { _ = registration.Close(context.Background()) }()
	}

	grpcServer := grpc.NewServer(grpc.UnaryInterceptor(grpc_prometheus.UnaryServerInterceptor))
	pb.RegisterCommerceOrderServiceServer(grpcServer, server)
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("commerce.order.v1.CommerceOrderService", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	grpc_prometheus.Register(grpcServer)

	go runExpiryWorker(ctx, service)
	go runOperationWorker(ctx, service)
	if messagingRuntime.publisher != nil {
		go messaging.RunOutboxPublisher(ctx, messagingRuntime.outbox, messagingRuntime.publisher, time.Second, 100)
		handler, handlerErr := order.NewEventHandler(service)
		if handlerErr != nil {
			log.Fatalf("order event handler create failed: %v", handlerErr)
		}
		go messaging.RunRabbitConsumer(ctx, config.Conf.MQ.URL, contracts.ServiceOrder, messagingRuntime.inbox, handler.Handlers(), 5)
	}
	log.Printf("order service started addr=%s repository=%s", listener.Addr(), repositoryName(repository))
	serveErr := make(chan error, 1)
	go func() { serveErr <- grpcServer.Serve(listener) }()
	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Fatalf("order service stopped: %v", err)
		}
	case <-ctx.Done():
		grpcServer.GracefulStop()
		if err := <-serveErr; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Printf("order service graceful stop: %v", err)
		}
	}
}

func runOperationWorker(ctx context.Context, service *order.Service) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			created, err := service.RecoverCreateIntents(ctx, 100)
			if err != nil {
				log.Printf("order create intent recovery failed: %v", err)
			} else if created > 0 {
				log.Printf("order create intent recovery completed count=%d", created)
			}
			count, err := service.RetryOperations(ctx, 100)
			if err != nil {
				log.Printf("order compensation retry failed: %v", err)
			} else if count > 0 {
				log.Printf("order compensation retry completed count=%d", count)
			}
		}
	}
}

func buildRepository() order.Repository {
	dsn := strings.TrimSpace(config.Conf.Order.MySQLDSN)
	if dsn == "" {
		log.Println("order mysql dsn is empty, using memory repository")
		return order.NewMemoryRepository()
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		log.Fatalf("order mysql connect failed: %v", err)
	}
	repository, err := order.NewMySQLRepository(db)
	if err != nil {
		log.Fatalf("order repository create failed: %v", err)
	}
	return repository
}

func dialDependencies() []*grpc.ClientConn {
	addresses := []string{config.Conf.Catalog.Address, config.Conf.Identity.Address, config.Conf.Inventory.Address}
	connections := make([]*grpc.ClientConn, 0, len(addresses))
	for index, address := range addresses {
		if strings.TrimSpace(address) == "" {
			log.Fatalf("order dependency address missing index=%d", index)
		}
		connection, err := grpc.Dial(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Fatalf("order dependency dial failed address=%s err=%v", address, err)
		}
		connections = append(connections, connection)
	}
	return connections
}

func orderTTL() time.Duration { return 15 * time.Minute }

func runExpiryWorker(ctx context.Context, service *order.Service) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count, err := service.Expire(ctx, 100)
			if err != nil {
				log.Printf("order expiry failed: %v", err)
			} else if count > 0 {
				log.Printf("order expiry completed count=%d", count)
			}
		}
	}
}

func repositoryName(repository order.Repository) string {
	switch repository.(type) {
	case *order.MemoryRepository:
		return "memory"
	case *order.MySQLRepository:
		return "mysql"
	default:
		return "custom"
	}
}

type messageRuntime struct {
	outbox    messaging.OutboxStore
	inbox     messaging.InboxStore
	publisher *messaging.RabbitPublisher
}

func configureMessaging(repository order.Repository) messageRuntime {
	var outbox messaging.OutboxStore
	var inbox messaging.InboxStore
	switch value := repository.(type) {
	case *order.MemoryRepository:
		store := messaging.NewMemoryStore()
		value.SetEventSink(store)
		outbox, inbox = store.Outbox(), store.Inbox()
	case *order.MySQLRepository:
		store, err := messaging.NewSQLStore(value.Database(), "order_outbox_events", "order_inbox_events")
		if err != nil {
			log.Fatalf("order message store create failed: %v", err)
		}
		value.SetEventSink(store)
		outbox, inbox = store.Outbox(), store.Inbox()
	default:
		return messageRuntime{}
	}
	runtime := messageRuntime{outbox: outbox, inbox: inbox}
	if strings.TrimSpace(config.Conf.MQ.URL) != "" {
		runtime.publisher = messaging.NewRabbitPublisher(config.Conf.MQ.URL)
	}
	return runtime
}
