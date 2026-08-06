// Package orderclient 提供 Payment/Fulfillment 使用的 Order Service 客户端。
// 客户端统一负责 deadline、内部身份签名和 gRPC 错误转换。
package orderclient

import (
	"context"
	"fmt"
	"os"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"seckill-mall/common/internalcall"
	"seckill-mall/common/pb"
)

type Summary struct {
	OrderID          string
	UserID           uint64
	Status           string
	TotalAmountCents int64
}
type Transition struct {
	OrderID string
	Status  string
	Reused  bool
}

type Client interface {
	Get(ctx context.Context, userID uint64, orderID string) (Summary, error)
	ConfirmPayment(ctx context.Context, userID uint64, orderID, paymentNo, callbackRef string) (Transition, error)
	Refund(ctx context.Context, userID uint64, orderID, refundNo, reason string) (Transition, error)
	Ship(ctx context.Context, actorID uint64, orderID, carrier, trackingNo string) (Transition, error)
	ConfirmReceipt(ctx context.Context, userID uint64, orderID string) (Transition, error)
}

type GRPCClient struct {
	client pb.CommerceOrderServiceClient
	secret string
}

func NewGRPCClient(client pb.CommerceOrderServiceClient, secret string) *GRPCClient {
	return &GRPCClient{client: client, secret: secret}
}

func (c *GRPCClient) Get(ctx context.Context, userID uint64, orderID string) (Summary, error) {
	if c == nil || c.client == nil {
		return Summary{}, status.Error(codes.Unavailable, "订单服务客户端未配置")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ctx = internalcall.AppendUser(ctx, c.secret, pb.CommerceOrderService_Get_FullMethodName, userID, time.Now())
	value, err := c.client.Get(ctx, &pb.OrderGetRequest{UserId: userID, OrderId: orderID})
	if err != nil {
		return Summary{}, mapError(err)
	}
	return Summary{OrderID: value.GetOrderId(), UserID: value.GetUserId(), Status: value.GetStatus(), TotalAmountCents: value.GetTotalAmountCents()}, nil
}
func (c *GRPCClient) ConfirmPayment(ctx context.Context, userID uint64, orderID, paymentNo, callbackRef string) (Transition, error) {
	if c == nil || c.client == nil {
		return Transition{}, status.Error(codes.Unavailable, "订单服务客户端未配置")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ctx = internalcall.AppendUser(ctx, c.secret, pb.CommerceOrderService_ConfirmPayment_FullMethodName, userID, time.Now())
	value, err := c.client.ConfirmPayment(ctx, &pb.OrderPaymentConfirmRequest{UserId: userID, OrderId: orderID, PaymentNo: paymentNo, CallbackRef: callbackRef})
	if err != nil {
		return Transition{}, mapError(err)
	}
	return Transition{OrderID: value.GetOrderId(), Status: value.GetStatus(), Reused: value.GetReused()}, nil
}
func (c *GRPCClient) Refund(ctx context.Context, userID uint64, orderID, refundNo, reason string) (Transition, error) {
	if c == nil || c.client == nil {
		return Transition{}, status.Error(codes.Unavailable, "订单服务客户端未配置")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ctx = internalcall.AppendUser(ctx, c.secret, pb.CommerceOrderService_Refund_FullMethodName, userID, time.Now())
	value, err := c.client.Refund(ctx, &pb.OrderRefundRequest{UserId: userID, OrderId: orderID, RefundNo: refundNo, Reason: reason})
	if err != nil {
		return Transition{}, mapError(err)
	}
	return Transition{OrderID: value.GetOrderId(), Status: value.GetStatus(), Reused: value.GetReused()}, nil
}
func (c *GRPCClient) Ship(ctx context.Context, actorID uint64, orderID, carrier, trackingNo string) (Transition, error) {
	if c == nil || c.client == nil {
		return Transition{}, status.Error(codes.Unavailable, "订单服务客户端未配置")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ctx = internalcall.AppendRole(ctx, c.secret, pb.CommerceOrderService_Ship_FullMethodName, actorID, "admin", time.Now())
	value, err := c.client.Ship(ctx, &pb.OrderShipRequest{ActorId: actorID, OrderId: orderID, Carrier: carrier, TrackingNo: trackingNo})
	if err != nil {
		return Transition{}, mapError(err)
	}
	return Transition{OrderID: value.GetOrderId(), Status: value.GetStatus(), Reused: value.GetReused()}, nil
}
func (c *GRPCClient) ConfirmReceipt(ctx context.Context, userID uint64, orderID string) (Transition, error) {
	if c == nil || c.client == nil {
		return Transition{}, status.Error(codes.Unavailable, "订单服务客户端未配置")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ctx = internalcall.AppendUser(ctx, c.secret, pb.CommerceOrderService_ConfirmReceipt_FullMethodName, userID, time.Now())
	value, err := c.client.ConfirmReceipt(ctx, &pb.OrderConfirmReceiptRequest{UserId: userID, OrderId: orderID})
	if err != nil {
		return Transition{}, mapError(err)
	}
	return Transition{OrderID: value.GetOrderId(), Status: value.GetStatus(), Reused: value.GetReused()}, nil
}

func mapError(err error) error {
	switch status.Code(err) {
	case codes.InvalidArgument, codes.NotFound, codes.PermissionDenied, codes.ResourceExhausted, codes.Aborted, codes.FailedPrecondition, codes.Unavailable, codes.DeadlineExceeded:
		return err
	default:
		return fmt.Errorf("订单服务调用失败: %w", err)
	}
}

func SecretFromEnv() string { return os.Getenv("SECKILL_INTERNAL_CALL_SECRET") }

var _ Client = (*GRPCClient)(nil)
