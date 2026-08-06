// catalog-service 是独立的商品目录 gRPC 服务。
// 未配置 Catalog DSN 时使用内存 Repository，便于本地和无 Docker 环境验收。
package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"strings"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"seckill-mall/catalog_service"
	"seckill-mall/common/config"
	"seckill-mall/common/discovery"
	"seckill-mall/common/pb"
	"seckill-mall/common/tracer"
)

func main() {
	config.InitConfig("catalog")
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
		serviceName = "catalog"
	}
	advertiseAddr := config.Conf.Catalog.Address
	if advertiseAddr == "" {
		advertiseAddr = "127.0.0.1:" + port
	}
	advertiseAddr = config.AdvertiseAddr(advertiseAddr)
	registration, err := discovery.Register(context.Background(), config.Conf.Etcd.Addr, serviceName, advertiseAddr)
	if err != nil {
		log.Printf("catalog etcd registration skipped service=%s err=%v", serviceName, err)
	} else {
		defer func() { _ = registration.Close(context.Background()) }()
		log.Printf("catalog registered service=%s addr=%s", serviceName, advertiseAddr)
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
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("commerce.catalog.v1.CatalogService", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	grpc_prometheus.Register(grpcServer)

	log.Printf("catalog service started addr=%s repository=%s", grpcAddr, repositoryName(repository))
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("catalog service serve failed: %v", err)
	}
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
