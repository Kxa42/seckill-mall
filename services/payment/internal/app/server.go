// Payment Service 只写支付和退款单据，订单状态由 Order Service 唯一维护。
package paymentservice

import (
	"context"
	"errors"
	"os"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"seckill-mall/shared/gen/commerce"
	"seckill-mall/shared/platform/internalcall"
)

type Server struct {
	pb.UnimplementedPaymentServiceServer
	service *Service
}

func NewServer(service *Service) (*Server, error) {
	if service == nil {
		return nil, errors.New("payment service is required")
	}
	return &Server{service: service}, nil
}
func (s *Server) Create(ctx context.Context, req *pb.PaymentCreateRequest) (*pb.PaymentCreateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	if err := s.userAuth(ctx, pb.PaymentService_Create_FullMethodName, req.GetUserId()); err != nil {
		return nil, err
	}
	value, signature, reused, err := s.service.Create(ctx, req.GetUserId(), req.GetOrderId())
	if err != nil {
		return nil, mapError(err)
	}
	response := paymentProto(value, signature)
	response.Reused = reused
	return response, nil
}
func (s *Server) Callback(ctx context.Context, req *pb.PaymentCallbackRequest) (*pb.PaymentCallbackResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	if err := s.systemAuth(ctx, pb.PaymentService_Callback_FullMethodName); err != nil {
		return nil, err
	}
	value, transition, reused, err := s.service.Callback(ctx, req.GetPaymentNo(), req.GetCallbackRef(), req.GetSignature())
	if err != nil {
		return nil, mapError(err)
	}
	return &pb.PaymentCallbackResponse{Payment: paymentProto(value, ""), OrderStatus: transition.Status, Reused: reused}, nil
}
func (s *Server) Refund(ctx context.Context, req *pb.PaymentRefundRequest) (*pb.PaymentRefundResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	if err := s.userAuth(ctx, pb.PaymentService_Refund_FullMethodName, req.GetUserId()); err != nil {
		return nil, err
	}
	value, transition, reused, err := s.service.Refund(ctx, req.GetUserId(), req.GetOrderId(), req.GetReason())
	if err != nil {
		return nil, mapError(err)
	}
	return &pb.PaymentRefundResponse{RefundNo: value.RefundNo, OrderId: value.OrderID, PaymentNo: value.PaymentNo, AmountCents: value.AmountCents, Reason: value.Reason, Status: value.Status, OrderStatus: transition.Status, Reused: reused}, nil
}
func (s *Server) userAuth(ctx context.Context, method string, userID uint64) error {
	return internalcall.AuthorizeUser(ctx, os.Getenv("SECKILL_INTERNAL_CALL_SECRET"), method, userID, time.Now())
}
func (s *Server) systemAuth(ctx context.Context, method string) error {
	return internalcall.AuthorizeRole(ctx, os.Getenv("SECKILL_INTERNAL_CALL_SECRET"), method, internalcall.SystemActorID, internalcall.SystemRole, time.Now())
}
func paymentProto(value Payment, signature string) *pb.PaymentCreateResponse {
	return &pb.PaymentCreateResponse{PaymentNo: value.PaymentNo, AmountCents: value.AmountCents, Status: value.Status, OrderId: value.OrderID, UserId: value.UserID, Provider: value.Provider, CallbackSignature: signature}
}
func mapError(err error) error {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		return status.Error(codes.InvalidArgument, "支付请求参数无效")
	case errors.Is(err, ErrUnauthorized):
		return status.Error(codes.Unauthenticated, "支付回调签名无效")
	case errors.Is(err, ErrNotFound):
		return status.Error(codes.NotFound, "支付资源不存在")
	case errors.Is(err, ErrConflict):
		return status.Error(codes.Aborted, "支付请求冲突")
	case errors.Is(err, ErrInvalidTransition):
		return status.Error(codes.FailedPrecondition, "当前订单不能执行支付操作")
	case status.Code(err) == codes.Unavailable:
		return err
	default:
		return status.Error(codes.Internal, "支付服务暂时不可用")
	}
}

var _ pb.PaymentServiceServer = (*Server)(nil)
