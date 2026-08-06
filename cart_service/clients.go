// Cart Service 的 Catalog gRPC 客户端只依赖 SKU 快照契约。
package cartservice

import (
	"context"
	"fmt"
	"time"

	"seckill-mall/common/pb"
)

type GRPCCatalogClient struct{ client pb.CatalogServiceClient }

func NewGRPCCatalogClient(client pb.CatalogServiceClient) *GRPCCatalogClient {
	return &GRPCCatalogClient{client: client}
}
func (c *GRPCCatalogClient) GetSKUSnapshot(ctx context.Context, ids []uint64, includeInactive bool) ([]SKU, error) {
	if c == nil || c.client == nil {
		return nil, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	response, err := c.client.GetSKUSnapshot(ctx, &pb.CatalogGetSKUSnapshotRequest{SkuIds: ids, IncludeInactive: includeInactive})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	result := make([]SKU, 0, len(response.GetItems()))
	for _, item := range response.GetItems() {
		result = append(result, SKU{ID: item.GetSkuId(), Code: item.GetSkuCode(), Name: item.GetSkuName(), PriceCents: item.GetPriceCents(), Active: item.GetActive(), AvailableStock: item.GetAvailableStock()})
	}
	return result, nil
}

var _ CatalogClient = (*GRPCCatalogClient)(nil)
