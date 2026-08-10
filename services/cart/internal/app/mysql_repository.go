package cartservice

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

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

var _ Repository = (*MySQLRepository)(nil)
