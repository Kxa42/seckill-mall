package paymentservice

import (
	"context"
	"encoding/json"
	"fmt"

	"seckill-mall/shared/contracts"
	"seckill-mall/shared/platform/messaging"
)

// EventHandler 消费订单事实事件并校验 payload，不跨库修改 Order。
type EventHandler struct {
	service *Service
}

func NewEventHandler(service *Service) (*EventHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("payment service is required")
	}
	return &EventHandler{service: service}, nil
}

func (h *EventHandler) Handlers() map[string]messaging.Handler {
	return map[string]messaging.Handler{
		contracts.EventOrderCreated:   h.orderCreated,
		contracts.EventOrderCancelled: h.orderCancelled,
	}
}

func (h *EventHandler) orderCreated(_ context.Context, event contracts.EventEnvelope) error {
	var payload contracts.OrderCreatedPayload
	return decodePaymentPayload(event, &payload)
}

func (h *EventHandler) orderCancelled(_ context.Context, event contracts.EventEnvelope) error {
	var payload contracts.OrderCancelledPayload
	return decodePaymentPayload(event, &payload)
}

func decodePaymentPayload(event contracts.EventEnvelope, target any) error {
	if err := contracts.ValidateEventPayload(event); err != nil {
		return err
	}
	return json.Unmarshal(event.Payload, target)
}
