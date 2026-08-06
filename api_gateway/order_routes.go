// 本文件承载 Gateway 到新 Order Service 的 HTTP 适配。
// /api/v1 订单接口只调用 CommerceOrderService，不再由 Gateway 先扣库存。
package main

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"seckill-mall/api_gateway/middleware"
	"seckill-mall/common/pb"
	"seckill-mall/internal/order"
	"seckill-mall/internal/platform/httpx"
)

const orderRPCTimeout = 3 * time.Second

type orderItemRequest struct {
	SKUID    uint64 `json:"sku_id"`
	Quantity int32  `json:"quantity"`
}

func registerOrderRoutes(router *gin.Engine, client pb.CommerceOrderServiceClient, cartClients ...pb.CartServiceClient) {
	if client == nil {
		return
	}
	authenticated := router.Group("/api/v1")
	authenticated.Use(middleware.CommerceJWTAuth())
	authenticated.POST("/orders", func(c *gin.Context) {
		var request struct {
			AddressID uint64             `json:"address_id"`
			Items     []orderItemRequest `json:"items"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			httpx.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数格式不正确")
			return
		}
		userID, ok := gatewayUserID(c)
		if !ok {
			return
		}
		items := request.Items
		if len(items) == 0 && len(cartClients) > 0 && cartClients[0] != nil {
			items = listCartForOrder(c, cartClients[0], userID)
			if items == nil {
				return
			}
		}
		response := createOrderRPC(c, client, &pb.OrderCreateRequest{IdempotencyKey: strings.TrimSpace(c.GetHeader("Idempotency-Key")), UserId: userID, AddressId: request.AddressID, OrderType: "normal", Items: orderItemsToProto(items)})
		if response == nil {
			return
		}
		statusCode := http.StatusCreated
		if response.GetReused() {
			statusCode = http.StatusOK
		}
		httpx.OK(c, statusCode, response)
	})

	authenticated.POST("/seckill/orders", func(c *gin.Context) {
		var request struct {
			AddressID  uint64 `json:"address_id"`
			SKUID      uint64 `json:"sku_id"`
			Quantity   int32  `json:"quantity"`
			ActivityID uint64 `json:"activity_id"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			httpx.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数格式不正确")
			return
		}
		if request.ActivityID == 0 {
			request.ActivityID = 1
		}
		userID, ok := gatewayUserID(c)
		if !ok {
			return
		}
		response := createOrderRPC(c, client, &pb.OrderCreateRequest{IdempotencyKey: strings.TrimSpace(c.GetHeader("Idempotency-Key")), UserId: userID, AddressId: request.AddressID, OrderType: "seckill", ActivityId: request.ActivityID, Items: []*pb.OrderItemInput{{SkuId: request.SKUID, Quantity: request.Quantity}}})
		if response == nil {
			return
		}
		statusCode := http.StatusCreated
		if response.GetReused() {
			statusCode = http.StatusOK
		}
		httpx.OK(c, statusCode, response)
	})

	authenticated.GET("/orders", func(c *gin.Context) {
		userID, ok := gatewayUserID(c)
		if !ok {
			return
		}
		offset := parseOrderQuery(c, "offset", 0)
		limit := parseOrderQuery(c, "limit", 20)
		if offset < 0 || limit <= 0 || limit > 100 {
			httpx.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "分页参数无效")
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), orderRPCTimeout)
		defer cancel()
		ctx = order.AppendSignedMetadata(ctx, os.Getenv("SECKILL_INTERNAL_CALL_SECRET"), pb.CommerceOrderService_List_FullMethodName, userID, time.Now())
		response, err := client.List(ctx, &pb.OrderListRequest{UserId: userID, Offset: int32(offset), Limit: int32(limit)})
		if err != nil {
			respondOrderRPCError(c, err)
			return
		}
		httpx.OK(c, http.StatusOK, response)
	})

	authenticated.GET("/orders/:order_id", func(c *gin.Context) {
		userID, ok := gatewayUserID(c)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), orderRPCTimeout)
		defer cancel()
		ctx = order.AppendSignedMetadata(ctx, os.Getenv("SECKILL_INTERNAL_CALL_SECRET"), pb.CommerceOrderService_Get_FullMethodName, userID, time.Now())
		response, err := client.Get(ctx, &pb.OrderGetRequest{UserId: userID, OrderId: c.Param("order_id")})
		if err != nil {
			respondOrderRPCError(c, err)
			return
		}
		httpx.OK(c, http.StatusOK, response)
	})

	authenticated.POST("/orders/:order_id/cancel", func(c *gin.Context) {
		userID, ok := gatewayUserID(c)
		if !ok {
			return
		}
		var request struct {
			Reason string `json:"reason"`
		}
		if c.Request.ContentLength > 0 {
			if err := c.ShouldBindJSON(&request); err != nil {
				httpx.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数格式不正确")
				return
			}
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), orderRPCTimeout)
		defer cancel()
		ctx = order.AppendSignedMetadata(ctx, os.Getenv("SECKILL_INTERNAL_CALL_SECRET"), pb.CommerceOrderService_Cancel_FullMethodName, userID, time.Now())
		response, err := client.Cancel(ctx, &pb.OrderCancelRequest{UserId: userID, OrderId: c.Param("order_id"), Reason: request.Reason})
		if err != nil {
			respondOrderRPCError(c, err)
			return
		}
		httpx.OK(c, http.StatusOK, response)
	})
}

