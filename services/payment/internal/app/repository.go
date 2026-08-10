package paymentservice

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidRequest    = errors.New("payment request is invalid")
	ErrUnauthorized      = errors.New("payment callback is unauthorized")
	ErrNotFound          = errors.New("payment resource not found")
	ErrConflict          = errors.New("payment request conflicts")
	ErrInvalidTransition = errors.New("payment transition invalid")
)

type Repository interface {
	Create(ctx context.Context, payment Payment, now time.Time) (Payment, bool, error)
	Get(ctx context.Context, paymentNo string) (Payment, error)
	MarkSucceeded(ctx context.Context, paymentNo, callbackRef string, now time.Time) (Payment, bool, error)
	CreateRefund(ctx context.Context, refund Refund, now time.Time) (Refund, bool, error)
}
