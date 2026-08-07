package inventoryservice

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"seckill-mall/shared/gen/commerce"
)

// Server 实现 Inventory/Seckill gRPC 服务。
type Server struct {
	pb.UnimplementedInventoryServiceServer
	store Store
}

// NewServer 创建库存服务端。
func NewServer(store Store) (*Server, error) {
	if store == nil {
		return nil, errors.New("inventory store is required")
	}
	return &Server{store: store}, nil
}

func (s *Server) Reserve(ctx context.Context, req *pb.InventoryReserveRequest) (*pb.InventoryReservationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	reservation, err := s.store.Reserve(ctx, ReserveCommand{ReservationID: req.ReservationId, OrderID: req.OrderId, UserID: req.UserId, SKUID: req.SkuId, Quantity: req.Quantity, Mode: req.Mode})
	if err != nil {
		return nil, mapError(err)
	}
	return reservationResponse(reservation, "库存预占成功"), nil
}

func (s *Server) Confirm(ctx context.Context, req *pb.InventoryReservationRequest) (*pb.InventoryReservationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	reservation, err := s.store.Confirm(ctx, req.ReservationId, req.OrderId)
	if err != nil {
		return nil, mapError(err)
	}
	return reservationResponse(reservation, "库存预占已确认"), nil
}

func (s *Server) Release(ctx context.Context, req *pb.InventoryReservationRequest) (*pb.InventoryReservationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	reservation, err := s.store.Release(ctx, req.ReservationId, req.OrderId)
	if err != nil {
		return nil, mapError(err)
	}
	return reservationResponse(reservation, "库存预占已释放"), nil
}

func (s *Server) Restock(ctx context.Context, req *pb.InventoryReservationRequest) (*pb.InventoryReservationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	reservation, err := s.store.Restock(ctx, req.ReservationId, req.OrderId)
	if err != nil {
		return nil, mapError(err)
	}
	return reservationResponse(reservation, "已退款并恢复库存"), nil
}

func (s *Server) AdmitSeckill(ctx context.Context, req *pb.InventorySeckillAdmitRequest) (*pb.InventorySeckillAdmitResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	reservation, err := s.store.AdmitSeckill(ctx, SeckillAdmissionCommand{RequestID: req.RequestId, ActivityID: req.ActivityId, UserID: req.UserId, SKUID: req.SkuId, Quantity: req.Quantity, OrderID: req.OrderId})
	if err != nil {
		return nil, mapError(err)
	}
	return &pb.InventorySeckillAdmitResponse{AdmissionId: reservation.ReservationID, Status: reservation.Status, Message: "秒杀准入成功"}, nil
}

func reservationResponse(reservation Reservation, message string) *pb.InventoryReservationResponse {
	return &pb.InventoryReservationResponse{ReservationId: reservation.ReservationID, Status: reservation.Status, Message: message}
}

func mapError(err error) error {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		return status.Error(codes.InvalidArgument, "库存请求参数无效")
	case errors.Is(err, ErrNotFound):
		return status.Error(codes.NotFound, "库存预占不存在")
	case errors.Is(err, ErrOutOfStock):
		return status.Error(codes.ResourceExhausted, "库存不足")
	case errors.Is(err, ErrPurchaseLimit):
		return status.Error(codes.AlreadyExists, "超过秒杀限购数量")
	case errors.Is(err, ErrConflict):
		return status.Error(codes.Aborted, "库存预占状态冲突")
	default:
		return status.Error(codes.Internal, "库存服务暂时不可用")
	}
}
