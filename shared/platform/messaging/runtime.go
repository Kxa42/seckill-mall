package messaging

import (
	"context"
	"log"
	"time"
)

// RunRabbitConsumer 在 RabbitMQ 断线后退避重连，服务关闭时通过 ctx 结束。
func RunRabbitConsumer(ctx context.Context, url, consumer string, inbox InboxStore, handlers map[string]Handler, maxAttempts int) {
	if url == "" || inbox == nil {
		return
	}
	delay := time.Second
	for ctx.Err() == nil {
		err := NewRabbitConsumer(url).Run(ctx, consumer, inbox, handlers, maxAttempts)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("rabbitmq consumer reconnect consumer=%s err=%v", consumer, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if delay < 30*time.Second {
			delay *= 2
		}
	}
}
