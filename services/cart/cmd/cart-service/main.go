// cart-service 是独立的购物车 gRPC 服务。
// 未配置 Cart DSN 时使用内存 Repository，便于本地和无 Docker 环境验收。
package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	cartservice "seckill-mall/services/cart/internal/app"
	"seckill-mall/shared/contracts"
	pb "seckill-mall/shared/gen/commerce"
	"seckill-mall/shared/platform/appkit"
	"seckill-mall/shared/platform/config"
	"seckill-mall/shared/platform/internalcall"
)

func main() {
	config.InitConfig("cart")
	if strings.EqualFold(config.Conf.Server.Mode, "release") {
		if err := internalcall.ValidateSecret(os.Getenv("SECKILL_INTERNAL_CALL_SECRET")); err != nil {
			log.Fatalf("cart internal call config invalid: %v", err)
		}
	}
	appkit.ValidateContract(contracts.ServiceCart)
	port := config.Conf.Server.Port
	if port == "" {
		port = "51004"
	}
	repository := buildRepository()
	connection, err := grpc.NewClient(config.Conf.Catalog.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("failed to create gRPC client: %v", err)
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
	appkit.RegisterHealth(grpcServer, "commerce.cart.v1.CartService")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	registration := appkit.RegisterService(ctx, contracts.ServiceCart, config.Conf.Cart.Address, port)
	if registration != nil {
		defer func() { _ = registration.Close(context.Background()) }()
	}
	log.Printf("cart service started addr=%s repository=%s", listener.Addr(), repositoryName(repository))
	appkit.ServeWithShutdown(ctx, grpcServer, listener)
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
