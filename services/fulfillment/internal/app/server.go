// Fulfillment Service 通过 gRPC 暴露物流命令，并校验管理员或用户身份。
package fulfillmentservice

import (
	"context"
	"errors"
	"os"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"seckill-mall/shared/clients/order"
	"seckill-mall/shared/gen/commerce"
	"seckill-mall/shared/platform/internalcall"
)

type Server struct {
	pb.UnimplementedFulfillmentServiceServer
	service *Service
}

func NewServer(service *Service) (*Server, error) {
	if service == nil {
		return nil, errors.New("fulfillment service is required")
	}
	return &Server{service: service}, nil
}
func (s *Server) Ship(ctx context.Context, req *pb.FulfillmentShipRequest) (*pb.FulfillmentShipResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	if err := internalcall.AuthorizeRole(ctx, os.Getenv("SECKILL_INTERNAL_CALL_SECRET"), pb.FulfillmentService_Ship_FullMethodName, req.GetActorId(), "admin", time.Now()); err != nil {
		return nil, err
	}
	value, transition, reused, err := s.service.Ship(ctx, req.GetActorId(), req.GetOrderId(), req.GetCarrier(), req.GetTrackingNo())
	if err != nil {
		return nil, mapError(err)
	}
	return response(value, transition, reused), nil
}
func (s *Server) ConfirmReceipt(ctx context.Context, req *pb.FulfillmentConfirmReceiptRequest) (*pb.FulfillmentShipResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	if err := internalcall.AuthorizeUser(ctx, os.Getenv("SECKILL_INTERNAL_CALL_SECRET"), pb.FulfillmentService_ConfirmReceipt_FullMethodName, req.GetUserId(), time.Now()); err != nil {
		return nil, err
	}
	value, transition, reused, err := s.service.ConfirmReceipt(ctx, req.GetUserId(), req.GetOrderId())
	if err != nil {
		return nil, mapError(err)
	}
	return response(value, transition, reused), nil
}
func (s *Server) Get(ctx context.Context, req *pb.FulfillmentGetRequest) (*pb.FulfillmentShipResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	if err := internalcall.AuthorizeUser(ctx, os.Getenv("SECKILL_INTERNAL_CALL_SECRET"), pb.FulfillmentService_Get_FullMethodName, req.GetUserId(), time.Now()); err != nil {
		return nil, err
	}
	value, err := s.service.Get(ctx, req.GetUserId(), req.GetOrderId())
	if err != nil {
		return nil, mapError(err)
	}
	return response(value, orderclient.Transition{OrderID: value.OrderID}, false), nil
}
func response(value Shipment, transition orderclient.Transition, reused bool) *pb.FulfillmentShipResponse {
	return &pb.FulfillmentShipResponse{ShipmentId: PublicID(value), Status: value.Status, Message: "履约状态已更新", OrderId: value.OrderID, Carrier: value.Carrier, TrackingNo: value.TrackingNo, OrderStatus: transition.Status, Reused: reused}
}
func mapError(err error) error {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		return status.Error(codes.InvalidArgument, "履约请求参数无效")
	case errors.Is(err, ErrNotFound):
		return status.Error(codes.NotFound, "物流记录不存在")
	case errors.Is(err, ErrConflict):
		return status.Error(codes.Aborted, "物流信息冲突")
	default:
		if code := status.Code(err); code != codes.Unknown {
			return err
		}
		return status.Error(codes.Internal, "履约服务暂时不可用")
	}
}

var _ pb.FulfillmentServiceServer = (*Server)(nil)
