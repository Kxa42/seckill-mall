// Inventory/Seckill 测试覆盖库存状态机和不依赖 Redis 的 gRPC Fake E2E。
package inventoryservice

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"seckill-mall/common/pb"
)

func TestMemoryStoreReservationStateMachine(t *testing.T) {
	store := NewMemoryStore(map[uint64]int32{7: 2}, 1)
	command := ReserveCommand{ReservationID: "reserve-1", OrderID: "order-1", UserID: 9, SKUID: 7, Quantity: 1, Mode: "normal"}
	first, err := store.Reserve(context.Background(), command)
	if err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	repeated, err := store.Reserve(context.Background(), command)
	if err != nil || repeated.ReservationID != first.ReservationID {
		t.Fatalf("Reserve(repeated) reservation=%+v error=%v", repeated, err)
	}
	command.Quantity = 2
	if _, err := store.Reserve(context.Background(), command); err != ErrConflict {
		t.Fatalf("Reserve(mismatched retry) error = %v, want %v", err, ErrConflict)
	}
	if _, err := store.Reserve(context.Background(), ReserveCommand{ReservationID: "reserve-2", OrderID: "order-2", UserID: 10, SKUID: 7, Quantity: 2}); err != ErrOutOfStock {
		t.Fatalf("Reserve(out of stock) error = %v, want %v", err, ErrOutOfStock)
	}

	confirmed, err := store.Confirm(context.Background(), first.ReservationID, first.OrderID)
	if err != nil || confirmed.Status != ReservationConfirmed {
		t.Fatalf("Confirm() reservation=%+v error=%v", confirmed, err)
	}
	if _, err := store.Release(context.Background(), first.ReservationID, first.OrderID); err != ErrConflict {
		t.Fatalf("Release(confirmed) error = %v, want %v", err, ErrConflict)
	}
	restocked, err := store.Restock(context.Background(), first.ReservationID, first.OrderID)
	if err != nil || restocked.Status != ReservationRestocked {
		t.Fatalf("Restock() reservation=%+v error=%v", restocked, err)
	}
	repeatedRestock, err := store.Restock(context.Background(), first.ReservationID, first.OrderID)
	if err != nil || repeatedRestock.Status != ReservationRestocked {
		t.Fatalf("Restock(repeated) reservation=%+v error=%v", repeatedRestock, err)
	}

	second, err := store.Reserve(context.Background(), ReserveCommand{ReservationID: "reserve-3", OrderID: "order-3", UserID: 11, SKUID: 7, Quantity: 1})
	if err != nil {
		t.Fatalf("Reserve(second) error = %v", err)
	}
	if _, err := store.Release(context.Background(), second.ReservationID, second.OrderID); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if _, err := store.Release(context.Background(), second.ReservationID, second.OrderID); err != nil {
		t.Fatalf("Release(repeated) error = %v", err)
	}
	if available := store.AvailableStock(7); available != 2 {
		t.Fatalf("available stock after confirm/restock/release = %d, want 2", available)
	}
}

func TestMemoryStoreWritesStreamOutboxExactlyOnce(t *testing.T) {
	store := NewMemoryStore(map[uint64]int32{7: 2}, 1)
	stream := NewMemoryEventStream()
	store.SetEventStream(stream)
	command := ReserveCommand{ReservationID: "stream-reserve", OrderID: "stream-order", UserID: 9, SKUID: 7, Quantity: 1, Mode: "normal"}
	if _, err := store.Reserve(context.Background(), command); err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	if _, err := store.Reserve(context.Background(), command); err != nil {
		t.Fatalf("duplicate Reserve() error = %v", err)
	}
	if _, err := store.Confirm(context.Background(), command.ReservationID, command.OrderID); err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if _, err := store.Restock(context.Background(), command.ReservationID, command.OrderID); err != nil {
		t.Fatalf("Restock() error = %v", err)
	}
	events := stream.Events()
	if len(events) != 2 || events[0].Event.EventType != "inventory.reserved.v1" || events[1].Event.EventType != "inventory.restocked.v1" {
		t.Fatalf("unexpected stream events: %+v", events)
	}
}

func TestMemoryStoreRestockRollsBackSeckillPurchaseLimit(t *testing.T) {
	store := NewMemoryStore(map[uint64]int32{13: 1}, 1)
	reservation, err := store.AdmitSeckill(context.Background(), SeckillAdmissionCommand{RequestID: "refund-request", OrderID: "refund-order", ActivityID: 100, UserID: 9, SKUID: 13, Quantity: 1})
	if err != nil {
		t.Fatalf("AdmitSeckill() error = %v", err)
	}
	if _, err := store.Confirm(context.Background(), reservation.ReservationID, reservation.OrderID); err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if _, err := store.Restock(context.Background(), reservation.ReservationID, reservation.OrderID); err != nil {
		t.Fatalf("Restock() error = %v", err)
	}
	if store.AvailableStock(13) != 1 {
		t.Fatalf("available stock after restock = %d, want 1", store.AvailableStock(13))
	}
	if _, err := store.AdmitSeckill(context.Background(), SeckillAdmissionCommand{RequestID: "new-request", OrderID: "new-order", ActivityID: 100, UserID: 9, SKUID: 13, Quantity: 1}); err != nil {
		t.Fatalf("AdmitSeckill(after restock) error = %v", err)
	}
}

