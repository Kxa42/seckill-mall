package identityservice

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"seckill-mall/common/pb"
)

type Server struct {
	pb.UnimplementedIdentityServiceServer
	repository Repository
}

func NewServer(repository Repository) (*Server, error) {
	if repository == nil {
		return nil, errors.New("identity repository is required")
	}
	return &Server{repository: repository}, nil
}

func (s *Server) GetAddressSnapshot(ctx context.Context, req *pb.IdentityAddressSnapshotRequest) (*pb.IdentityAddressSnapshot, error) {
	if req == nil || req.GetUserId() == 0 || req.GetAddressId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "用户和地址不能为空")
	}
	value, err := s.repository.GetAddress(ctx, req.GetUserId(), req.GetAddressId())
	if err != nil {
		if errors.Is(err, ErrAddressNotFound) {
			return nil, status.Error(codes.NotFound, "地址不存在")
		}
		return nil, status.Error(codes.Internal, "身份服务暂时不可用")
	}
	return &pb.IdentityAddressSnapshot{AddressId: value.ID, UserId: value.UserID, Recipient: value.Recipient, Phone: value.Phone, Province: value.Province, City: value.City, District: value.District, Detail: value.Detail}, nil
}
