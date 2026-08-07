package inventoryservice

import (
	"context"
	"encoding/json"
	"fmt"

	"seckill-mall/shared/contracts"
	"seckill-mall/shared/platform/messaging"
)

// EventHandler 只调用 Inventory 本地 Store，禁止写入 Order/Payment 数据。
type EventHandler struct{ store Store }

func NewEventHandler(store Store) (*EventHandler, error) {
	if store == nil {
		return nil, fmt.Errorf("inventory store is required")
	}
	return &EventHandler{store: store}, nil
}

func (h *EventHandler) Handlers() map[string]messaging.Handler {
	return map[string]messaging.Handler{contracts.EventOrderCancelled: h.orderCancelled, contracts.EventPaymentRefunded: h.paymentRefunded}
}

func (h *EventHandler) orderCancelled(ctx context.Context, event contracts.EventEnvelope) error {
	var payload contracts.OrderCancelledPayload
	if err := decodeInventoryPayload(event, &payload); err != nil {
		return err
	}
	for _, reservationID := range payload.ReservationIDs {
		if _, err := h.store.Release(ctx, reservationID, payload.OrderID); err != nil && err != ErrNotFound {
			return err
		}
	}
	return nil
}

func (h *EventHandler) paymentRefunded(ctx context.Context, event contracts.EventEnvelope) error {
	var payload contracts.PaymentRefundedPayload
	if err := decodeInventoryPayload(event, &payload); err != nil {
		return err
	}
	if lookup, ok := h.store.(interface {
		ReservationIDsByOrder(context.Context, string) ([]string, error)
	}); ok {
		ids, err := lookup.ReservationIDsByOrder(ctx, payload.OrderID)
		if err != nil {
			return err
		}
		for _, reservationID := range ids {
			if _, err := h.store.Restock(ctx, reservationID, payload.OrderID); err != nil && err != ErrNotFound {
				return err
			}
		}
	}
	return nil
}

func decodeInventoryPayload(event contracts.EventEnvelope, target any) error {
	if err := contracts.ValidateEventPayload(event); err != nil {
		return err
	}
	return json.Unmarshal(event.Payload, target)
}
