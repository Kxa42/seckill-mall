// payment-service 是独立的支付与退款 gRPC 服务。
// 支付签名密钥缺失或过短时拒绝启动，避免使用可伪造的默认密钥。
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

	"seckill-mall/services/payment/internal/app"
	"seckill-mall/shared/contracts"
	"seckill-mall/shared/gen/commerce"
	"seckill-mall/shared/platform/appkit"
	"seckill-mall/shared/platform/config"
	"seckill-mall/shared/platform/internalcall"
	"seckill-mall/shared/platform/messaging"
)

func main() {
	config.InitConfig("payment")
	if strings.EqualFold(config.Conf.Server.Mode, "release") {
		if err := internalcall.ValidateSecret(os.Getenv("SECKILL_INTERNAL_CALL_SECRET")); err != nil {
			log.Fatalf("payment internal call config invalid: %v", err)
		}
	}
	appkit.ValidateContract(contracts.ServicePayment)
	secret := strings.TrimSpace(config.Conf.Payment.Secret)
	if len(secret) < 32 {
		log.Fatalf("payment secret must be at least 32 bytes")
	}
	port := config.Conf.Server.Port
	if port == "" {
		port = "51006"
	}
	repository := buildRepository()
	messagingRuntime := configureMessaging(repository)
	connection, err := grpc.Dial(config.Conf.Order.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("payment order dial failed: %v", err)
	}
	defer connection.Close()
	service, err := paymentservice.NewService(repository, paymentservice.NewGRPCOrderClient(pb.NewCommerceOrderServiceClient(connection), os.Getenv("SECKILL_INTERNAL_CALL_SECRET")), secret)
	if err != nil {
		log.Fatalf("payment service create failed: %v", err)
	}
	server, err := paymentservice.NewServer(service)
	if err != nil {
		log.Fatalf("payment grpc server create failed: %v", err)
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("", port))
	if err != nil {
		log.Fatalf("payment listen failed: %v", err)
	}
	grpcServer := grpc.NewServer()
	pb.RegisterPaymentServiceServer(grpcServer, server)
	appkit.RegisterHealth(grpcServer, "commerce.payment.v1.PaymentService")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if messagingRuntime.publisher != nil {
		defer func() { _ = messagingRuntime.publisher.Close() }()
	}
	registration := appkit.RegisterService(ctx, contracts.ServicePayment, config.Conf.Payment.Address, port)
	if registration != nil {
		defer func() { _ = registration.Close(context.Background()) }()
	}
	if messagingRuntime.publisher != nil {
		go messaging.RunOutboxPublisher(ctx, messagingRuntime.outbox, messagingRuntime.publisher, time.Second, 100)
		handler, handlerErr := paymentservice.NewEventHandler(service)
		if handlerErr != nil {
			log.Fatalf("payment event handler create failed: %v", handlerErr)
		}
		go messaging.RunRabbitConsumer(ctx, config.Conf.MQ.URL, contracts.ServicePayment, messagingRuntime.inbox, handler.Handlers(), 5)
	}
	log.Printf("payment service started addr=%s repository=%s", listener.Addr(), repositoryName(repository))
	appkit.ServeWithShutdown(ctx, grpcServer, listener)
}

func buildRepository() paymentservice.Repository {
	dsn := strings.TrimSpace(config.Conf.Payment.MySQLDSN)
	if dsn == "" {
		log.Println("payment mysql dsn is empty, using memory repository")
		return paymentservice.NewMemoryRepository()
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		log.Fatalf("payment mysql connect failed: %v", err)
	}
	repository, err := paymentservice.NewMySQLRepository(db)
	if err != nil {
		log.Fatalf("payment repository create failed: %v", err)
	}
	return repository
}

func repositoryName(repository paymentservice.Repository) string {
	switch repository.(type) {
	case *paymentservice.MemoryRepository:
		return "memory"
	case *paymentservice.MySQLRepository:
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

func configureMessaging(repository paymentservice.Repository) messageRuntime {
	var outbox messaging.OutboxStore
	var inbox messaging.InboxStore
	switch value := repository.(type) {
	case *paymentservice.MemoryRepository:
		store := messaging.NewMemoryStore()
		value.SetEventSink(store)
		outbox, inbox = store.Outbox(), store.Inbox()
	case *paymentservice.MySQLRepository:
		store, err := messaging.NewSQLStore(value.Database(), "payment_outbox_events", "payment_inbox_events")
		if err != nil {
			log.Fatalf("payment message store create failed: %v", err)
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
