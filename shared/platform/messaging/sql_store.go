package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"seckill-mall/shared/contracts"
)

// SQLStore 将 Outbox/Inbox 状态限制在调用方提供的服务表内。
type SQLStore struct {
	db          *gorm.DB
	outboxTable string
	inboxTable  string
}

// SQLOutboxStore 是单个服务的 SQL Outbox 适配器。
type SQLOutboxStore struct{ store *SQLStore }

// SQLInboxStore 是单个服务的 SQL Inbox 适配器。
type SQLInboxStore struct{ store *SQLStore }

type sqlOutboxRecord struct {
	ID            uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	EventID       string    `gorm:"column:event_id"`
	AggregateType string    `gorm:"column:aggregate_type"`
	AggregateID   string    `gorm:"column:aggregate_id"`
	EventType     string    `gorm:"column:event_type"`
	EventVersion  int       `gorm:"column:event_version"`
	Payload       []byte    `gorm:"column:payload;type:json"`
	Headers       []byte    `gorm:"column:headers;type:json"`
	Status        string    `gorm:"column:status"`
	Attempts      int       `gorm:"column:attempts"`
	NextRetryAt   time.Time `gorm:"column:next_retry_at"`
	LastError     string    `gorm:"column:last_error"`
	CreatedAt     time.Time `gorm:"column:created_at"`
	UpdatedAt     time.Time `gorm:"column:updated_at"`
}

