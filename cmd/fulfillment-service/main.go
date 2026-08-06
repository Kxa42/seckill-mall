// fulfillment-service 是独立的发货、收货与物流查询 gRPC 服务。
// 未配置 Fulfillment DSN 时使用内存 Repository，便于本地和无 Docker 环境验收。
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

	"seckill-mall/common/config"
	"seckill-mall/common/contracts"
	"seckill-mall/common/discovery"
	"seckill-mall/common/internalcall"
	"seckill-mall/common/orderclient"
	"seckill-mall/common/pb"
	"seckill-mall/fulfillment_service"
)

func main() {
	config.InitConfig("fulfillment")
	if strings.EqualFold(config.Conf.Server.Mode, "release") {
		if err := internalcall.ValidateSecret(os.Getenv("SECKILL_INTERNAL_CALL_SECRET")); err != nil {
			log.Fatalf("fulfillment internal call config invalid: %v", err)
		}
	}
	validateContract(contracts.ServiceFulfillment)
	port := config.Conf.Server.Port
	if port == "" {
		port = "51007"
	}
	repository := buildRepository()
	connection, err := grpc.Dial(config.Conf.Order.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("fulfillment order dial failed: %v", err)
	}
	defer connection.Close()
	service, err := fulfillmentservice.NewService(repository, orderclient.NewGRPCClient(pb.NewCommerceOrderServiceClient(connection), os.Getenv("SECKILL_INTERNAL_CALL_SECRET")))
	if err != nil {
		log.Fatalf("fulfillment service create failed: %v", err)
	}
	server, err := fulfillmentservice.NewServer(service)
	if err != nil {
		log.Fatalf("fulfillment grpc server create failed: %v", err)
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("", port))
	if err != nil {
		log.Fatalf("fulfillment listen failed: %v", err)
	}
	grpcServer := grpc.NewServer()
	pb.RegisterFulfillmentServiceServer(grpcServer, server)
	registerHealth(grpcServer, "commerce.fulfillment.v1.FulfillmentService")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	registration := registerService(ctx, contracts.ServiceFulfillment, config.Conf.Fulfillment.Address, port)
	if registration != nil {
		defer func() { _ = registration.Close(context.Background()) }()
	}
	log.Printf("fulfillment service started addr=%s repository=%s", listener.Addr(), repositoryName(repository))
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

func buildRepository() fulfillmentservice.Repository {
	dsn := strings.TrimSpace(config.Conf.Fulfillment.MySQLDSN)
	if dsn == "" {
		log.Println("fulfillment mysql dsn is empty, using memory repository")
		return fulfillmentservice.NewMemoryRepository()
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		log.Fatalf("fulfillment mysql connect failed: %v", err)
	}
	repository, err := fulfillmentservice.NewMySQLRepository(db)
	if err != nil {
		log.Fatalf("fulfillment repository create failed: %v", err)
	}
	return repository
}

func repositoryName(repository fulfillmentservice.Repository) string {
	switch repository.(type) {
	case *fulfillmentservice.MemoryRepository:
		return "memory"
	case *fulfillmentservice.MySQLRepository:
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
		log.Printf("fulfillment etcd registration skipped service=%s err=%v", service, err)
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
			log.Fatalf("fulfillment service stopped: %v", err)
		}
	case <-ctx.Done():
		server.GracefulStop()
		if err := <-serveErr; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Printf("fulfillment service graceful stop: %v", err)
		}
	}
}
