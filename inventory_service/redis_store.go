package inventoryservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"seckill-mall/common/contracts"
	"seckill-mall/common/messaging"
)

const reserveLua = `
local existing = redis.call('HGET', KEYS[3], 'status')
if existing then return 10 end
if redis.call('EXISTS', KEYS[1]) == 0 then return 0 end
local quantity = tonumber(ARGV[1])
local stock = tonumber(redis.call('GET', KEYS[1])) or 0
if stock < quantity then return 2 end
redis.call('DECRBY', KEYS[1], quantity)
redis.call('HSET', KEYS[3], 'status', 'reserved', 'order_id', ARGV[2], 'user_id', ARGV[3], 'sku_id', ARGV[4], 'quantity', ARGV[1], 'mode', ARGV[5], 'created_at', ARGV[6], 'updated_at', ARGV[6])
if ARGV[9] ~= '' then redis.call('XADD', KEYS[4], '*', 'event_id', ARGV[7], 'event_type', ARGV[8], 'payload', ARGV[9]) end
return 1
`

const seckillLua = `
local existing = redis.call('HGET', KEYS[3], 'status')
if existing then return 10 end
if redis.call('EXISTS', KEYS[1]) == 0 then return 0 end
local quantity = tonumber(ARGV[1])
local limit = tonumber(ARGV[4])
local current = tonumber(redis.call('HGET', KEYS[2], ARGV[2])) or 0
if current + quantity > limit then return 3 end
local stock = tonumber(redis.call('GET', KEYS[1])) or 0
if stock < quantity then return 2 end
redis.call('DECRBY', KEYS[1], quantity)
redis.call('HINCRBY', KEYS[2], ARGV[2], quantity)
redis.call('HSET', KEYS[3], 'status', 'reserved', 'order_id', ARGV[6], 'user_id', ARGV[2], 'activity_id', ARGV[5], 'sku_id', ARGV[3], 'quantity', ARGV[1], 'mode', 'seckill', 'created_at', ARGV[7], 'updated_at', ARGV[7])
if ARGV[10] ~= '' then redis.call('XADD', KEYS[4], '*', 'event_id', ARGV[8], 'event_type', ARGV[9], 'payload', ARGV[10]) end
return 1
`

const transitionLua = `
local current = redis.call('HGET', KEYS[3], 'status')
if not current then return 0 end
local currentOrder = redis.call('HGET', KEYS[3], 'order_id') or ''
if ARGV[4] ~= '' and currentOrder ~= '' and currentOrder ~= ARGV[4] then return 5 end
if current == ARGV[2] then return 1 end
local expected = 'reserved'
if ARGV[1] == 'restocked' then expected = 'confirmed' end
if current ~= expected then return 4 end
if ARGV[1] == 'released' or ARGV[1] == 'restocked' then
	local quantity = tonumber(redis.call('HGET', KEYS[3], 'quantity'))
	redis.call('INCRBY', KEYS[1], quantity)
	local mode = redis.call('HGET', KEYS[3], 'mode') or ''
	if mode == 'seckill' then
		local userID = redis.call('HGET', KEYS[3], 'user_id')
		local currentPurchased = tonumber(redis.call('HGET', KEYS[2], userID)) or 0
		if currentPurchased <= quantity then
			redis.call('HDEL', KEYS[2], userID)
		else
			redis.call('HINCRBY', KEYS[2], userID, -quantity)
		end
	end
end
if ARGV[4] ~= '' and currentOrder == '' then
	redis.call('HSET', KEYS[3], 'order_id', ARGV[4])
end
redis.call('HSET', KEYS[3], 'status', ARGV[1], 'updated_at', ARGV[3])
if (ARGV[1] == 'released' or ARGV[1] == 'restocked') and ARGV[7] ~= '' then redis.call('XADD', KEYS[4], '*', 'event_id', ARGV[5], 'event_type', ARGV[6], 'payload', ARGV[7]) end
return 1
`

// RedisStore 使用 Redis Lua 实现秒杀热路径和 reservation 幂等状态转换。
type RedisStore struct {
	client            *redis.Client
	stockPrefix       string
	userPrefix        string
	reservationPrefix string
	purchaseLimit     int32
	streamKey         string
	now               func() time.Time
}

// RedisStoreOptions 描述 RedisStore 的键空间和限购配置。
type RedisStoreOptions struct {
	StockPrefix       string
	UserPrefix        string
	ReservationPrefix string
	PurchaseLimit     int32
	StreamKey         string
}

