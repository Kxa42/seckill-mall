// Cart Service 将购物车应用服务暴露为 gRPC，并在服务端校验用户调用身份。
package cartservice

import (
	"context"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"seckill-mall/shared/gen/commerce"
	"seckill-mall/shared/platform/internalcall"
)

type Server struct {
	pb.UnimplementedCartServiceServer
	service *Service
}

func NewServer(service *Service) (*Server, error) {
	if service == nil {
		return nil, status.Error(codes.InvalidArgument, "cart service is required")
	}
	return &Server{service: service}, nil
}

func (s *Server) Set(ctx context.Context, req *pb.CartSetRequest) (*pb.CartItem, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	if err := s.auth(ctx, pb.CartService_Set_FullMethodName, req.GetUserId()); err != nil {
		return nil, err
	}
	value, err := s.service.Set(ctx, req.GetUserId(), req.GetSkuId(), req.GetQuantity())
	if err != nil {
		return nil, statusError(err)
	}
	return itemProto(value), nil
}
func (s *Server) Delete(ctx context.Context, req *pb.CartDeleteRequest) (*pb.CartDeleteResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	if err := s.auth(ctx, pb.CartService_Delete_FullMethodName, req.GetUserId()); err != nil {
		return nil, err
	}
	if err := s.service.Delete(ctx, req.GetUserId(), req.GetSkuId()); err != nil {
		return nil, statusError(err)
	}
	return &pb.CartDeleteResponse{Deleted: true}, nil
}
func (s *Server) List(ctx context.Context, req *pb.CartListRequest) (*pb.CartListResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	if err := s.auth(ctx, pb.CartService_List_FullMethodName, req.GetUserId()); err != nil {
		return nil, err
	}
	values, err := s.service.List(ctx, req.GetUserId())
	if err != nil {
		return nil, statusError(err)
	}
	result := &pb.CartListResponse{Items: make([]*pb.CartItem, 0, len(values))}
	for _, value := range values {
		result.Items = append(result.Items, itemProto(value))
	}
	return result, nil
}
func (s *Server) Preview(ctx context.Context, req *pb.CartPreviewRequest) (*pb.CartPreviewResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	if err := s.auth(ctx, pb.CartService_Preview_FullMethodName, req.GetUserId()); err != nil {
		return nil, err
	}
	value, err := s.service.Preview(ctx, req.GetUserId())
	if err != nil {
		return nil, statusError(err)
	}
	result := &pb.CartPreviewResponse{TotalAmountCents: value.TotalAmountCents, Available: value.Available, Items: make([]*pb.CartItem, 0, len(value.Items))}
	for _, item := range value.Items {
		result.Items = append(result.Items, itemProto(item))
	}
	return result, nil
}

func (s *Server) auth(ctx context.Context, method string, userID uint64) error {
	return internalcall.AuthorizeUser(ctx, strings.TrimSpace(os.Getenv("SECKILL_INTERNAL_CALL_SECRET")), method, userID, time.Now())
}
func itemProto(value Item) *pb.CartItem {
	return &pb.CartItem{SkuId: value.SKUID, Quantity: value.Quantity, SkuCode: value.SKU.Code, SkuName: value.SKU.Name, UnitPriceCents: value.SKU.PriceCents, SubtotalCents: value.SubtotalCents, Available: value.SKU.Active && value.Quantity <= value.SKU.AvailableStock}
}
