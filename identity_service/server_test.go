package identityservice

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"seckill-mall/common/pb"
	platformauth "seckill-mall/internal/platform/auth"
)

func TestServerReturnsOnlyOwnedAddress(t *testing.T) {
	server, err := NewServer(NewMemoryRepository(Address{ID: 7, UserID: 9, Recipient: "测试用户", Phone: "13800000000", Detail: "测试地址"}))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	value, err := server.GetAddressSnapshot(context.Background(), &pb.IdentityAddressSnapshotRequest{UserId: 9, AddressId: 7})
	if err != nil || value.GetRecipient() != "测试用户" {
		t.Fatalf("GetAddressSnapshot() = %+v, err=%v", value, err)
	}
	_, err = server.GetAddressSnapshot(context.Background(), &pb.IdentityAddressSnapshotRequest{UserId: 10, AddressId: 7})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("foreign address code = %s, want %s", status.Code(err), codes.NotFound)
	}
}

func TestRegisterLoginAndRefreshTokenRotation(t *testing.T) {
	repository := NewMemoryRepository()
	manager, err := platformauth.NewManager(strings.Repeat("i", 32), time.Minute, time.Hour)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	server, err := NewServer(repository, manager)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	registered, err := server.Register(context.Background(), &pb.IdentityCredentialsRequest{Email: " User@Example.com ", Password: "correct-password"})
	if err != nil || registered.GetUser().GetId() == 0 || registered.GetRefreshToken() == "" {
		t.Fatalf("Register() = %+v, err=%v", registered, err)
	}
	if registered.GetUser().GetEmail() != "user@example.com" || registered.GetUser().GetRole() != RoleCustomer {
		t.Fatalf("registered user = %+v", registered.GetUser())
	}
	if _, err := server.Register(context.Background(), &pb.IdentityCredentialsRequest{Email: "user@example.com", Password: "correct-password"}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("duplicate register code = %s, want %s", status.Code(err), codes.AlreadyExists)
	}
	if _, err := server.Login(context.Background(), &pb.IdentityCredentialsRequest{Email: "user@example.com", Password: "wrong-password"}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("wrong password code = %s, want %s", status.Code(err), codes.Unauthenticated)
	}

	rotated, err := server.Refresh(context.Background(), &pb.IdentityRefreshRequest{RefreshToken: registered.GetRefreshToken()})
	if err != nil || rotated.GetRefreshToken() == "" || rotated.GetRefreshToken() == registered.GetRefreshToken() {
		t.Fatalf("Refresh() = %+v, err=%v", rotated, err)
	}
	if _, err := server.Refresh(context.Background(), &pb.IdentityRefreshRequest{RefreshToken: registered.GetRefreshToken()}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("reused old refresh code = %s, want %s", status.Code(err), codes.Unauthenticated)
	}
	if _, err := server.Refresh(context.Background(), &pb.IdentityRefreshRequest{RefreshToken: rotated.GetRefreshToken()}); err != nil {
		t.Fatalf("replacement refresh token cannot rotate: %v", err)
	}
}

func TestAddressValidationDefaultsAndOwnership(t *testing.T) {
	repository := NewMemoryRepository()
	server, err := NewServer(repository)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	invalid := &pb.IdentityAddress{Recipient: "用户", Phone: "13800000000", Detail: "地址"}
	if _, err := server.CreateAddress(context.Background(), &pb.IdentityAddressCreateRequest{UserId: 9, Address: invalid}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("invalid address code = %s, want %s", status.Code(err), codes.InvalidArgument)
	}
	create := func(detail string) *pb.IdentityAddress {
		value, createErr := server.CreateAddress(context.Background(), &pb.IdentityAddressCreateRequest{UserId: 9, Address: &pb.IdentityAddress{Recipient: "用户", Phone: "13800000000", Province: "浙江省", City: "杭州市", District: "西湖区", Detail: detail, IsDefault: true}})
		if createErr != nil {
			t.Fatalf("CreateAddress() error = %v", createErr)
		}
		return value
	}
	first := create("地址一")
	second := create("地址二")
	list, err := server.ListAddresses(context.Background(), &pb.IdentityAddressListRequest{UserId: 9})
	if err != nil || len(list.GetItems()) != 2 || list.GetItems()[0].GetId() != second.GetId() || !list.GetItems()[0].GetIsDefault() {
		t.Fatalf("ListAddresses() = %+v, err=%v", list, err)
	}
	if list.GetItems()[1].GetId() != first.GetId() || list.GetItems()[1].GetIsDefault() {
		t.Fatalf("first address default was not cleared: %+v", list.GetItems()[1])
	}
	if _, err := server.UpdateAddress(context.Background(), &pb.IdentityAddressUpdateRequest{UserId: 10, AddressId: first.GetId(), Address: first}); status.Code(err) != codes.NotFound {
		t.Fatalf("foreign update code = %s, want %s", status.Code(err), codes.NotFound)
	}
}
