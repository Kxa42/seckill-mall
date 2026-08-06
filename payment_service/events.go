package paymentservice

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"seckill-mall/common/contracts"
	"seckill-mall/common/messaging"
)

// EventHandler 只维护 Payment 本地对订单事实的观察，不跨库修改 Order。
type EventHandler struct {
	mu      sync.RWMutex
	service *Service
	pending map[string]contracts.OrderCreatedPayload
}

func NewEventHandler(service *Service) (*EventHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("payment service is required")
	}
	return &EventHandler{service: service, pending: make(map[string]contracts.OrderCreatedPayload)}, nil
}

func (h *EventHandler) Handlers() map[string]messaging.Handler {
	return map[string]messaging.Handler{
		contracts.EventOrderCreated:   h.orderCreated,
		contracts.EventOrderCancelled: h.orderCancelled,
	}
}

func (h *EventHandler) orderCreated(_ context.Context, event contracts.EventEnvelope) error {
	var payload contracts.OrderCreatedPayload
	if err := decodePaymentPayload(event, &payload); err != nil {
		return err
	}
	h.mu.Lock()
	h.pending[payload.OrderID] = payload
	h.mu.Unlock()
	return nil
}

func (h *EventHandler) orderCancelled(_ context.Context, event contracts.EventEnvelope) error {
	var payload contracts.OrderCancelledPayload
	if err := decodePaymentPayload(event, &payload); err != nil {
		return err
	}
	h.mu.Lock()
	delete(h.pending, payload.OrderID)
	h.mu.Unlock()
	return nil
}

func (h *EventHandler) PendingOrder(orderID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.pending[orderID]
	return ok
}

func decodePaymentPayload(event contracts.EventEnvelope, target any) error {
	if err := contracts.ValidateEventPayload(event); err != nil {
		return err
	}
	return json.Unmarshal(event.Payload, target)
}
