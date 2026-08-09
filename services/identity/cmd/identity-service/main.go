// identity-service 是独立的身份、认证和地址快照 gRPC 服务。
// 未配置 Identity DSN 时使用内存地址，仅用于本地和无 Docker 验收。
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

	"google.golang.org/grpc"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"seckill-mall/services/identity/internal/app"
	"seckill-mall/shared/contracts"
	"seckill-mall/shared/gen/commerce"
	platformauth "seckill-mall/shared/platform/auth"
	"seckill-mall/shared/platform/appkit"
	"seckill-mall/shared/platform/config"
	"seckill-mall/shared/platform/internalcall"
)

func main() {
	config.InitConfig("identity")
	if strings.EqualFold(config.Conf.Server.Mode, "release") {
		if err := internalcall.ValidateSecret(os.Getenv("SECKILL_INTERNAL_CALL_SECRET")); err != nil {
			log.Fatalf("identity internal call config invalid: %v", err)
		}
	}
	appkit.ValidateContract(contracts.ServiceIdentity)
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
	appkit.RegisterHealth(grpcServer, "commerce.identity.v1.IdentityService")

	serviceName := config.Conf.Identity.ServiceName
	if serviceName == "" {
		serviceName = contracts.ServiceIdentity
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	registration := appkit.RegisterService(ctx, serviceName, config.Conf.Identity.Address, port)
	if registration != nil {
		defer func() { _ = registration.Close(context.Background()) }()
	}
	log.Printf("identity service started addr=%s", listener.Addr())
	appkit.ServeWithShutdown(ctx, grpcServer, listener)
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
