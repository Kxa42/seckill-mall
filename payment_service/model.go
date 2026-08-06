// Package paymentservice 定义支付与退款服务的模型和数据边界。
package paymentservice

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"seckill-mall/common/orderclient"
)

var (
	ErrInvalidRequest    = errors.New("payment request is invalid")
	ErrUnauthorized      = errors.New("payment callback is unauthorized")
	ErrNotFound          = errors.New("payment resource not found")
	ErrConflict          = errors.New("payment request conflicts")
	ErrInvalidTransition = errors.New("payment transition invalid")
)

const (
	StatusPending   = "pending"
	StatusSucceeded = "succeeded"
	RefundSucceeded = "succeeded"
)

type Payment struct {
	PaymentNo, OrderID, Provider, Status, CallbackRef string
	UserID                                            uint64
	AmountCents                                       int64
	PaidAt, CreatedAt, UpdatedAt                      time.Time
}
type Refund struct {
	RefundNo, OrderID, PaymentNo, Reason, Status string
	UserID                                       uint64
	AmountCents                                  int64
	CreatedAt, CompletedAt                       time.Time
}

type Repository interface {
	Create(ctx context.Context, payment Payment, now time.Time) (Payment, bool, error)
	Get(ctx context.Context, paymentNo string) (Payment, error)
	MarkSucceeded(ctx context.Context, paymentNo, callbackRef string, now time.Time) (Payment, bool, error)
	CreateRefund(ctx context.Context, refund Refund, now time.Time) (Refund, bool, error)
}

type Service struct {
	repository Repository
	orders     orderclient.Client
	secret     []byte
	now        func() time.Time
	newID      func(string) (string, error)
}

func NewService(repository Repository, orders orderclient.Client, secret string) (*Service, error) {
	if repository == nil || orders == nil {
		return nil, errors.New("payment repository and order client are required")
	}
	if len(strings.TrimSpace(secret)) < 32 {
		return nil, errors.New("payment secret length must be at least 32")
	}
	return &Service{repository: repository, orders: orders, secret: []byte(secret), now: time.Now, newID: randomID}, nil
}

func (s *Service) Create(ctx context.Context, userID uint64, orderID string) (Payment, string, bool, error) {
	if userID == 0 || strings.TrimSpace(orderID) == "" {
		return Payment{}, "", false, ErrInvalidRequest
	}
	order, err := s.orders.Get(ctx, userID, orderID)
	if err != nil {
		return Payment{}, "", false, err
	}
	if order.UserID != userID || order.Status != "pending_payment" {
		return Payment{}, "", false, ErrInvalidTransition
	}
	paymentNo, err := s.newID("pay")
	if err != nil {
		return Payment{}, "", false, err
	}
	payment, reused, err := s.repository.Create(ctx, Payment{PaymentNo: paymentNo, OrderID: order.OrderID, UserID: userID, AmountCents: order.TotalAmountCents, Provider: "mock", Status: StatusPending, CreatedAt: s.now().UTC(), UpdatedAt: s.now().UTC()}, s.now().UTC())
	if err != nil {
		return Payment{}, "", false, err
	}
	return payment, s.sign(payment.PaymentNo), reused, nil
}

func (s *Service) Callback(ctx context.Context, paymentNo, callbackRef, signature string) (Payment, orderclient.Transition, bool, error) {
	paymentNo, callbackRef = strings.TrimSpace(paymentNo), strings.TrimSpace(callbackRef)
	if paymentNo == "" || callbackRef == "" {
		return Payment{}, orderclient.Transition{}, false, ErrInvalidRequest
	}
	if !hmac.Equal([]byte(strings.TrimSpace(signature)), []byte(s.sign(paymentNo))) {
		return Payment{}, orderclient.Transition{}, false, ErrUnauthorized
	}
	payment, err := s.repository.Get(ctx, paymentNo)
	if err != nil {
		return Payment{}, orderclient.Transition{}, false, err
	}
	if payment.Status == StatusSucceeded {
		if payment.CallbackRef != callbackRef {
			return Payment{}, orderclient.Transition{}, false, ErrConflict
		}
		transition, err := s.orders.ConfirmPayment(ctx, payment.UserID, payment.OrderID, payment.PaymentNo, callbackRef)
		if err != nil {
			return Payment{}, orderclient.Transition{}, false, err
		}
		return payment, transition, true, nil
	}
	updated, reused, err := s.repository.MarkSucceeded(ctx, paymentNo, callbackRef, s.now().UTC())
	if err != nil {
		return Payment{}, orderclient.Transition{}, false, err
	}
	transition, err := s.orders.ConfirmPayment(ctx, payment.UserID, payment.OrderID, payment.PaymentNo, callbackRef)
	if err != nil {
		return Payment{}, orderclient.Transition{}, false, err
	}
	return updated, transition, reused, nil
}

