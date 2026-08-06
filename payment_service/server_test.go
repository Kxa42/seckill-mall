package paymentservice

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"seckill-mall/common/orderclient"
)

type fakeOrders struct {
	summaries                       map[string]orderclient.Summary
	confirmCalls, refundCalls       int
	confirmTransition, refundResult orderclient.Transition
	confirmErr                      error
}

func (f *fakeOrders) Get(_ context.Context, userID uint64, orderID string) (orderclient.Summary, error) {
	value, ok := f.summaries[orderID]
	if !ok || value.UserID != userID {
		return orderclient.Summary{}, status.Error(codes.NotFound, "订单不存在")
	}
	return value, nil
}
func (f *fakeOrders) ConfirmPayment(_ context.Context, _ uint64, _, _, _ string) (orderclient.Transition, error) {
	f.confirmCalls++
	if f.confirmErr != nil {
		err := f.confirmErr
		f.confirmErr = nil
		return orderclient.Transition{}, err
	}
	return f.confirmTransition, nil
}
func (f *fakeOrders) Refund(_ context.Context, _ uint64, _, _, _ string) (orderclient.Transition, error) {
	f.refundCalls++
	return f.refundResult, nil
}
func (f *fakeOrders) Ship(context.Context, uint64, string, string, string) (orderclient.Transition, error) {
	return orderclient.Transition{}, errors.New("unexpected Ship call")
}
func (f *fakeOrders) ConfirmReceipt(context.Context, uint64, string) (orderclient.Transition, error) {
	return orderclient.Transition{}, errors.New("unexpected ConfirmReceipt call")
}

func newTestService(t *testing.T, orders *fakeOrders) *Service {
	t.Helper()
	service, err := NewService(NewMemoryRepository(), orders, "payment-test-secret-value-32-bytes")
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	var sequence int
	service.newID = func(prefix string) (string, error) {
		sequence++
		return prefix + "-" + string(rune('0'+sequence)), nil
	}
	return service
}

func TestCreateAndCallbackIdempotency(t *testing.T) {
	orders := &fakeOrders{summaries: map[string]orderclient.Summary{"order-1": {OrderID: "order-1", UserID: 9, Status: "pending_payment", TotalAmountCents: 1200}}, confirmTransition: orderclient.Transition{OrderID: "order-1", Status: "paid"}}
	service := newTestService(t, orders)
	first, signature, reused, err := service.Create(context.Background(), 9, "order-1")
	if err != nil || reused || first.AmountCents != 1200 || signature == "" {
		t.Fatalf("first Create() = %+v signature=%q reused=%v err=%v", first, signature, reused, err)
	}
	second, secondSignature, reused, err := service.Create(context.Background(), 9, "order-1")
	if err != nil || !reused || second.PaymentNo != first.PaymentNo || secondSignature != signature {
		t.Fatalf("duplicate Create() = %+v signature=%q reused=%v err=%v", second, secondSignature, reused, err)
	}
	if _, _, _, err := service.Callback(context.Background(), first.PaymentNo, "callback-1", "bad-signature"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("invalid callback signature error = %v", err)
	}
	paid, _, reused, err := service.Callback(context.Background(), first.PaymentNo, "callback-1", signature)
	if err != nil || reused || paid.Status != StatusSucceeded || orders.confirmCalls != 1 {
		t.Fatalf("Callback() = %+v reused=%v calls=%d err=%v", paid, reused, orders.confirmCalls, err)
	}
	_, _, reused, err = service.Callback(context.Background(), first.PaymentNo, "callback-1", signature)
	if err != nil || !reused || orders.confirmCalls != 2 {
		t.Fatalf("duplicate Callback() reused=%v calls=%d err=%v", reused, orders.confirmCalls, err)
	}
	if _, _, _, err := service.Callback(context.Background(), first.PaymentNo, "callback-2", signature); !errors.Is(err, ErrConflict) {
		t.Fatalf("callback ref conflict error = %v", err)
	}
	orders.summaries["order-2"] = orderclient.Summary{OrderID: "order-2", UserID: 9, Status: "pending_payment", TotalAmountCents: 300}
	secondOrderPayment, secondOrderSignature, _, err := service.Create(context.Background(), 9, "order-2")
	if err != nil {
		t.Fatalf("second order Create() error = %v", err)
	}
	if _, _, _, err := service.Callback(context.Background(), secondOrderPayment.PaymentNo, "callback-1", secondOrderSignature); !errors.Is(err, ErrConflict) {
		t.Fatalf("cross-payment callback ref conflict error = %v", err)
	}
	if orders.confirmCalls != 2 {
		t.Fatalf("conflicting callback changed remote order, calls=%d", orders.confirmCalls)
	}
}

