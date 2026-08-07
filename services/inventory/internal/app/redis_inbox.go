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
)

var claimInboxScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if not current then
  redis.call('SET', KEYS[1], 'processing', 'PX', ARGV[1])
  return 1
end
if current == 'processed' then return 2 end
local ttl = redis.call('PTTL', KEYS[1])
if ttl <= 0 then
  redis.call('SET', KEYS[1], 'processing', 'PX', ARGV[1])
  return 1
end
return 0
`)

var markProcessedInboxScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) ~= 'processing' then return 0 end
redis.call('SET', KEYS[1], 'processed', 'EX', ARGV[1])
return 1
`)

var markFailedInboxScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) ~= 'processing' then return 0 end
return redis.call('DEL', KEYS[1])
`)

// RedisInbox 使用短租约保护处理中的事件，已完成事件保留一段时间用于重复投递收敛。
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
	result, err := claimInboxScript.Run(ctx, s.client, []string{redisInboxPrefix + key}, lease.Milliseconds()).Int()
	if err != nil {
		return messaging.InboxClaim{}, err
	}
	switch result {
	case 1:
		return messaging.InboxClaim{Claimed: true, Attempts: 1}, nil
	case 2:
		return messaging.InboxClaim{Processed: true}, nil
	default:
		return messaging.InboxClaim{}, nil
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
	_, err = markFailedInboxScript.Run(ctx, s.client, []string{redisInboxPrefix + key}).Result()
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
