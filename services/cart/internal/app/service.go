package cartservice

import (
	"context"
	"errors"
	"fmt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"time"
)

type Service struct {
	repository Repository
	catalog    CatalogClient
	now        func() time.Time
}

func NewService(repository Repository, catalog CatalogClient) (*Service, error) {
	if repository == nil || catalog == nil {
		return nil, errors.New("cart repository and catalog client are required")
	}
	return &Service{repository: repository, catalog: catalog, now: time.Now}, nil
}

func (s *Service) Set(ctx context.Context, userID, skuID uint64, quantity int32) (Item, error) {
	if userID == 0 || skuID == 0 || quantity <= 0 || quantity > 99 {
		return Item{}, ErrInvalidRequest
	}
	skus, err := s.catalog.GetSKUSnapshot(ctx, []uint64{skuID}, false)
	if err != nil || len(skus) != 1 {
		return Item{}, mapCatalogError(err)
	}
	if skus[0].ID != skuID || !skus[0].Active {
		return Item{}, ErrNotFound
	}
	item, err := itemWithSKU(Item{UserID: userID, SKUID: skuID, Quantity: quantity}, skus[0])
	if err != nil {
		return Item{}, err
	}
	if err := s.repository.Set(ctx, userID, skuID, quantity, s.now().UTC()); err != nil {
		return Item{}, err
	}
	return item, nil
}

func (s *Service) Delete(ctx context.Context, userID, skuID uint64) error {
	if userID == 0 || skuID == 0 {
		return ErrInvalidRequest
	}
	return s.repository.Delete(ctx, userID, skuID)
}

func (s *Service) List(ctx context.Context, userID uint64) ([]Item, error) {
	if userID == 0 {
		return nil, ErrInvalidRequest
	}
	items, err := s.repository.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return []Item{}, nil
	}
	ids := make([]uint64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.SKUID)
	}
	skus, err := s.catalog.GetSKUSnapshot(ctx, ids, true)
	if err != nil {
		return nil, mapCatalogError(err)
	}
	byID := make(map[uint64]SKU, len(skus))
	for _, sku := range skus {
		byID[sku.ID] = sku
	}
	result := make([]Item, 0, len(items))
	for _, item := range items {
		sku, ok := byID[item.SKUID]
		if !ok {
			item.SKU.Active = false
		} else {
			item, err = itemWithSKU(item, sku)
			if err != nil {
				return nil, err
			}
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *Service) Preview(ctx context.Context, userID uint64) (Preview, error) {
	items, err := s.List(ctx, userID)
	if err != nil {
		return Preview{}, err
	}
	preview := Preview{Items: items, Available: true}
	for _, item := range items {
		if !item.SKU.Active || item.Quantity <= 0 || item.Quantity > item.SKU.AvailableStock {
			preview.Available = false
		}
		if item.SubtotalCents < 0 || preview.TotalAmountCents > maxInt64-item.SubtotalCents {
			return Preview{}, ErrInvalidRequest
		}
		preview.TotalAmountCents += item.SubtotalCents
	}
	return preview, nil
}

func itemWithSKU(item Item, sku SKU) (Item, error) {
	if sku.PriceCents < 0 || item.Quantity <= 0 {
		return Item{}, ErrInvalidRequest
	}
	if sku.PriceCents != 0 && int64(item.Quantity) > maxInt64/sku.PriceCents {
		return Item{}, ErrInvalidRequest
	}
	item.SKU = sku
	item.SubtotalCents = sku.PriceCents * int64(item.Quantity)
	return item, nil
}

const maxInt64 = int64(^uint64(0) >> 1)

func mapCatalogError(err error) error {
	if err == nil {
		return ErrNotFound
	}
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}

func statusError(err error) error {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		return status.Error(codes.InvalidArgument, "购物车参数无效")
	case errors.Is(err, ErrNotFound):
		return status.Error(codes.NotFound, "购物车商品不存在")
	case errors.Is(err, ErrUnavailable):
		return status.Error(codes.Unavailable, "目录服务暂时不可用")
	default:
		return status.Error(codes.Internal, "购物车服务暂时不可用")
	}
}
