package paymentservice

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

type paymentRecord struct {
	ID          uint64 `gorm:"column:id;primaryKey"`
	PaymentNo   string `gorm:"column:payment_no"`
	OrderID     string `gorm:"column:order_id"`
	UserID      uint64 `gorm:"column:user_id"`
	AmountCents int64  `gorm:"column:amount_cents"`
	Provider    string
	Status      string
	CallbackRef *string    `gorm:"column:callback_ref"`
	PaidAt      *time.Time `gorm:"column:paid_at"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (paymentRecord) TableName() string { return "payments" }

type refundRecord struct {
	ID          uint64 `gorm:"column:id;primaryKey"`
	RefundNo    string `gorm:"column:refund_no"`
	OrderID     string `gorm:"column:order_id"`
	PaymentNo   string `gorm:"column:payment_no"`
	AmountCents int64  `gorm:"column:amount_cents"`
	Reason      string
	Status      string
	CreatedAt   time.Time
	CompletedAt *time.Time `gorm:"column:completed_at"`
}

func (refundRecord) TableName() string { return "refunds" }

func NewMySQLRepository(db *gorm.DB) (*MySQLRepository, error) {
	if db == nil {
		return nil, errors.New("payment db is required")
	}
	return &MySQLRepository{db: db}, nil
}

// SetEventSink 注入 Payment 自有 SQL Outbox。
func (r *MySQLRepository) SetEventSink(sink messaging.EventSink) { r.eventSink = sink }

// Database 供服务启动装配本服务消息表。
func (r *MySQLRepository) Database() *gorm.DB { return r.db }

func paymentFromRecord(v paymentRecord) Payment {
	var callback string
	if v.CallbackRef != nil {
		callback = *v.CallbackRef
	}
	var paid time.Time
	if v.PaidAt != nil {
		paid = *v.PaidAt
	}
	return Payment{PaymentNo: v.PaymentNo, OrderID: v.OrderID, UserID: v.UserID, AmountCents: v.AmountCents, Provider: v.Provider, Status: v.Status, CallbackRef: callback, PaidAt: paid, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}

func refundFromRecord(v refundRecord) Refund {
	var completed time.Time
	if v.CompletedAt != nil {
		completed = *v.CompletedAt
	}
	return Refund{RefundNo: v.RefundNo, OrderID: v.OrderID, PaymentNo: v.PaymentNo, AmountCents: v.AmountCents, Reason: v.Reason, Status: v.Status, CreatedAt: v.CreatedAt, CompletedAt: completed}
}

func (r *MySQLRepository) Create(ctx context.Context, value Payment, now time.Time) (Payment, bool, error) {
	var result Payment
	reused := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing paymentRecord
		if err := tx.Where("order_id = ?", value.OrderID).First(&existing).Error; err == nil {
			result = paymentFromRecord(existing)
			reused = true
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		record := paymentRecord{PaymentNo: value.PaymentNo, OrderID: value.OrderID, UserID: value.UserID, AmountCents: value.AmountCents, Provider: value.Provider, Status: value.Status, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		result = paymentFromRecord(record)
		return nil
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		var existing paymentRecord
		if loadErr := r.db.WithContext(ctx).Where("order_id = ?", value.OrderID).First(&existing).Error; loadErr != nil {
			return Payment{}, false, loadErr
		}
		return paymentFromRecord(existing), true, nil
	}
	return result, reused, err
}

func (r *MySQLRepository) Get(ctx context.Context, paymentNo string) (Payment, error) {
	var record paymentRecord
	if err := r.db.WithContext(ctx).Where("payment_no = ?", paymentNo).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Payment{}, ErrNotFound
		}
		return Payment{}, err
	}
	return paymentFromRecord(record), nil
}

func (r *MySQLRepository) MarkSucceeded(ctx context.Context, paymentNo, callbackRef string, now time.Time) (Payment, bool, error) {
	var result Payment
	reused := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record paymentRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("payment_no = ?", paymentNo).First(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if record.Status == StatusSucceeded {
			if record.CallbackRef == nil || *record.CallbackRef != callbackRef {
				return ErrConflict
			}
			result = paymentFromRecord(record)
			reused = true
			return nil
		}
		callback := callbackRef
		record.Status, record.CallbackRef, record.PaidAt, record.UpdatedAt = StatusSucceeded, &callback, &now, now
		if err := tx.Save(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return ErrConflict
			}
			return err
		}
		if r.eventSink != nil {
			event, err := paymentSucceededEvent(paymentFromRecord(record), callbackRef, now)
			if err != nil {
				return err
			}
			if transactional, ok := r.eventSink.(interface {
				AppendTx(context.Context, *gorm.DB, contracts.EventEnvelope, amqp.Table) error
			}); ok {
				if err := transactional.AppendTx(ctx, tx, event, nil); err != nil {
					return err
				}
			} else if err := r.eventSink.AppendEvent(ctx, event, nil); err != nil {
				return err
			}
		}
		result = paymentFromRecord(record)
		return nil
	})
	return result, reused, err
}

func (r *MySQLRepository) FindSucceededPayment(ctx context.Context, orderID string) (Payment, error) {
	var record paymentRecord
	if err := r.db.WithContext(ctx).Where("order_id = ? AND status = ?", orderID, StatusSucceeded).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Payment{}, ErrInvalidTransition
		}
		return Payment{}, err
	}
	return paymentFromRecord(record), nil
}

func (r *MySQLRepository) FindRefundByOrder(ctx context.Context, userID uint64, orderID string) (Refund, bool, error) {
	var payment paymentRecord
	if err := r.db.WithContext(ctx).Where("order_id = ? AND user_id = ?", orderID, userID).First(&payment).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Refund{}, false, ErrNotFound
		}
		return Refund{}, false, err
	}
	var record refundRecord
	if err := r.db.WithContext(ctx).Where("order_id = ?", orderID).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Refund{}, false, nil
		}
		return Refund{}, false, err
	}
	value := refundFromRecord(record)
	value.UserID = userID
	return value, true, nil
}

func (r *MySQLRepository) CreateRefund(ctx context.Context, value Refund, now time.Time) (Refund, bool, error) {
	var result Refund
	reused := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing refundRecord
		if err := tx.Where("order_id = ?", value.OrderID).First(&existing).Error; err == nil {
			result = refundFromRecord(existing)
			result.UserID = value.UserID
			reused = true
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		record := refundRecord{RefundNo: value.RefundNo, OrderID: value.OrderID, PaymentNo: value.PaymentNo, AmountCents: value.AmountCents, Reason: value.Reason, Status: value.Status, CreatedAt: now, CompletedAt: &now}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		if r.eventSink != nil {
			event, err := messaging.NewEvent(fmt.Sprintf("payment.refunded:%s", value.RefundNo), contracts.EventPaymentRefunded, "order", value.OrderID, contracts.PaymentRefundedPayload{RefundNo: value.RefundNo, PaymentNo: value.PaymentNo, OrderID: value.OrderID, UserID: value.UserID, AmountCents: value.AmountCents, Reason: value.Reason}, now)
			if err != nil {
				return err
			}
			if transactional, ok := r.eventSink.(interface {
				AppendTx(context.Context, *gorm.DB, contracts.EventEnvelope, amqp.Table) error
			}); ok {
				if err := transactional.AppendTx(ctx, tx, event, nil); err != nil {
					return err
				}
			} else if err := r.eventSink.AppendEvent(ctx, event, nil); err != nil {
				return err
			}
		}
		result = refundFromRecord(record)
		result.UserID = value.UserID
		return nil
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		var existing refundRecord
		if loadErr := r.db.WithContext(ctx).Where("order_id = ?", value.OrderID).First(&existing).Error; loadErr != nil {
			return Refund{}, false, loadErr
		}
		result = refundFromRecord(existing)
		result.UserID = value.UserID
		return result, true, nil
	}
	return result, reused, err
}

var _ Repository = (*MySQLRepository)(nil)
