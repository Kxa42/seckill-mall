// Package messaging 提供新商城统一事件的存储、发布和消费抽象。
// 领域服务只通过本包协作，不直接依赖 RabbitMQ 或其他服务的数据库模型。
package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"seckill-mall/shared/contracts"
)

const (
	StatusPending    = "pending"
	StatusPublishing = "publishing"
	StatusPublished  = "published"
	StatusFailed     = "failed"
	StatusProcessing = "processing"
	StatusProcessed  = "processed"

	DefaultExchange = "commerce.events.v1"
	RetryExchange   = "commerce.events.retry.v1"
	DeadExchange    = "commerce.events.dlx.v1"
)

// OutboxEvent 是待发布的统一事件记录，Payload 必须是完整 EventEnvelope JSON。
type OutboxEvent struct {
	ID            uint64
	EventID       string
	AggregateType string
	AggregateID   string
	EventType     string
	EventVersion  int
	Payload       []byte
	Headers       amqp.Table
	Status        string
	Attempts      int
	NextRetryAt   time.Time
	LastError     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// InboxClaim 描述消费者是否取得了某个事件的处理权。
type InboxClaim struct {
	Claimed   bool
	Processed bool
	Attempts  int
}

// OutboxStore 是单个服务自己的事件存储边界。
type OutboxStore interface {
	Claim(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]OutboxEvent, error)
	MarkPublished(ctx context.Context, eventID string) error
	MarkRetry(ctx context.Context, eventID string, nextRetryAt time.Time, reason string) error
	MarkFailed(ctx context.Context, eventID string, reason string) error
}

// InboxStore 是单个消费者自己的幂等存储边界。
type InboxStore interface {
	Claim(ctx context.Context, consumer string, event contracts.EventEnvelope, lease time.Duration) (InboxClaim, error)
	MarkProcessed(ctx context.Context, consumer, eventID string) error
	MarkFailed(ctx context.Context, consumer, eventID, reason string) error
}

// Publisher 只负责一次事件发布，不负责修改 Outbox 状态。
type Publisher interface {
	Publish(ctx context.Context, event contracts.EventEnvelope, headers amqp.Table) error
}

// EventSink 是领域仓储写入本服务 Outbox 的最小边界。
type EventSink interface {
	AppendEvent(ctx context.Context, event contracts.EventEnvelope, headers amqp.Table) error
}

// NewEvent 是领域仓储创建统一事件信封的便捷入口。
func NewEvent(eventID, eventType, aggregateType, aggregateID string, payload any, occurredAt time.Time) (contracts.EventEnvelope, error) {
	return contracts.NewEventEnvelope(eventID, eventType, aggregateType, aggregateID, 1, payload, occurredAt)
}

// Handler 处理一个已通过传输层校验的事件。
type Handler func(context.Context, contracts.EventEnvelope) error

// MarshalEnvelope 将事件信封序列化为 RabbitMQ 消息正文。
func MarshalEnvelope(event contracts.EventEnvelope) ([]byte, error) {
	if err := event.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(event)
}

// UnmarshalEnvelope 反序列化并校验事件信封。
func UnmarshalEnvelope(body []byte) (contracts.EventEnvelope, error) {
	var event contracts.EventEnvelope
	if err := json.Unmarshal(body, &event); err != nil {
		return event, fmt.Errorf("decode event envelope: %w", err)
	}
	if err := event.Validate(); err != nil {
		return event, err
	}
	return event, nil
}

// RetryDelay 返回受上限约束的指数退避时间。
func RetryDelay(attempt int, base time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if base <= 0 {
		base = time.Second
	}
	if attempt > 6 {
		attempt = 6
	}
	delay := base
	for i := 1; i < attempt; i++ {
		delay *= 2
	}
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}

// ValidateTableName 只接受代码内配置的 SQL 标识符，避免动态表名成为注入入口。
func ValidateTableName(name string) error {
	if strings.TrimSpace(name) != name || name == "" {
		return errors.New("table name is required")
	}
	for _, char := range name {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' {
			continue
		}
		return fmt.Errorf("invalid table name: %q", name)
	}
	return nil
}