func listCartForOrder(c *gin.Context, client pb.CartServiceClient, userID uint64) []orderItemRequest {
	ctx, cancel := context.WithTimeout(c.Request.Context(), orderRPCTimeout)
	defer cancel()
	ctx = order.AppendSignedMetadata(ctx, os.Getenv("SECKILL_INTERNAL_CALL_SECRET"), pb.CartService_List_FullMethodName, userID, time.Now())
	response, err := client.List(ctx, &pb.CartListRequest{UserId: userID})
	if err != nil {
		respondOrderRPCError(c, err)
		return nil
	}
	items := make([]orderItemRequest, 0, len(response.GetItems()))
	for _, item := range response.GetItems() {
		items = append(items, orderItemRequest{SKUID: item.GetSkuId(), Quantity: item.GetQuantity()})
	}
	if len(items) == 0 {
		httpx.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "购物车为空")
		return nil
	}
	return items
}

func createOrderRPC(c *gin.Context, client pb.CommerceOrderServiceClient, request *pb.OrderCreateRequest) *pb.OrderCreateResponse {
	ctx, cancel := context.WithTimeout(c.Request.Context(), orderRPCTimeout)
	defer cancel()
	ctx = order.AppendSignedMetadata(ctx, os.Getenv("SECKILL_INTERNAL_CALL_SECRET"), pb.CommerceOrderService_Create_FullMethodName, request.GetUserId(), time.Now())
	response, err := client.Create(ctx, request)
	if err != nil {
		respondOrderRPCError(c, err)
		return nil
	}
	return response
}

func orderItemsToProto(items []orderItemRequest) []*pb.OrderItemInput {
	result := make([]*pb.OrderItemInput, 0, len(items))
	for _, item := range items {
		result = append(result, &pb.OrderItemInput{SkuId: item.SKUID, Quantity: item.Quantity})
	}
	return result
}

func gatewayUserID(c *gin.Context) (uint64, bool) {
	value, exists := c.Get("userID")
	if !exists {
		httpx.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "用户未认证")
		return 0, false
	}
	userID, ok := value.(int64)
	if !ok || userID <= 0 {
		httpx.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "用户身份无效")
		return 0, false
	}
	return uint64(userID), true
}

func parseOrderQuery(c *gin.Context, name string, fallback int) int {
	value := strings.TrimSpace(c.Query(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return -1
	}
	return parsed
}

func respondOrderRPCError(c *gin.Context, err error) {
	code := status.Code(err)
	statusCode := http.StatusInternalServerError
	errorCode := "INTERNAL_ERROR"
	message := "订单服务暂时不可用"
	switch code {
	case codes.InvalidArgument:
		statusCode, errorCode, message = http.StatusBadRequest, "VALIDATION_ERROR", "订单参数无效"
	case codes.NotFound:
		statusCode, errorCode, message = http.StatusNotFound, "NOT_FOUND", "订单或关联资源不存在"
	case codes.PermissionDenied:
		statusCode, errorCode, message = http.StatusForbidden, "FORBIDDEN", "权限不足"
	case codes.ResourceExhausted:
		statusCode, errorCode, message = http.StatusConflict, "OUT_OF_STOCK", "库存不足"
	case codes.Aborted, codes.FailedPrecondition:
		statusCode, errorCode, message = http.StatusConflict, "CONFLICT", "订单操作冲突"
	case codes.Unavailable, codes.DeadlineExceeded:
		statusCode, errorCode, message = http.StatusBadGateway, "UPSTREAM_UNAVAILABLE", "订单服务暂时不可用"
	}
	httpx.Error(c, statusCode, errorCode, message)
}
