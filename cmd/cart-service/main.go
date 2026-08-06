// cart-service 是独立的购物车 gRPC 服务。
// 未配置 Cart DSN 时使用内存 Repository，便于本地和无 Docker 环境验收。
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

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"seckill-mall/cart_service"
	"seckill-mall/common/config"
	"seckill-mall/common/contracts"
	"seckill-mall/common/discovery"
	"seckill-mall/common/internalcall"
	"seckill-mall/common/pb"
)

func main() {
	config.InitConfig("cart")
	if strings.EqualFold(config.Conf.Server.Mode, "release") {
		if err := internalcall.ValidateSecret(os.Getenv("SECKILL_INTERNAL_CALL_SECRET")); err != nil {
			log.Fatalf("cart internal call config invalid: %v", err)
		}
	}
	validateContract(contracts.ServiceCart)
	port := config.Conf.Server.Port
	if port == "" {
		port = "51004"
	}
	repository := buildRepository()
	connection, err := grpc.Dial(config.Conf.Catalog.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("cart catalog dial failed: %v", err)
	}
	defer connection.Close()
	service, err := cartservice.NewService(repository, cartservice.NewGRPCCatalogClient(pb.NewCatalogServiceClient(connection)))
	if err != nil {
		log.Fatalf("cart service create failed: %v", err)
	}
	server, err := cartservice.NewServer(service)
	if err != nil {
		log.Fatalf("cart grpc server create failed: %v", err)
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("", port))
	if err != nil {
		log.Fatalf("cart listen failed: %v", err)
	}
	grpcServer := grpc.NewServer()
	pb.RegisterCartServiceServer(grpcServer, server)
	registerHealth(grpcServer, "commerce.cart.v1.CartService")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	registration := registerService(ctx, contracts.ServiceCart, config.Conf.Cart.Address, port)
	if registration != nil {
		defer func() { _ = registration.Close(context.Background()) }()
	}
	log.Printf("cart service started addr=%s repository=%s", listener.Addr(), repositoryName(repository))
	serveWithShutdown(ctx, grpcServer, listener)
}

func validateContract(service string) {
	if err := contracts.ValidateServiceBoundaries(); err != nil {
		log.Fatalf("service contract validation failed: %v", err)
	}
	if _, ok := contracts.ServiceBoundaryFor(service); !ok {
		log.Fatalf("service contract is not defined: %s", service)
	}
}

func buildRepository() cartservice.Repository {
	dsn := strings.TrimSpace(config.Conf.Cart.MySQLDSN)
	if dsn == "" {
		log.Println("cart mysql dsn is empty, using memory repository")
		return cartservice.NewMemoryRepository()
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		log.Fatalf("cart mysql connect failed: %v", err)
	}
	repository, err := cartservice.NewMySQLRepository(db)
	if err != nil {
		log.Fatalf("cart repository create failed: %v", err)
	}
	return repository
}

func repositoryName(repository cartservice.Repository) string {
	switch repository.(type) {
	case *cartservice.MemoryRepository:
		return "memory"
	case *cartservice.MySQLRepository:
		return "mysql"
	default:
		return "custom"
	}
}

func registerService(ctx context.Context, service, address, port string) *discovery.Registration {
	if strings.TrimSpace(address) == "" {
		address = "127.0.0.1:" + port
	}
	registration, err := discovery.Register(ctx, config.Conf.Etcd.Addr, service, config.AdvertiseAddr(address))
	if err != nil {
		log.Printf("cart etcd registration skipped service=%s err=%v", service, err)
		return nil
	}
	return registration
}

func registerHealth(server *grpc.Server, service string) {
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus(service, grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(server, healthServer)
}

func serveWithShutdown(ctx context.Context, server *grpc.Server, listener net.Listener) {
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Fatalf("cart service stopped: %v", err)
		}
	case <-ctx.Done():
		server.GracefulStop()
		if err := <-serveErr; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Printf("cart service graceful stop: %v", err)
		}
	}
}
