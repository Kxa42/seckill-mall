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

// EnsureGroup 幂等创建 Redis Stream 消费组（XGROUP CREATE MKSTREAM）。
// Stream 不存在时自动创建；消费组已存在时忽略 BUSYGROUP 错误。
// 首次创建从 0-0（头部）开始，保证新启动的发布桥也能读到历史事件。
func (s *RedisStreamOutbox) EnsureGroup(ctx context.Context) error {
	err := s.client.XGroupCreateMkStream(ctx, s.stream, s.group, "0-0").Err()
	if err != nil && !stringsContains(err.Error(), "BUSYGROUP") {
		return err
	}
	return nil
}

// Append 将一条事件信封追加到 Redis Stream 末尾（XADD）。
// 正常路径下事件由库存 Lua 脚本在状态变更的同一次原子执行中写入，
// 本方法主要供测试和内存外备用路径直接投递事件。
func (s *RedisStreamOutbox) Append(ctx context.Context, event contracts.EventEnvelope) error {
	body, err := messaging.MarshalEnvelope(event)
	if err != nil {
		return err
	}
	_, err = s.client.XAdd(ctx, &redis.XAddArgs{Stream: s.stream, Values: map[string]any{"event_id": event.EventID, "event_type": event.EventType, "payload": string(body)}}).Result()
	return err
}

// Read 从消费组读取新增事件（XREADGROUP，Streams 使用 ">" 只读未被投递的消息）。
// 每次最多读 count 条（默认 100），无新消息时阻塞 block 时长后返回空列表。
// 返回的每条消息都已完成信封反序列化与校验，校验失败的消息会被跳过。
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

// ClaimPending 认领消费组中超时未确认的 pending 消息（XAUTOCLAIM）。
// minIdle 表示消息在 pending 列表中空闲多久后允许被重新认领（发布桥使用 30s），
// 用于消费者崩溃或发布失败后重放事件，配合 XAck 实现 at-least-once 投递。
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

// Ack 确认一批 Stream 消息（XAck），标记对应事件已成功发布到 RabbitMQ。
// 只有确认后才不会在下次 ClaimPending 中被重新投递。
func (s *RedisStreamOutbox) Ack(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	return s.client.XAck(ctx, s.stream, s.group, ids...).Err()
}

// streamEvents 将 XReadGroup 返回的 Stream 列表扁平化为事件列表，
// 每个 Stream 内的消息经 streamMessages 解析与校验。
func streamEvents(streams []redis.XStream) []StreamEvent {
	values := make([]StreamEvent, 0)
	for _, stream := range streams {
		values = append(values, streamMessages(stream.Messages)...)
	}
	return values
}

// streamMessages 将 Redis Stream 原始消息转换为 StreamEvent：
// 从消息字段中取出 payload，反序列化为事件信封并执行 Validate 校验，
// 缺失 payload 或信封不合法的消息直接跳过（由发布桥忽略，不阻塞后续消息）。
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

// stringsContains 是 strings.Contains 的最小实现，判断 value 是否包含 fragment，
// 用于避免本文件引入标准库 strings 依赖。
func stringsContains(value, fragment string) bool {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return true
		}
	}
	return false
}

var _ EventStream = (*RedisStreamOutbox)(nil)
