package order

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"seckill-mall/shared/gen/commerce"
)

// GRPCServer 将 Order Service 应用服务暴露为版本化 gRPC 接口。
type GRPCServer struct {
	pb.UnimplementedCommerceOrderServiceServer
	service *Service
}

func NewGRPCServer(service *Service) (*GRPCServer, error) {
	if service == nil {
		return nil, errors.New("order service is required")
	}
	return &GRPCServer{service: service}, nil
}

func (s *GRPCServer) Create(ctx context.Context, req *pb.OrderCreateRequest) (*pb.OrderCreateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	if err := AuthorizeInternalRequest(ctx, pb.CommerceOrderService_Create_FullMethodName, req.GetUserId()); err != nil {
		return nil, err
	}
	items := make([]CreateItem, 0, len(req.GetItems()))
	for _, item := range req.GetItems() {
		if item == nil {
			return nil, status.Error(codes.InvalidArgument, "订单项不能为空")
		}
		items = append(items, CreateItem{SKUID: item.GetSkuId(), Quantity: item.GetQuantity()})
	}
	value, reused, err := s.service.Create(ctx, CreateCommand{UserID: req.GetUserId(), AddressID: req.GetAddressId(), OrderType: req.GetOrderType(), ActivityID: req.GetActivityId(), IdempotencyKey: req.GetIdempotencyKey(), Items: items})
	if err != nil {
		return nil, mapError(err)
	}
	return &pb.OrderCreateResponse{OrderId: value.OrderID, Status: value.Status, TotalAmountCents: value.TotalAmountCents, Reused: reused, Message: "订单创建成功"}, nil
}

func (s *GRPCServer) Get(ctx context.Context, req *pb.OrderGetRequest) (*pb.OrderGetResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	if err := AuthorizeInternalRequest(ctx, pb.CommerceOrderService_Get_FullMethodName, req.GetUserId()); err != nil {
		return nil, err
	}
	value, err := s.service.Get(ctx, req.GetUserId(), req.GetOrderId())
	if err != nil {
		return nil, mapError(err)
	}
	return orderResponse(value), nil
}

func (s *GRPCServer) List(ctx context.Context, req *pb.OrderListRequest) (*pb.OrderListResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	if err := AuthorizeInternalRequest(ctx, pb.CommerceOrderService_List_FullMethodName, req.GetUserId()); err != nil {
		return nil, err
	}
	items, total, err := s.service.List(ctx, req.GetUserId(), int(req.GetOffset()), int(req.GetLimit()))
	if err != nil {
		return nil, mapError(err)
	}
	response := &pb.OrderListResponse{Total: total, Offset: req.GetOffset(), Limit: req.GetLimit(), Items: make([]*pb.OrderGetResponse, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, orderResponse(item))
	}
	return response, nil
}

func (s *GRPCServer) Cancel(ctx context.Context, req *pb.OrderCancelRequest) (*pb.OrderCancelResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	if err := AuthorizeInternalRequest(ctx, pb.CommerceOrderService_Cancel_FullMethodName, req.GetUserId()); err != nil {
		return nil, err
	}
	value, err := s.service.Cancel(ctx, req.GetUserId(), req.GetOrderId(), req.GetReason())
	if err != nil {
		return nil, mapError(err)
	}
	return &pb.OrderCancelResponse{OrderId: value.OrderID, Status: value.Status, Message: "订单已取消"}, nil
}

func (s *GRPCServer) Expire(ctx context.Context, req *pb.OrderExpireRequest) (*pb.OrderExpireResponse, error) {
	if err := AuthorizeInternalRoleRequest(ctx, pb.CommerceOrderService_Expire_FullMethodName, systemActorID, systemRole); err != nil {
		return nil, err
	}
	limit := 100
	if req != nil && req.GetLimit() > 0 {
		limit = int(req.GetLimit())
	}
	count, err := s.service.Expire(ctx, limit)
	if err != nil {
		return nil, mapError(err)
	}
	return &pb.OrderExpireResponse{Count: int32(count)}, nil
}

