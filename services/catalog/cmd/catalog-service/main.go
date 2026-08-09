// catalog-service 是独立的商品目录 gRPC 服务。
// 未配置 Catalog DSN 时使用内存 Repository，便于本地和无 Docker 环境验收。
package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"seckill-mall/services/catalog/internal/app"
	"seckill-mall/shared/contracts"
	"seckill-mall/shared/gen/commerce"
	"seckill-mall/shared/platform/appkit"
	"seckill-mall/shared/platform/config"
	"seckill-mall/shared/platform/tracer"
)

func main() {
	config.InitConfig("catalog")
	appkit.ValidateContract(contracts.ServiceCatalog)
	shutdown := tracer.InitTracer("catalog-service", tracer.EndpointFromEnv())
	defer shutdown(context.Background())

	port := config.Conf.Server.Port
	if port == "" {
		port = "51002"
	}
	repository := buildRepository()

	grpcAddr := net.JoinHostPort("", port)
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatalf("catalog listen failed addr=%s err=%v", grpcAddr, err)
	}

	serviceName := config.Conf.Catalog.ServiceName
	if serviceName == "" {
		serviceName = contracts.ServiceCatalog
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	registration := appkit.RegisterService(ctx, serviceName, config.Conf.Catalog.Address, port)
	if registration != nil {
		defer func() { _ = registration.Close(context.Background()) }()
	}
	startMetricsServer()

	server, err := catalogservice.NewServer(repository)
	if err != nil {
		log.Fatalf("catalog server create failed: %v", err)
	}
	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.StreamInterceptor(grpc_prometheus.StreamServerInterceptor),
		grpc.UnaryInterceptor(grpc_prometheus.UnaryServerInterceptor),
	)
	pb.RegisterCatalogServiceServer(grpcServer, server)
	appkit.RegisterHealth(grpcServer, "commerce.catalog.v1.CatalogService")
	grpc_prometheus.Register(grpcServer)

	log.Printf("catalog service started addr=%s repository=%s", grpcAddr, repositoryName(repository))
	appkit.ServeWithShutdown(ctx, grpcServer, lis)
}

func buildRepository() catalogservice.Repository {
	dsn := strings.TrimSpace(config.Conf.Catalog.MySQLDSN)
	if dsn == "" {
		log.Println("catalog mysql dsn is empty, using memory repository")
		return catalogservice.NewMemoryRepository()
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("catalog mysql connect failed: %v", err)
	}
	repository, err := catalogservice.NewMySQLRepository(db)
	if err != nil {
		log.Fatalf("catalog repository create failed: %v", err)
	}
	return repository
}

func repositoryName(repository catalogservice.Repository) string {
	switch repository.(type) {
	case *catalogservice.MemoryRepository:
		return "memory"
	case *catalogservice.MySQLRepository:
		return "mysql"
	default:
		return "custom"
	}
}

func startMetricsServer() {
	port := strings.TrimSpace(config.Conf.Server.MetricsPort)
	if port == "" {
		return
	}
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		addr := net.JoinHostPort("", port)
		log.Printf("catalog metrics server started addr=%s", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Printf("catalog metrics server stopped err=%v", err)
		}
	}()
}