func TestCallbackRetryConvergesAfterRemoteFailure(t *testing.T) {
	orders := &fakeOrders{
		summaries:         map[string]orderclient.Summary{"order-1": {OrderID: "order-1", UserID: 9, Status: "pending_payment", TotalAmountCents: 1200}},
		confirmTransition: orderclient.Transition{OrderID: "order-1", Status: "paid", Reused: true},
		confirmErr:        status.Error(codes.Unavailable, "temporary failure"),
	}
	service := newTestService(t, orders)
	payment, signature, _, err := service.Create(context.Background(), 9, "order-1")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, _, _, err := service.Callback(context.Background(), payment.PaymentNo, "callback-1", signature); status.Code(err) != codes.Unavailable {
		t.Fatalf("first Callback() code = %s, want %s", status.Code(err), codes.Unavailable)
	}
	recovered, transition, reused, err := service.Callback(context.Background(), payment.PaymentNo, "callback-1", signature)
	if err != nil || !reused || recovered.Status != StatusSucceeded || transition.Status != "paid" || orders.confirmCalls != 2 {
		t.Fatalf("recovered Callback()=%+v transition=%+v reused=%v calls=%d err=%v", recovered, transition, reused, orders.confirmCalls, err)
	}
}

func TestRefundOwnershipAndRecoveryAfterRemoteCompletion(t *testing.T) {
	orders := &fakeOrders{summaries: map[string]orderclient.Summary{
		"order-1": {OrderID: "order-1", UserID: 9, Status: "refunded", TotalAmountCents: 1200},
	}, confirmTransition: orderclient.Transition{OrderID: "order-1", Status: "paid"}, refundResult: orderclient.Transition{OrderID: "order-1", Status: "refunded", Reused: true}}
	service := newTestService(t, orders)
	payment, signature, _, err := service.Create(context.Background(), 9, "order-1")
	if !errors.Is(err, ErrInvalidTransition) {
		// 先模拟订单支付前状态创建支付单，再恢复为已退款状态。
		t.Fatalf("Create() with refunded order error = %v", err)
	}
	orders.summaries["order-1"] = orderclient.Summary{OrderID: "order-1", UserID: 9, Status: "pending_payment", TotalAmountCents: 1200}
	payment, signature, _, err = service.Create(context.Background(), 9, "order-1")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, _, _, err := service.Callback(context.Background(), payment.PaymentNo, "callback-1", signature); err != nil {
		t.Fatalf("Callback() error = %v", err)
	}
	orders.summaries["order-1"] = orderclient.Summary{OrderID: "order-1", UserID: 9, Status: "refunded", TotalAmountCents: 1200}
	if _, _, _, err := service.Refund(context.Background(), 10, "order-1", "越权退款"); status.Code(err) != codes.NotFound {
		t.Fatalf("foreign Refund() code = %s, want %s", status.Code(err), codes.NotFound)
	}
	refund, transition, reused, err := service.Refund(context.Background(), 9, "order-1", "用户申请退款")
	if err != nil || reused || refund.Status != RefundSucceeded || transition.Status != "refunded" || orders.refundCalls != 1 {
		t.Fatalf("recovery Refund() = %+v transition=%+v reused=%v calls=%d err=%v", refund, transition, reused, orders.refundCalls, err)
	}
	_, _, reused, err = service.Refund(context.Background(), 9, "order-1", "用户申请退款")
	if err != nil || !reused || orders.refundCalls != 1 {
		t.Fatalf("duplicate Refund() reused=%v calls=%d err=%v", reused, orders.refundCalls, err)
	}
}
