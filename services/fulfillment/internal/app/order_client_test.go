package fulfillmentservice

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

type fulfillmentOrderTestServer struct {
	pb.UnimplementedCommerceOrderServiceServer
	secret string
	getErr error
}

func (s *fulfillmentOrderTestServer) Get(ctx context.Context, req *pb.OrderGetRequest) (*pb.OrderGetResponse, error) {
	if err := internalcall.AuthorizeUser(ctx, s.secret, pb.CommerceOrderService_Get_FullMethodName, req.GetUserId(), time.Now()); err != nil {
		return nil, err
	}
	if s.getErr != nil {
		return nil, s.getErr
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, status.Error(codes.Internal, "missing deadline")
	}
	return &pb.OrderGetResponse{OrderId: req.GetOrderId(), UserId: req.GetUserId(), Status: "paid"}, nil
}

func (s *fulfillmentOrderTestServer) Ship(ctx context.Context, req *pb.OrderShipRequest) (*pb.OrderTransitionResponse, error) {
	if err := internalcall.AuthorizeRole(ctx, s.secret, pb.CommerceOrderService_Ship_FullMethodName, req.GetActorId(), "admin", time.Now()); err != nil {
		return nil, err
	}
	return &pb.OrderTransitionResponse{OrderId: req.GetOrderId(), Status: "shipped"}, nil
}

func (s *fulfillmentOrderTestServer) ConfirmReceipt(ctx context.Context, req *pb.OrderConfirmReceiptRequest) (*pb.OrderTransitionResponse, error) {
	if err := internalcall.AuthorizeUser(ctx, s.secret, pb.CommerceOrderService_ConfirmReceipt_FullMethodName, req.GetUserId(), time.Now()); err != nil {
		return nil, err
	}
	return &pb.OrderTransitionResponse{OrderId: req.GetOrderId(), Status: "completed", Reused: true}, nil
}

func newFulfillmentOrderClient(t *testing.T, service pb.CommerceOrderServiceServer, secret string) *GRPCOrderClient {
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

func TestGRPCOrderClientSignsUserAndAdminCalls(t *testing.T) {
	secret := strings.Repeat("f", 32)
	server := &fulfillmentOrderTestServer{secret: secret}
	client := newFulfillmentOrderClient(t, server, secret)

	if err := client.Get(context.Background(), 9, "order-1"); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	transition, err := client.Ship(context.Background(), 7, "order-1", "SF", "SF-1")
	if err != nil || transition.Status != "shipped" {
		t.Fatalf("Ship() = %+v, err = %v", transition, err)
	}
	transition, err = client.ConfirmReceipt(context.Background(), 9, "order-1")
	if err != nil || transition.Status != "completed" || !transition.Reused {
		t.Fatalf("ConfirmReceipt() = %+v, err = %v", transition, err)
	}
	server.getErr = status.Error(codes.NotFound, "订单不存在")
	if err := client.Get(context.Background(), 9, "missing"); status.Code(err) != codes.NotFound {
		t.Fatalf("Get() code = %s, want %s", status.Code(err), codes.NotFound)
	}
}
