// Identity Service 提供认证、地址归属校验和订单地址快照。
// 所有用户资源在服务端再次校验 HMAC metadata，不信任 Gateway 请求体中的 user_id。
package identityservice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"seckill-mall/shared/gen/commerce"
	platformauth "seckill-mall/shared/platform/auth"
	"seckill-mall/shared/platform/internalcall"
)

type Server struct {
	pb.UnimplementedIdentityServiceServer
	repository Repository
	auth       *platformauth.Manager
	now        func() time.Time
}

func NewServer(repository Repository, managers ...*platformauth.Manager) (*Server, error) {
	if repository == nil {
		return nil, errors.New("identity repository is required")
	}
	server := &Server{repository: repository, now: time.Now}
	if len(managers) > 0 {
		server.auth = managers[0]
	}
	return server, nil
}

func (s *Server) Register(ctx context.Context, req *pb.IdentityCredentialsRequest) (*pb.IdentityTokenPair, error) {
	if err := s.authorizeSystem(ctx, pb.IdentityService_Register_FullMethodName); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	email, password, err := normalizeCredentials(req.GetEmail(), req.GetPassword())
	if err != nil {
		return nil, err
	}
	hash, err := platformauth.HashPassword(password)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	user, err := s.repository.CreateUser(ctx, email, hash, RoleCustomer, s.now().UTC())
	if err != nil {
		return nil, mapError(err)
	}
	return s.issueTokenPair(ctx, user)
}

func (s *Server) Login(ctx context.Context, req *pb.IdentityCredentialsRequest) (*pb.IdentityTokenPair, error) {
	if err := s.authorizeSystem(ctx, pb.IdentityService_Login_FullMethodName); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	email, password, err := normalizeCredentials(req.GetEmail(), req.GetPassword())
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "邮箱或密码错误")
	}
	user, err := s.repository.FindUserByEmail(ctx, email)
	if err != nil || user.Status != StatusActive || platformauth.VerifyPassword(user.PasswordHash, password) != nil {
		return nil, status.Error(codes.Unauthenticated, "邮箱或密码错误")
	}
	return s.issueTokenPair(ctx, user)
}

func (s *Server) Refresh(ctx context.Context, req *pb.IdentityRefreshRequest) (*pb.IdentityTokenPair, error) {
	if err := s.authorizeSystem(ctx, pb.IdentityService_Refresh_FullMethodName); err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.GetRefreshToken()) == "" {
		return nil, status.Error(codes.Unauthenticated, "刷新令牌无效")
	}
	if s.auth == nil {
		return nil, status.Error(codes.Unavailable, "身份服务认证未就绪")
	}
	replacement, err := s.auth.IssueRefreshToken()
	if err != nil {
		return nil, status.Error(codes.Internal, "刷新令牌生成失败")
	}
	user, err := s.repository.RotateRefreshToken(ctx, platformauth.HashOpaqueToken(req.GetRefreshToken()), RefreshToken{TokenHash: replacement.Hash, ExpiresAt: replacement.ExpiresAt, CreatedAt: s.now().UTC()}, s.now().UTC())
	if err != nil {
		return nil, mapError(err)
	}
	return s.issueTokenPairWithRefresh(ctx, user, replacement, false)
}

func (s *Server) CreateAddress(ctx context.Context, req *pb.IdentityAddressCreateRequest) (*pb.IdentityAddress, error) {
	if req == nil || req.GetUserId() == 0 || req.GetAddress() == nil {
		return nil, status.Error(codes.InvalidArgument, "地址参数无效")
	}
	if err := s.authorizeUser(ctx, pb.IdentityService_CreateAddress_FullMethodName, req.GetUserId()); err != nil {
		return nil, err
	}
	address := addressFromProto(req.GetUserId(), req.GetAddress())
	if err := validateAddress(address); err != nil {
		return nil, err
	}
	value, err := s.repository.CreateAddress(ctx, address, s.now().UTC())
	if err != nil {
		return nil, mapError(err)
	}
	return addressProto(value), nil
}