// NewRedisStore 创建 Redis Lua 库存实现。
func NewRedisStore(client *redis.Client, options RedisStoreOptions) (*RedisStore, error) {
	if client == nil {
		return nil, errors.New("redis client is required")
	}
	if options.StockPrefix == "" {
		options.StockPrefix = "inventory:stock:"
	}
	if options.UserPrefix == "" {
		options.UserPrefix = "inventory:users:"
	}
	if options.ReservationPrefix == "" {
		options.ReservationPrefix = "inventory:reservation:"
	}
	if options.PurchaseLimit <= 0 {
		options.PurchaseLimit = 1
	}
	if options.StreamKey == "" {
		options.StreamKey = "inventory:events"
	}
	return &RedisStore{client: client, stockPrefix: options.StockPrefix, userPrefix: options.UserPrefix, reservationPrefix: options.ReservationPrefix, purchaseLimit: options.PurchaseLimit, streamKey: options.StreamKey, now: func() time.Time { return time.Now().UTC() }}, nil
}

// EventStream 返回与 Lua 状态机使用同一 Redis 客户端和 key 的 Stream Outbox。
func (s *RedisStore) EventStream() (*RedisStreamOutbox, error) {
	return NewRedisStreamOutbox(s.client, s.streamKey, "inventory-publisher")
}

// EventInbox 返回 Inventory 自有的 Redis Inbox，避免消费幂等状态落到进程内存。
func (s *RedisStore) EventInbox() (messaging.InboxStore, error) {
	return NewRedisInbox(s.client)
}

// Close 释放 Inventory 自有 Redis 连接。
func (s *RedisStore) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}

func (s *RedisStore) Reserve(ctx context.Context, command ReserveCommand) (Reservation, error) {
	if err := validateReserveCommand(command); err != nil {
		return Reservation{}, err
	}
	now := s.now()
	event, err := inventoryEvent(contracts.EventInventoryReserved, command.ReservationID, command.OrderID, command.UserID, command.SKUID, command.Quantity, command.Mode, now)
	if err != nil {
		return Reservation{}, err
	}
	payload, _ := json.Marshal(event)
	code, err := s.client.Eval(ctx, reserveLua, []string{s.stockKey(command.SKUID), s.userKey(0, command.SKUID), s.reservationKey(command.ReservationID), s.streamKey}, command.Quantity, command.OrderID, command.UserID, command.SKUID, command.Mode, now.UnixNano(), event.EventID, event.EventType, string(payload)).Int()
	if err != nil {
		return Reservation{}, err
	}
	if code == 0 || code == 2 {
		return Reservation{}, ErrOutOfStock
	}
	if code != 1 && code != 10 {
		return Reservation{}, ErrConflict
	}
	reservation, err := s.loadReservation(ctx, command.ReservationID)
	if err != nil {
		return Reservation{}, err
	}
	if code == 10 && !sameReservation(reservation, command) {
		return Reservation{}, ErrConflict
	}
	return reservation, nil
}

func (s *RedisStore) Confirm(ctx context.Context, reservationID, orderID string) (Reservation, error) {
	return s.transition(ctx, reservationID, orderID, ReservationConfirmed)
}

func (s *RedisStore) Release(ctx context.Context, reservationID, orderID string) (Reservation, error) {
	return s.transition(ctx, reservationID, orderID, ReservationReleased)
}

func (s *RedisStore) Restock(ctx context.Context, reservationID, orderID string) (Reservation, error) {
	return s.transition(ctx, reservationID, orderID, ReservationRestocked)
}

func (s *RedisStore) AdmitSeckill(ctx context.Context, command SeckillAdmissionCommand) (Reservation, error) {
	if !validOpaqueID(command.RequestID) || command.ActivityID == 0 || command.UserID == 0 || command.SKUID == 0 || command.Quantity <= 0 {
		return Reservation{}, ErrInvalidRequest
	}
	reservationID := "admit_" + command.RequestID
	now := s.now()
	event, err := inventoryEvent(contracts.EventSeckillAccepted, reservationID, command.OrderID, command.UserID, command.SKUID, command.Quantity, "seckill", now)
	if err != nil {
		return Reservation{}, err
	}
	payload, _ := json.Marshal(event)
	code, err := s.client.Eval(ctx, seckillLua, []string{s.stockKey(command.SKUID), s.userKey(command.ActivityID, command.SKUID), s.reservationKey(reservationID), s.streamKey}, command.Quantity, command.UserID, command.SKUID, s.purchaseLimit, command.ActivityID, command.OrderID, now.UnixNano(), event.EventID, event.EventType, string(payload)).Int()
	if err != nil {
		return Reservation{}, err
	}
	switch code {
	case 0, 2:
		return Reservation{}, ErrOutOfStock
	case 3:
		return Reservation{}, ErrPurchaseLimit
	case 1, 10:
		reservation, loadErr := s.loadReservation(ctx, reservationID)
		if loadErr != nil {
			return Reservation{}, loadErr
		}
		if code == 10 && (reservation.ActivityID != command.ActivityID || reservation.UserID != command.UserID || reservation.SKUID != command.SKUID || reservation.Quantity != command.Quantity || (command.OrderID != "" && reservation.OrderID != command.OrderID)) {
			return Reservation{}, ErrConflict
		}
		return reservation, nil
	default:
		return Reservation{}, ErrConflict
	}
}

