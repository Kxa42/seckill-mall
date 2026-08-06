package identityservice

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"seckill-mall/common/pb"
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