func (s *Service) Refund(ctx context.Context, userID uint64, orderID, reason string) (Refund, orderclient.Transition, bool, error) {
	reason = strings.TrimSpace(reason)
	if userID == 0 || orderID == "" || reason == "" || len([]rune(reason)) > 255 {
		return Refund{}, orderclient.Transition{}, false, ErrInvalidRequest
	}
	order, err := s.orders.Get(ctx, userID, orderID)
	if err != nil {
		return Refund{}, orderclient.Transition{}, false, err
	}
	if order.UserID != userID {
		return Refund{}, orderclient.Transition{}, false, ErrNotFound
	}
	existing, exists, err := s.findRefundByOrder(ctx, userID, orderID)
	if err != nil {
		return Refund{}, orderclient.Transition{}, false, err
	}
	if exists {
		transition := orderclient.Transition{OrderID: orderID, Status: "refunded", Reused: true}
		return existing, transition, true, nil
	}
	payment, err := s.findSucceededPayment(ctx, orderID)
	if err != nil {
		return Refund{}, orderclient.Transition{}, false, err
	}
	refundNo, err := s.newID("ref")
	if err != nil {
		return Refund{}, orderclient.Transition{}, false, err
	}
	transition, err := s.orders.Refund(ctx, userID, orderID, refundNo, reason)
	if err != nil {
		return Refund{}, orderclient.Transition{}, false, err
	}
	refund, reused, err := s.repository.CreateRefund(ctx, Refund{RefundNo: refundNo, OrderID: orderID, PaymentNo: payment.PaymentNo, UserID: userID, AmountCents: payment.AmountCents, Reason: reason, Status: RefundSucceeded, CreatedAt: s.now().UTC(), CompletedAt: s.now().UTC()}, s.now().UTC())
	if err != nil {
		return Refund{}, orderclient.Transition{}, false, err
	}
	return refund, transition, reused, nil
}

type refundFinder interface {
	FindRefundByOrder(ctx context.Context, userID uint64, orderID string) (Refund, bool, error)
}

func (s *Service) findSucceededPayment(ctx context.Context, orderID string) (Payment, error) {
	finder, ok := s.repository.(interface {
		FindSucceededPayment(context.Context, string) (Payment, error)
	})
	if !ok {
		return Payment{}, ErrInvalidTransition
	}
	return finder.FindSucceededPayment(ctx, orderID)
}
func (s *Service) findRefundByOrder(ctx context.Context, userID uint64, orderID string) (Refund, bool, error) {
	finder, ok := s.repository.(refundFinder)
	if !ok {
		return Refund{}, false, ErrInvalidTransition
	}
	return finder.FindRefundByOrder(ctx, userID, orderID)
}

func (s *Service) sign(paymentNo string) string {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(paymentNo))
	return hex.EncodeToString(mac.Sum(nil))
}
func randomID(prefix string) (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(buffer)), nil
}

type MemoryRepository struct {
	mu             sync.RWMutex
	next           uint64
	payments       map[string]Payment
	paymentByOrder map[string]string
	callbacks      map[string]string
	refunds        map[string]Refund
	refundByOrder  map[string]string
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{next: 1, payments: make(map[string]Payment), paymentByOrder: make(map[string]string), callbacks: make(map[string]string), refunds: make(map[string]Refund), refundByOrder: make(map[string]string)}
}
func (r *MemoryRepository) Create(_ context.Context, value Payment, _ time.Time) (Payment, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if no, ok := r.paymentByOrder[value.OrderID]; ok {
		return r.payments[no], true, nil
	}
	r.payments[value.PaymentNo] = value
	r.paymentByOrder[value.OrderID] = value.PaymentNo
	return value, false, nil
}
func (r *MemoryRepository) Get(_ context.Context, paymentNo string) (Payment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.payments[paymentNo]
	if !ok {
		return Payment{}, ErrNotFound
	}
	return value, nil
}
func (r *MemoryRepository) MarkSucceeded(_ context.Context, paymentNo, callbackRef string, now time.Time) (Payment, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.payments[paymentNo]
	if !ok {
		return Payment{}, false, ErrNotFound
	}
	if existing, used := r.callbacks[callbackRef]; used && existing != paymentNo {
		return Payment{}, false, ErrConflict
	}
	if value.Status == StatusSucceeded {
		if value.CallbackRef != callbackRef {
			return Payment{}, false, ErrConflict
		}
		return value, true, nil
	}
	value.Status, value.CallbackRef, value.PaidAt, value.UpdatedAt = StatusSucceeded, callbackRef, now, now
	r.payments[paymentNo] = value
	r.callbacks[callbackRef] = paymentNo
	return value, false, nil
}
func (r *MemoryRepository) FindSucceededPayment(_ context.Context, orderID string) (Payment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	no, ok := r.paymentByOrder[orderID]
	if !ok || r.payments[no].Status != StatusSucceeded {
		return Payment{}, ErrInvalidTransition
	}
	return r.payments[no], nil
}
func (r *MemoryRepository) FindRefundByOrder(_ context.Context, userID uint64, orderID string) (Refund, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	no, ok := r.refundByOrder[orderID]
	if !ok {
		return Refund{}, false, nil
	}
	value := r.refunds[no]
	if value.UserID != userID {
		return Refund{}, false, ErrNotFound
	}
	return value, true, nil
}
func (r *MemoryRepository) CreateRefund(_ context.Context, value Refund, _ time.Time) (Refund, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if no, ok := r.refundByOrder[value.OrderID]; ok {
		return r.refunds[no], true, nil
	}
	r.refunds[value.RefundNo] = value
	r.refundByOrder[value.OrderID] = value.RefundNo
	return value, false, nil
}

type MySQLRepository struct{ db *gorm.DB }
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

var _ Repository = (*MemoryRepository)(nil)
var _ Repository = (*MySQLRepository)(nil)
