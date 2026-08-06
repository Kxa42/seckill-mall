package messaging

import (
	"context"
	"testing"
	"time"

	"seckill-mall/common/contracts"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	event, err := contracts.NewEventEnvelope("evt-1", contracts.EventOrderCreated, "order", "ord-1", 1, map[string]any{"order_id": "ord-1"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	body, err := MarshalEnvelope(event)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := UnmarshalEnvelope(body)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.EventID != event.EventID || decoded.EventType != event.EventType {
		t.Fatalf("decoded event = %+v", decoded)
	}
}

func TestRetryDelayIsBounded(t *testing.T) {
	if got := RetryDelay(1, time.Second); got != time.Second {
		t.Fatalf("first retry delay = %s", got)
	}
	if got := RetryDelay(99, time.Second); got > 5*time.Minute {
		t.Fatalf("retry delay exceeded bound: %s", got)
	}
}

func TestFakeBrokerDispatch(t *testing.T) {
	broker := NewFakeBroker()
	event, err := contracts.NewEventEnvelope("evt-2", contracts.EventPaymentSucceeded, "order", "ord-2", 1, map[string]string{"order_id": "ord-2"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := broker.Publish(context.Background(), event, nil); err != nil {
		t.Fatal(err)
	}
	called := 0
	if err := broker.Dispatch(context.Background(), func(_ context.Context, got contracts.EventEnvelope) error {
		called++
		if got.EventID != event.EventID {
			t.Fatalf("event id = %s", got.EventID)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("handler called %d times", called)
	}
}

func TestMemoryOutboxAndInboxAreIdempotent(t *testing.T) {
	store := NewMemoryStore()
	event, err := contracts.NewEventEnvelope("evt-memory", contracts.EventPaymentSucceeded, "order", "ord-1", 1, contracts.PaymentSucceededPayload{
		PaymentNo: "pay-1", OrderID: "ord-1", UserID: 9, AmountCents: 100, CallbackRef: "cb-1",
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AppendEvent(context.Background(), event, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendEvent(context.Background(), event, nil); err != nil {
		t.Fatal(err)
	}
	if got := len(store.OutboxEvents()); got != 1 {
		t.Fatalf("duplicate event count = %d, want 1", got)
	}
	claim, err := store.Inbox().Claim(context.Background(), contracts.ServiceOrder, event, time.Second)
	if err != nil || !claim.Claimed {
		t.Fatalf("first inbox claim = %+v, %v", claim, err)
	}
	if err := store.Inbox().MarkProcessed(context.Background(), contracts.ServiceOrder, event.EventID); err != nil {
		t.Fatal(err)
	}
	claim, err = store.Inbox().Claim(context.Background(), contracts.ServiceOrder, event, time.Second)
	if err != nil || !claim.Processed {
		t.Fatalf("duplicate inbox claim = %+v, %v", claim, err)
	}
}

func TestDispatchWithInboxIsolatesFutureVersion(t *testing.T) {
	store := NewMemoryStore()
	event, err := contracts.NewEventEnvelope("evt-future", contracts.EventOrderCreated, "order", "ord-1", 2, contracts.OrderCreatedPayload{OrderID: "ord-1", UserID: 9, TotalAmountCents: 100, Items: []contracts.OrderItemPayload{{SKUID: 1, Quantity: 1}}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	called := false
	err = DispatchWithInbox(context.Background(), store.Inbox(), contracts.ServicePayment, event, map[string]Handler{contracts.EventOrderCreated: func(context.Context, contracts.EventEnvelope) error { called = true; return nil }})
	if err != nil || called {
		t.Fatalf("future event dispatch = err=%v called=%v", err, called)
	}
}
