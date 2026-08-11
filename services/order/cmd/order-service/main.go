// order-service 是商城唯一订单编排 gRPC 服务。
// 未配置 Order DSN 时使用内存 Repository，便于本地和无 Docker 验收。
package main

import (
	"context"
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
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"seckill-mall/services/order/internal/app"
	"seckill-mall/shared/contracts"
	"seckill-mall/shared/gen/commerce"
	"seckill-mall/shared/platform/appkit"
	"seckill-mall/shared/platform/config"
	"seckill-mall/shared/platform/internalcall"
	"seckill-mall/shared/platform/messaging"
)

func main() {
	config.InitConfig("order")
	if strings.EqualFold(config.Conf.Server.Mode, "release") {
		if err := internalcall.ValidateSecret(os.Getenv("SECKILL_INTERNAL_CALL_SECRET")); err != nil {
			log.Fatalf("order internal call config invalid: %v", err)
		}
	}
	appkit.ValidateContract(contracts.ServiceOrder)

	repository := buildRepository()
	messagingRuntime := configureMessaging(repository)
	if messagingRuntime != nil {
		defer func() { _ = messagingRuntime.Close() }()
	}
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	registration := appkit.RegisterService(ctx, serviceName, config.Conf.Order.Address, port)
	if registration != nil {
		defer func() { _ = registration.Close(context.Background()) }()
	}

	grpcServer := grpc.NewServer(grpc.UnaryInterceptor(grpc_prometheus.UnaryServerInterceptor))
	pb.RegisterCommerceOrderServiceServer(grpcServer, server)
	appkit.RegisterHealth(grpcServer, "commerce.order.v1.CommerceOrderService")
	grpc_prometheus.Register(grpcServer)

	go runExpiryWorker(ctx, service)
	go runOperationWorker(ctx, service)
	if messagingRuntime != nil && messagingRuntime.Enabled() {
		handler, handlerErr := order.NewEventHandler(service)
		if handlerErr != nil {
			log.Fatalf("order event handler create failed: %v", handlerErr)
		}
		messagingRuntime.Start(ctx, contracts.ServiceOrder, handler.Handlers())
	}
	log.Printf("order service started addr=%s repository=%s", listener.Addr(), repositoryName(repository))
	appkit.ServeWithShutdown(ctx, grpcServer, listener)
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

func configureMessaging(repository order.Repository) *messaging.ServiceRuntime {
	switch value := repository.(type) {
	case *order.MemoryRepository:
		runtime, err := messaging.NewMemoryServiceRuntime(value, config.Conf.MQ.URL)
		if err != nil {
			log.Fatalf("order message runtime create failed: %v", err)
		}
		return runtime
	case *order.MySQLRepository:
		runtime, err := messaging.NewSQLServiceRuntime(value, value.Database(), "order_outbox_events", "order_inbox_events", config.Conf.MQ.URL)
		if err != nil {
			log.Fatalf("order message runtime create failed: %v", err)
		}
		return runtime
	default:
		return nil
	}
}
