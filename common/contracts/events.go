// Package contracts 定义跨服务稳定共享的事件信封、事件类型和服务标识。
// 本包不依赖数据库、Gin 或 RabbitMQ，业务服务只能通过这些契约协作。
package contracts

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	ServiceGateway     = "api-gateway"
	ServiceIdentity    = "identity-service"
	ServiceCatalog     = "catalog-service"
	ServiceInventory   = "inventory-service"
	ServiceCart        = "cart-service"
	ServiceOrder       = "order-service"
	ServicePayment     = "payment-service"
	ServiceFulfillment = "fulfillment-service"
)

const (
	EventSeckillAccepted   = "seckill.accepted.v1"
	EventOrderCreated      = "order.created.v1"
	EventOrderCancelled    = "order.cancelled.v1"
	EventOrderCanceled     = EventOrderCancelled // 保留美式拼写别名，避免契约包升级时破坏调用方。
	EventPaymentSucceeded  = "payment.succeeded.v1"
	EventPaymentRefunded   = "payment.refunded.v1"
	EventInventoryReserved = "inventory.reserved.v1"
	EventInventoryReleased = "inventory.released.v1"
	EventShipmentCreated   = "shipment.created.v1"
	EventShipmentDelivered = "shipment.delivered.v1"
)

var knownEventTypes = map[string]struct{}{
	EventSeckillAccepted:   {},
	EventOrderCreated:      {},
	EventOrderCancelled:    {},
	EventPaymentSucceeded:  {},
	EventPaymentRefunded:   {},
	EventInventoryReserved: {},
	EventInventoryReleased: {},
	EventShipmentCreated:   {},
	EventShipmentDelivered: {},
}

// EventEnvelope 是 RabbitMQ 事件的稳定外层格式。Payload 保持 JSON，便于事件版本独立演进。
type EventEnvelope struct {
	EventID string `json:"event_id"`
	// EventType 是事件路由标识，通常包含不可变的主版本后缀，例如 order.created.v1。
	EventType string `json:"event_type"`
	// EventVersion 是同一事件类型的 Payload/信封演进版本，与 EventType 的主版本独立。
	EventVersion  int             `json:"event_version"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	OccurredAt    time.Time       `json:"occurred_at"`
	TraceID       string          `json:"trace_id,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}

// NewEventEnvelope 创建并校验事件信封。事件类型和正数未来版本均允许通过，具体消费者负责能力检查。
func NewEventEnvelope(eventID, eventType, aggregateType, aggregateID string, version int, payload any, occurredAt time.Time) (EventEnvelope, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return EventEnvelope{}, fmt.Errorf("marshal event payload: %w", err)
	}
	envelope := EventEnvelope{
		EventID:       strings.TrimSpace(eventID),
		EventType:     strings.TrimSpace(eventType),
		EventVersion:  version,
		AggregateType: strings.TrimSpace(aggregateType),
		AggregateID:   strings.TrimSpace(aggregateID),
		OccurredAt:    occurredAt.UTC(),
		Payload:       body,
	}
	if err := envelope.Validate(); err != nil {
		return EventEnvelope{}, err
	}
	return envelope, nil
}

// Validate 检查事件信封的传输级约束，不判断具体业务 payload 或消费者是否支持该版本。
func (e EventEnvelope) Validate() error {
	switch {
	case e.EventID == "":
		return errors.New("event_id is required")
	case e.EventType == "":
		return errors.New("event_type is required")
	case e.EventVersion <= 0:
		return errors.New("event_version must be positive")
	case e.AggregateType == "":
		return errors.New("aggregate_type is required")
	case e.AggregateID == "":
		return errors.New("aggregate_id is required")
	case e.OccurredAt.IsZero():
		return errors.New("occurred_at is required")
	case len(e.Payload) == 0 || string(e.Payload) == "null":
		return errors.New("payload is required")
	default:
		return nil
	}
}

// InboxKey 返回消费者维度的幂等键。相同消费者收到同一个 event_id 时必须得到相同键。
func (e EventEnvelope) InboxKey(consumer string) (string, error) {
	if err := e.Validate(); err != nil {
		return "", err
	}
	consumer = strings.TrimSpace(consumer)
	if consumer == "" {
		return "", errors.New("consumer is required")
	}
	return consumer + ":" + e.EventID, nil
}

// IsKnownEventType 用于监控和路由，不应被消费者用来拒绝所有未知事件，以保证向前兼容。
func IsKnownEventType(eventType string) bool {
	_, ok := knownEventTypes[strings.TrimSpace(eventType)]
	return ok
}
