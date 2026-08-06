package catalogservice

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"seckill-mall/common/pb"
)

// Server 实现 Catalog gRPC 服务。
type Server struct {
	pb.UnimplementedCatalogServiceServer
	repository Repository
}

// NewServer 创建 Catalog gRPC 服务端。
func NewServer(repository Repository) (*Server, error) {
	if repository == nil {
		return nil, errors.New("catalog repository is required")
	}
	return &Server{repository: repository}, nil
}

func (s *Server) ListProducts(ctx context.Context, req *pb.CatalogListProductsRequest) (*pb.CatalogListProductsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "请求不能为空")
	}
	offset, limit := int(req.Offset), int(req.Limit)
	if offset < 0 {
		return nil, status.Error(codes.InvalidArgument, "offset 不能为负数")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	page, err := s.repository.ListProducts(ctx, offset, limit, req.IncludeInactive)
	if err != nil {
		return nil, mapError(err)
	}
	response := &pb.CatalogListProductsResponse{Total: page.Total, Offset: int32(page.Offset), Limit: int32(page.Limit), Items: make([]*pb.CatalogProduct, 0, len(page.Items))}
	for _, item := range page.Items {
		response.Items = append(response.Items, productToProto(item))
	}
	return response, nil
}

func (s *Server) GetProduct(ctx context.Context, req *pb.CatalogGetProductRequest) (*pb.CatalogProduct, error) {
	if req == nil || req.SpuId == 0 {
		return nil, status.Error(codes.InvalidArgument, "spu_id 无效")
	}
	product, err := s.repository.GetProduct(ctx, req.SpuId, req.IncludeInactive)
	if err != nil {
		return nil, mapError(err)
	}
	return productToProto(product), nil
}

func (s *Server) GetSKUSnapshot(ctx context.Context, req *pb.CatalogGetSKUSnapshotRequest) (*pb.CatalogGetSKUSnapshotResponse, error) {
	if req == nil || len(req.SkuIds) == 0 {
		return nil, status.Error(codes.InvalidArgument, "sku_ids 不能为空")
	}
	items, err := s.repository.GetSKUSnapshot(ctx, req.SkuIds, req.IncludeInactive)
	if err != nil {
		return nil, mapError(err)
	}
	response := &pb.CatalogGetSKUSnapshotResponse{Items: make([]*pb.CatalogSKUSnapshot, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, skuToProto(item))
	}
	return response, nil
}

func productToProto(product Product) *pb.CatalogProduct {
	result := &pb.CatalogProduct{SpuId: product.SPUID, CategoryId: product.CategoryID, Name: product.Name, Description: product.Description, Active: product.Active, Images: append([]string(nil), product.Images...)}
	for _, sku := range product.SKUs {
		result.Skus = append(result.Skus, skuToProto(sku))
	}
	return result
}

func skuToProto(sku SKU) *pb.CatalogSKUSnapshot {
	result := &pb.CatalogSKUSnapshot{SkuId: sku.ID, SpuId: sku.SPUID, SkuCode: sku.Code, SkuName: sku.Name, PriceCents: sku.PriceCents, Active: sku.Active, AvailableStock: sku.AvailableStock, ReservedStock: sku.ReservedStock}
	if !sku.CreatedAt.IsZero() {
		result.CreatedAt = timestamppb.New(sku.CreatedAt)
	}
	if !sku.UpdatedAt.IsZero() {
		result.UpdatedAt = timestamppb.New(sku.UpdatedAt)
	}
	return result
}

func mapError(err error) error {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		return status.Error(codes.InvalidArgument, "目录请求参数无效")
	case errors.Is(err, ErrNotFound):
		return status.Error(codes.NotFound, "商品或 SKU 不存在")
	default:
		return status.Error(codes.Internal, "目录服务暂时不可用")
	}
}
