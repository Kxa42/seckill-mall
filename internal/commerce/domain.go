// Package commerce 定义完整商城后端的领域模型、状态与错误契约。
package commerce

import (
	"errors"
	"fmt"
	"math"
	"net/mail"
	"strings"
	"time"
)

const (
	RoleCustomer = "customer"
	RoleAdmin    = "admin"

	UserStatusActive = "active"

	OrderTypeNormal  = "normal"
	OrderTypeSeckill = "seckill"

	OrderStatusPendingPayment = "pending_payment"
	OrderStatusPaid           = "paid"
	OrderStatusShipped        = "shipped"
	OrderStatusCompleted      = "completed"
	OrderStatusCanceled       = "canceled"
	OrderStatusRefundPending  = "refund_pending"
	OrderStatusRefunded       = "refunded"

	ReservationStatusReserved  = "reserved"
	ReservationStatusConfirmed = "confirmed"
	ReservationStatusReleased  = "released"
	ReservationStatusExpired   = "expired"

	PaymentStatusPending   = "pending"
	PaymentStatusSucceeded = "succeeded"

	RefundStatusSucceeded  = "succeeded"
	ShipmentStatusShipped  = "shipped"
	ShipmentStatusReceived = "received"
)

const (
	CodeValidation        = "VALIDATION_ERROR"
	CodeNotFound          = "NOT_FOUND"
	CodeConflict          = "CONFLICT"
	CodeUnauthorized      = "UNAUTHORIZED"
	CodeForbidden         = "FORBIDDEN"
	CodeOutOfStock        = "OUT_OF_STOCK"
	CodeInvalidTransition = "INVALID_TRANSITION"
	CodeInternal          = "INTERNAL_ERROR"
)

// Error 是可稳定映射到 HTTP 响应的领域错误。
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

// NewError 创建不泄漏内部实现细节的领域错误。
func NewError(code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, Cause: cause}
}

// ErrorCode 提取错误码，未知错误统一映射为 INTERNAL_ERROR。
func ErrorCode(err error) string {
	var domainErr *Error
	if errors.As(err, &domainErr) {
		return domainErr.Code
	}
	return CodeInternal
}

// PublicMessage 提取可返回客户端的安全消息。
func PublicMessage(err error) string {
	var domainErr *Error
	if errors.As(err, &domainErr) {
		return domainErr.Message
	}
	return "服务暂时不可用"
}

