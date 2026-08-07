// 本文件承载 Gateway 到 Cart Service 的购物车 HTTP 适配。
package gateway

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"seckill-mall/services/gateway/internal/middleware"
	"seckill-mall/shared/gen/commerce"
	"seckill-mall/shared/platform/httpx"
)

func registerCartRoutes(router *gin.Engine, client pb.CartServiceClient) {
	if client == nil {
		return
	}
	authenticated := router.Group("/api/v1/cart")
	authenticated.Use(middleware.CommerceJWTAuth())
	authenticated.POST("/items", func(c *gin.Context) {
		userID, ok := gatewayUserID(c)
		if !ok {
			return
		}
		var request struct {
			SKUID    uint64 `json:"sku_id"`
			Quantity int32  `json:"quantity"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			httpx.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数格式不正确")
			return
		}
		ctx, cancel := signedUserContext(c, pb.CartService_Set_FullMethodName, userID)
		defer cancel()
		response, err := client.Set(ctx, &pb.CartSetRequest{UserId: userID, SkuId: request.SKUID, Quantity: request.Quantity})
		if err != nil {
			respondStage4RPCError(c, err, "购物车")
			return
		}
		httpx.OK(c, http.StatusOK, response)
	})
	authenticated.DELETE("/items/:sku_id", func(c *gin.Context) {
		userID, ok := gatewayUserID(c)
		if !ok {
			return
		}
		skuID, err := strconv.ParseUint(c.Param("sku_id"), 10, 64)
		if err != nil || skuID == 0 {
			httpx.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "SKU ID 无效")
			return
		}
		ctx, cancel := signedUserContext(c, pb.CartService_Delete_FullMethodName, userID)
		defer cancel()
		response, err := client.Delete(ctx, &pb.CartDeleteRequest{UserId: userID, SkuId: skuID})
		if err != nil {
			respondStage4RPCError(c, err, "购物车")
			return
		}
		httpx.OK(c, http.StatusOK, response)
	})
	authenticated.GET("", func(c *gin.Context) {
		userID, ok := gatewayUserID(c)
		if !ok {
			return
		}
		ctx, cancel := signedUserContext(c, pb.CartService_List_FullMethodName, userID)
		defer cancel()
		response, err := client.List(ctx, &pb.CartListRequest{UserId: userID})
		if err != nil {
			respondStage4RPCError(c, err, "购物车")
			return
		}
		httpx.OK(c, http.StatusOK, response)
	})
	authenticated.GET("/preview", func(c *gin.Context) {
		userID, ok := gatewayUserID(c)
		if !ok {
			return
		}
		ctx, cancel := signedUserContext(c, pb.CartService_Preview_FullMethodName, userID)
		defer cancel()
		response, err := client.Preview(ctx, &pb.CartPreviewRequest{UserId: userID})
		if err != nil {
			respondStage4RPCError(c, err, "购物车")
			return
		}
		httpx.OK(c, http.StatusOK, response)
	})
}
