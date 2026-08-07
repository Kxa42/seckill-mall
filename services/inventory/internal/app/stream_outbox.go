package inventoryservice

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"seckill-mall/shared/contracts"
	"seckill-mall/shared/platform/messaging"
)

// RedisStreamOutbox 将库存 Lua 状态转换和事件写入同一个 Redis 脚本完成，
// 消费侧通过 consumer group 读取并以 XAUTOCLAIM 重放超时 pending。
type RedisStreamOutbox struct {
	client *redis.Client
	stream string
	group  string
}

func NewRedisStreamOutbox(client *redis.Client, stream, group string) (*RedisStreamOutbox, error) {
	if client == nil {
		return nil, errors.New("redis stream client is required")
	}
	if stream == "" {
		stream = "inventory:events"
	}
	if group == "" {
		group = "inventory-service"
	}
	return &RedisStreamOutbox{client: client, stream: stream, group: group}, nil
}

func (s *RedisStreamOutbox) EnsureGroup(ctx context.Context) error {
	err := s.client.XGroupCreateMkStream(ctx, s.stream, s.group, "0-0").Err()
	if err != nil && !stringsContains(err.Error(), "BUSYGROUP") {
		return err
	}
	return nil
}

func (s *RedisStreamOutbox) Append(ctx context.Context, event contracts.EventEnvelope) error {
	body, err := messaging.MarshalEnvelope(event)
	if err != nil {
		return err
	}
	_, err = s.client.XAdd(ctx, &redis.XAddArgs{Stream: s.stream, Values: map[string]any{"event_id": event.EventID, "event_type": event.EventType, "payload": string(body)}}).Result()
	return err
}

func (s *RedisStreamOutbox) Read(ctx context.Context, consumer string, count int64, block time.Duration) ([]StreamEvent, error) {
	if err := s.EnsureGroup(ctx); err != nil {
		return nil, err
	}
	if count <= 0 {
		count = 100
	}
	streams, err := s.client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: s.group, Consumer: consumer, Streams: []string{s.stream, ">"}, Count: count, Block: block}).Result()
	if err != nil {
		return nil, err
	}
	return streamEvents(streams), nil
}

func (s *RedisStreamOutbox) ClaimPending(ctx context.Context, consumer string, minIdle time.Duration, count int64) ([]StreamEvent, error) {
	if err := s.EnsureGroup(ctx); err != nil {
		return nil, err
	}
	if count <= 0 {
		count = 100
	}
	items, _, err := s.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{Stream: s.stream, Group: s.group, Consumer: consumer, MinIdle: minIdle, Start: "0-0", Count: count}).Result()
	if err != nil {
		return nil, err
	}
	return streamMessages(items), nil
}

func (s *RedisStreamOutbox) Ack(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	return s.client.XAck(ctx, s.stream, s.group, ids...).Err()
}

func streamEvents(streams []redis.XStream) []StreamEvent {
	values := make([]StreamEvent, 0)
	for _, stream := range streams {
		values = append(values, streamMessages(stream.Messages)...)
	}
	return values
}

func streamMessages(messages []redis.XMessage) []StreamEvent {
	values := make([]StreamEvent, 0, len(messages))
	for _, message := range messages {
		body, ok := message.Values["payload"]
		if !ok {
			continue
		}
		var raw string
		switch value := body.(type) {
		case string:
			raw = value
		case []byte:
			raw = string(value)
		default:
			raw = fmt.Sprint(value)
		}
		event, err := messaging.UnmarshalEnvelope([]byte(raw))
		if err != nil {
			continue
		}
		values = append(values, StreamEvent{ID: message.ID, Event: event})
	}
	return values
}

func stringsContains(value, fragment string) bool {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return true
		}
	}
	return false
}

var _ EventStream = (*RedisStreamOutbox)(nil)
