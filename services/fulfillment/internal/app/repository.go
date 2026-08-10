package fulfillmentservice

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidRequest = errors.New("fulfillment request is invalid")
	ErrNotFound       = errors.New("shipment not found")
	ErrConflict       = errors.New("shipment conflicts")
)

type Repository interface {
	Create(ctx context.Context, shipment Shipment, now time.Time) (Shipment, bool, error)
	Get(ctx context.Context, orderID string) (Shipment, error)
	MarkReceived(ctx context.Context, orderID string, now time.Time) (Shipment, bool, error)
}
