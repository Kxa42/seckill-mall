// Package httpapi 将商城应用服务暴露为版本化 Gin HTTP API。
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"seckill-mall/internal/commerce"
	platformauth "seckill-mall/internal/platform/auth"
	"seckill-mall/internal/platform/httpx"
)

const principalKey = "commerce_principal"

// Principal 是认证中间件写入请求上下文的身份。
type Principal struct {
	UserID uint64
	Role   string
}

// RouterConfig 描述 HTTP Router 的依赖。
type RouterConfig struct {
	Service   *commerce.Service
	Auth      *platformauth.Manager
	Readiness func(context.Context) error
}

// NewRouter 创建完整商城 HTTP Router。
func NewRouter(config RouterConfig) (*gin.Engine, error) {
	if config.Service == nil || config.Auth == nil {
		return nil, errors.New("service 和 auth 不能为空")
	}
	if config.Readiness == nil {
		config.Readiness = func(context.Context) error { return nil }
	}

	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery(), requestIDMiddleware())
	router.GET("/healthz", func(c *gin.Context) {
		httpx.OK(c, http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/readyz", func(c *gin.Context) {
		if err := config.Readiness(c.Request.Context()); err != nil {
			httpx.Error(c, http.StatusServiceUnavailable, "NOT_READY", "服务尚未就绪")
			return
		}
		httpx.OK(c, http.StatusOK, gin.H{"status": "ready"})
	})
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	handler := &handler{service: config.Service}
	v1 := router.Group("/api/v1")
	v1.POST("/auth/register", handler.register)
	v1.POST("/auth/login", handler.login)
	v1.POST("/auth/refresh", handler.refresh)
	v1.GET("/products", handler.listProducts)
	v1.GET("/products/:id", handler.getProduct)
	v1.POST("/payments/mock/callback", handler.completeMockPayment)

	authenticated := v1.Group("")
	authenticated.Use(authMiddleware(config.Auth))
	authenticated.GET("/addresses", handler.listAddresses)
	authenticated.POST("/addresses", handler.createAddress)
	authenticated.PUT("/addresses/:id", handler.updateAddress)
	authenticated.DELETE("/addresses/:id", handler.deleteAddress)
	authenticated.GET("/cart/items", handler.listCart)
	authenticated.GET("/cart/checkout-preview", handler.previewCheckout)
	authenticated.POST("/cart/items", handler.setCartItem)
	authenticated.PUT("/cart/items/:sku_id", handler.updateCartItem)
	authenticated.DELETE("/cart/items/:sku_id", handler.deleteCartItem)
	authenticated.POST("/orders", handler.createOrder)
	authenticated.POST("/seckill/orders", handler.createSeckillOrder)
	authenticated.GET("/orders", handler.listOrders)
	authenticated.GET("/orders/:order_id", handler.getOrder)
	authenticated.POST("/orders/:order_id/cancel", handler.cancelOrder)
	authenticated.POST("/orders/:order_id/confirm", handler.confirmOrder)
	authenticated.POST("/orders/:order_id/refunds", handler.refundOrder)
	authenticated.POST("/payments/mock", handler.createMockPayment)

	admin := v1.Group("/admin")
	admin.Use(authMiddleware(config.Auth), requireRole(commerce.RoleAdmin))
	admin.POST("/products", handler.upsertProduct)
	admin.POST("/orders/:order_id/ship", handler.shipOrder)

	return router, nil
}

type handler struct {
	service *commerce.Service
}

