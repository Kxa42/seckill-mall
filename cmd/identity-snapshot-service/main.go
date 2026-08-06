// identity-snapshot-service 是阶段 3 的最小 Identity gRPC 适配器。
// 未配置 Identity DSN 时使用内存地址，仅用于本地和无 Docker 验收。
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

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"seckill-mall/common/config"
	"seckill-mall/common/contracts"
	"seckill-mall/common/discovery"
	"seckill-mall/common/internalcall"
	"seckill-mall/common/pb"
	"seckill-mall/identity_service"
	platformauth "seckill-mall/internal/platform/auth"
)

func main() {
	config.InitConfig("identity")
	if strings.EqualFold(config.Conf.Server.Mode, "release") {
		if err := internalcall.ValidateSecret(os.Getenv("SECKILL_INTERNAL_CALL_SECRET")); err != nil {
			log.Fatalf("identity internal call config invalid: %v", err)
		}
	}
	if _, ok := contracts.ServiceBoundaryFor(contracts.ServiceIdentity); !ok {
		log.Fatalf("identity service contract is not defined")
	}
	port := config.Conf.Server.Port
	if port == "" {
		port = "51001"
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("", port))
	if err != nil {
		log.Fatalf("identity listen failed: %v", err)
	}
	repository := buildRepository()
	secret := strings.TrimSpace(config.Conf.JWT.Secret)
	authManager, err := platformauth.NewManager(secret, 15*time.Minute, 24*time.Hour)
	if err != nil {
		log.Fatalf("identity jwt config invalid: %v", err)
	}
	server, err := identityservice.NewServer(repository, authManager)
	if err != nil {
		log.Fatalf("identity server create failed: %v", err)
	}
	if email, password := strings.TrimSpace(os.Getenv("SECKILL_ADMIN_EMAIL")), os.Getenv("SECKILL_ADMIN_PASSWORD"); email != "" || password != "" {
		hash, hashErr := platformauth.HashPassword(password)
		if hashErr != nil {
			log.Fatalf("identity admin password invalid: %v", hashErr)
		}
		if adminErr := repository.EnsureAdmin(context.Background(), email, hash, time.Now().UTC()); adminErr != nil {
			log.Fatalf("identity admin initialization failed: %v", adminErr)
		}
	}
	grpcServer := grpc.NewServer()
	pb.RegisterIdentityServiceServer(grpcServer, server)
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("commerce.identity.v1.IdentityService", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)

	serviceName := config.Conf.Identity.ServiceName
	if serviceName == "" {
		serviceName = contracts.ServiceIdentity
	}
	address := config.Conf.Identity.Address
	if address == "" {
		address = "127.0.0.1:" + port
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	registration, err := discovery.Register(ctx, config.Conf.Etcd.Addr, serviceName, config.AdvertiseAddr(address))
	if err != nil {
		log.Printf("identity etcd registration skipped service=%s err=%v", serviceName, err)
	} else {
		defer func() { _ = registration.Close(context.Background()) }()
	}
	log.Printf("identity snapshot service started addr=%s", listener.Addr())
	serveErr := make(chan error, 1)
	go func() { serveErr <- grpcServer.Serve(listener) }()
	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Fatalf("identity service stopped: %v", err)
		}
	case <-ctx.Done():
		grpcServer.GracefulStop()
		if err := <-serveErr; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Printf("identity service graceful stop: %v", err)
		}
	}
}

func buildRepository() identityservice.Repository {
	dsn := strings.TrimSpace(config.Conf.Identity.MySQLDSN)
	if dsn == "" {
		log.Println("identity mysql dsn is empty, using memory repository")
		return identityservice.NewMemoryRepository()
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("identity mysql connect failed: %v", err)
	}
	repository, err := identityservice.NewMySQLRepository(db)
	if err != nil {
		log.Fatalf("identity repository create failed: %v", err)
	}
	return repository
}
