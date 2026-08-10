package cartservice

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidRequest = errors.New("cart request is invalid")
	ErrNotFound       = errors.New("cart item not found")
	ErrUnavailable    = errors.New("cart dependency unavailable")
)

type Repository interface {
	Set(ctx context.Context, userID, skuID uint64, quantity int32, now time.Time) error
	Delete(ctx context.Context, userID, skuID uint64) error
	List(ctx context.Context, userID uint64) ([]Item, error)
}
