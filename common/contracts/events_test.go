package contracts

import (
	"testing"
	"time"
)

func TestNewEventEnvelope(t *testing.T) {
	event, err := NewEventEnvelope(
		"evt-1",
		EventOrderCreated,
		"order",
		"ord-1",
		1,
		map[string]any{"order_id": "ord-1"},
		time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("NewEventEnvelope() error = %v", err)
	}
	if !IsKnownEventType(event.EventType) {
		t.Fatalf("event type %q should be known", event.EventType)
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestEventEnvelopeAcceptsUnknownEventTypeForForwardCompatibility(t *testing.T) {
	event, err := NewEventEnvelope("evt-2", "future.event.v2", "future", "id-1", 99, map[string]string{"ok": "true"}, time.Now())
	if err != nil {
		t.Fatalf("unknown event type should remain structurally valid: %v", err)
	}
	if IsKnownEventType(event.EventType) {
		t.Fatal("future event type should not be reported as known")
	}
}

func TestEventEnvelopeAcceptsFutureVersionForKnownEventType(t *testing.T) {
	event, err := NewEventEnvelope("evt-future", EventOrderCreated, "order", "ord-1", 99, map[string]string{"order_id": "ord-1"}, time.Now())
	if err != nil {
		t.Fatalf("known event with a future payload version should remain structurally valid: %v", err)
	}
	if !IsKnownEventType(event.EventType) {
		t.Fatalf("event type %q should remain known", event.EventType)
	}
}

func TestEventTypeAndPayloadVersionMayEvolveIndependently(t *testing.T) {
	event, err := NewEventEnvelope("evt-payload-v2", EventOrderCreated, "order", "ord-1", 2, map[string]any{
		"order_id":  "ord-1",
		"new_field": true,
	}, time.Now())
	if err != nil {
		t.Fatalf("event type v1 with payload version 2 should be accepted: %v", err)
	}
	if event.EventType != EventOrderCreated || event.EventVersion != 2 {
		t.Fatalf("unexpected event version contract: type=%q version=%d", event.EventType, event.EventVersion)
	}
}

func TestEventEnvelopeRejectsMissingTransportFields(t *testing.T) {
	tests := []EventEnvelope{
		{EventType: EventOrderCreated},
		{EventID: "evt", EventType: EventOrderCreated, EventVersion: 0, AggregateType: "order", AggregateID: "ord", OccurredAt: time.Now(), Payload: []byte(`{}`)},
		{EventID: "evt", EventType: EventOrderCreated, EventVersion: 1, AggregateType: "order", AggregateID: "ord", OccurredAt: time.Now()},
	}
	for i, event := range tests {
		if err := event.Validate(); err == nil {
			t.Fatalf("case %d should fail validation", i)
		}
	}
}

func TestServiceBoundariesHaveExclusiveDataOwnership(t *testing.T) {
	if err := ValidateServiceBoundaries(); err != nil {
		t.Fatalf("ValidateServiceBoundaries() error = %v", err)
	}

	boundaries := ServiceBoundaries()
	if len(boundaries) != 7 {
		t.Fatalf("expected 7 business service boundaries, got %d", len(boundaries))
	}

	order, ok := ServiceBoundaryFor(ServiceOrder)
	if !ok {
		t.Fatal("order service boundary should exist")
	}
	if len(order.SynchronousDependencies) != 3 {
		t.Fatalf("order service should have 3 synchronous dependencies, got %d", len(order.SynchronousDependencies))
	}

	order.OwnedData[0] = "mutated"
	original, _ := ServiceBoundaryFor(ServiceOrder)
	if original.OwnedData[0] == "mutated" {
		t.Fatal("service boundary result must not mutate the shared catalog")
	}
}

func TestOrderCancelledEventUsesCanonicalSpelling(t *testing.T) {
	if EventOrderCancelled != "order.cancelled.v1" {
		t.Fatalf("unexpected canonical cancellation event: %q", EventOrderCancelled)
	}
	if EventOrderCanceled != EventOrderCancelled {
		t.Fatal("legacy spelling alias should resolve to the canonical event")
	}
}

func TestInboxKeyIsStableForDuplicateEvents(t *testing.T) {
	occurredAt := time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC)
	first, err := NewEventEnvelope("evt-duplicate", EventPaymentSucceeded, "order", "ord-1", 1, map[string]string{"order_id": "ord-1"}, occurredAt)
	if err != nil {
		t.Fatalf("first envelope error = %v", err)
	}
	duplicate, err := NewEventEnvelope("evt-duplicate", EventPaymentSucceeded, "order", "ord-1", 1, map[string]string{"order_id": "ord-1"}, occurredAt.Add(time.Second))
	if err != nil {
		t.Fatalf("duplicate envelope error = %v", err)
	}

	firstKey, err := first.InboxKey(ServiceOrder)
	if err != nil {
		t.Fatalf("first InboxKey() error = %v", err)
	}
	duplicateKey, err := duplicate.InboxKey(ServiceOrder)
	if err != nil {
		t.Fatalf("duplicate InboxKey() error = %v", err)
	}
	if firstKey != duplicateKey {
		t.Fatalf("duplicate events should have the same inbox key: %q != %q", firstKey, duplicateKey)
	}
}
