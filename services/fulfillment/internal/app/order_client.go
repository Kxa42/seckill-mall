// Fulfillment 的 Order 客户端仅暴露订单校验、发货和收货所需能力。
// gRPC 适配器统一负责 deadline、内部身份签名和错误转换。
package fulfillmentservice

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"seckill-mall/shared/gen/commerce"
	"seckill-mall/shared/platform/internalcall"
)

type OrderTransition struct {
	OrderID string
	Status  string
	Reused  bool
}

type OrderClient interface {
	Get(ctx context.Context, userID uint64, orderID string) error
	Ship(ctx context.Context, actorID uint64, orderID, carrier, trackingNo string) (OrderTransition, error)
	ConfirmReceipt(ctx context.Context, userID uint64, orderID string) (OrderTransition, error)
}

type GRPCOrderClient struct {
	client pb.CommerceOrderServiceClient
	secret string
}

func NewGRPCOrderClient(client pb.CommerceOrderServiceClient, secret string) *GRPCOrderClient {
	return &GRPCOrderClient{client: client, secret: secret}
}

func (c *GRPCOrderClient) Get(ctx context.Context, userID uint64, orderID string) error {
	if c == nil || c.client == nil {
		return status.Error(codes.Unavailable, "订单服务客户端未配置")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ctx = internalcall.AppendUser(ctx, c.secret, pb.CommerceOrderService_Get_FullMethodName, userID, time.Now())
	_, err := c.client.Get(ctx, &pb.OrderGetRequest{UserId: userID, OrderId: orderID})
	return mapOrderClientError(err)
}

func (c *GRPCOrderClient) Ship(ctx context.Context, actorID uint64, orderID, carrier, trackingNo string) (OrderTransition, error) {
	if c == nil || c.client == nil {
		return OrderTransition{}, status.Error(codes.Unavailable, "订单服务客户端未配置")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ctx = internalcall.AppendRole(ctx, c.secret, pb.CommerceOrderService_Ship_FullMethodName, actorID, "admin", time.Now())
	value, err := c.client.Ship(ctx, &pb.OrderShipRequest{ActorId: actorID, OrderId: orderID, Carrier: carrier, TrackingNo: trackingNo})
	if err != nil {
		return OrderTransition{}, mapOrderClientError(err)
	}
	return OrderTransition{OrderID: value.GetOrderId(), Status: value.GetStatus(), Reused: value.GetReused()}, nil
}

func (c *GRPCOrderClient) ConfirmReceipt(ctx context.Context, userID uint64, orderID string) (OrderTransition, error) {
	if c == nil || c.client == nil {
		return OrderTransition{}, status.Error(codes.Unavailable, "订单服务客户端未配置")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ctx = internalcall.AppendUser(ctx, c.secret, pb.CommerceOrderService_ConfirmReceipt_FullMethodName, userID, time.Now())
	value, err := c.client.ConfirmReceipt(ctx, &pb.OrderConfirmReceiptRequest{UserId: userID, OrderId: orderID})
	if err != nil {
		return OrderTransition{}, mapOrderClientError(err)
	}
	return OrderTransition{OrderID: value.GetOrderId(), Status: value.GetStatus(), Reused: value.GetReused()}, nil
}

func mapOrderClientError(err error) error {
	if err == nil {
		return nil
	}
	switch status.Code(err) {
	case codes.InvalidArgument, codes.NotFound, codes.PermissionDenied, codes.ResourceExhausted, codes.Aborted, codes.FailedPrecondition, codes.Unavailable, codes.DeadlineExceeded:
		return err
	default:
		return fmt.Errorf("订单服务调用失败: %w", err)
	}
}

var _ OrderClient = (*GRPCOrderClient)(nil)
