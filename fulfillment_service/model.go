// Package fulfillmentservice 定义履约服务模型和数据所有权。
// 本包只访问 shipments，订单状态转换通过 Order Service 完成。
package fulfillmentservice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"seckill-mall/common/orderclient"
)

var (
	ErrInvalidRequest = errors.New("fulfillment request is invalid")
	ErrNotFound       = errors.New("shipment not found")
	ErrConflict       = errors.New("shipment conflicts")
)

const (
	StatusShipped  = "shipped"
	StatusReceived = "received"
)

type Shipment struct {
	ID          uint64
	OrderID     string
	Carrier     string
	TrackingNo  string
	Status      string
	ShippedAt   time.Time
	DeliveredAt time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Repository interface {
	Create(ctx context.Context, shipment Shipment, now time.Time) (Shipment, bool, error)
	Get(ctx context.Context, orderID string) (Shipment, error)
	MarkReceived(ctx context.Context, orderID string, now time.Time) (Shipment, bool, error)
}

type Service struct {
	repository Repository
	orders     orderclient.Client
	now        func() time.Time
}

func NewService(repository Repository, orders orderclient.Client) (*Service, error) {
	if repository == nil || orders == nil {
		return nil, errors.New("fulfillment repository and order client are required")
	}
	return &Service{repository: repository, orders: orders, now: time.Now}, nil
}

func (s *Service) Ship(ctx context.Context, actorID uint64, orderID, carrier, trackingNo string) (Shipment, orderclient.Transition, bool, error) {
	orderID, carrier, trackingNo = strings.TrimSpace(orderID), strings.TrimSpace(carrier), strings.TrimSpace(trackingNo)
	if actorID == 0 || orderID == "" || carrier == "" || trackingNo == "" || len(carrier) > 64 || len(trackingNo) > 128 {
		return Shipment{}, orderclient.Transition{}, false, ErrInvalidRequest
	}
	if existing, err := s.repository.Get(ctx, orderID); err == nil {
		if existing.Carrier != carrier || existing.TrackingNo != trackingNo {
			return Shipment{}, orderclient.Transition{}, false, ErrConflict
		}
		transition, callErr := s.orders.Ship(ctx, actorID, orderID, carrier, trackingNo)
		return existing, transition, true, callErr
	} else if !errors.Is(err, ErrNotFound) {
		return Shipment{}, orderclient.Transition{}, false, err
	}
	now := s.now().UTC()
	value, reused, err := s.repository.Create(ctx, Shipment{OrderID: orderID, Carrier: carrier, TrackingNo: trackingNo, Status: StatusShipped, ShippedAt: now, CreatedAt: now, UpdatedAt: now}, now)
	if err != nil {
		return Shipment{}, orderclient.Transition{}, false, err
	}
	transition, err := s.orders.Ship(ctx, actorID, orderID, carrier, trackingNo)
	if err != nil {
		return Shipment{}, orderclient.Transition{}, false, err
	}
	return value, transition, reused, nil
}

func (s *Service) ConfirmReceipt(ctx context.Context, userID uint64, orderID string) (Shipment, orderclient.Transition, bool, error) {
	orderID = strings.TrimSpace(orderID)
	if userID == 0 || orderID == "" {
		return Shipment{}, orderclient.Transition{}, false, ErrInvalidRequest
	}
	if _, err := s.orders.Get(ctx, userID, orderID); err != nil {
		return Shipment{}, orderclient.Transition{}, false, err
	}
	transition, err := s.orders.ConfirmReceipt(ctx, userID, orderID)
	if err != nil {
		return Shipment{}, orderclient.Transition{}, false, err
	}
	value, reused, err := s.repository.MarkReceived(ctx, orderID, s.now().UTC())
	return value, transition, reused, err
}

func (s *Service) Get(ctx context.Context, userID uint64, orderID string) (Shipment, error) {
	orderID = strings.TrimSpace(orderID)
	if userID == 0 || orderID == "" {
		return Shipment{}, ErrInvalidRequest
	}
	if _, err := s.orders.Get(ctx, userID, orderID); err != nil {
		return Shipment{}, err
	}
	return s.repository.Get(ctx, orderID)
}

type MemoryRepository struct {
	mu        sync.RWMutex
	nextID    uint64
	shipments map[string]Shipment
	tracking  map[string]string
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{nextID: 1, shipments: make(map[string]Shipment), tracking: make(map[string]string)}
}
func trackingKey(carrier, number string) string { return carrier + ":" + number }
func (r *MemoryRepository) Create(_ context.Context, value Shipment, _ time.Time) (Shipment, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.shipments[value.OrderID]; ok {
		if existing.Carrier != value.Carrier || existing.TrackingNo != value.TrackingNo {
			return Shipment{}, false, ErrConflict
		}
		return existing, true, nil
	}
	key := trackingKey(value.Carrier, value.TrackingNo)
	if owner, exists := r.tracking[key]; exists && owner != value.OrderID {
		return Shipment{}, false, ErrConflict
	}
	value.ID = r.nextID
	r.nextID++
	r.shipments[value.OrderID], r.tracking[key] = value, value.OrderID
	return value, false, nil
}
func (r *MemoryRepository) Get(_ context.Context, orderID string) (Shipment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.shipments[orderID]
	if !ok {
		return Shipment{}, ErrNotFound
	}
	return value, nil
}
func (r *MemoryRepository) MarkReceived(_ context.Context, orderID string, now time.Time) (Shipment, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.shipments[orderID]
	if !ok {
		return Shipment{}, false, ErrNotFound
	}
	if value.Status == StatusReceived {
		return value, true, nil
	}
	value.Status, value.DeliveredAt, value.UpdatedAt = StatusReceived, now, now
	r.shipments[orderID] = value
	return value, false, nil
}

type MySQLRepository struct{ db *gorm.DB }
type shipmentRecord struct {
	ID          uint64 `gorm:"column:id;primaryKey"`
	OrderID     string `gorm:"column:order_id"`
	Carrier     string
	TrackingNo  string `gorm:"column:tracking_no"`
	Status      string
	ShippedAt   time.Time  `gorm:"column:shipped_at"`
	DeliveredAt *time.Time `gorm:"column:delivered_at"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (shipmentRecord) TableName() string { return "shipments" }
func NewMySQLRepository(db *gorm.DB) (*MySQLRepository, error) {
	if db == nil {
		return nil, errors.New("fulfillment db is required")
	}
	return &MySQLRepository{db: db}, nil
}
func shipmentFromRecord(v shipmentRecord) Shipment {
	var delivered time.Time
	if v.DeliveredAt != nil {
		delivered = *v.DeliveredAt
	}
	return Shipment{ID: v.ID, OrderID: v.OrderID, Carrier: v.Carrier, TrackingNo: v.TrackingNo, Status: v.Status, ShippedAt: v.ShippedAt, DeliveredAt: delivered, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}
func (r *MySQLRepository) Create(ctx context.Context, value Shipment, now time.Time) (Shipment, bool, error) {
	var existing shipmentRecord
	if err := r.db.WithContext(ctx).Where("order_id = ?", value.OrderID).First(&existing).Error; err == nil {
		result := shipmentFromRecord(existing)
		if result.Carrier != value.Carrier || result.TrackingNo != value.TrackingNo {
			return Shipment{}, false, ErrConflict
		}
		return result, true, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return Shipment{}, false, err
	}
	record := shipmentRecord{OrderID: value.OrderID, Carrier: value.Carrier, TrackingNo: value.TrackingNo, Status: value.Status, ShippedAt: value.ShippedAt, CreatedAt: now, UpdatedAt: now}
	if err := r.db.WithContext(ctx).Create(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			var duplicate shipmentRecord
			if loadErr := r.db.WithContext(ctx).Where("order_id = ?", value.OrderID).First(&duplicate).Error; loadErr == nil {
				result := shipmentFromRecord(duplicate)
				if result.Carrier == value.Carrier && result.TrackingNo == value.TrackingNo {
					return result, true, nil
				}
			}
			return Shipment{}, false, ErrConflict
		}
		return Shipment{}, false, err
	}
	return shipmentFromRecord(record), false, nil
}
func (r *MySQLRepository) Get(ctx context.Context, orderID string) (Shipment, error) {
	var record shipmentRecord
	if err := r.db.WithContext(ctx).Where("order_id = ?", orderID).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Shipment{}, ErrNotFound
		}
		return Shipment{}, err
	}
	return shipmentFromRecord(record), nil
}
func (r *MySQLRepository) MarkReceived(ctx context.Context, orderID string, now time.Time) (Shipment, bool, error) {
	var record shipmentRecord
	if err := r.db.WithContext(ctx).Where("order_id = ?", orderID).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Shipment{}, false, ErrNotFound
		}
		return Shipment{}, false, err
	}
	if record.Status == StatusReceived {
		return shipmentFromRecord(record), true, nil
	}
	if err := r.db.WithContext(ctx).Model(&record).Updates(map[string]any{"status": StatusReceived, "delivered_at": now, "updated_at": now}).Error; err != nil {
		return Shipment{}, false, err
	}
	record.Status, record.DeliveredAt, record.UpdatedAt = StatusReceived, &now, now
	return shipmentFromRecord(record), false, nil
}

func PublicID(value Shipment) string { return fmt.Sprintf("ship_%d", value.ID) }

var _ Repository = (*MemoryRepository)(nil)
var _ Repository = (*MySQLRepository)(nil)
