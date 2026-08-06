package commerce

import "testing"

func TestOutboxEventIDFitsSchemaAndSeparatesEventTypes(t *testing.T) {
	aggregateID := "ord_0123456789abcdef0123456789abcdef"
	paymentID := outboxEventID("commerce.payment.succeeded.v1", aggregateID)
	refundID := outboxEventID("commerce.refund.succeeded.v1", aggregateID)

	if len(paymentID) != 64 {
		t.Fatalf("outboxEventID() length = %d, want 64", len(paymentID))
	}
	if paymentID == refundID {
		t.Fatal("different event types produced the same event ID")
	}
	if paymentID != outboxEventID("commerce.payment.succeeded.v1", aggregateID) {
		t.Fatal("outbox event ID must be deterministic for transaction retries")
	}
}
