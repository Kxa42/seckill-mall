package order

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// MemoryRepository 是无 MySQL 环境下的 Order Service 验收实现。
type MemoryRepository struct {
	mu          sync.RWMutex
	orders      map[string]Order
	idempotency map[string]string
	intents     map[string]CreateIntent
	operations  map[string]Operation
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		orders:      make(map[string]Order),
		idempotency: make(map[string]string),
		intents:     make(map[string]CreateIntent),
		operations:  make(map[string]Operation),
	}
}

func (r *MemoryRepository) FindByIdempotency(_ context.Context, userID uint64, key string) (Order, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	orderID, ok := r.idempotency[idempotencyKey(userID, key)]
	if !ok {
		return Order{}, false, nil
	}
	order, ok := r.orders[orderID]
	if !ok {
		return Order{}, false, NewError(CodeConflict, "订单幂等记录不完整", nil)
	}
	return cloneOrder(order), true, nil
}

func (r *MemoryRepository) Create(_ context.Context, value Order) (Order, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := idempotencyKey(value.UserID, value.IdempotencyKey)
	if orderID, exists := r.idempotency[key]; exists {
		existing := r.orders[orderID]
		if existing.RequestDigest != value.RequestDigest {
			return Order{}, false, NewError(CodeConflict, "Idempotency-Key 对应的请求参数不一致", nil)
		}
		return cloneOrder(existing), true, nil
	}
	r.orders[value.OrderID] = cloneOrder(value)
	r.idempotency[key] = value.OrderID
	return cloneOrder(value), false, nil
}

func (r *MemoryRepository) FindCreateIntent(_ context.Context, userID uint64, key string) (CreateIntent, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.intents[idempotencyKey(userID, key)]
	return value, ok, nil
}

func (r *MemoryRepository) SaveCreateIntent(_ context.Context, value CreateIntent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := idempotencyKey(value.UserID, value.IdempotencyKey)
	if existing, ok := r.intents[key]; ok && existing.RequestDigest != value.RequestDigest {
		return NewError(CodeConflict, "创建意图参数不一致", nil)
	}
	r.intents[key] = value
	return nil
}

func (r *MemoryRepository) UpdateCreateIntent(_ context.Context, value CreateIntent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := idempotencyKey(value.UserID, value.IdempotencyKey)
	if _, ok := r.intents[key]; !ok {
		return NewError(CodeNotFound, "创建意图不存在", nil)
	}
	r.intents[key] = value
	return nil
}

func (r *MemoryRepository) ListPendingCreateIntents(_ context.Context, now time.Time, limit int) ([]CreateIntent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := make([]CreateIntent, 0)
	for _, value := range r.intents {
		if (value.Status == CreateIntentStarted || value.Status == CreateIntentFailed) && value.Attempts < maxCreateIntentAttempts && !value.NextRetryAt.After(now) {
			values = append(values, value)
			if len(values) >= limit {
				break
			}
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].NextRetryAt.Before(values[j].NextRetryAt) })
	return values, nil
}

func (r *MemoryRepository) Get(_ context.Context, userID uint64, orderID string) (Order, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.orders[orderID]
	if !ok || (userID != 0 && value.UserID != userID) {
		return Order{}, NewError(CodeNotFound, "订单不存在", nil)
	}
	return cloneOrder(value), nil
}

func (r *MemoryRepository) List(_ context.Context, userID uint64, offset, limit int) ([]Order, int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := make([]Order, 0)
	for _, value := range r.orders {
		if value.UserID == userID {
			values = append(values, cloneOrder(value))
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].CreatedAt.After(values[j].CreatedAt) })
	total := int64(len(values))
	if offset > len(values) {
		offset = len(values)
	}
	end := offset + limit
	if end > len(values) {
		end = len(values)
	}
	return values[offset:end], total, nil
}

func (r *MemoryRepository) ListExpired(_ context.Context, now time.Time, limit int) ([]Order, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := make([]Order, 0)
	for _, value := range r.orders {
		if value.Status == StatusPendingPayment && !value.ExpiresAt.After(now) {
			values = append(values, cloneOrder(value))
			if len(values) >= limit {
				break
			}
		}
	}
	return values, nil
}

func (r *MemoryRepository) Transition(_ context.Context, orderID string, userID uint64, target, reason, actorType string, actorID uint64, now time.Time) (Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.orders[orderID]
	if !ok || (userID != 0 && value.UserID != userID) {
		return Order{}, NewError(CodeNotFound, "订单不存在", nil)
	}
	if value.Status == target {
		return cloneOrder(value), nil
	}
	if !canTransition(value.Status, target) {
		return Order{}, NewError(CodeInvalidTransition, "当前订单状态不能执行该操作", nil)
	}
	value.StatusHistory = append(value.StatusHistory, StatusHistory{FromStatus: value.Status, ToStatus: target, Reason: reason, ActorType: actorType, ActorID: actorID, CreatedAt: now})
	value.Status = target
	value.UpdatedAt = now
	r.orders[orderID] = value
	return cloneOrder(value), nil
}

func (r *MemoryRepository) SaveOperation(_ context.Context, value Operation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.operations[value.ID]; ok && existing.Status == OperationDone {
		return nil
	}
	r.operations[value.ID] = cloneOperation(value)
	return nil
}

func (r *MemoryRepository) ListPendingOperations(_ context.Context, now time.Time, limit int) ([]Operation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := make([]Operation, 0)
	for _, value := range r.operations {
		if value.Status == OperationPending && !value.NextRetryAt.After(now) {
			values = append(values, cloneOperation(value))
			if len(values) >= limit {
				break
			}
		}
	}
	return values, nil
}

func (r *MemoryRepository) UpdateOperation(_ context.Context, value Operation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.operations[value.ID] = cloneOperation(value)
	return nil
}

func (r *MemoryRepository) Operation(id string) (Operation, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.operations[id]
	return cloneOperation(value), ok
}

// CreateIntent 返回测试用的创建意图快照。
func (r *MemoryRepository) CreateIntent(userID uint64, key string) (CreateIntent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.intents[idempotencyKey(userID, key)]
	return value, ok
}

func idempotencyKey(userID uint64, key string) string { return fmt.Sprintf("%d:%s", userID, key) }