func (s *Server) UpdateAddress(ctx context.Context, req *pb.IdentityAddressUpdateRequest) (*pb.IdentityAddress, error) {
	if req == nil || req.GetUserId() == 0 || req.GetAddressId() == 0 || req.GetAddress() == nil {
		return nil, status.Error(codes.InvalidArgument, "地址参数无效")
	}
	if err := s.authorizeUser(ctx, pb.IdentityService_UpdateAddress_FullMethodName, req.GetUserId()); err != nil {
		return nil, err
	}
	value := addressFromProto(req.GetUserId(), req.GetAddress())
	value.ID = req.GetAddressId()
	if err := validateAddress(value); err != nil {
		return nil, err
	}
	updated, err := s.repository.UpdateAddress(ctx, value, s.now().UTC())
	if err != nil {
		return nil, mapError(err)
	}
	return addressProto(updated), nil
}

func (s *Server) DeleteAddress(ctx context.Context, req *pb.IdentityAddressDeleteRequest) (*pb.IdentityAddressDeleteResponse, error) {
	if req == nil || req.GetUserId() == 0 || req.GetAddressId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "地址参数无效")
	}
	if err := s.authorizeUser(ctx, pb.IdentityService_DeleteAddress_FullMethodName, req.GetUserId()); err != nil {
		return nil, err
	}
	if err := s.repository.DeleteAddress(ctx, req.GetUserId(), req.GetAddressId()); err != nil {
		return nil, mapError(err)
	}
	return &pb.IdentityAddressDeleteResponse{Deleted: true}, nil
}

func (s *Server) ListAddresses(ctx context.Context, req *pb.IdentityAddressListRequest) (*pb.IdentityAddressListResponse, error) {
	if req == nil || req.GetUserId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "用户不能为空")
	}
	if err := s.authorizeUser(ctx, pb.IdentityService_ListAddresses_FullMethodName, req.GetUserId()); err != nil {
		return nil, err
	}
	values, err := s.repository.ListAddresses(ctx, req.GetUserId())
	if err != nil {
		return nil, mapError(err)
	}
	result := &pb.IdentityAddressListResponse{Items: make([]*pb.IdentityAddress, 0, len(values))}
	for _, value := range values {
		result.Items = append(result.Items, addressProto(value))
	}
	return result, nil
}

func (s *Server) GetAddressSnapshot(ctx context.Context, req *pb.IdentityAddressSnapshotRequest) (*pb.IdentityAddressSnapshot, error) {
	if req == nil || req.GetUserId() == 0 || req.GetAddressId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "用户和地址不能为空")
	}
	if err := s.authorizeUser(ctx, pb.IdentityService_GetAddressSnapshot_FullMethodName, req.GetUserId()); err != nil {
		return nil, err
	}
	value, err := s.repository.GetAddress(ctx, req.GetUserId(), req.GetAddressId())
	if err != nil {
		return nil, mapError(err)
	}
	return &pb.IdentityAddressSnapshot{AddressId: value.ID, UserId: value.UserID, Recipient: value.Recipient, Phone: value.Phone, Province: value.Province, City: value.City, District: value.District, Detail: value.Detail}, nil
}

func (s *Server) issueTokenPair(ctx context.Context, user User) (*pb.IdentityTokenPair, error) {
	if s.auth == nil {
		return nil, status.Error(codes.Unavailable, "身份服务认证未就绪")
	}
	refresh, err := s.auth.IssueRefreshToken()
	if err != nil {
		return nil, status.Error(codes.Internal, "刷新令牌生成失败")
	}
	return s.issueTokenPairWithRefresh(ctx, user, refresh, true)
}

