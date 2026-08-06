package catalogservice

import (
	"context"
	"errors"
)

var (
	ErrInvalidRequest = errors.New("catalog request is invalid")
	ErrNotFound       = errors.New("catalog item not found")
)

// Repository 是 Catalog Service 的数据边界，只允许访问目录数据集。
type Repository interface {
	ListProducts(ctx context.Context, offset, limit int, includeInactive bool) (ProductPage, error)
	GetProduct(ctx context.Context, spuID uint64, includeInactive bool) (Product, error)
	GetSKUSnapshot(ctx context.Context, skuIDs []uint64, includeInactive bool) ([]SKU, error)
}