func TestMemoryStoreSeckillReleaseRollsBackPurchaseLimit(t *testing.T) {
	store := NewMemoryStore(map[uint64]int32{8: 2}, 1)
	command := SeckillAdmissionCommand{RequestID: "request-1", ActivityID: 100, UserID: 9, SKUID: 8, Quantity: 1}
	first, err := store.AdmitSeckill(context.Background(), command)
	if err != nil {
		t.Fatalf("AdmitSeckill() error = %v", err)
	}
	if _, err := store.AdmitSeckill(context.Background(), command); err != nil {
		t.Fatalf("AdmitSeckill(repeated) error = %v", err)
	}
	if _, err := store.AdmitSeckill(context.Background(), SeckillAdmissionCommand{RequestID: "request-2", ActivityID: 100, UserID: 9, SKUID: 8, Quantity: 1}); err != ErrPurchaseLimit {
		t.Fatalf("AdmitSeckill(limit) error = %v, want %v", err, ErrPurchaseLimit)
	}
	if _, err := store.Release(context.Background(), first.ReservationID, ""); err != nil {
		t.Fatalf("Release(seckill) error = %v", err)
	}
	if _, err := store.AdmitSeckill(context.Background(), SeckillAdmissionCommand{RequestID: "request-3", ActivityID: 100, UserID: 9, SKUID: 8, Quantity: 1}); err != nil {
		t.Fatalf("AdmitSeckill(after release) error = %v", err)
	}
	if _, err := store.AdmitSeckill(context.Background(), SeckillAdmissionCommand{RequestID: "request-4", ActivityID: 200, UserID: 9, SKUID: 8, Quantity: 1}); err != nil {
		t.Fatalf("AdmitSeckill(other activity) error = %v", err)
	}
}

func TestMemoryStoreSeckillBindsOrderDuringAdmission(t *testing.T) {
	store := NewMemoryStore(map[uint64]int32{12: 2}, 1)
	reservation, err := store.AdmitSeckill(context.Background(), SeckillAdmissionCommand{RequestID: "request-bind", OrderID: "order-bind", ActivityID: 100, UserID: 9, SKUID: 12, Quantity: 1})
	if err != nil {
		t.Fatalf("AdmitSeckill() error = %v", err)
	}
	if reservation.OrderID != "order-bind" {
		t.Fatalf("reservation order_id = %q, want order-bind", reservation.OrderID)
	}
	if _, err := store.Release(context.Background(), reservation.ReservationID, "another-order"); err != ErrConflict {
		t.Fatalf("Release(foreign order) error = %v, want %v", err, ErrConflict)
	}
}

func TestInventoryServerOverBufconn(t *testing.T) {
	server, err := NewServer(NewMemoryStore(map[uint64]int32{9: 2}, 1))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	pb.RegisterInventoryServiceServer(grpcServer, server)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = listener.Close()
	})
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.DialContext() error = %v", err)
	}
	defer conn.Close()
	client := pb.NewInventoryServiceClient(conn)

	reserved, err := client.Reserve(ctx, &pb.InventoryReserveRequest{ReservationId: "grpc-reserve", OrderId: "grpc-order", UserId: 1, SkuId: 9, Quantity: 1, Mode: "normal"})
	if err != nil || reserved.GetStatus() != ReservationReserved {
		t.Fatalf("Reserve() response=%+v error=%v", reserved, err)
	}
	released, err := client.Release(ctx, &pb.InventoryReservationRequest{ReservationId: "grpc-reserve", OrderId: "grpc-order"})
	if err != nil || released.GetStatus() != ReservationReleased {
		t.Fatalf("Release() response=%+v error=%v", released, err)
	}
	confirmed, err := client.Reserve(ctx, &pb.InventoryReserveRequest{ReservationId: "grpc-confirm", OrderId: "grpc-order-2", UserId: 1, SkuId: 9, Quantity: 1, Mode: "normal"})
	if err != nil {
		t.Fatalf("Reserve(confirm) error = %v", err)
	}
	if _, err := client.Confirm(ctx, &pb.InventoryReservationRequest{ReservationId: confirmed.GetReservationId(), OrderId: "grpc-order-2"}); err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	restocked, err := client.Restock(ctx, &pb.InventoryReservationRequest{ReservationId: confirmed.GetReservationId(), OrderId: "grpc-order-2"})
	if err != nil || restocked.GetStatus() != ReservationRestocked {
		t.Fatalf("Restock() response=%+v error=%v", restocked, err)
	}
	_, err = client.Reserve(ctx, &pb.InventoryReserveRequest{ReservationId: "bad/id", OrderId: "order", UserId: 1, SkuId: 9, Quantity: 1})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Reserve(invalid id) code = %s, want %s", status.Code(err), codes.InvalidArgument)
	}
}
