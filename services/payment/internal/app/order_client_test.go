package paymentservice

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"seckill-mall/shared/gen/commerce"
	"seckill-mall/shared/platform/internalcall"
)

type paymentOrderTestServer struct {
	pb.UnimplementedCommerceOrderServiceServer
	secret    string
	refundErr error
}

func (s *paymentOrderTestServer) Get(ctx context.Context, req *pb.OrderGetRequest) (*pb.OrderGetResponse, error) {
	if err := internalcall.AuthorizeUser(ctx, s.secret, pb.CommerceOrderService_Get_FullMethodName, req.GetUserId(), time.Now()); err != nil {
		return nil, err
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, status.Error(codes.Internal, "missing deadline")
	}
	return &pb.OrderGetResponse{OrderId: req.GetOrderId(), UserId: req.GetUserId(), Status: "pending_payment", TotalAmountCents: 1200}, nil
}

func (s *paymentOrderTestServer) ConfirmPayment(ctx context.Context, req *pb.OrderPaymentConfirmRequest) (*pb.OrderTransitionResponse, error) {
	if err := internalcall.AuthorizeUser(ctx, s.secret, pb.CommerceOrderService_ConfirmPayment_FullMethodName, req.GetUserId(), time.Now()); err != nil {
		return nil, err
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, status.Error(codes.Internal, "missing deadline")
	}
	return &pb.OrderTransitionResponse{OrderId: req.GetOrderId(), Status: "paid"}, nil
}

func (s *paymentOrderTestServer) Refund(ctx context.Context, req *pb.OrderRefundRequest) (*pb.OrderTransitionResponse, error) {
	if err := internalcall.AuthorizeUser(ctx, s.secret, pb.CommerceOrderService_Refund_FullMethodName, req.GetUserId(), time.Now()); err != nil {
		return nil, err
	}
	if s.refundErr != nil {
		return nil, s.refundErr
	}
	return &pb.OrderTransitionResponse{OrderId: req.GetOrderId(), Status: "refunded", Reused: true}, nil
}

func newPaymentOrderClient(t *testing.T, service pb.CommerceOrderServiceServer, secret string) *GRPCOrderClient {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	pb.RegisterCommerceOrderServiceServer(grpcServer, service)
	go func() { _ = grpcServer.Serve(listener) }()
	conn, err := grpc.DialContext(context.Background(), "bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.DialContext() error = %v", err)
	}
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = conn.Close()
		_ = listener.Close()
	})
	return NewGRPCOrderClient(pb.NewCommerceOrderServiceClient(conn), secret)
}

func TestGRPCOrderClientSignsCallsAndMapsErrors(t *testing.T) {
	secret := strings.Repeat("p", 32)
	server := &paymentOrderTestServer{secret: secret, refundErr: status.Error(codes.NotFound, "订单不存在")}
	client := newPaymentOrderClient(t, server, secret)

	summary, err := client.Get(context.Background(), 9, "order-1")
	if err != nil || summary.TotalAmountCents != 1200 || summary.Status != "pending_payment" {
		t.Fatalf("Get() = %+v, err = %v", summary, err)
	}
	transition, err := client.ConfirmPayment(context.Background(), 9, "order-1", "pay-1", "callback-1")
	if err != nil || transition.Status != "paid" {
		t.Fatalf("ConfirmPayment() = %+v, err = %v", transition, err)
	}
	_, err = client.Refund(context.Background(), 9, "order-1", "ref-1", "用户申请退款")
	if status.Code(err) != codes.NotFound {
		t.Fatalf("Refund() code = %s, want %s", status.Code(err), codes.NotFound)
	}
}
