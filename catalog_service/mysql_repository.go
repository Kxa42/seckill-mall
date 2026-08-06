package catalogservice

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// MySQLRepository 只读写 Catalog 所拥有的 categories/spus/skus/product_images 数据集。
type MySQLRepository struct {
	db *gorm.DB
}

type spuRecord struct {
	ID          uint64 `gorm:"column:id;primaryKey"`
	CategoryID  uint64
	Name        string
	Description string
	Active      bool
}

func (spuRecord) TableName() string { return "spus" }

type skuRecord struct {
	ID             uint64 `gorm:"column:id;primaryKey"`
	SPUID          uint64 `gorm:"column:spu_id"`
	Code           string
	Name           string
	PriceCents     int64
	AvailableStock int32
	ReservedStock  int32
	Active         bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (skuRecord) TableName() string { return "skus" }

type imageRecord struct {
	ID        uint64 `gorm:"column:id;primaryKey"`
	SPUID     uint64 `gorm:"column:spu_id"`
	URL       string `gorm:"column:url"`
	SortOrder int    `gorm:"column:sort_order"`
}

func (imageRecord) TableName() string { return "product_images" }

// NewMySQLRepository 创建 Catalog MySQL Repository。
func NewMySQLRepository(db *gorm.DB) (*MySQLRepository, error) {
	if db == nil {
		return nil, errors.New("catalog db is required")
	}
	return &MySQLRepository{db: db}, nil
}

func (r *MySQLRepository) ListProducts(ctx context.Context, offset, limit int, includeInactive bool) (ProductPage, error) {
	if offset < 0 || limit <= 0 {
		return ProductPage{}, ErrInvalidRequest
	}
	query := r.db.WithContext(ctx).Model(&spuRecord{})
	if !includeInactive {
		query = query.Where("active = ?", true)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return ProductPage{}, err
	}
	var records []spuRecord
	if err := query.Order("id ASC").Offset(offset).Limit(limit).Find(&records).Error; err != nil {
		return ProductPage{}, err
	}
	items, err := r.productsFromSPUs(ctx, records, includeInactive)
	if err != nil {
		return ProductPage{}, err
	}
	return ProductPage{Items: items, Total: total, Offset: offset, Limit: limit}, nil
}

func (r *MySQLRepository) GetProduct(ctx context.Context, spuID uint64, includeInactive bool) (Product, error) {
	if spuID == 0 {
		return Product{}, ErrInvalidRequest
	}
	query := r.db.WithContext(ctx).Where("id = ?", spuID)
	if !includeInactive {
		query = query.Where("active = ?", true)
	}
	var record spuRecord
	if err := query.First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Product{}, ErrNotFound
		}
		return Product{}, err
	}
	items, err := r.productsFromSPUs(ctx, []spuRecord{record}, includeInactive)
	if err != nil {
		return Product{}, err
	}
	return items[0], nil
}

func (r *MySQLRepository) GetSKUSnapshot(ctx context.Context, skuIDs []uint64, includeInactive bool) ([]SKU, error) {
	if len(skuIDs) == 0 {
		return nil, ErrInvalidRequest
	}
	for _, skuID := range skuIDs {
		if skuID == 0 {
			return nil, ErrInvalidRequest
		}
	}
	query := r.db.WithContext(ctx).Where("id IN ?", skuIDs)
	if !includeInactive {
		query = query.Where("active = ?", true)
	}
	var records []skuRecord
	if err := query.Find(&records).Error; err != nil {
		return nil, err
	}
	byID := make(map[uint64]SKU, len(records))
	for _, record := range records {
		byID[record.ID] = skuFromRecord(record)
	}
	items := make([]SKU, 0, len(skuIDs))
	for _, skuID := range skuIDs {
		sku, ok := byID[skuID]
		if !ok {
			return nil, ErrNotFound
		}
		items = append(items, sku)
	}
	return items, nil
}

func (r *MySQLRepository) productsFromSPUs(ctx context.Context, records []spuRecord, includeInactive bool) ([]Product, error) {
	if len(records) == 0 {
		return []Product{}, nil
	}
	ids := make([]uint64, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	query := r.db.WithContext(ctx).Where("spu_id IN ?", ids)
	if !includeInactive {
		query = query.Where("active = ?", true)
	}
	var skuRecords []skuRecord
	if err := query.Order("id ASC").Find(&skuRecords).Error; err != nil {
		return nil, err
	}
	var imageRecords []imageRecord
	if err := r.db.WithContext(ctx).Where("spu_id IN ?", ids).Order("sort_order ASC, id ASC").Find(&imageRecords).Error; err != nil {
		return nil, err
	}
	skusBySPU := make(map[uint64][]SKU)
	for _, record := range skuRecords {
		skusBySPU[record.SPUID] = append(skusBySPU[record.SPUID], skuFromRecord(record))
	}
	imagesBySPU := make(map[uint64][]string)
	for _, record := range imageRecords {
		imagesBySPU[record.SPUID] = append(imagesBySPU[record.SPUID], record.URL)
	}
	items := make([]Product, 0, len(records))
	for _, record := range records {
		items = append(items, Product{
			SPUID: record.ID, CategoryID: record.CategoryID, Name: record.Name,
			Description: record.Description, Active: record.Active,
			SKUs: append([]SKU(nil), skusBySPU[record.ID]...), Images: append([]string(nil), imagesBySPU[record.ID]...),
		})
	}
	return items, nil
}

func skuFromRecord(record skuRecord) SKU {
	return SKU{ID: record.ID, SPUID: record.SPUID, Code: record.Code, Name: record.Name, PriceCents: record.PriceCents, AvailableStock: record.AvailableStock, ReservedStock: record.ReservedStock, Active: record.Active, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
}

var _ Repository = (*MySQLRepository)(nil)
