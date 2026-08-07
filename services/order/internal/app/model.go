// Package order 定义商城 Order Service 的领域模型、状态和持久化边界。
// 该包不依赖 Gin、GORM 或其他服务的数据库模型。
package order

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	OrderTypeNormal  = "normal"
	OrderTypeSeckill = "seckill"

	StatusPendingPayment = "pending_payment"
	StatusPaid           = "paid"
	StatusShipped        = "shipped"
	StatusCompleted      = "completed"
	StatusCanceled       = "canceled"
	StatusRefundPending  = "refund_pending"
	StatusRefunded       = "refunded"
)

const (
	CodeValidation        = "VALIDATION_ERROR"
	CodeNotFound          = "NOT_FOUND"
	CodeConflict          = "CONFLICT"
	CodeForbidden         = "FORBIDDEN"
	CodeOutOfStock        = "OUT_OF_STOCK"
	CodeInvalidTransition = "INVALID_TRANSITION"
	CodeUnavailable       = "UPSTREAM_UNAVAILABLE"
)

// Error 是 Order Service 对外暴露的稳定错误类型。
type Error struct {
	Code    string
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause == nil {
		return e.Message
	}
	return e.Message + ": " + e.Cause.Error()
}

func (e *Error) Unwrap() error { return e.Cause }

func NewError(code, message string, cause error) error {
	return &Error{Code: code, Message: message, Cause: cause}
}

func ErrorCode(err error) string {
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}
	return "INTERNAL_ERROR"
}

func PublicMessage(err error) string {
	var target *Error
	if errors.As(err, &target) {
		return target.Message
	}
	return "订单服务暂时不可用"
}

// SKU 是 Catalog 返回的下单时商品快照。
type SKU struct {
	ID         uint64
	Code       string
	Name       string
	PriceCents int64
	Active     bool
}

// Address 是 Identity 返回并被复制到订单中的地址快照。
type Address struct {
	ID        uint64 `json:"address_id"`
	UserID    uint64 `json:"user_id"`
	Recipient string `json:"recipient"`
	Phone     string `json:"phone"`
	Province  string `json:"province"`
	City      string `json:"city"`
	District  string `json:"district"`
	Detail    string `json:"detail"`
}

type Item struct {
	SKUID          uint64 `json:"sku_id"`
	SKUCode        string `json:"sku_code"`
	SKUName        string `json:"sku_name"`
	UnitPriceCents int64  `json:"unit_price_cents"`
	Quantity       int32  `json:"quantity"`
	SubtotalCents  int64  `json:"subtotal_cents"`
	ReservationID  string `json:"reservation_id"`
}

type StatusHistory struct {
	FromStatus string    `json:"from_status"`
	ToStatus   string    `json:"to_status"`
	Reason     string    `json:"reason"`
	ActorType  string    `json:"actor_type"`
	ActorID    uint64    `json:"actor_id"`
	CreatedAt  time.Time `json:"created_at"`
}