func (s *Server) issueTokenPairWithRefresh(ctx context.Context, user User, refresh platformauth.RefreshToken, store bool) (*pb.IdentityTokenPair, error) {
	access, accessExpires, err := s.auth.IssueAccessToken(user.ID, user.Role)
	if err != nil {
		return nil, status.Error(codes.Internal, "访问令牌生成失败")
	}
	if store {
		if err := s.repository.StoreRefreshToken(ctx, RefreshToken{UserID: user.ID, TokenHash: refresh.Hash, ExpiresAt: refresh.ExpiresAt, CreatedAt: s.now().UTC()}); err != nil {
			return nil, status.Error(codes.Internal, "刷新令牌保存失败")
		}
	}
	return &pb.IdentityTokenPair{AccessToken: access, AccessExpiresAtUnix: accessExpires.Unix(), RefreshToken: refresh.Raw, RefreshExpiresAtUnix: refresh.ExpiresAt.Unix(), User: &pb.IdentityUser{Id: user.ID, Email: user.Email, Role: user.Role, Status: user.Status}}, nil
}

func (s *Server) authorizeSystem(ctx context.Context, method string) error {
	return internalcall.AuthorizeRole(ctx, strings.TrimSpace(os.Getenv("SECKILL_INTERNAL_CALL_SECRET")), method, internalcall.SystemActorID, internalcall.SystemRole, s.now())
}
func (s *Server) authorizeUser(ctx context.Context, method string, userID uint64) error {
	return internalcall.AuthorizeUser(ctx, strings.TrimSpace(os.Getenv("SECKILL_INTERNAL_CALL_SECRET")), method, userID, s.now())
}

func normalizeCredentials(email, password string) (string, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	password = strings.TrimSpace(password)
	if email == "" || !strings.Contains(email, "@") || len(email) > 255 {
		return "", "", status.Error(codes.InvalidArgument, "邮箱格式无效")
	}
	if len(password) < 8 || len(password) > 72 {
		return "", "", status.Error(codes.InvalidArgument, "密码长度必须在 8 到 72 个字符之间")
	}
	return email, password, nil
}

func addressFromProto(userID uint64, value *pb.IdentityAddress) Address {
	return Address{ID: value.GetId(), UserID: userID, Recipient: strings.TrimSpace(value.GetRecipient()), Phone: strings.TrimSpace(value.GetPhone()), Province: strings.TrimSpace(value.GetProvince()), City: strings.TrimSpace(value.GetCity()), District: strings.TrimSpace(value.GetDistrict()), Detail: strings.TrimSpace(value.GetDetail()), IsDefault: value.GetIsDefault()}
}

func validateAddress(address Address) error {
	fields := []struct {
		name  string
		value string
		max   int
	}{
		{name: "收件人", value: address.Recipient, max: 64},
		{name: "手机号", value: address.Phone, max: 32},
		{name: "省份", value: address.Province, max: 64},
		{name: "城市", value: address.City, max: 64},
		{name: "区县", value: address.District, max: 64},
		{name: "详细地址", value: address.Detail, max: 255},
	}
	for _, field := range fields {
		if field.value == "" || len([]rune(field.value)) > field.max {
			return status.Error(codes.InvalidArgument, fmt.Sprintf("%s不能为空且长度不能超过 %d", field.name, field.max))
		}
	}
	return nil
}
func addressProto(value Address) *pb.IdentityAddress {
	return &pb.IdentityAddress{Id: value.ID, UserId: value.UserID, Recipient: value.Recipient, Phone: value.Phone, Province: value.Province, City: value.City, District: value.District, Detail: value.Detail, IsDefault: value.IsDefault}
}

func mapError(err error) error {
	switch {
	case errors.Is(err, ErrAddressNotFound), errors.Is(err, ErrUserNotFound):
		return status.Error(codes.NotFound, "资源不存在")
	case errors.Is(err, ErrEmailConflict):
		return status.Error(codes.AlreadyExists, "邮箱已注册")
	case errors.Is(err, ErrTokenInvalid):
		return status.Error(codes.Unauthenticated, "刷新令牌无效")
	default:
		return status.Error(codes.Internal, "身份服务暂时不可用")
	}
}
