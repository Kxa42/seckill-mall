package inventoryservice

import (
	"context"
	"errors"
)

var (
	ErrInvalidRequest = errors.New("inventory request is invalid")
	ErrNotFound       = errors.New("inventory reservation not found")
	ErrOutOfStock     = errors.New("inventory is insufficient")
	ErrPurchaseLimit  = errors.New("purchase limit exceeded")
	ErrConflict       = errors.New("inventory reservation state conflict")
)

// Store 是 Inventory/Seckill Service 的数据边界。
type Store interface {
	Reserve(ctx context.Context, command ReserveCommand) (Reservation, error)
	Confirm(ctx context.Context, reservationID, orderID string) (Reservation, error)
	Release(ctx context.Context, reservationID, orderID string) (Reservation, error)
	Restock(ctx context.Context, reservationID, orderID string) (Reservation, error)
	AdmitSeckill(ctx context.Context, command SeckillAdmissionCommand) (Reservation, error)
}
