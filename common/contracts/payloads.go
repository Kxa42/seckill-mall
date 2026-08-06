package contracts

import (
	"encoding/json"
	"errors"
	"fmt"
)

// 事件 payload 只包含跨服务事实，不复用任一服务的数据库模型。
type SeckillAcceptedPayload struct {
	OrderID       string `json:"order_id"`
	ReservationID string `json:"reservation_id"`
	UserID        uint64 `json:"user_id"`
	ActivityID    uint64 `json:"activity_id"`
	SKUID         uint64 `json:"sku_id"`
	Quantity      int32  `json:"quantity"`
}

type OrderItemPayload struct {
	SKUID          uint64 `json:"sku_id"`
	Quantity       int32  `json:"quantity"`
	ReservationID  string `json:"reservation_id"`
	UnitPriceCents int64  `json:"unit_price_cents"`
}

type OrderCreatedPayload struct {
	OrderID          string             `json:"order_id"`
	UserID           uint64             `json:"user_id"`
	OrderType        string             `json:"order_type"`
	TotalAmountCents int64              `json:"total_amount_cents"`
	Items            []OrderItemPayload `json:"items"`
}

type OrderCancelledPayload struct {
	OrderID        string   `json:"order_id"`
	UserID         uint64   `json:"user_id"`
	Reason         string   `json:"reason"`
	ReservationIDs []string `json:"reservation_ids"`
}

type PaymentSucceededPayload struct {
	PaymentNo   string `json:"payment_no"`
	OrderID     string `json:"order_id"`
	UserID      uint64 `json:"user_id"`
	AmountCents int64  `json:"amount_cents"`
	CallbackRef string `json:"callback_ref"`
}

type PaymentRefundedPayload struct {
	RefundNo    string `json:"refund_no"`
	PaymentNo   string `json:"payment_no"`
	OrderID     string `json:"order_id"`
	UserID      uint64 `json:"user_id"`
	AmountCents int64  `json:"amount_cents"`
	Reason      string `json:"reason"`
}

type InventoryReservationPayload struct {
	ReservationID string `json:"reservation_id"`
	OrderID       string `json:"order_id"`
	UserID        uint64 `json:"user_id"`
	SKUID         uint64 `json:"sku_id"`
	Quantity      int32  `json:"quantity"`
	Mode          string `json:"mode"`
}

type ShipmentPayload struct {
	OrderID    string `json:"order_id"`
	Carrier    string `json:"carrier,omitempty"`
	TrackingNo string `json:"tracking_no,omitempty"`
	Status     string `json:"status"`
}

// ValidateEventPayload 验证已知 v1 事件的跨服务最小字段。未知事件和未来 payload 版本保留给兼容路由处理。
func ValidateEventPayload(event EventEnvelope) error {
	if err := event.Validate(); err != nil {
		return err
	}
	if event.EventVersion != 1 || !IsKnownEventType(event.EventType) {
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(event.Payload, &raw); err != nil {
		return fmt.Errorf("event payload must be a JSON object: %w", err)
	}
	required := map[string][]string{
		EventSeckillAccepted:    {"order_id", "reservation_id", "user_id", "sku_id", "quantity"},
		EventOrderCreated:       {"order_id", "user_id", "total_amount_cents", "items"},
		EventOrderCancelled:     {"order_id", "user_id", "reason", "reservation_ids"},
		EventPaymentSucceeded:   {"payment_no", "order_id", "user_id", "amount_cents", "callback_ref"},
		EventPaymentRefunded:    {"refund_no", "payment_no", "order_id", "user_id", "amount_cents", "reason"},
		EventInventoryReserved:  {"reservation_id", "order_id", "user_id", "sku_id", "quantity"},
		EventInventoryReleased:  {"reservation_id", "order_id", "user_id", "sku_id", "quantity"},
		EventInventoryRestocked: {"reservation_id", "order_id", "user_id", "sku_id", "quantity"},
		EventShipmentCreated:    {"order_id", "status"},
		EventShipmentDelivered:  {"order_id", "status"},
	}
	for _, field := range required[event.EventType] {
		value, ok := raw[field]
		if !ok || len(value) == 0 || string(value) == "null" {
			return errors.New("event payload field is required: " + field)
		}
	}
	return nil
}
