package inventoryservice

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
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
return 1
`

// RedisStore 使用 Redis Lua 实现秒杀热路径和 reservation 幂等状态转换。
type RedisStore struct {
	client            *redis.Client
	stockPrefix       string
	userPrefix        string
	reservationPrefix string
	purchaseLimit     int32
	now               func() time.Time
}

// RedisStoreOptions 描述 RedisStore 的键空间和限购配置。
type RedisStoreOptions struct {
	StockPrefix       string
	UserPrefix        string
	ReservationPrefix string
	PurchaseLimit     int32
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
	return &RedisStore{client: client, stockPrefix: options.StockPrefix, userPrefix: options.UserPrefix, reservationPrefix: options.ReservationPrefix, purchaseLimit: options.PurchaseLimit, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *RedisStore) Reserve(ctx context.Context, command ReserveCommand) (Reservation, error) {
	if err := validateReserveCommand(command); err != nil {
		return Reservation{}, err
	}
	now := s.now()
	code, err := s.client.Eval(ctx, reserveLua, []string{s.stockKey(command.SKUID), s.userKey(0, command.SKUID), s.reservationKey(command.ReservationID)}, command.Quantity, command.OrderID, command.UserID, command.SKUID, command.Mode, now.UnixNano()).Int()
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
	code, err := s.client.Eval(ctx, seckillLua, []string{s.stockKey(command.SKUID), s.userKey(command.ActivityID, command.SKUID), s.reservationKey(reservationID)}, command.Quantity, command.UserID, command.SKUID, s.purchaseLimit, command.ActivityID, command.OrderID, now.UnixNano()).Int()
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
	code, err := s.client.Eval(ctx, transitionLua, []string{s.stockKey(skuID), s.userKey(activityID, skuID), key}, target, target, s.now().UnixNano(), orderID).Int()
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
