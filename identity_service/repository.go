// Package identityservice 提供阶段 3 所需的最小 Identity 地址快照边界。
// 完整认证与地址写入能力将在后续阶段迁移，本包只提供按用户读取快照。
package identityservice

import (
	"context"
	"errors"
	"sync"

	"gorm.io/gorm"
)

var ErrAddressNotFound = errors.New("identity address not found")

type Address struct {
	ID        uint64
	UserID    uint64
	Recipient string
	Phone     string
	Province  string
	City      string
	District  string
	Detail    string
}

type Repository interface {
	GetAddress(ctx context.Context, userID, addressID uint64) (Address, error)
}

type MemoryRepository struct {
	mu        sync.RWMutex
	addresses map[uint64]Address
}

func NewMemoryRepository(addresses ...Address) *MemoryRepository {
	values := make(map[uint64]Address, len(addresses))
	for _, address := range addresses {
		values[address.ID] = address
	}
	if len(values) == 0 {
		values[1] = Address{ID: 1, UserID: 1, Recipient: "演示用户", Phone: "13800000000", Province: "浙江省", City: "杭州市", District: "西湖区", Detail: "演示地址 1 号"}
	}
	return &MemoryRepository{addresses: values}
}

func (r *MemoryRepository) GetAddress(_ context.Context, userID, addressID uint64) (Address, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.addresses[addressID]
	if !ok || value.UserID != userID {
		return Address{}, ErrAddressNotFound
	}
	return value, nil
}

type MySQLRepository struct{ db *gorm.DB }

type addressRecord struct {
	ID        uint64 `gorm:"column:id;primaryKey"`
	UserID    uint64 `gorm:"column:user_id"`
	Recipient string
	Phone     string
	Province  string
	City      string
	District  string
	Detail    string
}

func (addressRecord) TableName() string { return "user_addresses" }

func NewMySQLRepository(db *gorm.DB) (*MySQLRepository, error) {
	if db == nil {
		return nil, errors.New("identity db is required")
	}
	return &MySQLRepository{db: db}, nil
}

func (r *MySQLRepository) GetAddress(ctx context.Context, userID, addressID uint64) (Address, error) {
	var record addressRecord
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", addressID, userID).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Address{}, ErrAddressNotFound
		}
		return Address{}, err
	}
	return Address{ID: record.ID, UserID: record.UserID, Recipient: record.Recipient, Phone: record.Phone, Province: record.Province, City: record.City, District: record.District, Detail: record.Detail}, nil
}

var _ Repository = (*MemoryRepository)(nil)
var _ Repository = (*MySQLRepository)(nil)
