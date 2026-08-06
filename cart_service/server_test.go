package cartservice

import (
	"context"
	"sync"
	"testing"

	"seckill-mall/common/pb"
)

type fakeCatalog struct{ items map[uint64]SKU }

func (f fakeCatalog) GetSKUSnapshot(_ context.Context, ids []uint64, _ bool) ([]SKU, error) {
	result := make([]SKU, 0, len(ids))
	for _, id := range ids {
		value, ok := f.items[id]
		if !ok {
			return nil, ErrNotFound
		}
		result = append(result, value)
	}
	return result, nil
}

func TestCartRejectsInactiveSKUWithoutPersisting(t *testing.T) {
	repository := NewMemoryRepository()
	service, _ := NewService(repository, fakeCatalog{items: map[uint64]SKU{1: {ID: 1, PriceCents: 100, Active: false, AvailableStock: 1}}})
	if _, err := service.Set(context.Background(), 9, 1, 1); err != ErrNotFound {
		t.Fatalf("inactive SKU error = %v, want %v", err, ErrNotFound)
	}
	items, err := repository.List(context.Background(), 9)
	if err != nil || len(items) != 0 {
		t.Fatalf("invalid SKU was persisted: items=%+v err=%v", items, err)
	}
}

func TestCartConcurrentSetKeepsOneOwnedItem(t *testing.T) {
	repository := NewMemoryRepository()
	service, _ := NewService(repository, fakeCatalog{items: map[uint64]SKU{1: {ID: 1, PriceCents: 100, Active: true, AvailableStock: 99}}})
	var wait sync.WaitGroup
	for quantity := int32(1); quantity <= 20; quantity++ {
		wait.Add(1)
		go func(value int32) {
			defer wait.Done()
			if _, err := service.Set(context.Background(), 9, 1, value); err != nil {
				t.Errorf("Set(%d) error = %v", value, err)
			}
		}(quantity)
	}
	wait.Wait()
	items, err := service.List(context.Background(), 9)
	if err != nil || len(items) != 1 || items[0].UserID != 9 || items[0].Quantity < 1 || items[0].Quantity > 20 {
		t.Fatalf("concurrent items=%+v err=%v", items, err)
	}
}

func TestCartPreviewUsesCatalogSnapshotAndUserIsolation(t *testing.T) {
	repository := NewMemoryRepository()
	service, err := NewService(repository, fakeCatalog{items: map[uint64]SKU{1: {ID: 1, Code: "SKU-1", Name: "演示商品", PriceCents: 699900, Active: true, AvailableStock: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Set(context.Background(), 9, 1, 2); err != nil {
		t.Fatal(err)
	}
	preview, err := service.Preview(context.Background(), 9)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Available || preview.TotalAmountCents != 1399800 {
		t.Fatalf("preview=%+v", preview)
	}
	other, err := service.List(context.Background(), 10)
	if err != nil || len(other) != 0 {
		t.Fatalf("other=%+v err=%v", other, err)
	}
}

func TestCartGRPCServerRequiresUserIdentityWhenConfigured(t *testing.T) {
	t.Setenv("SECKILL_INTERNAL_CALL_SECRET", "cart-secret")
	service, _ := NewService(NewMemoryRepository(), fakeCatalog{items: map[uint64]SKU{1: {ID: 1, PriceCents: 1, Active: true, AvailableStock: 1}}})
	server, _ := NewServer(service)
	if _, err := server.Set(context.Background(), &pb.CartSetRequest{UserId: 9, SkuId: 1, Quantity: 1}); err == nil {
		t.Fatal("unsigned cart request accepted")
	}
}
