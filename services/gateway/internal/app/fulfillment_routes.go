// 本文件承载 Gateway 到 Fulfillment Service 的发货、收货和物流查询 HTTP 适配。
package gateway

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"seckill-mall/services/gateway/internal/middleware"
	"seckill-mall/shared/gen/commerce"
	"seckill-mall/shared/platform/httpx"
)

func registerFulfillmentRoutes(router *gin.Engine, client pb.FulfillmentServiceClient) {
	if client == nil {
		return
	}
	authenticated := router.Group("/api/v1")
	authenticated.Use(middleware.CommerceJWTAuth())
	authenticated.POST("/admin/orders/:order_id/ship", func(c *gin.Context) {
		actorID, ok := gatewayUserID(c)
		if !ok {
			return
		}
		role, _ := c.Get(middleware.UserRoleKey)
		if role != "admin" {
			httpx.Error(c, http.StatusForbidden, "FORBIDDEN", "仅管理员可执行发货")
			return
		}
		var request struct {
			Carrier    string `json:"carrier"`
			TrackingNo string `json:"tracking_no"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			httpx.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数格式不正确")
			return
		}
		ctx, cancel := signedAdminContext(c, pb.FulfillmentService_Ship_FullMethodName, actorID)
		defer cancel()
		response, err := client.Ship(ctx, &pb.FulfillmentShipRequest{ActorId: actorID, OrderId: c.Param("order_id"), Carrier: request.Carrier, TrackingNo: request.TrackingNo})
		if err != nil {
			respondStage4RPCError(c, err, "发货")
			return
		}
		httpx.OK(c, http.StatusOK, response)
	})
	authenticated.POST("/orders/:order_id/receipt", func(c *gin.Context) {
		userID, ok := gatewayUserID(c)
		if !ok {
			return
		}
		ctx, cancel := signedUserContext(c, pb.FulfillmentService_ConfirmReceipt_FullMethodName, userID)
		defer cancel()
		response, err := client.ConfirmReceipt(ctx, &pb.FulfillmentConfirmReceiptRequest{UserId: userID, OrderId: c.Param("order_id")})
		if err != nil {
			respondStage4RPCError(c, err, "确认收货")
			return
		}
		httpx.OK(c, http.StatusOK, response)
	})
	authenticated.GET("/orders/:order_id/shipment", func(c *gin.Context) {
		userID, ok := gatewayUserID(c)
		if !ok {
			return
		}
		ctx, cancel := signedUserContext(c, pb.FulfillmentService_Get_FullMethodName, userID)
		defer cancel()
		response, err := client.Get(ctx, &pb.FulfillmentGetRequest{UserId: userID, OrderId: c.Param("order_id")})
		if err != nil {
			respondStage4RPCError(c, err, "物流")
			return
		}
		httpx.OK(c, http.StatusOK, response)
	})
}
