// Package messaging 提供服务级 Outbox/Inbox 与 RabbitMQ 生命周期装配。
package messaging

import (
	"context"
	"errors"
	"log"
	"strings"

	"gorm.io/gorm"
)

// EventSinkSetter 由拥有服务级 Outbox 的 Repository 实现。
type EventSinkSetter interface {
	SetEventSink(EventSink)
}

// ServiceRuntime 统一管理单个领域服务的 Outbox、Inbox 和 RabbitMQ worker。
type ServiceRuntime struct {
	outbox    OutboxStore
	inbox     InboxStore
	publisher *RabbitPublisher
	url       string
}

// NewMemoryServiceRuntime 为内存 Repository 装配进程内 Outbox/Inbox。
func NewMemoryServiceRuntime(owner EventSinkSetter, rabbitURL string) (*ServiceRuntime, error) {
	if owner == nil {
		return nil, errors.New("message event sink owner is required")
	}
	store := NewMemoryStore()
	owner.SetEventSink(store)
	return newServiceRuntime(store.Outbox(), store.Inbox(), rabbitURL), nil
}

// NewSQLServiceRuntime 为 MySQL Repository 装配服务自有的 Outbox/Inbox 表。
func NewSQLServiceRuntime(owner EventSinkSetter, db *gorm.DB, outboxTable, inboxTable, rabbitURL string) (*ServiceRuntime, error) {
	if owner == nil {
		return nil, errors.New("message event sink owner is required")
	}
	store, err := NewSQLStore(db, outboxTable, inboxTable)
	if err != nil {
		return nil, err
	}
	owner.SetEventSink(store)
	return newServiceRuntime(store.Outbox(), store.Inbox(), rabbitURL), nil
}

func newServiceRuntime(outbox OutboxStore, inbox InboxStore, rabbitURL string) *ServiceRuntime {
	rabbitURL = strings.TrimSpace(rabbitURL)
	runtime := &ServiceRuntime{outbox: outbox, inbox: inbox, url: rabbitURL}
	if rabbitURL != "" {
		runtime.publisher = NewRabbitPublisher(rabbitURL)
	}
	return runtime
}

// Enabled 表示当前服务已配置 RabbitMQ 发布和消费。
func (r *ServiceRuntime) Enabled() bool {
	return r != nil && r.publisher != nil
}

// Start 启动 Outbox 发布和 RabbitMQ 消费 worker。
func (r *ServiceRuntime) Start(ctx context.Context, consumer string, handlers map[string]Handler) {
	if !r.Enabled() {
		return
	}
	go func() {
		if err := RunOutboxPublisher(ctx, r.outbox, r.publisher, 0, 0); err != nil && ctx.Err() == nil {
			log.Printf("outbox publisher stopped consumer=%s err=%v", consumer, err)
		}
	}()
	go RunRabbitConsumer(ctx, r.url, consumer, r.inbox, handlers, 5)
}

// Close 关闭运行时持有的 RabbitMQ Publisher；未启用 MQ 时为空操作。
func (r *ServiceRuntime) Close() error {
	if r == nil || r.publisher == nil {
		return nil
	}
	return r.publisher.Close()
}
