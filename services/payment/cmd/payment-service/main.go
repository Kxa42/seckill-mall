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
	if messagingRuntime != nil {
		defer func() { _ = messagingRuntime.Close() }()
	}
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
	registration := appkit.RegisterService(ctx, contracts.ServicePayment, config.Conf.Payment.Address, port)
	if registration != nil {
		defer func() { _ = registration.Close(context.Background()) }()
	}
	if messagingRuntime != nil && messagingRuntime.Enabled() {
		handler, handlerErr := paymentservice.NewEventHandler(service)
		if handlerErr != nil {
			log.Fatalf("payment event handler create failed: %v", handlerErr)
		}
		messagingRuntime.Start(ctx, contracts.ServicePayment, handler.Handlers())
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

func configureMessaging(repository paymentservice.Repository) *messaging.ServiceRuntime {
	switch value := repository.(type) {
	case *paymentservice.MemoryRepository:
		runtime, err := messaging.NewMemoryServiceRuntime(value, config.Conf.MQ.URL)
		if err != nil {
			log.Fatalf("payment message runtime create failed: %v", err)
		}
		return runtime
	case *paymentservice.MySQLRepository:
		runtime, err := messaging.NewSQLServiceRuntime(value, value.Database(), "payment_outbox_events", "payment_inbox_events", config.Conf.MQ.URL)
		if err != nil {
			log.Fatalf("payment message runtime create failed: %v", err)
		}
		return runtime
	default:
		return nil
	}
}
