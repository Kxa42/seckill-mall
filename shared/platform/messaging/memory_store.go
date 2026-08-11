package messaging

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"seckill-mall/shared/contracts"
)

// MemoryStore 提供无需外部基础设施的 Outbox/Inbox 实现，用于单元测试和内存 E2E。
type MemoryStore struct {
	mu     sync.Mutex
	nextID uint64
	outbox map[string]OutboxEvent
	inbox  map[string]memoryInboxRecord
}

type MemoryOutboxStore struct{ store *MemoryStore }
type MemoryInboxStore struct{ store *MemoryStore }

type memoryInboxRecord struct {
	consumer string
	eventID  string
	status   string
	attempts int
	updated  time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{nextID: 1, outbox: make(map[string]OutboxEvent), inbox: make(map[string]memoryInboxRecord)}
}

func (s *MemoryStore) Outbox() *MemoryOutboxStore { return &MemoryOutboxStore{store: s} }
func (s *MemoryStore) Inbox() *MemoryInboxStore   { return &MemoryInboxStore{store: s} }

func (s *MemoryStore) AppendEvent(_ context.Context, event contracts.EventEnvelope, headers amqp.Table) error {
	if err := contracts.ValidateEventPayload(event); err != nil {
		return err
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.outbox[event.EventID]; exists {
		return nil
	}
	s.outbox[event.EventID] = OutboxEvent{ID: s.nextID, EventID: event.EventID, AggregateType: event.AggregateType, AggregateID: event.AggregateID, EventType: event.EventType, EventVersion: event.EventVersion, Payload: append([]byte(nil), event.Payload...), Headers: cloneAMQPTable(headers), Status: StatusPending, NextRetryAt: now, CreatedAt: now, UpdatedAt: now}
	s.nextID++
	return nil
}

func (s *MemoryOutboxStore) Claim(_ context.Context, now time.Time, limit int, lease time.Duration) ([]OutboxEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	if lease <= 0 {
		lease = 30 * time.Second
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	candidates := make([]OutboxEvent, 0, len(s.store.outbox))
	for _, value := range s.store.outbox {
		if (value.Status != StatusPending && value.Status != StatusPublishing) || value.NextRetryAt.After(now) {
			continue
		}
		candidates = append(candidates, value)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if !candidates[i].NextRetryAt.Equal(candidates[j].NextRetryAt) {
			return candidates[i].NextRetryAt.Before(candidates[j].NextRetryAt)
		}
		if !candidates[i].CreatedAt.Equal(candidates[j].CreatedAt) {
			return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
		}
		if candidates[i].ID != candidates[j].ID {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].EventID < candidates[j].EventID
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	values := make([]OutboxEvent, 0, len(candidates))
	for _, value := range candidates {
		value.Status = StatusPublishing
		value.Attempts++
		value.NextRetryAt = now.Add(lease)
		value.UpdatedAt = now
		s.store.outbox[value.EventID] = value
		values = append(values, cloneOutboxEvent(value))
	}
	return values, nil
}

func (s *MemoryOutboxStore) MarkPublished(_ context.Context, eventID string) error {
	return s.updateOutbox(eventID, func(value *OutboxEvent) { value.Status, value.LastError = StatusPublished, "" })
}
func (s *MemoryOutboxStore) MarkRetry(_ context.Context, eventID string, nextRetryAt time.Time, reason string) error {
	return s.updateOutbox(eventID, func(value *OutboxEvent) {
		value.Status, value.NextRetryAt, value.LastError = StatusPending, nextRetryAt, truncateError(reason)
	})
}
func (s *MemoryOutboxStore) MarkFailed(_ context.Context, eventID string, reason string) error {
	return s.updateOutbox(eventID, func(value *OutboxEvent) { value.Status, value.LastError = StatusFailed, truncateError(reason) })
}

func (s *MemoryOutboxStore) updateOutbox(eventID string, update func(*OutboxEvent)) error {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	value, ok := s.store.outbox[eventID]
	if !ok {
		return errors.New("outbox event not found")
	}
	update(&value)
	value.UpdatedAt = time.Now().UTC()
	s.store.outbox[eventID] = value
	return nil
}

func (s *MemoryInboxStore) Claim(ctx context.Context, consumer string, event contracts.EventEnvelope, lease time.Duration) (InboxClaim, error) {
	if err := event.Validate(); err != nil {
		return InboxClaim{}, err
	}
	if lease <= 0 {
		lease = 30 * time.Second
	}
	now := time.Now().UTC()
	key, err := event.InboxKey(consumer)
	if err != nil {
		return InboxClaim{}, err
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	value, exists := s.store.inbox[key]
	if !exists {
		s.store.inbox[key] = memoryInboxRecord{consumer: consumer, eventID: event.EventID, status: StatusProcessing, attempts: 1, updated: now}
		return InboxClaim{Claimed: true, Attempts: 1}, nil
	}
	if value.status == StatusProcessed {
		return InboxClaim{Processed: true, Attempts: value.attempts}, nil
	}
	if value.updated.After(now.Add(-lease)) {
		return InboxClaim{Attempts: value.attempts}, nil
	}
	value.status, value.attempts, value.updated = StatusProcessing, value.attempts+1, now
	s.store.inbox[key] = value
	return InboxClaim{Claimed: true, Attempts: value.attempts}, nil
}

func (s *MemoryInboxStore) MarkProcessed(_ context.Context, consumer, eventID string) error {
	return s.updateInbox(consumer, eventID, func(value *memoryInboxRecord) { value.status = StatusProcessed })
}
func (s *MemoryInboxStore) MarkFailed(_ context.Context, consumer, eventID, _ string) error {
	return s.updateInbox(consumer, eventID, func(value *memoryInboxRecord) { value.status = StatusProcessing })
}
func (s *MemoryInboxStore) updateInbox(consumer, eventID string, update func(*memoryInboxRecord)) error {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	key := consumer + ":" + eventID
	value, ok := s.store.inbox[key]
	if !ok {
		return errors.New("inbox event not found")
	}
	update(&value)
	value.updated = time.Now().UTC()
	s.store.inbox[key] = value
	return nil
}

func (s *MemoryStore) OutboxEvents() []OutboxEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	values := make([]OutboxEvent, 0, len(s.outbox))
	for _, value := range s.outbox {
		values = append(values, cloneOutboxEvent(value))
	}
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	return values
}

func cloneOutboxEvent(value OutboxEvent) OutboxEvent {
	value.Payload = append([]byte(nil), value.Payload...)
	value.Headers = cloneAMQPTable(value.Headers)
	return value
}

func cloneAMQPTable(headers amqp.Table) amqp.Table {
	if headers == nil {
		return nil
	}
	cloned := make(amqp.Table, len(headers))
	for key, value := range headers {
		cloned[key] = cloneAMQPHeaderValue(value)
	}
	return cloned
}

func cloneAMQPHeaderValue(value any) any {
	switch typed := value.(type) {
	case []byte:
		return append([]byte(nil), typed...)
	case amqp.Table:
		return cloneAMQPTable(typed)
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for key, item := range typed {
			cloned[key] = cloneAMQPHeaderValue(item)
		}
		return cloned
	case []any:
		cloned := make([]any, len(typed))
		for index, item := range typed {
			cloned[index] = cloneAMQPHeaderValue(item)
		}
		return cloned
	default:
		return value
	}
}

var _ EventSink = (*MemoryStore)(nil)
var _ OutboxStore = (*MemoryOutboxStore)(nil)
var _ InboxStore = (*MemoryInboxStore)(nil)