type sqlInboxRecord struct {
	ID           uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	Consumer     string     `gorm:"column:consumer"`
	EventID      string     `gorm:"column:event_id"`
	EventType    string     `gorm:"column:event_type"`
	EventVersion int        `gorm:"column:event_version"`
	Status       string     `gorm:"column:status"`
	Attempts     int        `gorm:"column:attempts"`
	LastError    string     `gorm:"column:last_error"`
	ProcessedAt  *time.Time `gorm:"column:processed_at"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
	UpdatedAt    time.Time  `gorm:"column:updated_at"`
}

// NewSQLStore 创建指定服务的 SQL 消息存储。
func NewSQLStore(db *gorm.DB, outboxTable, inboxTable string) (*SQLStore, error) {
	if db == nil {
		return nil, errors.New("message db is required")
	}
	if err := ValidateTableName(outboxTable); err != nil {
		return nil, err
	}
	if err := ValidateTableName(inboxTable); err != nil {
		return nil, err
	}
	return &SQLStore{db: db, outboxTable: outboxTable, inboxTable: inboxTable}, nil
}

func (s *SQLStore) Outbox() *SQLOutboxStore { return &SQLOutboxStore{store: s} }
func (s *SQLStore) Inbox() *SQLInboxStore   { return &SQLInboxStore{store: s} }

// Append 在调用方事务中追加事件；tx 必须属于当前服务自己的数据库。
func (s *SQLStore) Append(tx *gorm.DB, event OutboxEvent) error {
	if tx == nil {
		return errors.New("message transaction is required")
	}
	return tx.Table(s.outboxTable).Create(&sqlOutboxRecord{
		EventID: event.EventID, AggregateType: event.AggregateType, AggregateID: event.AggregateID,
		EventType: event.EventType, EventVersion: event.EventVersion, Payload: event.Payload,
		Headers: event.HeadersBytes(), Status: StatusPending, Attempts: 0,
		NextRetryAt: event.NextRetryAt, CreatedAt: event.CreatedAt, UpdatedAt: event.UpdatedAt,
	}).Error
}

// AppendEvent 在独立事务中写入事件；领域事务应优先调用 AppendTx。
func (s *SQLStore) AppendEvent(ctx context.Context, event contracts.EventEnvelope, headers amqp.Table) error {
	record, err := outboxEventFromEnvelope(event, headers)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return s.Append(tx, record) })
}

// AppendTx 将事件与调用方领域状态变更放入同一事务。
func (s *SQLStore) AppendTx(ctx context.Context, tx *gorm.DB, event contracts.EventEnvelope, headers amqp.Table) error {
	if tx == nil {
		return errors.New("message transaction is required")
	}
	record, err := outboxEventFromEnvelope(event, headers)
	if err != nil {
		return err
	}
	return s.Append(tx.WithContext(ctx), record)
}

func outboxEventFromEnvelope(event contracts.EventEnvelope, headers amqp.Table) (OutboxEvent, error) {
	if err := contracts.ValidateEventPayload(event); err != nil {
		return OutboxEvent{}, err
	}
	now := time.Now().UTC()
	return OutboxEvent{EventID: event.EventID, AggregateType: event.AggregateType, AggregateID: event.AggregateID, EventType: event.EventType, EventVersion: event.EventVersion, Payload: append([]byte(nil), event.Payload...), Headers: headers, Status: StatusPending, NextRetryAt: now, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *SQLOutboxStore) Claim(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]OutboxEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	if lease <= 0 {
		lease = 30 * time.Second
	}
	var records []sqlOutboxRecord
	err := s.store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Table(s.store.outboxTable).
			Where("status IN ? AND next_retry_at <= ?", []string{StatusPending, StatusPublishing}, now).
			Order("next_retry_at ASC").Limit(limit).
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		if err := query.Find(&records).Error; err != nil {
			return err
		}
		if len(records) == 0 {
			return nil
		}
		ids := make([]string, 0, len(records))
		for _, record := range records {
			ids = append(ids, record.EventID)
		}
		return tx.Table(s.store.outboxTable).Where("event_id IN ?", ids).Updates(map[string]any{
			"status": StatusPublishing, "attempts": gorm.Expr("attempts + 1"), "next_retry_at": now.Add(lease), "updated_at": now,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	items := make([]OutboxEvent, 0, len(records))
	for _, record := range records {
		items = append(items, claimedOutboxFromRecord(record, now, lease))
	}
	return items, nil
}

func (s *SQLOutboxStore) MarkPublished(ctx context.Context, eventID string) error {
	return s.store.db.WithContext(ctx).Table(s.store.outboxTable).Where("event_id = ?", eventID).Updates(map[string]any{"status": StatusPublished, "last_error": "", "updated_at": time.Now().UTC()}).Error
}

func (s *SQLOutboxStore) MarkRetry(ctx context.Context, eventID string, nextRetryAt time.Time, reason string) error {
	return s.store.db.WithContext(ctx).Table(s.store.outboxTable).Where("event_id = ?", eventID).Updates(map[string]any{"status": StatusPending, "next_retry_at": nextRetryAt, "last_error": truncateError(reason), "updated_at": time.Now().UTC()}).Error
}

func (s *SQLOutboxStore) MarkFailed(ctx context.Context, eventID string, reason string) error {
	return s.store.db.WithContext(ctx).Table(s.store.outboxTable).Where("event_id = ?", eventID).Updates(map[string]any{"status": StatusFailed, "last_error": truncateError(reason), "updated_at": time.Now().UTC()}).Error
}

func (s *SQLInboxStore) Claim(ctx context.Context, consumer string, event contracts.EventEnvelope, lease time.Duration) (InboxClaim, error) {
	if lease <= 0 {
		lease = 30 * time.Second
	}
	now := time.Now().UTC()
	record := sqlInboxRecord{Consumer: consumer, EventID: event.EventID, EventType: event.EventType, EventVersion: event.EventVersion, Status: StatusProcessing, Attempts: 1, CreatedAt: now, UpdatedAt: now}
	err := s.store.db.WithContext(ctx).Table(s.store.inboxTable).Create(&record).Error
	if err == nil {
		return InboxClaim{Claimed: true, Attempts: 1}, nil
	}
	if !errors.Is(err, gorm.ErrDuplicatedKey) {
		return InboxClaim{}, err
	}
	var existing sqlInboxRecord
	if err := s.store.db.WithContext(ctx).Table(s.store.inboxTable).Where("consumer = ? AND event_id = ?", consumer, event.EventID).First(&existing).Error; err != nil {
		return InboxClaim{}, err
	}
	if existing.Status == StatusProcessed {
		return InboxClaim{Processed: true, Attempts: existing.Attempts}, nil
	}
	if existing.UpdatedAt.After(now.Add(-lease)) {
		return InboxClaim{Attempts: existing.Attempts}, nil
	}
	result := s.store.db.WithContext(ctx).Table(s.store.inboxTable).Where("consumer = ? AND event_id = ? AND updated_at = ?", consumer, event.EventID, existing.UpdatedAt).Updates(map[string]any{"status": StatusProcessing, "attempts": gorm.Expr("attempts + 1"), "last_error": "", "updated_at": now})
	if result.Error != nil {
		return InboxClaim{}, result.Error
	}
	return InboxClaim{Claimed: result.RowsAffected == 1, Attempts: existing.Attempts + 1}, nil
}

func (s *SQLInboxStore) MarkProcessed(ctx context.Context, consumer, eventID string) error {
	now := time.Now().UTC()
	return s.store.db.WithContext(ctx).Table(s.store.inboxTable).Where("consumer = ? AND event_id = ?", consumer, eventID).Updates(map[string]any{"status": StatusProcessed, "processed_at": now, "updated_at": now, "last_error": ""}).Error
}

func (s *SQLInboxStore) MarkFailed(ctx context.Context, consumer, eventID, reason string) error {
	return s.store.db.WithContext(ctx).Table(s.store.inboxTable).Where("consumer = ? AND event_id = ?", consumer, eventID).Updates(map[string]any{"status": StatusProcessing, "last_error": truncateError(reason), "updated_at": time.Now().UTC()}).Error
}

func outboxFromRecord(record sqlOutboxRecord) OutboxEvent {
	return OutboxEvent{ID: record.ID, EventID: record.EventID, AggregateType: record.AggregateType, AggregateID: record.AggregateID, EventType: record.EventType, EventVersion: record.EventVersion, Payload: append([]byte(nil), record.Payload...), Headers: parseHeaders(record.Headers), Status: record.Status, Attempts: record.Attempts, NextRetryAt: record.NextRetryAt, LastError: record.LastError, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
}

func claimedOutboxFromRecord(record sqlOutboxRecord, now time.Time, lease time.Duration) OutboxEvent {
	record.Status = StatusPublishing
	record.Attempts++
	record.NextRetryAt = now.Add(lease)
	record.UpdatedAt = now
	return outboxFromRecord(record)
}

func (e OutboxEvent) HeadersBytes() []byte {
	if len(e.Headers) == 0 {
		return nil
	}
	body, _ := json.Marshal(e.Headers)
	return body
}

func parseHeaders(raw []byte) amqp.Table {
	if len(raw) == 0 {
		return nil
	}
	var headers amqp.Table
	if err := json.Unmarshal(raw, &headers); err != nil {
		return nil
	}
	return headers
}

func truncateError(reason string) string {
	if len(reason) > 255 {
		return reason[:255]
	}
	return reason
}
