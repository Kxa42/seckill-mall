// 本文件承载 Gateway 到 Payment Service 的支付、回调和退款 HTTP 适配。
package gateway

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"seckill-mall/services/gateway/internal/middleware"
	"seckill-mall/shared/gen/commerce"
	"seckill-mall/shared/platform/httpx"
)

func registerPaymentRoutes(router *gin.Engine, client pb.PaymentServiceClient) {
	if client == nil {
		return
	}
	authenticated := router.Group("/api/v1")
	authenticated.Use(middleware.CommerceJWTAuth())
	authenticated.POST("/orders/:order_id/payments", func(c *gin.Context) {
		userID, ok := gatewayUserID(c)
		if !ok {
			return
		}
		ctx, cancel := signedUserContext(c, pb.PaymentService_Create_FullMethodName, userID)
		defer cancel()
		response, err := client.Create(ctx, &pb.PaymentCreateRequest{UserId: userID, OrderId: c.Param("order_id")})
		if err != nil {
			respondStage4RPCError(c, err, "支付")
			return
		}
		statusCode := http.StatusCreated
		if response.GetReused() {
			statusCode = http.StatusOK
		}
		httpx.OK(c, statusCode, response)
	})
	authenticated.POST("/orders/:order_id/refunds", func(c *gin.Context) {
		userID, ok := gatewayUserID(c)
		if !ok {
			return
		}
		var request struct {
			Reason string `json:"reason"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			httpx.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数格式不正确")
			return
		}
		ctx, cancel := signedUserContext(c, pb.PaymentService_Refund_FullMethodName, userID)
		defer cancel()
		response, err := client.Refund(ctx, &pb.PaymentRefundRequest{UserId: userID, OrderId: c.Param("order_id"), Reason: request.Reason})
		if err != nil {
			respondStage4RPCError(c, err, "退款")
			return
		}
		httpx.OK(c, http.StatusOK, response)
	})

	// Mock 回调模拟支付渠道服务端通知，不接受用户 JWT，仅接受 Payment 签名和内部系统签名。
	router.POST("/api/v1/payments/callback", func(c *gin.Context) {
		var request struct {
			PaymentNo   string `json:"payment_no"`
			CallbackRef string `json:"callback_ref"`
			Signature   string `json:"signature"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			httpx.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数格式不正确")
			return
		}
		ctx, cancel := signedSystemContext(c, pb.PaymentService_Callback_FullMethodName)
		defer cancel()
		response, err := client.Callback(ctx, &pb.PaymentCallbackRequest{PaymentNo: request.PaymentNo, CallbackRef: request.CallbackRef, Signature: request.Signature})
		if err != nil {
			respondStage4RPCError(c, err, "支付回调")
			return
		}
		httpx.OK(c, http.StatusOK, response)
	})
}