func (h *handler) register(c *gin.Context) {
	var request struct {
		Email    string `json:"email" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if !bindJSON(c, &request) {
		return
	}
	tokens, err := h.service.Register(c.Request.Context(), request.Email, request.Password)
	respond(c, http.StatusCreated, tokens, err)
}

func (h *handler) login(c *gin.Context) {
	var request struct {
		Email    string `json:"email" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if !bindJSON(c, &request) {
		return
	}
	tokens, err := h.service.Login(c.Request.Context(), request.Email, request.Password)
	respond(c, http.StatusOK, tokens, err)
}

func (h *handler) refresh(c *gin.Context) {
	var request struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if !bindJSON(c, &request) {
		return
	}
	tokens, err := h.service.Refresh(c.Request.Context(), request.RefreshToken)
	respond(c, http.StatusOK, tokens, err)
}

func (h *handler) createAddress(c *gin.Context) {
	var request commerce.Address
	if !bindJSON(c, &request) {
		return
	}
	address, err := h.service.CreateAddress(c.Request.Context(), mustPrincipal(c).UserID, request)
	respond(c, http.StatusCreated, address, err)
}

func (h *handler) updateAddress(c *gin.Context) {
	id, ok := uintParam(c, "id")
	if !ok {
		return
	}
	var request commerce.Address
	if !bindJSON(c, &request) {
		return
	}
	address, err := h.service.UpdateAddress(c.Request.Context(), mustPrincipal(c).UserID, id, request)
	respond(c, http.StatusOK, address, err)
}

func (h *handler) deleteAddress(c *gin.Context) {
	id, ok := uintParam(c, "id")
	if !ok {
		return
	}
	if err := h.service.DeleteAddress(c.Request.Context(), mustPrincipal(c).UserID, id); err != nil {
		respondError(c, err)
		return
	}
	httpx.NoContent(c)
}

func (h *handler) listAddresses(c *gin.Context) {
	addresses, err := h.service.ListAddresses(c.Request.Context(), mustPrincipal(c).UserID)
	respond(c, http.StatusOK, addresses, err)
}

func (h *handler) listProducts(c *gin.Context) {
	offset := intQuery(c, "offset", 0)
	limit := intQuery(c, "limit", 20)
	page, err := h.service.ListProducts(c.Request.Context(), offset, limit)
	respond(c, http.StatusOK, page, err)
}

func (h *handler) getProduct(c *gin.Context) {
	id, ok := uintParam(c, "id")
	if !ok {
		return
	}
	product, err := h.service.GetProduct(c.Request.Context(), id)
	respond(c, http.StatusOK, product, err)
}

func (h *handler) upsertProduct(c *gin.Context) {
	var request commerce.AdminProductInput
	if !bindJSON(c, &request) {
		return
	}
	product, err := h.service.UpsertProduct(c.Request.Context(), request)
	respond(c, http.StatusOK, product, err)
}

func (h *handler) setCartItem(c *gin.Context) {
	var request struct {
		SKUID    uint64 `json:"sku_id" binding:"required"`
		Quantity int32  `json:"quantity" binding:"required"`
	}
	if !bindJSON(c, &request) {
		return
	}
	item, err := h.service.SetCartItem(c.Request.Context(), mustPrincipal(c).UserID, request.SKUID, request.Quantity)
	respond(c, http.StatusOK, item, err)
}

func (h *handler) updateCartItem(c *gin.Context) {
	skuID, ok := uintParam(c, "sku_id")
	if !ok {
		return
	}
	var request struct {
		Quantity int32 `json:"quantity" binding:"required"`
	}
	if !bindJSON(c, &request) {
		return
	}
	item, err := h.service.SetCartItem(c.Request.Context(), mustPrincipal(c).UserID, skuID, request.Quantity)
	respond(c, http.StatusOK, item, err)
}

func (h *handler) deleteCartItem(c *gin.Context) {
	skuID, ok := uintParam(c, "sku_id")
	if !ok {
		return
	}
	if err := h.service.DeleteCartItem(c.Request.Context(), mustPrincipal(c).UserID, skuID); err != nil {
		respondError(c, err)
		return
	}
	httpx.NoContent(c)
}

func (h *handler) listCart(c *gin.Context) {
	items, err := h.service.ListCartItems(c.Request.Context(), mustPrincipal(c).UserID)
	respond(c, http.StatusOK, items, err)
}

func (h *handler) previewCheckout(c *gin.Context) {
	preview, err := h.service.PreviewCheckout(c.Request.Context(), mustPrincipal(c).UserID)
	respond(c, http.StatusOK, preview, err)
}

func (h *handler) createOrder(c *gin.Context) {
	var request struct {
		AddressID uint64 `json:"address_id" binding:"required"`
	}
	if !bindJSON(c, &request) {
		return
	}
	order, reused, err := h.service.CreateOrder(
		c.Request.Context(), mustPrincipal(c).UserID, request.AddressID, c.GetHeader("Idempotency-Key"),
	)
	status := http.StatusCreated
	if reused {
		status = http.StatusOK
	}
	respond(c, status, order, err)
}

func (h *handler) createSeckillOrder(c *gin.Context) {
	var request struct {
		AddressID uint64 `json:"address_id" binding:"required"`
		SKUID     uint64 `json:"sku_id" binding:"required"`
		Quantity  int32  `json:"quantity" binding:"required"`
	}
	if !bindJSON(c, &request) {
		return
	}
	order, reused, err := h.service.CreateSeckillOrder(
		c.Request.Context(), mustPrincipal(c).UserID, request.AddressID, request.SKUID,
		request.Quantity, c.GetHeader("Idempotency-Key"),
	)
	status := http.StatusCreated
	if reused {
		status = http.StatusOK
	}
	respond(c, status, order, err)
}

func (h *handler) listOrders(c *gin.Context) {
	offset := intQuery(c, "offset", 0)
	limit := intQuery(c, "limit", 20)
	orders, total, err := h.service.ListOrders(c.Request.Context(), mustPrincipal(c).UserID, offset, limit)
	respond(c, http.StatusOK, gin.H{"items": orders, "total": total, "offset": offset, "limit": limit}, err)
}

func (h *handler) getOrder(c *gin.Context) {
	order, err := h.service.GetOrder(c.Request.Context(), mustPrincipal(c).UserID, c.Param("order_id"))
	respond(c, http.StatusOK, order, err)
}

func (h *handler) cancelOrder(c *gin.Context) {
	var request struct {
		Reason string `json:"reason"`
	}
	if !bindJSON(c, &request) {
		return
	}
	order, err := h.service.CancelOrder(c.Request.Context(), mustPrincipal(c).UserID, c.Param("order_id"), request.Reason)
	respond(c, http.StatusOK, order, err)
}

func (h *handler) createMockPayment(c *gin.Context) {
	var request struct {
		OrderID string `json:"order_id" binding:"required"`
	}
	if !bindJSON(c, &request) {
		return
	}
	payment, signature, err := h.service.CreateMockPayment(c.Request.Context(), mustPrincipal(c).UserID, request.OrderID)
	respond(c, http.StatusCreated, gin.H{"payment": payment, "callback_signature": signature}, err)
}

func (h *handler) completeMockPayment(c *gin.Context) {
	var request struct {
		PaymentNo   string `json:"payment_no" binding:"required"`
		CallbackRef string `json:"callback_ref" binding:"required"`
		Signature   string `json:"signature" binding:"required"`
	}
	if !bindJSON(c, &request) {
		return
	}
	order, err := h.service.CompleteMockPayment(c.Request.Context(), request.PaymentNo, request.CallbackRef, request.Signature)
	respond(c, http.StatusOK, order, err)
}

func (h *handler) shipOrder(c *gin.Context) {
	var request struct {
		Carrier    string `json:"carrier" binding:"required"`
		TrackingNo string `json:"tracking_no" binding:"required"`
	}
	if !bindJSON(c, &request) {
		return
	}
	order, err := h.service.ShipOrder(c.Request.Context(), mustPrincipal(c).UserID, c.Param("order_id"), request.Carrier, request.TrackingNo)
	respond(c, http.StatusOK, order, err)
}

func (h *handler) confirmOrder(c *gin.Context) {
	order, err := h.service.ConfirmOrder(c.Request.Context(), mustPrincipal(c).UserID, c.Param("order_id"))
	respond(c, http.StatusOK, order, err)
}

func (h *handler) refundOrder(c *gin.Context) {
	var request struct {
		Reason string `json:"reason" binding:"required"`
	}
	if !bindJSON(c, &request) {
		return
	}
	refund, order, err := h.service.RefundOrder(c.Request.Context(), mustPrincipal(c).UserID, c.Param("order_id"), request.Reason)
	respond(c, http.StatusOK, gin.H{"refund": refund, "order": order}, err)
}

func authMiddleware(manager *platformauth.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			httpx.Error(c, http.StatusUnauthorized, commerce.CodeUnauthorized, "请提供 Bearer 访问令牌")
			return
		}
		claims, err := manager.ParseAccessToken(parts[1])
		if err != nil {
			httpx.Error(c, http.StatusUnauthorized, commerce.CodeUnauthorized, "访问令牌无效或已过期")
			return
		}
		c.Set(principalKey, Principal{UserID: claims.UserID, Role: claims.Role})
		c.Next()
	}
}