type User struct {
	ID           uint64    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type RefreshTokenRecord struct {
	ID        uint64
	UserID    uint64
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

type Address struct {
	ID        uint64    `json:"id"`
	UserID    uint64    `json:"-"`
	Recipient string    `json:"recipient"`
	Phone     string    `json:"phone"`
	Province  string    `json:"province"`
	City      string    `json:"city"`
	District  string    `json:"district"`
	Detail    string    `json:"detail"`
	IsDefault bool      `json:"is_default"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Category struct {
	ID       uint64 `json:"id"`
	ParentID uint64 `json:"parent_id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Active   bool   `json:"active"`
}

type SPU struct {
	ID          uint64 `json:"id"`
	CategoryID  uint64 `json:"category_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Active      bool   `json:"active"`
}

type SKU struct {
	ID             uint64    `json:"id"`
	SPUID          uint64    `json:"spu_id"`
	Code           string    `json:"code"`
	Name           string    `json:"name"`
	PriceCents     int64     `json:"price_cents"`
	AvailableStock int32     `json:"available_stock"`
	ReservedStock  int32     `json:"reserved_stock,omitempty"`
	Active         bool      `json:"active"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Product struct {
	SPU    SPU      `json:"spu"`
	SKUs   []SKU    `json:"skus"`
	Images []string `json:"images"`
}

type ProductPage struct {
	Items  []Product `json:"items"`
	Limit  int       `json:"limit"`
	Offset int       `json:"offset"`
	Total  int64     `json:"total"`
}

type CartItem struct {
	ID        uint64    `json:"id"`
	UserID    uint64    `json:"-"`
	SKUID     uint64    `json:"sku_id"`
	Quantity  int32     `json:"quantity"`
	SKU       SKU       `json:"sku"`
	Subtotal  int64     `json:"subtotal_cents"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CheckoutPreview struct {
	Items            []CartItem `json:"items"`
	TotalAmountCents int64      `json:"total_amount_cents"`
	Available        bool       `json:"available"`
}

type Order struct {
	ID               uint64        `json:"id"`
	OrderID          string        `json:"order_id"`
	UserID           uint64        `json:"-"`
	OrderType        string        `json:"order_type"`
	Status           string        `json:"status"`
	TotalAmountCents int64         `json:"total_amount_cents"`
	IdempotencyKey   string        `json:"-"`
	AddressSnapshot  Address       `json:"address"`
	Items            []OrderItem   `json:"items"`
	Payment          *Payment      `json:"payment,omitempty"`
	Shipment         *Shipment     `json:"shipment,omitempty"`
	ExpiresAt        time.Time     `json:"expires_at"`
	PaidAt           *time.Time    `json:"paid_at,omitempty"`
	ShippedAt        *time.Time    `json:"shipped_at,omitempty"`
	CompletedAt      *time.Time    `json:"completed_at,omitempty"`
	CanceledAt       *time.Time    `json:"canceled_at,omitempty"`
	RefundedAt       *time.Time    `json:"refunded_at,omitempty"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
	StatusHistory    []OrderStatus `json:"status_history,omitempty"`
}

type OrderItem struct {
	ID             uint64 `json:"id"`
	OrderID        string `json:"order_id"`
	SKUID          uint64 `json:"sku_id"`
	SKUCode        string `json:"sku_code"`
	SKUName        string `json:"sku_name"`
	UnitPriceCents int64  `json:"unit_price_cents"`
	Quantity       int32  `json:"quantity"`
	SubtotalCents  int64  `json:"subtotal_cents"`
}

type OrderStatus struct {
	ID         uint64    `json:"id"`
	OrderID    string    `json:"order_id"`
	FromStatus string    `json:"from_status"`
	ToStatus   string    `json:"to_status"`
	Reason     string    `json:"reason"`
	ActorType  string    `json:"actor_type"`
	ActorID    uint64    `json:"actor_id"`
	CreatedAt  time.Time `json:"created_at"`
}

type InventoryReservation struct {
	ID            uint64
	ReservationID string
	OrderID       string
	SKUID         uint64
	Quantity      int32
	Status        string
	ExpiresAt     time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Payment struct {
	ID          uint64     `json:"id"`
	PaymentNo   string     `json:"payment_no"`
	OrderID     string     `json:"order_id"`
	AmountCents int64      `json:"amount_cents"`
	Provider    string     `json:"provider"`
	Status      string     `json:"status"`
	CallbackRef string     `json:"callback_ref,omitempty"`
	PaidAt      *time.Time `json:"paid_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type Refund struct {
	ID          uint64     `json:"id"`
	RefundNo    string     `json:"refund_no"`
	OrderID     string     `json:"order_id"`
	PaymentNo   string     `json:"payment_no"`
	AmountCents int64      `json:"amount_cents"`
	Reason      string     `json:"reason"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type Shipment struct {
	ID          uint64     `json:"id"`
	OrderID     string     `json:"order_id"`
	Carrier     string     `json:"carrier"`
	TrackingNo  string     `json:"tracking_no"`
	Status      string     `json:"status"`
	ShippedAt   time.Time  `json:"shipped_at"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type TokenPair struct {
	AccessToken      string    `json:"access_token"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
	User             User      `json:"user"`
}

type CreateOrderItem struct {
	SKUID    uint64
	Quantity int32
}

type CreateOrderCommand struct {
	OrderID        string
	UserID         uint64
	AddressID      uint64
	OrderType      string
	IdempotencyKey string
	Items          []CreateOrderItem
	ExpiresAt      time.Time
	Now            time.Time
}

type AdminProductInput struct {
	Category Category
	SPU      SPU
	SKUs     []SKU
}

// NormalizeEmail 校验并规范化邮箱。
func NormalizeEmail(value string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || len(email) > 255 {
		return "", NewError(CodeValidation, "邮箱格式不正确", err)
	}
	return email, nil
}

// CalculateSubtotal 使用整数分安全计算小计。
func CalculateSubtotal(priceCents int64, quantity int32) (int64, error) {
	if priceCents < 0 || quantity <= 0 {
		return 0, NewError(CodeValidation, "价格和数量必须有效", nil)
	}
	if quantity > 99 {
		return 0, NewError(CodeValidation, "单个 SKU 数量不能超过 99", nil)
	}
	if priceCents > math.MaxInt64/int64(quantity) {
		return 0, NewError(CodeValidation, "金额超出允许范围", nil)
	}
	return priceCents * int64(quantity), nil
}

func addAmount(total, delta int64) (int64, error) {
	if total < 0 || delta < 0 || total > math.MaxInt64-delta {
		return 0, NewError(CodeValidation, "订单金额无效", nil)
	}
	return total + delta, nil
}

// CanTransitionOrder 判断订单状态转换是否合法。
func CanTransitionOrder(from, to string) bool {
	allowed := map[string]map[string]bool{
		OrderStatusPendingPayment: {
			OrderStatusPaid:     true,
			OrderStatusCanceled: true,
		},
		OrderStatusPaid: {
			OrderStatusShipped:       true,
			OrderStatusRefundPending: true,
		},
		OrderStatusShipped: {
			OrderStatusCompleted: true,
		},
		OrderStatusRefundPending: {
			OrderStatusRefunded: true,
		},
	}
	return allowed[from][to]
}

func validateAddress(address Address) error {
	values := []struct {
		name  string
		value string
		max   int
	}{
		{"收件人", address.Recipient, 64},
		{"手机号", address.Phone, 32},
		{"省份", address.Province, 64},
		{"城市", address.City, 64},
		{"区县", address.District, 64},
		{"详细地址", address.Detail, 255},
	}
	for _, item := range values {
		value := strings.TrimSpace(item.value)
		if value == "" || len([]rune(value)) > item.max {
			return NewError(CodeValidation, fmt.Sprintf("%s不能为空且长度不能超过 %d", item.name, item.max), nil)
		}
	}
	return nil
}
