// Package cartservice 定义购物车领域模型和应用服务。
// 购物车只保存用户与 SKU 数量，商品名称和价格由 Catalog 服务实时提供。
package cartservice

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInvalidRequest = errors.New("cart request is invalid")
	ErrNotFound       = errors.New("cart item not found")
	ErrUnavailable    = errors.New("cart dependency unavailable")
)

type SKU struct {
	ID             uint64
	Code, Name     string
	PriceCents     int64
	Active         bool
	AvailableStock int32
}
type Item struct {
	UserID, SKUID uint64
	Quantity      int32
	SKU           SKU
	SubtotalCents int64
}
type Preview struct {
	Items            []Item
	TotalAmountCents int64
	Available        bool
}

type CatalogClient interface {
	GetSKUSnapshot(ctx context.Context, skuIDs []uint64, includeInactive bool) ([]SKU, error)
}
type Repository interface {
	Set(ctx context.Context, userID, skuID uint64, quantity int32, now time.Time) error
	Delete(ctx context.Context, userID, skuID uint64) error
	List(ctx context.Context, userID uint64) ([]Item, error)
}

type Service struct {
	repository Repository
	catalog    CatalogClient
	now        func() time.Time
}

func NewService(repository Repository, catalog CatalogClient) (*Service, error) {
	if repository == nil || catalog == nil {
		return nil, errors.New("cart repository and catalog client are required")
	}
	return &Service{repository: repository, catalog: catalog, now: time.Now}, nil
}

func (s *Service) Set(ctx context.Context, userID, skuID uint64, quantity int32) (Item, error) {
	if userID == 0 || skuID == 0 || quantity <= 0 || quantity > 99 {
		return Item{}, ErrInvalidRequest
	}
	skus, err := s.catalog.GetSKUSnapshot(ctx, []uint64{skuID}, false)
	if err != nil || len(skus) != 1 {
		return Item{}, mapCatalogError(err)
	}
	if skus[0].ID != skuID || !skus[0].Active {
		return Item{}, ErrNotFound
	}
	item, err := itemWithSKU(Item{UserID: userID, SKUID: skuID, Quantity: quantity}, skus[0])
	if err != nil {
		return Item{}, err
	}
	if err := s.repository.Set(ctx, userID, skuID, quantity, s.now().UTC()); err != nil {
		return Item{}, err
	}
	return item, nil
}

func (s *Service) Delete(ctx context.Context, userID, skuID uint64) error {
	if userID == 0 || skuID == 0 {
		return ErrInvalidRequest
	}
	return s.repository.Delete(ctx, userID, skuID)
}