func requireRole(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if mustPrincipal(c).Role != role {
			httpx.Error(c, http.StatusForbidden, commerce.CodeForbidden, "权限不足")
			return
		}
		c.Next()
	}
}

func mustPrincipal(c *gin.Context) Principal {
	value, _ := c.Get(principalKey)
	principal, _ := value.(Principal)
	return principal
}

func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		if requestID == "" {
			buffer := make([]byte, 12)
			if _, err := rand.Read(buffer); err == nil {
				requestID = hex.EncodeToString(buffer)
			}
		}
		c.Set("request_id", requestID)
		if requestID != "" {
			c.Header("X-Request-ID", requestID)
		}
		c.Next()
	}
}

func bindJSON(c *gin.Context, target any) bool {
	if err := c.ShouldBindJSON(target); err != nil {
		httpx.Error(c, http.StatusBadRequest, commerce.CodeValidation, "请求参数格式不正确")
		return false
	}
	return true
}

func respond(c *gin.Context, status int, data any, err error) {
	if err != nil {
		respondError(c, err)
		return
	}
	httpx.OK(c, status, data)
}

func respondError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	switch commerce.ErrorCode(err) {
	case commerce.CodeValidation:
		status = http.StatusBadRequest
	case commerce.CodeUnauthorized:
		status = http.StatusUnauthorized
	case commerce.CodeForbidden:
		status = http.StatusForbidden
	case commerce.CodeNotFound:
		status = http.StatusNotFound
	case commerce.CodeConflict, commerce.CodeOutOfStock, commerce.CodeInvalidTransition:
		status = http.StatusConflict
	}
	httpx.Error(c, status, commerce.ErrorCode(err), commerce.PublicMessage(err))
}

func uintParam(c *gin.Context, name string) (uint64, bool) {
	value, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || value == 0 {
		httpx.Error(c, http.StatusBadRequest, commerce.CodeValidation, "路径参数无效")
		return 0, false
	}
	return value, true
}

func intQuery(c *gin.Context, name string, fallback int) int {
	value := strings.TrimSpace(c.Query(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
