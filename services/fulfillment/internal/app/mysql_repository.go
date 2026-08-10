package fulfillmentservice

import (
	"context"
	"errors"
	"fmt"
	amqp "github.com/rabbitmq/amqp091-go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"seckill-mall/shared/contracts"
	"seckill-mall/shared/platform/messaging"
	"time"
)

type MySQLRepository struct {
	db        *gorm.DB
	eventSink messaging.EventSink
}

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

// SetEventSink 注入 Fulfillment 自有 SQL Outbox。
func (r *MySQLRepository) SetEventSink(sink messaging.EventSink) { r.eventSink = sink }

// Database 供服务启动装配本服务消息表。
func (r *MySQLRepository) Database() *gorm.DB { return r.db }

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
	var record shipmentRecord
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		record = shipmentRecord{OrderID: value.OrderID, Carrier: value.Carrier, TrackingNo: value.TrackingNo, Status: value.Status, ShippedAt: value.ShippedAt, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		if r.eventSink != nil {
			event, err := shipmentEvent(fmt.Sprintf("shipment.created:%s", value.OrderID), contracts.EventShipmentCreated, value, now)
			if err != nil {
				return err
			}
			if err := appendEventTx(ctx, r.eventSink, tx, event); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
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
	reused := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("order_id = ?", orderID).First(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if record.Status == StatusReceived {
			reused = true
			return nil
		}
		if err := tx.Model(&record).Updates(map[string]any{"status": StatusReceived, "delivered_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		record.Status, record.DeliveredAt, record.UpdatedAt = StatusReceived, &now, now
		if r.eventSink != nil {
			event, err := shipmentEvent(fmt.Sprintf("shipment.delivered:%s", orderID), contracts.EventShipmentDelivered, shipmentFromRecord(record), now)
			if err != nil {
				return err
			}
			if err := appendEventTx(ctx, r.eventSink, tx, event); err != nil {
				return err
			}
		}
		return nil
	})
	return shipmentFromRecord(record), reused, err
}

func appendEventTx(ctx context.Context, sink messaging.EventSink, tx *gorm.DB, event contracts.EventEnvelope) error {
	if transactional, ok := sink.(interface {
		AppendTx(context.Context, *gorm.DB, contracts.EventEnvelope, amqp.Table) error
	}); ok {
		return transactional.AppendTx(ctx, tx, event, nil)
	}
	return sink.AppendEvent(ctx, event, nil)
}

var _ Repository = (*MySQLRepository)(nil)
