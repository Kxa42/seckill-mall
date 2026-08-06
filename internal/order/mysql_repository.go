package order

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MySQLRepository 只访问 commerce_orders、order_items、order_status_history 和 Order 操作表。
type MySQLRepository struct{ db *gorm.DB }

type orderRecord struct {
	ID               uint64 `gorm:"column:id;primaryKey"`
	OrderID          string `gorm:"column:order_id;uniqueIndex"`
	UserID           uint64 `gorm:"column:user_id"`
	OrderType        string `gorm:"column:order_type"`
	Status           string `gorm:"column:status"`
	TotalAmountCents int64  `gorm:"column:total_amount_cents"`
	IdempotencyKey   string `gorm:"column:idempotency_key"`
	AddressSnapshot  string `gorm:"column:address_snapshot;type:json"`
	RequestDigest    string `gorm:"column:request_digest"`
	ExpiresAt        time.Time
	PaidAt           *time.Time
	ShippedAt        *time.Time
	CompletedAt      *time.Time
	CanceledAt       *time.Time
	RefundedAt       *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (orderRecord) TableName() string { return "commerce_orders" }

type orderItemRecord struct {
	ID             uint64 `gorm:"column:id;primaryKey"`
	OrderID        string `gorm:"column:order_id"`
	SKUID          uint64 `gorm:"column:sku_id"`
	SKUCode        string `gorm:"column:sku_code"`
	SKUName        string `gorm:"column:sku_name"`
	UnitPriceCents int64  `gorm:"column:unit_price_cents"`
	Quantity       int32  `gorm:"column:quantity"`
	SubtotalCents  int64  `gorm:"column:subtotal_cents"`
	ReservationID  string `gorm:"column:reservation_id"`
	CreatedAt      time.Time
}

func (orderItemRecord) TableName() string { return "order_items" }

type statusRecord struct {
	ID         uint64 `gorm:"column:id;primaryKey"`
	OrderID    string
	FromStatus string
	ToStatus   string
	Reason     string
	ActorType  string
	ActorID    uint64
	CreatedAt  time.Time
}

func (statusRecord) TableName() string { return "order_status_history" }

type operationRecord struct {
	ID            string `gorm:"column:id;primaryKey"`
	OrderID       string
	Kind          string
	ReservationID string
	Status        string
	Attempts      int
	LastError     string
	NextRetryAt   time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (operationRecord) TableName() string { return "order_operations" }

type intentRecord struct {
	ID             uint64    `gorm:"column:id;primaryKey"`
	IntentID       string    `gorm:"column:intent_id"`
	UserID         uint64    `gorm:"column:user_id"`
	IdempotencyKey string    `gorm:"column:idempotency_key"`
	RequestDigest  string    `gorm:"column:request_digest"`
	OrderID        string    `gorm:"column:order_id"`
	Status         string    `gorm:"column:status"`
	Attempts       int       `gorm:"column:attempts"`
	Payload        string    `gorm:"column:payload;type:json"`
	LastError      string    `gorm:"column:last_error"`
	NextRetryAt    time.Time `gorm:"column:next_retry_at"`
	CreatedAt      time.Time `gorm:"column:created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at"`
}

func (intentRecord) TableName() string { return "order_create_intents" }

func NewMySQLRepository(db *gorm.DB) (*MySQLRepository, error) {
	if db == nil {
		return nil, errors.New("order db is required")
	}
	return &MySQLRepository{db: db}, nil
}

func (r *MySQLRepository) FindByIdempotency(ctx context.Context, userID uint64, key string) (Order, bool, error) {
	var record orderRecord
	err := r.db.WithContext(ctx).Where("user_id = ? AND idempotency_key = ?", userID, key).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Order{}, false, nil
	}
	if err != nil {
		return Order{}, false, err
	}
	value, err := r.loadOrder(r.db.WithContext(ctx), userID, record.OrderID)
	return value, err == nil, err
}

func (r *MySQLRepository) FindCreateIntent(ctx context.Context, userID uint64, key string) (CreateIntent, bool, error) {
	var record intentRecord
	err := r.db.WithContext(ctx).Where("user_id = ? AND idempotency_key = ?", userID, key).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return CreateIntent{}, false, nil
	}
	if err != nil {
		return CreateIntent{}, false, err
	}
	return intentFromRecord(record), true, nil
}

func (r *MySQLRepository) SaveCreateIntent(ctx context.Context, value CreateIntent) error {
	record := intentRecord{IntentID: value.IntentID, UserID: value.UserID, IdempotencyKey: value.IdempotencyKey, RequestDigest: value.RequestDigest, OrderID: value.OrderID, Status: value.Status, Attempts: value.Attempts, Payload: value.Payload, LastError: value.LastError, NextRetryAt: value.NextRetryAt, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "idempotency_key"}}, DoUpdates: clause.AssignmentColumns([]string{"request_digest", "order_id", "status", "attempts", "payload", "last_error", "next_retry_at", "updated_at"})}).Create(&record).Error
}

