// fulfillment-service 是独立的发货、收货与物流查询 gRPC 服务。
// 未配置 Fulfillment DSN 时使用内存 Repository，便于本地和无 Docker 环境验收。
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
	"google.golang.org/grpc/credentials/insecure"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"seckill-mall/services/fulfillment/internal/app"
	"seckill-mall/shared/contracts"
	"seckill-mall/shared/gen/commerce"
	"seckill-mall/shared/platform/appkit"
	"seckill-mall/shared/platform/config"
	"seckill-mall/shared/platform/internalcall"
	"seckill-mall/shared/platform/messaging"
)

func main() {
	config.InitConfig("fulfillment")
	if strings.EqualFold(config.Conf.Server.Mode, "release") {
		if err := internalcall.ValidateSecret(os.Getenv("SECKILL_INTERNAL_CALL_SECRET")); err != nil {
			log.Fatalf("fulfillment internal call config invalid: %v", err)
		}
	}
	appkit.ValidateContract(contracts.ServiceFulfillment)
	port := config.Conf.Server.Port
	if port == "" {
		port = "51007"
	}
	repository := buildRepository()
	messagingRuntime := configureMessaging(repository)
	connection, err := grpc.Dial(config.Conf.Order.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("fulfillment order dial failed: %v", err)
	}
	defer connection.Close()
	service, err := fulfillmentservice.NewService(repository, fulfillmentservice.NewGRPCOrderClient(pb.NewCommerceOrderServiceClient(connection), os.Getenv("SECKILL_INTERNAL_CALL_SECRET")))
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
	appkit.RegisterHealth(grpcServer, "commerce.fulfillment.v1.FulfillmentService")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if messagingRuntime.publisher != nil {
		defer func() { _ = messagingRuntime.publisher.Close() }()
	}
	registration := appkit.RegisterService(ctx, contracts.ServiceFulfillment, config.Conf.Fulfillment.Address, port)
	if registration != nil {
		defer func() { _ = registration.Close(context.Background()) }()
	}
	if messagingRuntime.publisher != nil {
		go messaging.RunOutboxPublisher(ctx, messagingRuntime.outbox, messagingRuntime.publisher, time.Second, 100)
		handler, handlerErr := fulfillmentservice.NewEventHandler(service)
		if handlerErr != nil {
			log.Fatalf("fulfillment event handler create failed: %v", handlerErr)
		}
		go messaging.RunRabbitConsumer(ctx, config.Conf.MQ.URL, contracts.ServiceFulfillment, messagingRuntime.inbox, handler.Handlers(), 5)
	}
	log.Printf("fulfillment service started addr=%s repository=%s", listener.Addr(), repositoryName(repository))
	appkit.ServeWithShutdown(ctx, grpcServer, listener)
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

type messageRuntime struct {
	outbox    messaging.OutboxStore
	inbox     messaging.InboxStore
	publisher *messaging.RabbitPublisher
}

func configureMessaging(repository fulfillmentservice.Repository) messageRuntime {
	var outbox messaging.OutboxStore
	var inbox messaging.InboxStore
	switch value := repository.(type) {
	case *fulfillmentservice.MemoryRepository:
		store := messaging.NewMemoryStore()
		value.SetEventSink(store)
		outbox, inbox = store.Outbox(), store.Inbox()
	case *fulfillmentservice.MySQLRepository:
		store, err := messaging.NewSQLStore(value.Database(), "fulfillment_outbox_events", "fulfillment_inbox_events")
		if err != nil {
			log.Fatalf("fulfillment message store create failed: %v", err)
		}
		value.SetEventSink(store)
		outbox, inbox = store.Outbox(), store.Inbox()
	default:
		return messageRuntime{}
	}
	runtime := messageRuntime{outbox: outbox, inbox: inbox}
	if strings.TrimSpace(config.Conf.MQ.URL) != "" {
		runtime.publisher = messaging.NewRabbitPublisher(config.Conf.MQ.URL)
	}
	return runtime
}
