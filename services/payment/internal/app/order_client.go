// Payment 的 Order 客户端仅暴露支付、回调和退款所需能力。
// gRPC 适配器统一负责 deadline、内部身份签名和错误转换。
package paymentservice

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"seckill-mall/shared/gen/commerce"
	"seckill-mall/shared/platform/internalcall"
)

type OrderSummary struct {
	OrderID          string
	UserID           uint64
	Status           string
	TotalAmountCents int64
}

type OrderTransition struct {
	OrderID string
	Status  string
	Reused  bool
}

type OrderClient interface {
	Get(ctx context.Context, userID uint64, orderID string) (OrderSummary, error)
	ConfirmPayment(ctx context.Context, userID uint64, orderID, paymentNo, callbackRef string) (OrderTransition, error)
	Refund(ctx context.Context, userID uint64, orderID, refundNo, reason string) (OrderTransition, error)
}

type GRPCOrderClient struct {
	client pb.CommerceOrderServiceClient
	secret string
}

func NewGRPCOrderClient(client pb.CommerceOrderServiceClient, secret string) *GRPCOrderClient {
	return &GRPCOrderClient{client: client, secret: secret}
}

func (c *GRPCOrderClient) Get(ctx context.Context, userID uint64, orderID string) (OrderSummary, error) {
	if c == nil || c.client == nil {
		return OrderSummary{}, status.Error(codes.Unavailable, "订单服务客户端未配置")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ctx = internalcall.AppendUser(ctx, c.secret, pb.CommerceOrderService_Get_FullMethodName, userID, time.Now())
	value, err := c.client.Get(ctx, &pb.OrderGetRequest{UserId: userID, OrderId: orderID})
	if err != nil {
		return OrderSummary{}, mapOrderClientError(err)
	}
	return OrderSummary{OrderID: value.GetOrderId(), UserID: value.GetUserId(), Status: value.GetStatus(), TotalAmountCents: value.GetTotalAmountCents()}, nil
}

func (c *GRPCOrderClient) ConfirmPayment(ctx context.Context, userID uint64, orderID, paymentNo, callbackRef string) (OrderTransition, error) {
	if c == nil || c.client == nil {
		return OrderTransition{}, status.Error(codes.Unavailable, "订单服务客户端未配置")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ctx = internalcall.AppendUser(ctx, c.secret, pb.CommerceOrderService_ConfirmPayment_FullMethodName, userID, time.Now())
	value, err := c.client.ConfirmPayment(ctx, &pb.OrderPaymentConfirmRequest{UserId: userID, OrderId: orderID, PaymentNo: paymentNo, CallbackRef: callbackRef})
	if err != nil {
		return OrderTransition{}, mapOrderClientError(err)
	}
	return OrderTransition{OrderID: value.GetOrderId(), Status: value.GetStatus(), Reused: value.GetReused()}, nil
}

func (c *GRPCOrderClient) Refund(ctx context.Context, userID uint64, orderID, refundNo, reason string) (OrderTransition, error) {
	if c == nil || c.client == nil {
		return OrderTransition{}, status.Error(codes.Unavailable, "订单服务客户端未配置")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ctx = internalcall.AppendUser(ctx, c.secret, pb.CommerceOrderService_Refund_FullMethodName, userID, time.Now())
	value, err := c.client.Refund(ctx, &pb.OrderRefundRequest{UserId: userID, OrderId: orderID, RefundNo: refundNo, Reason: reason})
	if err != nil {
		return OrderTransition{}, mapOrderClientError(err)
	}
	return OrderTransition{OrderID: value.GetOrderId(), Status: value.GetStatus(), Reused: value.GetReused()}, nil
}

func mapOrderClientError(err error) error {
	switch status.Code(err) {
	case codes.InvalidArgument, codes.NotFound, codes.PermissionDenied, codes.ResourceExhausted, codes.Aborted, codes.FailedPrecondition, codes.Unavailable, codes.DeadlineExceeded:
		return err
	default:
		return fmt.Errorf("订单服务调用失败: %w", err)
	}
}

var _ OrderClient = (*GRPCOrderClient)(nil)