func (r *MySQLRepository) UpdateCreateIntent(ctx context.Context, value CreateIntent) error {
	return r.db.WithContext(ctx).Model(&intentRecord{}).Where("user_id = ? AND idempotency_key = ?", value.UserID, value.IdempotencyKey).Updates(map[string]any{"status": value.Status, "attempts": value.Attempts, "last_error": value.LastError, "next_retry_at": value.NextRetryAt, "updated_at": value.UpdatedAt}).Error
}

func (r *MySQLRepository) ListPendingCreateIntents(ctx context.Context, now time.Time, limit int) ([]CreateIntent, error) {
	var records []intentRecord
	if err := r.db.WithContext(ctx).Where("status IN ? AND attempts < ? AND next_retry_at <= ?", []string{CreateIntentStarted, CreateIntentFailed}, maxCreateIntentAttempts, now).Order("next_retry_at ASC").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	items := make([]CreateIntent, 0, len(records))
	for _, record := range records {
		items = append(items, intentFromRecord(record))
	}
	return items, nil
}

func (r *MySQLRepository) Create(ctx context.Context, value Order) (Order, bool, error) {
	var created Order
	reused := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing orderRecord
		if err := tx.Where("user_id = ? AND idempotency_key = ?", value.UserID, value.IdempotencyKey).First(&existing).Error; err == nil {
			reused = true
			var loadErr error
			created, loadErr = r.loadOrder(tx, value.UserID, existing.OrderID)
			if loadErr != nil {
				return loadErr
			}
			if created.RequestDigest != value.RequestDigest {
				return NewError(CodeConflict, "Idempotency-Key 对应的请求参数不一致", nil)
			}
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		addressJSON, err := json.Marshal(value.AddressSnapshot)
		if err != nil {
			return err
		}
		record := orderRecord{OrderID: value.OrderID, UserID: value.UserID, OrderType: value.OrderType, Status: value.Status, TotalAmountCents: value.TotalAmountCents, IdempotencyKey: value.IdempotencyKey, AddressSnapshot: string(addressJSON), RequestDigest: value.RequestDigest, ExpiresAt: value.ExpiresAt, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		items := make([]orderItemRecord, 0, len(value.Items))
		for _, item := range value.Items {
			items = append(items, orderItemRecord{OrderID: value.OrderID, SKUID: item.SKUID, SKUCode: item.SKUCode, SKUName: item.SKUName, UnitPriceCents: item.UnitPriceCents, Quantity: item.Quantity, SubtotalCents: item.SubtotalCents, ReservationID: item.ReservationID, CreatedAt: value.CreatedAt})
		}
		if err := tx.Create(&items).Error; err != nil {
			return err
		}
		for _, history := range value.StatusHistory {
			if err := tx.Create(&statusRecord{OrderID: value.OrderID, FromStatus: history.FromStatus, ToStatus: history.ToStatus, Reason: history.Reason, ActorType: history.ActorType, ActorID: history.ActorID, CreatedAt: history.CreatedAt}).Error; err != nil {
				return err
			}
		}
		created = value
		return nil
	})
	if err != nil {
		var existing orderRecord
		if findErr := r.db.WithContext(ctx).Where("user_id = ? AND idempotency_key = ?", value.UserID, value.IdempotencyKey).First(&existing).Error; findErr == nil {
			loaded, loadErr := r.loadOrder(r.db.WithContext(ctx), value.UserID, existing.OrderID)
			if loadErr == nil && loaded.RequestDigest == value.RequestDigest {
				return loaded, true, nil
			}
		}
		return Order{}, false, err
	}
	return created, reused, nil
}

func (r *MySQLRepository) Get(ctx context.Context, userID uint64, orderID string) (Order, error) {
	return r.loadOrder(r.db.WithContext(ctx), userID, orderID)
}

func (r *MySQLRepository) List(ctx context.Context, userID uint64, offset, limit int) ([]Order, int64, error) {
	query := r.db.WithContext(ctx).Model(&orderRecord{}).Where("user_id = ?", userID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var records []orderRecord
	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&records).Error; err != nil {
		return nil, 0, err
	}
	items := make([]Order, 0, len(records))
	for _, record := range records {
		value, err := r.loadOrder(r.db.WithContext(ctx), userID, record.OrderID)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, value)
	}
	return items, total, nil
}