func (s *Service) List(ctx context.Context, userID uint64) ([]Item, error) {
	if userID == 0 {
		return nil, ErrInvalidRequest
	}
	items, err := s.repository.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return []Item{}, nil
	}
	ids := make([]uint64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.SKUID)
	}
	skus, err := s.catalog.GetSKUSnapshot(ctx, ids, true)
	if err != nil {
		return nil, mapCatalogError(err)
	}
	byID := make(map[uint64]SKU, len(skus))
	for _, sku := range skus {
		byID[sku.ID] = sku
	}
	result := make([]Item, 0, len(items))
	for _, item := range items {
		sku, ok := byID[item.SKUID]
		if !ok {
			item.SKU.Active = false
		} else {
			item, err = itemWithSKU(item, sku)
			if err != nil {
				return nil, err
			}
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *Service) Preview(ctx context.Context, userID uint64) (Preview, error) {
	items, err := s.List(ctx, userID)
	if err != nil {
		return Preview{}, err
	}
	preview := Preview{Items: items, Available: true}
	for _, item := range items {
		if !item.SKU.Active || item.Quantity <= 0 || item.Quantity > item.SKU.AvailableStock {
			preview.Available = false
		}
		if item.SubtotalCents < 0 || preview.TotalAmountCents > maxInt64-item.SubtotalCents {
			return Preview{}, ErrInvalidRequest
		}
		preview.TotalAmountCents += item.SubtotalCents
	}
	return preview, nil
}

func itemWithSKU(item Item, sku SKU) (Item, error) {
	if sku.PriceCents < 0 || item.Quantity <= 0 {
		return Item{}, ErrInvalidRequest
	}
	if sku.PriceCents != 0 && int64(item.Quantity) > maxInt64/sku.PriceCents {
		return Item{}, ErrInvalidRequest
	}
	item.SKU = sku
	item.SubtotalCents = sku.PriceCents * int64(item.Quantity)
	return item, nil
}

const maxInt64 = int64(^uint64(0) >> 1)

func mapCatalogError(err error) error {
	if err == nil {
		return ErrNotFound
	}
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}

type MemoryRepository struct {
	mu    sync.RWMutex
	items map[string]Item
}

func NewMemoryRepository() *MemoryRepository { return &MemoryRepository{items: make(map[string]Item)} }
func cartKey(userID, skuID uint64) string    { return fmt.Sprintf("%d:%d", userID, skuID) }
func (r *MemoryRepository) Set(_ context.Context, userID, skuID uint64, quantity int32, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[cartKey(userID, skuID)] = Item{UserID: userID, SKUID: skuID, Quantity: quantity}
	return nil
}
func (r *MemoryRepository) Delete(_ context.Context, userID, skuID uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := cartKey(userID, skuID)
	if _, ok := r.items[key]; !ok {
		return ErrNotFound
	}
	delete(r.items, key)
	return nil
}
func (r *MemoryRepository) List(_ context.Context, userID uint64) ([]Item, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Item, 0)
	for _, item := range r.items {
		if item.UserID == userID {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SKUID < result[j].SKUID })
	return result, nil
}

type MySQLRepository struct{ db *gorm.DB }
type cartRecord struct {
	ID        uint64 `gorm:"column:id;primaryKey"`
	UserID    uint64 `gorm:"column:user_id"`
	SKUID     uint64 `gorm:"column:sku_id"`
	Quantity  int32
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (cartRecord) TableName() string { return "cart_items" }
func NewMySQLRepository(db *gorm.DB) (*MySQLRepository, error) {
	if db == nil {
		return nil, errors.New("cart db is required")
	}
	return &MySQLRepository{db: db}, nil
}
func (r *MySQLRepository) Set(ctx context.Context, userID, skuID uint64, quantity int32, now time.Time) error {
	record := cartRecord{UserID: userID, SKUID: skuID, Quantity: quantity, CreatedAt: now, UpdatedAt: now}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "sku_id"}},
		DoUpdates: clause.Assignments(map[string]any{"quantity": quantity, "updated_at": now}),
	}).Create(&record).Error
}
func (r *MySQLRepository) Delete(ctx context.Context, userID, skuID uint64) error {
	result := r.db.WithContext(ctx).Where("user_id = ? AND sku_id = ?", userID, skuID).Delete(&cartRecord{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
func (r *MySQLRepository) List(ctx context.Context, userID uint64) ([]Item, error) {
	var records []cartRecord
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("id ASC").Find(&records).Error; err != nil {
		return nil, err
	}
	result := make([]Item, 0, len(records))
	for _, record := range records {
		result = append(result, Item{UserID: record.UserID, SKUID: record.SKUID, Quantity: record.Quantity})
	}
	return result, nil
}

func statusError(err error) error {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		return status.Error(codes.InvalidArgument, "购物车参数无效")
	case errors.Is(err, ErrNotFound):
		return status.Error(codes.NotFound, "购物车商品不存在")
	case errors.Is(err, ErrUnavailable):
		return status.Error(codes.Unavailable, "目录服务暂时不可用")
	default:
		return status.Error(codes.Internal, "购物车服务暂时不可用")
	}
}

var _ Repository = (*MemoryRepository)(nil)
var _ Repository = (*MySQLRepository)(nil)
