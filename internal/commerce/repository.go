package commerce

import (
	"context"
	"time"
)

// Repository 定义应用服务依赖的数据能力，具体实现负责事务与并发控制。
type Repository interface {
	CreateUser(ctx context.Context, email, passwordHash, role string, now time.Time) (User, error)
	FindUserByEmail(ctx context.Context, email string) (User, error)
	FindUserByID(ctx context.Context, userID uint64) (User, error)
	StoreRefreshToken(ctx context.Context, token RefreshTokenRecord) error
	RotateRefreshToken(ctx context.Context, oldHash string, replacement RefreshTokenRecord, now time.Time) (User, error)
	EnsureAdmin(ctx context.Context, email, passwordHash string, now time.Time) error

	CreateAddress(ctx context.Context, address Address, now time.Time) (Address, error)
	UpdateAddress(ctx context.Context, address Address, now time.Time) (Address, error)
	DeleteAddress(ctx context.Context, userID, addressID uint64) error
	ListAddresses(ctx context.Context, userID uint64) ([]Address, error)

	ListProducts(ctx context.Context, offset, limit int) (ProductPage, error)
	GetProduct(ctx context.Context, productID uint64) (Product, error)
	UpsertProduct(ctx context.Context, input AdminProductInput, now time.Time) (Product, error)

	SetCartItem(ctx context.Context, userID, skuID uint64, quantity int32, now time.Time) (CartItem, error)
	DeleteCartItem(ctx context.Context, userID, skuID uint64) error
	ListCartItems(ctx context.Context, userID uint64) ([]CartItem, error)

	CreateOrder(ctx context.Context, command CreateOrderCommand) (Order, bool, error)
	ListOrders(ctx context.Context, userID uint64, offset, limit int) ([]Order, int64, error)
	GetOrder(ctx context.Context, userID uint64, orderID string) (Order, error)
	CancelOrder(ctx context.Context, userID uint64, orderID, reason string, now time.Time) (Order, error)
	ExpireOrders(ctx context.Context, now time.Time, limit int) (int, error)

	CreatePayment(ctx context.Context, userID uint64, orderID, paymentNo string, now time.Time) (Payment, error)
	CompletePayment(ctx context.Context, paymentNo, callbackRef string, now time.Time) (Order, error)
	ShipOrder(ctx context.Context, orderID, carrier, trackingNo string, actorID uint64, now time.Time) (Order, error)
	ConfirmOrder(ctx context.Context, userID uint64, orderID string, now time.Time) (Order, error)
	RefundOrder(ctx context.Context, userID uint64, orderID, refundNo, reason string, now time.Time) (Refund, Order, error)
}
