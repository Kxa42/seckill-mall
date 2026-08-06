package order

import (
	"context"
	"encoding/json"
	"fmt"

	"seckill-mall/common/contracts"
	"seckill-mall/common/messaging"
)

// EventHandler 是 Order Service 的统一事件入口，实际幂等由调用方 Inbox 保证。
type EventHandler struct {
	service *Service
}

func NewEventHandler(service *Service) (*EventHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("order service is required")
	}
	return &EventHandler{service: service}, nil
}

func (h *EventHandler) Handlers() map[string]messaging.Handler {
	return map[string]messaging.Handler{
		contracts.EventPaymentSucceeded:  h.paymentSucceeded,
		contracts.EventPaymentRefunded:   h.paymentRefunded,
		contracts.EventShipmentCreated:   h.shipmentCreated,
		contracts.EventShipmentDelivered: h.shipmentDelivered,
		contracts.EventInventoryReserved: func(context.Context, contracts.EventEnvelope) error { return nil },
		contracts.EventInventoryReleased: func(context.Context, contracts.EventEnvelope) error { return nil },
		contracts.EventSeckillAccepted:   func(context.Context, contracts.EventEnvelope) error { return nil },
	}
}

func (h *EventHandler) paymentSucceeded(ctx context.Context, event contracts.EventEnvelope) error {
	var payload contracts.PaymentSucceededPayload
	if err := decodePayload(event, &payload); err != nil {
		return err
	}
	_, _, err := h.service.ConfirmPayment(ctx, payload.UserID, payload.OrderID, payload.PaymentNo, payload.CallbackRef)
	return err
}

func (h *EventHandler) paymentRefunded(ctx context.Context, event contracts.EventEnvelope) error {
	var payload contracts.PaymentRefundedPayload
	if err := decodePayload(event, &payload); err != nil {
		return err
	}
	_, err := h.service.Refund(ctx, payload.UserID, payload.OrderID, payload.RefundNo, payload.Reason)
	return err
}

func (h *EventHandler) shipmentCreated(ctx context.Context, event contracts.EventEnvelope) error {
	var payload contracts.ShipmentPayload
	if err := decodePayload(event, &payload); err != nil {
		return err
	}
	return h.service.ApplyShipmentCreated(ctx, payload.OrderID, payload.Carrier, payload.TrackingNo)
}

func (h *EventHandler) shipmentDelivered(ctx context.Context, event contracts.EventEnvelope) error {
	var payload contracts.ShipmentPayload
	if err := decodePayload(event, &payload); err != nil {
		return err
	}
	return h.service.ApplyShipmentDelivered(ctx, payload.OrderID)
}

func decodePayload(event contracts.EventEnvelope, target any) error {
	if err := contracts.ValidateEventPayload(event); err != nil {
		return err
	}
	return json.Unmarshal(event.Payload, target)
}
