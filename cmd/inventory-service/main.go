// inventory-service 是独立的库存与秒杀准入 gRPC 服务。
// debug 或显式 memory 模式使用内存状态机，release 模式可切换到 Redis Lua。
package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"strings"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	"seckill-mall/common/config"
	"seckill-mall/common/discovery"
	"seckill-mall/common/pb"
	"seckill-mall/common/tracer"
	"seckill-mall/inventory_service"
)

func main() {
	config.InitConfig("inventory")
	shutdown := tracer.InitTracer("inventory-service", tracer.EndpointFromEnv())
	defer shutdown(context.Background())

	port := config.Conf.Server.Port
	if port == "" {
		port = "51003"
	}
	store := buildStore()

	grpcAddr := net.JoinHostPort("", port)
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatalf("inventory listen failed addr=%s err=%v", grpcAddr, err)
	}
	serviceName := config.Conf.Inventory.ServiceName
	if serviceName == "" {
		serviceName = "inventory"
	}
	advertiseAddr := config.Conf.Inventory.Address
	if advertiseAddr == "" {
		advertiseAddr = "127.0.0.1:" + port
	}
	advertiseAddr = config.AdvertiseAddr(advertiseAddr)
	registration, err := discovery.Register(context.Background(), config.Conf.Etcd.Addr, serviceName, advertiseAddr)
	if err != nil {
		log.Printf("inventory etcd registration skipped service=%s err=%v", serviceName, err)
	} else {
		defer func() { _ = registration.Close(context.Background()) }()
		log.Printf("inventory registered service=%s addr=%s", serviceName, advertiseAddr)
	}
	startMetricsServer()

	server, err := inventoryservice.NewServer(store)
	if err != nil {
		log.Fatalf("inventory server create failed: %v", err)
	}
	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.StreamInterceptor(grpc_prometheus.StreamServerInterceptor),
		grpc.UnaryInterceptor(grpc_prometheus.UnaryServerInterceptor),
	)
	pb.RegisterInventoryServiceServer(grpcServer, server)
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("commerce.inventory.v1.InventoryService", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	grpc_prometheus.Register(grpcServer)

	log.Printf("inventory service started addr=%s store=%s", grpcAddr, storeName(store))
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("inventory service serve failed: %v", err)
	}
}

func buildStore() inventoryservice.Store {
	limit := config.Conf.Inventory.PurchaseLimit
	if limit <= 0 && config.Conf.Seckill.PurchaseLimit > 0 {
		limit = int32(config.Conf.Seckill.PurchaseLimit)
	}
	if limit <= 0 {
		limit = 5
	}
	storeMode := strings.ToLower(strings.TrimSpace(config.Conf.Inventory.Store))
	if storeMode == "" && config.Conf.Server.Mode == "debug" {
		storeMode = "memory"
	}
	if storeMode == "memory" {
		return inventoryservice.NewMemoryStore(map[uint64]int32{1: 100}, limit)
	}
	redisAddr := config.Conf.Inventory.RedisAddr
	if redisAddr == "" {
		redisAddr = config.Conf.Redis.Addr
	}
	redisPassword := config.Conf.Inventory.RedisPassword
	if redisPassword == "" {
		redisPassword = config.Conf.Redis.Password
	}
	client := redis.NewClient(&redis.Options{Addr: redisAddr, Password: redisPassword, DB: config.Conf.Inventory.RedisDB})
	if err := client.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("inventory redis connect failed: %v", err)
	}
	store, err := inventoryservice.NewRedisStore(client, inventoryservice.RedisStoreOptions{PurchaseLimit: limit})
	if err != nil {
		log.Fatalf("inventory redis store create failed: %v", err)
	}
	return store
}

func storeName(store inventoryservice.Store) string {
	switch store.(type) {
	case *inventoryservice.MemoryStore:
		return "memory"
	case *inventoryservice.RedisStore:
		return "redis"
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
		log.Printf("inventory metrics server started addr=%s", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Printf("inventory metrics server stopped err=%v", err)
		}
	}()
}
