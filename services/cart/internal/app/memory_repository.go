package cartservice

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

type MemoryRepository struct {
	mu    sync.RWMutex
	items map[string]Item
}

func NewMemoryRepository() *MemoryRepository { return &MemoryRepository{items: make(map[string]Item)} }

func cartKey(userID, skuID uint64) string { return fmt.Sprintf("%d:%d", userID, skuID) }

func (r *MemoryRepository) Set(_ context.Context, userID, skuID uint64, quantity int32, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[cartKey(userID, skuID)] = Item{UserID: userID, SKUID: skuID, Quantity: quantity}
	return nil
}

func (r *MemoryRepository) Delete(_ context.Context, userID, skuID uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := cartKey(userID, skuID)
	if _, ok := r.items[key]; !ok {
		return ErrNotFound
	}
	delete(r.items, key)
	return nil
}

func (r *MemoryRepository) List(_ context.Context, userID uint64) ([]Item, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Item, 0)
	for _, item := range r.items {
		if item.UserID == userID {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SKUID < result[j].SKUID })
	return result, nil
}

var _ Repository = (*MemoryRepository)(nil)