func (r *MySQLRepository) ListExpired(ctx context.Context, now time.Time, limit int) ([]Order, error) {
	var records []orderRecord
	if err := r.db.WithContext(ctx).Where("status = ? AND expires_at <= ?", StatusPendingPayment, now).Order("expires_at ASC").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	items := make([]Order, 0, len(records))
	for _, record := range records {
		value, err := r.loadOrder(r.db.WithContext(ctx), record.UserID, record.OrderID)
		if err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	return items, nil
}

func (r *MySQLRepository) Transition(ctx context.Context, orderID string, userID uint64, target, reason, actorType string, actorID uint64, now time.Time) (Order, error) {
	var result Order
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record orderRecord
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("order_id = ?", orderID)
		if userID != 0 {
			query = query.Where("user_id = ?", userID)
		}
		if err := query.First(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return NewError(CodeNotFound, "订单不存在", nil)
			}
			return err
		}
		if record.Status == target {
			var err error
			result, err = r.loadOrder(tx, record.UserID, orderID)
			return err
		}
		if !canTransition(record.Status, target) {
			return NewError(CodeInvalidTransition, "当前订单状态不能执行该操作", nil)
		}
		if err := tx.Model(&record).Updates(map[string]any{"status": target, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Create(&statusRecord{OrderID: orderID, FromStatus: record.Status, ToStatus: target, Reason: reason, ActorType: actorType, ActorID: actorID, CreatedAt: now}).Error; err != nil {
			return err
		}
		var err error
		result, err = r.loadOrder(tx, record.UserID, orderID)
		return err
	})
	return result, err
}

func (r *MySQLRepository) SaveOperation(ctx context.Context, value Operation) error {
	record := operationRecord{ID: value.ID, OrderID: value.OrderID, Kind: value.Kind, ReservationID: value.ReservationID, Status: value.Status, Attempts: value.Attempts, LastError: value.LastError, NextRetryAt: value.NextRetryAt, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"status", "attempts", "last_error", "next_retry_at", "updated_at"})}).Create(&record).Error
}

func (r *MySQLRepository) ListPendingOperations(ctx context.Context, now time.Time, limit int) ([]Operation, error) {
	var records []operationRecord
	if err := r.db.WithContext(ctx).Where("status = ? AND next_retry_at <= ?", OperationPending, now).Order("next_retry_at ASC").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	items := make([]Operation, 0, len(records))
	for _, record := range records {
		items = append(items, operationFromRecord(record))
	}
	return items, nil
}

func (r *MySQLRepository) UpdateOperation(ctx context.Context, value Operation) error {
	return r.db.WithContext(ctx).Model(&operationRecord{}).Where("id = ?", value.ID).Updates(map[string]any{"status": value.Status, "attempts": value.Attempts, "last_error": value.LastError, "next_retry_at": value.NextRetryAt, "updated_at": value.UpdatedAt}).Error
}

func (r *MySQLRepository) loadOrder(db *gorm.DB, userID uint64, orderID string) (Order, error) {
	var record orderRecord
	query := db.Where("order_id = ?", orderID)
	if userID != 0 {
		query = query.Where("user_id = ?", userID)
	}
	if err := query.First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Order{}, NewError(CodeNotFound, "订单不存在", nil)
		}
		return Order{}, err
	}
	var address Address
	if err := json.Unmarshal([]byte(record.AddressSnapshot), &address); err != nil {
		return Order{}, err
	}
	var itemRecords []orderItemRecord
	if err := db.Where("order_id = ?", orderID).Order("id ASC").Find(&itemRecords).Error; err != nil {
		return Order{}, err
	}
	var historyRecords []statusRecord
	if err := db.Where("order_id = ?", orderID).Order("id ASC").Find(&historyRecords).Error; err != nil {
		return Order{}, err
	}
	items := make([]Item, 0, len(itemRecords))
	for _, item := range itemRecords {
		items = append(items, Item{SKUID: item.SKUID, SKUCode: item.SKUCode, SKUName: item.SKUName, UnitPriceCents: item.UnitPriceCents, Quantity: item.Quantity, SubtotalCents: item.SubtotalCents, ReservationID: item.ReservationID})
	}
	history := make([]StatusHistory, 0, len(historyRecords))
	for _, item := range historyRecords {
		history = append(history, StatusHistory{FromStatus: item.FromStatus, ToStatus: item.ToStatus, Reason: item.Reason, ActorType: item.ActorType, ActorID: item.ActorID, CreatedAt: item.CreatedAt})
	}
	return Order{OrderID: record.OrderID, UserID: record.UserID, OrderType: record.OrderType, Status: record.Status, TotalAmountCents: record.TotalAmountCents, IdempotencyKey: record.IdempotencyKey, RequestDigest: record.RequestDigest, AddressSnapshot: address, Items: items, StatusHistory: history, ExpiresAt: record.ExpiresAt, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}, nil
}

func operationFromRecord(record operationRecord) Operation {
	return Operation{ID: record.ID, OrderID: record.OrderID, Kind: record.Kind, ReservationID: record.ReservationID, Status: record.Status, Attempts: record.Attempts, LastError: record.LastError, NextRetryAt: record.NextRetryAt, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
}

func intentFromRecord(record intentRecord) CreateIntent {
	return CreateIntent{IntentID: record.IntentID, UserID: record.UserID, IdempotencyKey: record.IdempotencyKey, RequestDigest: record.RequestDigest, OrderID: record.OrderID, Status: record.Status, Attempts: record.Attempts, Payload: record.Payload, LastError: record.LastError, NextRetryAt: record.NextRetryAt, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
}

var _ Repository = (*MySQLRepository)(nil)
