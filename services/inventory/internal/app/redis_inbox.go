package inventoryservice

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"seckill-mall/shared/contracts"
	"seckill-mall/shared/platform/messaging"
)

const (
	redisInboxPrefix    = "inventory:inbox:"
	redisInboxRetention = 7 * 24 * time.Hour
	redisInboxLease     = 30 * time.Second
)

var claimInboxScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if not current then
  redis.call('SET', KEYS[1], 'processing:1', 'PX', ARGV[1])
  return {1, 1}
end
if current == 'processed' then return {2, 0} end
local ttl = redis.call('PTTL', KEYS[1])
if ttl <= 0 then
  local attempt = tonumber(string.sub(current, 12))
  if not attempt or attempt < 1 then attempt = 1 end
  redis.call('SET', KEYS[1], 'processing:' .. (attempt + 1), 'PX', ARGV[1])
  return {1, attempt + 1}
end
local attempt = tonumber(string.sub(current, 12))
if not attempt or attempt < 1 then attempt = 1 end
return {0, attempt}
`)

var markProcessedInboxScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if not current or string.sub(current, 1, 10) ~= 'processing' then return 0 end
redis.call('SET', KEYS[1], 'processed', 'EX', ARGV[1])
return 1
`)

var markFailedInboxScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if not current or string.sub(current, 1, 10) ~= 'processing' then return 0 end
return redis.call('SET', KEYS[1], current, 'PX', ARGV[1])
`)

// RedisInbox 使用短租约保护处理中的事件，已完成事件保留一段时间用于重复投递收敛。
// 值格式为 processing:N（N 为处理尝试次数），失败续租不删键，与 SQL Inbox attempts 语义一致。
type RedisInbox struct {
	client *redis.Client
}

func NewRedisInbox(client *redis.Client) (*RedisInbox, error) {
	if client == nil {
		return nil, errors.New("redis inbox client is required")
	}
	return &RedisInbox{client: client}, nil
}

func (s *RedisInbox) Claim(ctx context.Context, consumer string, event contracts.EventEnvelope, lease time.Duration) (messaging.InboxClaim, error) {
	if s == nil || s.client == nil {
		return messaging.InboxClaim{}, errors.New("redis inbox client is required")
	}
	key, err := event.InboxKey(consumer)
	if err != nil {
		return messaging.InboxClaim{}, err
	}
	if lease <= 0 {
		lease = 30 * time.Second
	}
	values, err := claimInboxScript.Run(ctx, s.client, []string{redisInboxPrefix + key}, lease.Milliseconds()).Int64Slice()
	if err != nil {
		return messaging.InboxClaim{}, err
	}
	if len(values) != 2 {
		return messaging.InboxClaim{}, errors.New("redis inbox claim returned invalid result")
	}
	switch state, attempts := int(values[0]), int(values[1]); state {
	case 1:
		return messaging.InboxClaim{Claimed: true, Attempts: attempts}, nil
	case 2:
		return messaging.InboxClaim{Processed: true, Attempts: attempts}, nil
	default:
		return messaging.InboxClaim{Attempts: attempts}, nil
	}
}

func (s *RedisInbox) MarkProcessed(ctx context.Context, consumer, eventID string) error {
	if s == nil || s.client == nil {
		return errors.New("redis inbox client is required")
	}
	key, err := redisInboxKey(consumer, eventID)
	if err != nil {
		return err
	}
	_, err = markProcessedInboxScript.Run(ctx, s.client, []string{redisInboxPrefix + key}, int(redisInboxRetention/time.Second)).Result()
	return err
}

func (s *RedisInbox) MarkFailed(ctx context.Context, consumer, eventID, _ string) error {
	if s == nil || s.client == nil {
		return errors.New("redis inbox client is required")
	}
	key, err := redisInboxKey(consumer, eventID)
	if err != nil {
		return err
	}
	_, err = markFailedInboxScript.Run(ctx, s.client, []string{redisInboxPrefix + key}, int(redisInboxLease/time.Millisecond)).Result()
	return err
}

func redisInboxKey(consumer, eventID string) (string, error) {
	consumer = strings.TrimSpace(consumer)
	if consumer == "" {
		return "", errors.New("consumer is required")
	}
	if strings.TrimSpace(eventID) == "" {
		return "", errors.New("event_id is required")
	}
	return consumer + ":" + eventID, nil
}

var _ messaging.InboxStore = (*RedisInbox)(nil)
