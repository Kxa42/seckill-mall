package messaging

import (
	"context"
	"fmt"
	"time"
)

// RunOutboxPublisher 扫描单个服务 Outbox 并发布事件。
func RunOutboxPublisher(ctx context.Context, store OutboxStore, publisher Publisher, interval time.Duration, batchSize int) error {
	if store == nil || publisher == nil {
		return fmt.Errorf("outbox store and publisher are required")
	}
	if interval <= 0 {
		interval = time.Second
	}
	if batchSize <= 0 {
		batchSize = 100
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := publishOutboxBatch(ctx, store, publisher, batchSize); err != nil && ctx.Err() == nil {
			// 单条事件错误已经回写 Outbox，扫描循环继续服务其他事件。
			_ = err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func publishOutboxBatch(ctx context.Context, store OutboxStore, publisher Publisher, batchSize int) error {
	events, err := store.Claim(ctx, time.Now().UTC(), batchSize, 30*time.Second)
	if err != nil {
		return err
	}
	var firstErr error
	for _, record := range events {
		event, decodeErr := UnmarshalEnvelope(record.Payload)
		if decodeErr != nil {
			_ = store.MarkFailed(ctx, record.EventID, decodeErr.Error())
			if firstErr == nil {
				firstErr = decodeErr
			}
			continue
		}
		if publishErr := publisher.Publish(ctx, event, record.Headers); publishErr != nil {
			next := time.Now().UTC().Add(RetryDelay(record.Attempts, time.Second))
			if record.Attempts >= 5 {
				_ = store.MarkFailed(ctx, record.EventID, publishErr.Error())
			} else {
				_ = store.MarkRetry(ctx, record.EventID, next, publishErr.Error())
			}
			if firstErr == nil {
				firstErr = publishErr
			}
			continue
		}
		if markErr := store.MarkPublished(ctx, record.EventID); markErr != nil && firstErr == nil {
			firstErr = markErr
		}
	}
	return firstErr
}