func (s *RedisStore) transition(ctx context.Context, reservationID, orderID, target string) (Reservation, error) {
	if !validOpaqueID(reservationID) {
		return Reservation{}, ErrInvalidRequest
	}
	key := s.reservationKey(reservationID)
	fields, err := s.client.HGetAll(ctx, key).Result()
	if err != nil {
		return Reservation{}, err
	}
	if len(fields) == 0 {
		return Reservation{}, ErrNotFound
	}
	if orderID != "" && fields["order_id"] != "" && fields["order_id"] != orderID {
		return Reservation{}, ErrConflict
	}
	skuID := parseUint(fields["sku_id"])
	if skuID == 0 {
		return Reservation{}, ErrConflict
	}
	activityID := parseUint(fields["activity_id"])
	eventType := contracts.EventInventoryReleased
	if target == ReservationRestocked {
		eventType = contracts.EventInventoryRestocked
	}
	event, err := inventoryEvent(eventType, reservationID, orderID, parseUint(fields["user_id"]), skuID, int32(parseUint(fields["quantity"])), fields["mode"], s.now())
	if err != nil {
		return Reservation{}, err
	}
	payload, _ := json.Marshal(event)
	code, err := s.client.Eval(ctx, transitionLua, []string{s.stockKey(skuID), s.userKey(activityID, skuID), key, s.streamKey}, target, target, s.now().UnixNano(), orderID, event.EventID, event.EventType, string(payload)).Int()
	if err != nil {
		return Reservation{}, err
	}
	if code == 0 {
		return Reservation{}, ErrNotFound
	}
	if code == 4 {
		return Reservation{}, ErrConflict
	}
	if code == 5 {
		return Reservation{}, ErrConflict
	}
	return s.loadReservation(ctx, reservationID)
}

func inventoryEvent(eventType, reservationID, orderID string, userID, skuID uint64, quantity int32, mode string, now time.Time) (contracts.EventEnvelope, error) {
	return contracts.NewEventEnvelope(fmt.Sprintf("%s:%s", eventType, reservationID), eventType, "reservation", reservationID, 1, contracts.InventoryReservationPayload{ReservationID: reservationID, OrderID: orderID, UserID: userID, SKUID: skuID, Quantity: quantity, Mode: mode}, now)
}

func (s *RedisStore) loadReservation(ctx context.Context, reservationID string) (Reservation, error) {
	fields, err := s.client.HGetAll(ctx, s.reservationKey(reservationID)).Result()
	if err != nil {
		return Reservation{}, err
	}
	if len(fields) == 0 {
		return Reservation{}, ErrNotFound
	}
	return Reservation{ReservationID: reservationID, OrderID: fields["order_id"], UserID: parseUint(fields["user_id"]), ActivityID: parseUint(fields["activity_id"]), SKUID: parseUint(fields["sku_id"]), Quantity: int32(parseUint(fields["quantity"])), Status: fields["status"], Mode: fields["mode"], CreatedAt: parseTime(fields["created_at"]), UpdatedAt: parseTime(fields["updated_at"])}, nil
}

// ReservationIDsByOrder 只扫描 Inventory 自有 reservation key，供退款事件恢复使用。
func (s *RedisStore) ReservationIDsByOrder(ctx context.Context, orderID string) ([]string, error) {
	var result []string
	var cursor uint64
	for {
		keys, next, err := s.client.Scan(ctx, cursor, s.reservationPrefix+"*", 100).Result()
		if err != nil {
			return nil, err
		}
		for _, key := range keys {
			value, err := s.client.HGet(ctx, key, "order_id").Result()
			if err == nil && value == orderID {
				result = append(result, key[len(s.reservationPrefix):])
			}
		}
		cursor = next
		if cursor == 0 {
			return result, nil
		}
	}
}

func (s *RedisStore) stockKey(skuID uint64) string {
	return s.stockPrefix + strconv.FormatUint(skuID, 10)
}
func (s *RedisStore) userKey(activityID, skuID uint64) string {
	return s.userPrefix + strconv.FormatUint(activityID, 10) + ":" + strconv.FormatUint(skuID, 10)
}
func (s *RedisStore) reservationKey(reservationID string) string {
	return s.reservationPrefix + reservationID
}

func parseUint(value string) uint64 {
	parsed, _ := strconv.ParseUint(value, 10, 64)
	return parsed
}

func parseTime(value string) time.Time {
	nanos, _ := strconv.ParseInt(value, 10, 64)
	return time.Unix(0, nanos).UTC()
}

var _ Store = (*RedisStore)(nil)
