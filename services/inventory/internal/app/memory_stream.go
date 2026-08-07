package inventoryservice

import (
	"context"
	"sync"

	"seckill-mall/shared/contracts"
)

// MemoryEventStream 是 Redis Stream Outbox 的进程内替身。
type MemoryEventStream struct {
	mu     sync.Mutex
	events []StreamEvent
}

func NewMemoryEventStream() *MemoryEventStream { return &MemoryEventStream{} }

func (s *MemoryEventStream) Append(_ context.Context, event contracts.EventEnvelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, StreamEvent{ID: event.EventID, Event: event})
	return nil
}

func (s *MemoryEventStream) Events() []StreamEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	values := make([]StreamEvent, len(s.events))
	copy(values, s.events)
	return values
}

var _ EventStream = (*MemoryEventStream)(nil)
