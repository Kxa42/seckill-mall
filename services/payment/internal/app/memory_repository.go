package paymentservice

import (
	"context"
	"fmt"
	"seckill-mall/shared/contracts"
	"seckill-mall/shared/platform/messaging"
	"sync"
	"time"
)

type MemoryRepository struct {
	mu             sync.RWMutex
	next           uint64
	payments       map[string]Payment
	paymentByOrder map[string]string
	callbacks      map[string]string
	refunds        map[string]Refund
	refundByOrder  map[string]string
	eventSink      messaging.EventSink
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{next: 1, payments: make(map[string]Payment), paymentByOrder: make(map[string]string), callbacks: make(map[string]string), refunds: make(map[string]Refund), refundByOrder: make(map[string]string)}
}

// SetEventSink 注入 Payment 自有 Outbox。
func (r *MemoryRepository) SetEventSink(sink messaging.EventSink) { r.eventSink = sink }

func (r *MemoryRepository) Create(_ context.Context, value Payment, _ time.Time) (Payment, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if no, ok := r.paymentByOrder[value.OrderID]; ok {
		return r.payments[no], true, nil
	}
	r.payments[value.PaymentNo] = value
	r.paymentByOrder[value.OrderID] = value.PaymentNo
	return value, false, nil
}

func (r *MemoryRepository) Get(_ context.Context, paymentNo string) (Payment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.payments[paymentNo]
	if !ok {
		return Payment{}, ErrNotFound
	}
	return value, nil
}

func (r *MemoryRepository) MarkSucceeded(ctx context.Context, paymentNo, callbackRef string, now time.Time) (Payment, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.payments[paymentNo]
	if !ok {
		return Payment{}, false, ErrNotFound
	}
	if existing, used := r.callbacks[callbackRef]; used && existing != paymentNo {
		return Payment{}, false, ErrConflict
	}
	if value.Status == StatusSucceeded {
		if value.CallbackRef != callbackRef {
			return Payment{}, false, ErrConflict
		}
		return value, true, nil
	}
	if r.eventSink != nil {
		event, err := paymentSucceededEvent(value, callbackRef, now)
		if err != nil {
			return Payment{}, false, err
		}
		if err := r.eventSink.AppendEvent(ctx, event, nil); err != nil {
			return Payment{}, false, err
		}
	}
	value.Status, value.CallbackRef, value.PaidAt, value.UpdatedAt = StatusSucceeded, callbackRef, now, now
	r.payments[paymentNo] = value
	r.callbacks[callbackRef] = paymentNo
	return value, false, nil
}

func (r *MemoryRepository) FindSucceededPayment(_ context.Context, orderID string) (Payment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	no, ok := r.paymentByOrder[orderID]
	if !ok || r.payments[no].Status != StatusSucceeded {
		return Payment{}, ErrInvalidTransition
	}
	return r.payments[no], nil
}

func (r *MemoryRepository) FindRefundByOrder(_ context.Context, userID uint64, orderID string) (Refund, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	no, ok := r.refundByOrder[orderID]
	if !ok {
		return Refund{}, false, nil
	}
	value := r.refunds[no]
	if value.UserID != userID {
		return Refund{}, false, ErrNotFound
	}
	return value, true, nil
}

func (r *MemoryRepository) CreateRefund(ctx context.Context, value Refund, _ time.Time) (Refund, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if no, ok := r.refundByOrder[value.OrderID]; ok {
		return r.refunds[no], true, nil
	}
	if r.eventSink != nil {
		event, err := messaging.NewEvent(fmt.Sprintf("payment.refunded:%s", value.RefundNo), contracts.EventPaymentRefunded, "order", value.OrderID, contracts.PaymentRefundedPayload{RefundNo: value.RefundNo, PaymentNo: value.PaymentNo, OrderID: value.OrderID, UserID: value.UserID, AmountCents: value.AmountCents, Reason: value.Reason}, value.CreatedAt)
		if err != nil {
			return Refund{}, false, err
		}
		if err := r.eventSink.AppendEvent(ctx, event, nil); err != nil {
			return Refund{}, false, err
		}
	}
	r.refunds[value.RefundNo] = value
	r.refundByOrder[value.OrderID] = value.RefundNo
	return value, false, nil
}

var _ Repository = (*MemoryRepository)(nil)
