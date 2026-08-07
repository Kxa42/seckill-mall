package fulfillmentservice

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"seckill-mall/shared/contracts"
	"seckill-mall/shared/platform/messaging"
)

// EventHandler 记录已支付待履约事实，不伪造承运商或运单号。
type EventHandler struct {
	mu      sync.RWMutex
	service *Service
	pending map[string]contracts.PaymentSucceededPayload
}

func NewEventHandler(service *Service) (*EventHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("fulfillment service is required")
	}
	return &EventHandler{service: service, pending: make(map[string]contracts.PaymentSucceededPayload)}, nil
}

func (h *EventHandler) Handlers() map[string]messaging.Handler {
	return map[string]messaging.Handler{contracts.EventPaymentSucceeded: h.paymentSucceeded}
}

func (h *EventHandler) paymentSucceeded(_ context.Context, event contracts.EventEnvelope) error {
	var payload contracts.PaymentSucceededPayload
	if err := contracts.ValidateEventPayload(event); err != nil {
		return err
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return err
	}
	h.mu.Lock()
	h.pending[payload.OrderID] = payload
	h.mu.Unlock()
	return nil
}

func (h *EventHandler) PendingOrder(orderID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.pending[orderID]
	return ok
}
