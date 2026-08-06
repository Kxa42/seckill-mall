package messaging

import (
	"context"
	"time"

	"seckill-mall/common/contracts"
)

// DispatchWithInbox 提供所有服务共用的幂等消费流程。
// 未知事件和未来版本被安全确认，避免新事件把旧消费者打入 DLQ。
func DispatchWithInbox(ctx context.Context, inbox InboxStore, consumer string, event contracts.EventEnvelope, handlers map[string]Handler) error {
	if inbox == nil {
		return context.Canceled
	}
	if err := event.Validate(); err != nil {
		return err
	}
	if !contracts.IsKnownEventType(event.EventType) || event.EventVersion > 1 {
		return nil
	}
	handler, ok := handlers[event.EventType]
	if !ok {
		return nil
	}
	claim, err := inbox.Claim(ctx, consumer, event, 30*time.Second)
	if err != nil {
		return err
	}
	if claim.Processed || !claim.Claimed {
		return nil
	}
	if err := handler(ctx, event); err != nil {
		_ = inbox.MarkFailed(ctx, consumer, event.EventID, err.Error())
		return err
	}
	return inbox.MarkProcessed(ctx, consumer, event.EventID)
}
