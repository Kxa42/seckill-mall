// inventory-service 是独立的库存与秒杀准入 gRPC 服务。
// debug 或显式 memory 模式使用内存状态机，release 模式可切换到 Redis Lua。
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
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"

	"seckill-mall/services/inventory/internal/app"
	"seckill-mall/shared/contracts"
	"seckill-mall/shared/gen/commerce"
	"seckill-mall/shared/platform/appkit"
	"seckill-mall/shared/platform/config"
	"seckill-mall/shared/platform/messaging"
	"seckill-mall/shared/platform/tracer"
)

func main() {
	config.InitConfig("inventory")
	appkit.ValidateContract(contracts.ServiceInventory)
	shutdown := tracer.InitTracer("inventory-service", tracer.EndpointFromEnv())
	defer shutdown(context.Background())

	port := config.Conf.Server.Port
	if port == "" {
		port = "51003"
	}
	store := buildStore()
	if redisStore, ok := store.(*inventoryservice.RedisStore); ok {
		defer func() { _ = redisStore.Close() }()
	}
	stream := configureEventStream(store)
	var publisher *messaging.RabbitPublisher
	if stream != nil && strings.TrimSpace(config.Conf.MQ.URL) != "" {
		publisher = messaging.NewRabbitPublisher(config.Conf.MQ.URL)
		defer func() { _ = publisher.Close() }()
	}

	grpcAddr := net.JoinHostPort("", port)
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatalf("inventory listen failed addr=%s err=%v", grpcAddr, err)
	}
	serviceName := config.Conf.Inventory.ServiceName
	if serviceName == "" {
		serviceName = contracts.ServiceInventory
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	registration := appkit.RegisterService(ctx, serviceName, config.Conf.Inventory.Address, port)
	if registration != nil {
		defer func() { _ = registration.Close(context.Background()) }()
	}
	metricsDone := appkit.StartMetricsServer(ctx, config.Conf.Server.MetricsPort)

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
	appkit.RegisterHealth(grpcServer, "commerce.inventory.v1.InventoryService")
	grpc_prometheus.Register(grpcServer)
	if publisher != nil {
		go runStreamPublisher(ctx, stream, publisher)
	}
	if strings.TrimSpace(config.Conf.MQ.URL) != "" {
		handler, handlerErr := inventoryservice.NewEventHandler(store)
		if handlerErr != nil {
			log.Fatalf("inventory event handler create failed: %v", handlerErr)
		}
		inbox := messaging.InboxStore(messaging.NewMemoryStore().Inbox())
		if redisStore, ok := store.(*inventoryservice.RedisStore); ok {
			inbox, handlerErr = redisStore.EventInbox()
			if handlerErr != nil {
				log.Fatalf("inventory inbox create failed: %v", handlerErr)
			}
		}
		go messaging.RunRabbitConsumer(ctx, config.Conf.MQ.URL, contracts.ServiceInventory, inbox, handler.Handlers(), 5)
	}

	log.Printf("inventory service started addr=%s store=%s", grpcAddr, storeName(store))
	appkit.ServeWithShutdown(ctx, grpcServer, lis)
	<-metricsDone
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
	seedStock := config.Conf.Inventory.Stock
	if len(seedStock) == 0 {
		// 与内存模式默认一致，保证 Redis 部署未配置 stock 时也能运行。
		seedStock = map[uint64]int32{1: 100}
	}
	if err := store.SeedStock(context.Background(), seedStock); err != nil {
		log.Fatalf("inventory redis stock seed failed: %v", err)
	}
	log.Printf("inventory redis stock seed configured skus=%d", len(seedStock))
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

func configureEventStream(store inventoryservice.Store) *inventoryservice.RedisStreamOutbox {
	switch value := store.(type) {
	case *inventoryservice.MemoryStore:
		value.SetEventStream(inventoryservice.NewMemoryEventStream())
		return nil
	case *inventoryservice.RedisStore:
		stream, err := value.EventStream()
		if err != nil {
			log.Fatalf("inventory stream create failed: %v", err)
		}
		return stream
	default:
		return nil
	}
}

func runStreamPublisher(ctx context.Context, stream *inventoryservice.RedisStreamOutbox, publisher messaging.Publisher) {
	consumer := "inventory-publisher"
	for ctx.Err() == nil {
		items, err := stream.ClaimPending(ctx, consumer, 30*time.Second, 100)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("inventory stream pending claim failed: %v", err)
			}
		} else if !publishStreamItems(ctx, stream, publisher, items) {
			waitForStreamRetry(ctx)
			continue
		}
		items, err = stream.Read(ctx, consumer, 100, time.Second)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("inventory stream read failed: %v", err)
			}
			waitForStreamRetry(ctx)
			continue
		}
		if !publishStreamItems(ctx, stream, publisher, items) {
			waitForStreamRetry(ctx)
		}
	}
}

func publishStreamItems(ctx context.Context, stream *inventoryservice.RedisStreamOutbox, publisher messaging.Publisher, items []inventoryservice.StreamEvent) bool {
	for _, item := range items {
		if err := publisher.Publish(ctx, item.Event, nil); err != nil {
			log.Printf("inventory stream publish failed event=%s err=%v", item.Event.EventType, err)
			return false
		}
		if err := stream.Ack(ctx, item.ID); err != nil {
			log.Printf("inventory stream ack failed id=%s err=%v", item.ID, err)
			return false
		}
	}
	return true
}

func waitForStreamRetry(ctx context.Context) {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