type Order struct {
	OrderID          string          `json:"order_id"`
	UserID           uint64          `json:"user_id"`
	AddressID        uint64          `json:"address_id"`
	OrderType        string          `json:"order_type"`
	Status           string          `json:"status"`
	TotalAmountCents int64           `json:"total_amount_cents"`
	IdempotencyKey   string          `json:"-"`
	RequestDigest    string          `json:"-"`
	AddressSnapshot  Address         `json:"address_snapshot"`
	Items            []Item          `json:"items"`
	StatusHistory    []StatusHistory `json:"status_history"`
	ExpiresAt        time.Time       `json:"expires_at"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type CreateItem struct {
	SKUID    uint64
	Quantity int32
}

type CreateCommand struct {
	UserID         uint64
	AddressID      uint64
	OrderType      string
	ActivityID     uint64
	IdempotencyKey string
	Items          []CreateItem
}

type Reservation struct {
	ReservationID string
	OrderID       string
	SKUID         uint64
	Quantity      int32
	Status        string
}

type ReserveCommand struct {
	ReservationID string
	OrderID       string
	UserID        uint64
	ActivityID    uint64
	SKUID         uint64
	Quantity      int32
	Mode          string
}

type Operation struct {
	ID            string
	OrderID       string
	Kind          string
	ReservationID string
	Status        string
	Attempts      int
	LastError     string
	NextRetryAt   time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// CreateIntent 记录跨服务订单创建的本地意图，用于失败审计和后续恢复。
type CreateIntent struct {
	IntentID       string
	UserID         uint64
	IdempotencyKey string
	RequestDigest  string
	OrderID        string
	Status         string
	Attempts       int
	Payload        string
	LastError      string
	NextRetryAt    time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

const (
	CreateIntentStarted = "started"
	CreateIntentDone    = "done"
	CreateIntentFailed  = "failed"
)

const (
	OperationPending = "pending_retry"
	OperationDone    = "done"
	OperationFailed  = "failed"
)

const maxOperationAttempts = 8
const maxCreateIntentAttempts = 8

// Repository 是 Order Service 唯一的订单数据边界。
type Repository interface {
	FindByIdempotency(ctx context.Context, userID uint64, key string) (Order, bool, error)
	FindCreateIntent(ctx context.Context, userID uint64, key string) (CreateIntent, bool, error)
	SaveCreateIntent(ctx context.Context, intent CreateIntent) error
	UpdateCreateIntent(ctx context.Context, intent CreateIntent) error
	ListPendingCreateIntents(ctx context.Context, now time.Time, limit int) ([]CreateIntent, error)
	Create(ctx context.Context, order Order) (Order, bool, error)
	Get(ctx context.Context, userID uint64, orderID string) (Order, error)
	List(ctx context.Context, userID uint64, offset, limit int) ([]Order, int64, error)
	ListExpired(ctx context.Context, now time.Time, limit int) ([]Order, error)
	Transition(ctx context.Context, orderID string, userID uint64, target, reason, actorType string, actorID uint64, now time.Time) (Order, error)
	SaveOperation(ctx context.Context, operation Operation) error
	ListPendingOperations(ctx context.Context, now time.Time, limit int) ([]Operation, error)
	UpdateOperation(ctx context.Context, operation Operation) error
}

func validateCreate(command CreateCommand) error {
	if command.UserID == 0 || command.AddressID == 0 {
		return NewError(CodeValidation, "用户和地址不能为空", nil)
	}
	command.OrderType = strings.TrimSpace(command.OrderType)
	if command.OrderType != OrderTypeNormal && command.OrderType != OrderTypeSeckill {
		return NewError(CodeValidation, "订单类型无效", nil)
	}
	if strings.TrimSpace(command.IdempotencyKey) == "" || len(command.IdempotencyKey) > 128 {
		return NewError(CodeValidation, "Idempotency-Key 不能为空且长度不能超过 128", nil)
	}
	if len(command.Items) == 0 {
		return NewError(CodeValidation, "订单至少需要一个 SKU", nil)
	}
	if command.OrderType == OrderTypeSeckill && command.ActivityID == 0 {
		return NewError(CodeValidation, "秒杀活动不能为空", nil)
	}
	seen := make(map[uint64]struct{}, len(command.Items))
	for _, item := range command.Items {
		if item.SKUID == 0 || item.Quantity <= 0 || item.Quantity > 99 {
			return NewError(CodeValidation, "SKU 或数量无效", nil)
		}
		if _, exists := seen[item.SKUID]; exists {
			return NewError(CodeValidation, "同一 SKU 不能重复提交", nil)
		}
		seen[item.SKUID] = struct{}{}
	}
	if command.OrderType == OrderTypeSeckill && len(command.Items) != 1 {
		return NewError(CodeValidation, "秒杀订单只能包含一个 SKU", nil)
	}
	return nil
}

func canTransition(from, to string) bool {
	allowed := map[string]map[string]bool{
		StatusPendingPayment: {StatusPaid: true, StatusCanceled: true},
		StatusPaid:           {StatusShipped: true, StatusRefundPending: true},
		StatusShipped:        {StatusCompleted: true},
		StatusRefundPending:  {StatusRefunded: true},
	}
	return allowed[from][to]
}

func subtotal(price int64, quantity int32) (int64, error) {
	if price < 0 || quantity <= 0 || price > (int64(^uint64(0)>>1))/int64(quantity) {
		return 0, NewError(CodeValidation, "金额或数量无效", nil)
	}
	return price * int64(quantity), nil
}

func addAmount(left, right int64) (int64, error) {
	if left < 0 || right < 0 || left > (int64(^uint64(0)>>1))-right {
		return 0, NewError(CodeValidation, "订单金额超出范围", nil)
	}
	return left + right, nil
}

func cloneOrder(value Order) Order {
	value.Items = append([]Item(nil), value.Items...)
	value.StatusHistory = append([]StatusHistory(nil), value.StatusHistory...)
	return value
}

func cloneOperation(value Operation) Operation { return value }

func operationID(kind, orderID, reservationID string) string {
	return fmt.Sprintf("%s:%s:%s", kind, orderID, reservationID)
}
