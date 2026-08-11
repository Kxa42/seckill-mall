package messaging

import (
	"context"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"

	"seckill-mall/shared/contracts"
)

// FakeBroker 是不依赖 RabbitMQ 的进程内 Broker，用于契约和 Memory E2E。
type FakeBroker struct {
	mu       sync.Mutex
	messages []contracts.EventEnvelope
}

func NewFakeBroker() *FakeBroker { return &FakeBroker{} }

func (b *FakeBroker) Publish(_ context.Context, event contracts.EventEnvelope, _ amqp.Table) error {
	if err := event.Validate(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.messages = append(b.messages, event)
	return nil
}

// Messages 返回当前消息快照，调用方可将其重复投递给多个消费者。
func (b *FakeBroker) Messages() []contracts.EventEnvelope {
	b.mu.Lock()
	defer b.mu.Unlock()
	items := make([]contracts.EventEnvelope, len(b.messages))
	copy(items, b.messages)
	return items
}

// Dispatch 将消息按顺序交给处理器，返回第一个失败。
func (b *FakeBroker) Dispatch(ctx context.Context, handler Handler) error {
	for _, event := range b.Messages() {
		if err := handler(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

var _ Publisher = (*FakeBroker)(nil)
