package fulfillmentservice

import (
	"context"
	"encoding/json"
	"fmt"

	"seckill-mall/shared/contracts"
	"seckill-mall/shared/platform/messaging"
)

// EventHandler 消费支付事实事件并校验 payload，不伪造承运商或运单号。
type EventHandler struct {
	service *Service
}

func NewEventHandler(service *Service) (*EventHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("fulfillment service is required")
	}
	return &EventHandler{service: service}, nil
}

func (h *EventHandler) Handlers() map[string]messaging.Handler {
	return map[string]messaging.Handler{contracts.EventPaymentSucceeded: h.paymentSucceeded}
}

func (h *EventHandler) paymentSucceeded(_ context.Context, event contracts.EventEnvelope) error {
	var payload contracts.PaymentSucceededPayload
	if err := contracts.ValidateEventPayload(event); err != nil {
		return err
	}
	return json.Unmarshal(event.Payload, &payload)
}
