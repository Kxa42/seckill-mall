package commerce

import (
	"math"
	"testing"
)

func TestOrderTransitions(t *testing.T) {
	valid := [][2]string{
		{OrderStatusPendingPayment, OrderStatusPaid},
		{OrderStatusPendingPayment, OrderStatusCanceled},
		{OrderStatusPaid, OrderStatusShipped},
		{OrderStatusPaid, OrderStatusRefundPending},
		{OrderStatusShipped, OrderStatusCompleted},
		{OrderStatusRefundPending, OrderStatusRefunded},
	}
	for _, transition := range valid {
		if !CanTransitionOrder(transition[0], transition[1]) {
			t.Errorf("expected valid transition %s -> %s", transition[0], transition[1])
		}
	}
	if CanTransitionOrder(OrderStatusCompleted, OrderStatusPaid) {
		t.Fatal("completed order must be terminal")
	}
}

func TestCalculateSubtotal(t *testing.T) {
	got, err := CalculateSubtotal(699900, 2)
	if err != nil || got != 1399800 {
		t.Fatalf("CalculateSubtotal() = %d, %v", got, err)
	}
	if _, err := CalculateSubtotal(math.MaxInt64, 2); err == nil {
		t.Fatal("CalculateSubtotal() expected overflow error")
	}
}

func TestAddAmountRejectsOverflow(t *testing.T) {
	if _, err := addAmount(math.MaxInt64, 1); err == nil {
		t.Fatal("addAmount() expected overflow error")
	}
	if got, err := addAmount(100, 25); err != nil || got != 125 {
		t.Fatalf("addAmount() = %d, %v", got, err)
	}
}

func TestNormalizeEmail(t *testing.T) {
	got, err := NormalizeEmail(" USER@example.com ")
	if err != nil || got != "user@example.com" {
		t.Fatalf("NormalizeEmail() = %q, %v", got, err)
	}
}
