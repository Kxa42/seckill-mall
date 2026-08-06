package commerce

import (
	"context"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"seckill-mall/common/pb"
	"seckill-mall/internal/order"
)

const (
	OrderWriteModeLegacy       = "legacy"
	OrderWriteModeOrderService = "order_service"
)

// OrderTransition 是 Order Service 生命周期操作的最小响应模型。
type OrderTransition struct {
	OrderID string
	Status  string
	Reused  bool
}

// OrderLifecycleClient 描述 Commerce 过渡层调用唯一 Order Service 的状态接口。
type OrderLifecycleClient interface {
	ConfirmPayment(ctx context.Context, userID uint64, orderID, paymentNo, callbackRef string) (OrderTransition, error)
	Ship(ctx context.Context, actorID uint64, orderID, carrier, trackingNo string) (OrderTransition, error)
	ConfirmReceipt(ctx context.Context, userID uint64, orderID string) (OrderTransition, error)
	Refund(ctx context.Context, userID uint64, orderID, refundNo, reason string) (OrderTransition, error)
}

// GRPCOrderLifecycleClient 是 Commerce 到新 Order Service 的过渡客户端。
type GRPCOrderLifecycleClient struct {
	client pb.CommerceOrderServiceClient
	secret string
}

func NewGRPCOrderLifecycleClient(client pb.CommerceOrderServiceClient, secret string) *GRPCOrderLifecycleClient {
	return &GRPCOrderLifecycleClient{client: client, secret: strings.TrimSpace(secret)}
}

func (c *GRPCOrderLifecycleClient) ConfirmPayment(ctx context.Context, userID uint64, orderID, paymentNo, callbackRef string) (OrderTransition, error) {
	if c == nil || c.client == nil {
		return OrderTransition{}, NewError(CodeUnavailable, "Order Service 客户端未配置", nil)
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ctx = order.AppendSignedMetadata(ctx, c.secret, pb.CommerceOrderService_ConfirmPayment_FullMethodName, userID, time.Now())
	response, err := c.client.ConfirmPayment(ctx, &pb.OrderPaymentConfirmRequest{UserId: userID, OrderId: orderID, PaymentNo: paymentNo, CallbackRef: callbackRef})
	if err != nil {
		return OrderTransition{}, mapOrderClientError(err)
	}
	return transitionFromProto(response), nil
}

func (c *GRPCOrderLifecycleClient) Ship(ctx context.Context, actorID uint64, orderID, carrier, trackingNo string) (OrderTransition, error) {
	if c == nil || c.client == nil {
		return OrderTransition{}, NewError(CodeUnavailable, "Order Service 客户端未配置", nil)
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ctx = order.AppendSignedRoleMetadata(ctx, c.secret, pb.CommerceOrderService_Ship_FullMethodName, actorID, RoleAdmin, time.Now())
	response, err := c.client.Ship(ctx, &pb.OrderShipRequest{ActorId: actorID, OrderId: orderID, Carrier: carrier, TrackingNo: trackingNo})
	if err != nil {
		return OrderTransition{}, mapOrderClientError(err)
	}
	return transitionFromProto(response), nil
}

func (c *GRPCOrderLifecycleClient) ConfirmReceipt(ctx context.Context, userID uint64, orderID string) (OrderTransition, error) {
	if c == nil || c.client == nil {
		return OrderTransition{}, NewError(CodeUnavailable, "Order Service 客户端未配置", nil)
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ctx = order.AppendSignedMetadata(ctx, c.secret, pb.CommerceOrderService_ConfirmReceipt_FullMethodName, userID, time.Now())
	response, err := c.client.ConfirmReceipt(ctx, &pb.OrderConfirmReceiptRequest{UserId: userID, OrderId: orderID})
	if err != nil {
		return OrderTransition{}, mapOrderClientError(err)
	}
	return transitionFromProto(response), nil
}

func (c *GRPCOrderLifecycleClient) Refund(ctx context.Context, userID uint64, orderID, refundNo, reason string) (OrderTransition, error) {
	if c == nil || c.client == nil {
		return OrderTransition{}, NewError(CodeUnavailable, "Order Service 客户端未配置", nil)
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ctx = order.AppendSignedMetadata(ctx, c.secret, pb.CommerceOrderService_Refund_FullMethodName, userID, time.Now())
	response, err := c.client.Refund(ctx, &pb.OrderRefundRequest{UserId: userID, OrderId: orderID, RefundNo: refundNo, Reason: reason})
	if err != nil {
		return OrderTransition{}, mapOrderClientError(err)
	}
	return transitionFromProto(response), nil
}

func transitionFromProto(response *pb.OrderTransitionResponse) OrderTransition {
	if response == nil {
		return OrderTransition{}
	}
	return OrderTransition{OrderID: response.GetOrderId(), Status: response.GetStatus(), Reused: response.GetReused()}
}

func mapOrderClientError(err error) error {
	switch status.Code(err) {
	case codes.InvalidArgument:
		return NewError(CodeValidation, "订单状态操作参数无效", err)
	case codes.NotFound:
		return NewError(CodeNotFound, "订单不存在", err)
	case codes.PermissionDenied:
		return NewError(CodeForbidden, "订单状态操作权限不足", err)
	case codes.Aborted:
		return NewError(CodeConflict, "订单状态操作冲突", err)
	case codes.FailedPrecondition:
		return NewError(CodeInvalidTransition, "当前订单状态不能执行该操作", err)
	case codes.DeadlineExceeded, codes.Unavailable:
		return NewError(CodeUnavailable, "Order Service 暂时不可用", err)
	default:
		return NewError(CodeInternal, "Order Service 返回未知错误", err)
	}
}

// DialOrderLifecycle 为 commerce-api 创建独立的 Order Service 连接。
func DialOrderLifecycle(address, secret string) (*grpc.ClientConn, *GRPCOrderLifecycleClient, error) {
	connection, err := grpc.Dial(strings.TrimSpace(address), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, err
	}
	return connection, NewGRPCOrderLifecycleClient(pb.NewCommerceOrderServiceClient(connection), secret), nil
}
