package fulfillmentservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"seckill-mall/common/internalcall"
	"seckill-mall/common/orderclient"
	"seckill-mall/common/pb"
)

type fakeOrders struct {
	summary                 orderclient.Summary
	shipCalls, receiptCalls int
}

func TestShipmentConflictAndUserOwnership(t *testing.T) {
	orders := &fakeOrders{summary: orderclient.Summary{OrderID: "order-1", UserID: 9, Status: "paid"}}
	service, _ := NewService(NewMemoryRepository(), orders)
	if _, _, _, err := service.Ship(context.Background(), 1, "order-1", "SF", "SF-1"); err != nil {
		t.Fatalf("Ship() error = %v", err)
	}
	if _, _, _, err := service.Ship(context.Background(), 1, "order-1", "YT", "YT-2"); !errors.Is(err, ErrConflict) {
		t.Fatalf("different waybill error = %v, want conflict", err)
	}
	if _, _, _, err := service.Ship(context.Background(), 1, "order-2", "SF", "SF-1"); !errors.Is(err, ErrConflict) {
		t.Fatalf("reused tracking number error = %v, want conflict", err)
	}
	if orders.shipCalls != 1 {
		t.Fatalf("tracking conflict changed remote order, calls=%d", orders.shipCalls)
	}
	if _, err := service.Get(context.Background(), 10, "order-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign shipment error = %v, want not found", err)
	}
}

func TestFulfillmentServerRejectsForgedAdminRole(t *testing.T) {
	secret := "fulfillment-internal-secret"
	t.Setenv("SECKILL_INTERNAL_CALL_SECRET", secret)
	orders := &fakeOrders{summary: orderclient.Summary{OrderID: "order-1", UserID: 9, Status: "paid"}}
	service, _ := NewService(NewMemoryRepository(), orders)
	server, _ := NewServer(service)
	now := time.Now()
	request := &pb.FulfillmentShipRequest{ActorId: 7, OrderId: "order-1", Carrier: "SF", TrackingNo: "SF-1"}
	forged := incomingMetadata(internalcall.AppendRole(context.Background(), secret, pb.FulfillmentService_Ship_FullMethodName, 7, "customer", now))
	if _, err := server.Ship(forged, request); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("forged role code = %s, want %s", status.Code(err), codes.PermissionDenied)
	}
	admin := incomingMetadata(internalcall.AppendRole(context.Background(), secret, pb.FulfillmentService_Ship_FullMethodName, 7, "admin", now))
	if _, err := server.Ship(admin, request); err != nil {
		t.Fatalf("signed admin Ship() error = %v", err)
	}
}

func incomingMetadata(ctx context.Context) context.Context {
	values, _ := metadata.FromOutgoingContext(ctx)
	return metadata.NewIncomingContext(context.Background(), values)
}

func (f *fakeOrders) Get(_ context.Context, userID uint64, orderID string) (orderclient.Summary, error) {
	value := f.summary
	if value.UserID != userID || value.OrderID != orderID {
		return orderclient.Summary{}, ErrNotFound
	}
	return value, nil
}
func (f *fakeOrders) ConfirmPayment(context.Context, uint64, string, string, string) (orderclient.Transition, error) {
	return orderclient.Transition{}, nil
}
func (f *fakeOrders) Refund(context.Context, uint64, string, string, string) (orderclient.Transition, error) {
	return orderclient.Transition{}, nil
}
func (f *fakeOrders) Ship(_ context.Context, _ uint64, orderID, _, _ string) (orderclient.Transition, error) {
	f.shipCalls++
	return orderclient.Transition{OrderID: orderID, Status: "shipped", Reused: f.shipCalls > 1}, nil
}
func (f *fakeOrders) ConfirmReceipt(_ context.Context, _ uint64, orderID string) (orderclient.Transition, error) {
	f.receiptCalls++
	return orderclient.Transition{OrderID: orderID, Status: "completed", Reused: f.receiptCalls > 1}, nil
}

func TestShipAndReceiptAreIdempotent(t *testing.T) {
	orders := &fakeOrders{summary: orderclient.Summary{OrderID: "order-1", UserID: 9, Status: "paid"}}
	service, _ := NewService(NewMemoryRepository(), orders)
	first, _, reused, err := service.Ship(context.Background(), 1, "order-1", "SF", "SF-1")
	if err != nil || reused {
		t.Fatalf("first=%+v reused=%v err=%v", first, reused, err)
	}
	_, _, reused, err = service.Ship(context.Background(), 1, "order-1", "SF", "SF-1")
	if err != nil || !reused {
		t.Fatalf("duplicate reused=%v err=%v", reused, err)
	}
	received, _, reused, err := service.ConfirmReceipt(context.Background(), 9, "order-1")
	if err != nil || reused || received.Status != StatusReceived {
		t.Fatalf("received=%+v reused=%v err=%v", received, reused, err)
	}
	_, _, reused, err = service.ConfirmReceipt(context.Background(), 9, "order-1")
	if err != nil || !reused {
		t.Fatalf("duplicate receipt reused=%v err=%v", reused, err)
	}
}
