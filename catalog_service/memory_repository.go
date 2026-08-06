package catalogservice

import (
	"context"
	"sort"
	"sync"
	"time"
)

// MemoryRepository 是不依赖 MySQL 的 Catalog 验收存储。
type MemoryRepository struct {
	mu       sync.RWMutex
	products map[uint64]Product
	skus     map[uint64]SKU
}

// NewMemoryRepository 创建带演示商品的 Catalog 存储。
func NewMemoryRepository() *MemoryRepository {
	now := time.Now().UTC()
	product := Product{
		SPUID:       1,
		CategoryID:  1,
		Name:        "iPhone 15",
		Description: "演示商品",
		Active:      true,
		Images:      []string{},
		SKUs: []SKU{{
			ID: 1, SPUID: 1, Code: "IPHONE15-128-BLACK", Name: "iPhone 15 128GB 黑色",
			PriceCents: 699900, AvailableStock: 100, Active: true, CreatedAt: now, UpdatedAt: now,
		}},
	}
	return &MemoryRepository{
		products: map[uint64]Product{product.SPUID: product},
		skus:     map[uint64]SKU{1: product.SKUs[0]},
	}
}

func (r *MemoryRepository) ListProducts(_ context.Context, offset, limit int, includeInactive bool) (ProductPage, error) {
	if offset < 0 || limit <= 0 {
		return ProductPage{}, ErrInvalidRequest
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]uint64, 0, len(r.products))
	for id, product := range r.products {
		if includeInactive || product.Active {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	total := int64(len(ids))
	if offset > len(ids) {
		offset = len(ids)
	}
	end := offset + limit
	if end > len(ids) {
		end = len(ids)
	}
	items := make([]Product, 0, end-offset)
	for _, id := range ids[offset:end] {
		items = append(items, r.cloneProduct(r.products[id], includeInactive))
	}
	return ProductPage{Items: items, Total: total, Offset: offset, Limit: limit}, nil
}

func (r *MemoryRepository) GetProduct(_ context.Context, spuID uint64, includeInactive bool) (Product, error) {
	if spuID == 0 {
		return Product{}, ErrInvalidRequest
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	product, ok := r.products[spuID]
	if !ok || (!includeInactive && !product.Active) {
		return Product{}, ErrNotFound
	}
	return r.cloneProduct(product, includeInactive), nil
}

func (r *MemoryRepository) GetSKUSnapshot(_ context.Context, skuIDs []uint64, includeInactive bool) ([]SKU, error) {
	if len(skuIDs) == 0 {
		return nil, ErrInvalidRequest
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]SKU, 0, len(skuIDs))
	for _, skuID := range skuIDs {
		if skuID == 0 {
			return nil, ErrInvalidRequest
		}
		sku, ok := r.skus[skuID]
		if !ok || (!includeInactive && !sku.Active) {
			return nil, ErrNotFound
		}
		items = append(items, sku)
	}
	return items, nil
}

func (r *MemoryRepository) cloneProduct(product Product, includeInactive bool) Product {
	originalSKUs := product.SKUs
	product.SKUs = make([]SKU, 0, len(originalSKUs))
	for _, sku := range originalSKUs {
		if includeInactive || sku.Active {
			product.SKUs = append(product.SKUs, sku)
		}
	}
	product.Images = append([]string(nil), product.Images...)
	return product
}

var _ Repository = (*MemoryRepository)(nil)
