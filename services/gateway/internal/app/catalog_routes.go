// 本文件承载 Gateway 到 Catalog Service 的 HTTP 适配。
// 商品查询切换到 Catalog 后，仍保持既有 /api/v1 JSON 响应形状。
package gateway

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"seckill-mall/shared/gen/commerce"
	"seckill-mall/shared/platform/httpx"
)

const catalogRPCTimeout = 2 * time.Second

type gatewayProductPage struct {
	Items  []gatewayProduct `json:"items"`
	Limit  int              `json:"limit"`
	Offset int              `json:"offset"`
	Total  int64            `json:"total"`
}

type gatewayProduct struct {
	SPU    gatewaySPU   `json:"spu"`
	SKUs   []gatewaySKU `json:"skus"`
	Images []string     `json:"images"`
}

type gatewaySPU struct {
	ID          uint64 `json:"id"`
	CategoryID  uint64 `json:"category_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Active      bool   `json:"active"`
}

type gatewaySKU struct {
	ID             uint64    `json:"id"`
	SPUID          uint64    `json:"spu_id"`
	Code           string    `json:"code"`
	Name           string    `json:"name"`
	PriceCents     int64     `json:"price_cents"`
	AvailableStock int32     `json:"available_stock"`
	ReservedStock  int32     `json:"reserved_stock,omitempty"`
	Active         bool      `json:"active"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func registerCatalogRoutes(router *gin.Engine, client pb.CatalogServiceClient) {
	if client == nil {
		return
	}
	router.GET("/api/v1/products", func(c *gin.Context) {
		offset, limit, ok := parseCatalogPage(c)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), catalogRPCTimeout)
		defer cancel()
		response, err := client.ListProducts(ctx, &pb.CatalogListProductsRequest{Offset: int32(offset), Limit: int32(limit)})
		if err != nil {
			respondCatalogError(c, err)
			return
		}
		items := make([]gatewayProduct, 0, len(response.Items))
		for _, item := range response.Items {
			items = append(items, catalogProductToHTTP(item))
		}
		httpx.OK(c, http.StatusOK, gatewayProductPage{Items: items, Offset: int(response.Offset), Limit: int(response.Limit), Total: response.Total})
	})

	router.GET("/api/v1/products/:id", func(c *gin.Context) {
		id, err := strconv.ParseUint(c.Param("id"), 10, 64)
		if err != nil || id == 0 {
			httpx.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "商品 ID 无效")
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), catalogRPCTimeout)
		defer cancel()
		product, err := client.GetProduct(ctx, &pb.CatalogGetProductRequest{SpuId: id})
		if err != nil {
			respondCatalogError(c, err)
			return
		}
		httpx.OK(c, http.StatusOK, catalogProductToHTTP(product))
	})
}

func parseCatalogPage(c *gin.Context) (int, int, bool) {
	offset := 0
	limit := 20
	var err error
	if value := c.Query("offset"); value != "" {
		offset, err = strconv.Atoi(value)
		if err != nil || offset < 0 {
			httpx.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "offset 参数无效")
			return 0, 0, false
		}
	}
	if value := c.Query("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit <= 0 || limit > 100 {
			httpx.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "limit 参数无效")
			return 0, 0, false
		}
	}
	return offset, limit, true
}

func catalogProductToHTTP(product *pb.CatalogProduct) gatewayProduct {
	result := gatewayProduct{SPU: gatewaySPU{ID: product.GetSpuId(), CategoryID: product.GetCategoryId(), Name: product.GetName(), Description: product.GetDescription(), Active: product.GetActive()}, Images: append([]string(nil), product.GetImages()...)}
	result.SKUs = make([]gatewaySKU, 0, len(product.GetSkus()))
	for _, sku := range product.GetSkus() {
		result.SKUs = append(result.SKUs, catalogSKUToHTTP(sku))
	}
	return result
}

func catalogSKUToHTTP(sku *pb.CatalogSKUSnapshot) gatewaySKU {
	result := gatewaySKU{ID: sku.GetSkuId(), SPUID: sku.GetSpuId(), Code: sku.GetSkuCode(), Name: sku.GetSkuName(), PriceCents: sku.GetPriceCents(), AvailableStock: sku.GetAvailableStock(), ReservedStock: sku.GetReservedStock(), Active: sku.GetActive()}
	result.CreatedAt = timestampToTime(sku.GetCreatedAt())
	result.UpdatedAt = timestampToTime(sku.GetUpdatedAt())
	return result
}

func timestampToTime(value *timestamppb.Timestamp) time.Time {
	if value == nil || !value.IsValid() {
		return time.Time{}
	}
	return value.AsTime()
}

func respondCatalogError(c *gin.Context, err error) {
	switch status.Code(err) {
	case codes.InvalidArgument:
		httpx.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "商品查询参数无效")
	case codes.NotFound:
		httpx.Error(c, http.StatusNotFound, "NOT_FOUND", "商品不存在")
	case codes.Unavailable, codes.DeadlineExceeded:
		httpx.Error(c, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE", "目录服务暂时不可用")
	default:
		httpx.Error(c, http.StatusBadGateway, "UPSTREAM_ERROR", "目录服务暂时不可用")
	}
}