func (s *GRPCServer) ConfirmPayment(ctx context.Context, req *pb.OrderPaymentConfirmRequest) (*pb.OrderTransitionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	if err := AuthorizeInternalRequest(ctx, pb.CommerceOrderService_ConfirmPayment_FullMethodName, req.GetUserId()); err != nil {
		return nil, err
	}
	value, reused, err := s.service.ConfirmPayment(ctx, req.GetUserId(), req.GetOrderId(), req.GetPaymentNo(), req.GetCallbackRef())
	if err != nil {
		return nil, mapError(err)
	}
	return &pb.OrderTransitionResponse{OrderId: value.OrderID, Status: value.Status, Reused: reused, Message: "支付状态已确认"}, nil
}

func (s *GRPCServer) Ship(ctx context.Context, req *pb.OrderShipRequest) (*pb.OrderTransitionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	if err := AuthorizeInternalRoleRequest(ctx, pb.CommerceOrderService_Ship_FullMethodName, req.GetActorId(), "admin"); err != nil {
		return nil, err
	}
	value, err := s.service.Ship(ctx, req.GetActorId(), req.GetOrderId(), req.GetCarrier(), req.GetTrackingNo())
	if err != nil {
		return nil, mapError(err)
	}
	return &pb.OrderTransitionResponse{OrderId: value.OrderID, Status: value.Status, Message: "订单状态已更新为已发货"}, nil
}

func (s *GRPCServer) ConfirmReceipt(ctx context.Context, req *pb.OrderConfirmReceiptRequest) (*pb.OrderTransitionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	if err := AuthorizeInternalRequest(ctx, pb.CommerceOrderService_ConfirmReceipt_FullMethodName, req.GetUserId()); err != nil {
		return nil, err
	}
	value, err := s.service.ConfirmReceipt(ctx, req.GetUserId(), req.GetOrderId())
	if err != nil {
		return nil, mapError(err)
	}
	return &pb.OrderTransitionResponse{OrderId: value.OrderID, Status: value.Status, Message: "订单状态已更新为已完成"}, nil
}

func (s *GRPCServer) Refund(ctx context.Context, req *pb.OrderRefundRequest) (*pb.OrderTransitionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	if err := AuthorizeInternalRequest(ctx, pb.CommerceOrderService_Refund_FullMethodName, req.GetUserId()); err != nil {
		return nil, err
	}
	value, err := s.service.Refund(ctx, req.GetUserId(), req.GetOrderId(), req.GetRefundNo(), req.GetReason())
	if err != nil {
		return nil, mapError(err)
	}
	return &pb.OrderTransitionResponse{OrderId: value.OrderID, Status: value.Status, Message: "订单状态已更新为已退款"}, nil
}

func orderResponse(value Order) *pb.OrderGetResponse {
	return &pb.OrderGetResponse{OrderId: value.OrderID, UserId: value.UserID, OrderType: value.OrderType, Status: value.Status, TotalAmountCents: value.TotalAmountCents}
}

func mapError(err error) error {
	switch ErrorCode(err) {
	case CodeValidation:
		return status.Error(codes.InvalidArgument, PublicMessage(err))
	case CodeNotFound:
		return status.Error(codes.NotFound, PublicMessage(err))
	case CodeForbidden:
		return status.Error(codes.PermissionDenied, PublicMessage(err))
	case CodeConflict:
		return status.Error(codes.Aborted, PublicMessage(err))
	case CodeOutOfStock:
		return status.Error(codes.ResourceExhausted, PublicMessage(err))
	case CodeInvalidTransition:
		return status.Error(codes.FailedPrecondition, PublicMessage(err))
	case CodeUnavailable:
		return status.Error(codes.Unavailable, PublicMessage(err))
	default:
		if errors.Is(err, context.Canceled) {
			return status.Error(codes.Canceled, "请求已取消")
		}
		return status.Error(codes.Internal, "订单服务暂时不可用")
	}
}
