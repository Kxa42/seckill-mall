package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"seckill-mall/internal/commerce"
	platformauth "seckill-mall/internal/platform/auth"
)

type apiResponse[T any] struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Data      T      `json:"data"`
	RequestID string `json:"request_id"`
}

func newTestRouter(t *testing.T) (*gin.Engine, *commerce.Service) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repository := commerce.NewMemoryRepository()
	authManager, err := platformauth.NewManager(strings.Repeat("j", 32), 15*time.Minute, 24*time.Hour)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	service, err := commerce.NewService(repository, authManager, strings.Repeat("p", 32), 15*time.Minute)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if err := service.EnsureAdmin(context.Background(), "admin@example.com", "admin-password"); err != nil {
		t.Fatalf("EnsureAdmin() error = %v", err)
	}
	router, err := NewRouter(RouterConfig{Service: service, Auth: authManager})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	return router, service
}

func requestJSON[T any](t *testing.T, router http.Handler, method, path, token string, body any, headers map[string]string, wantStatus int) apiResponse[T] {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(payload))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != wantStatus {
		t.Fatalf("%s %s status = %d, body = %s", method, path, recorder.Code, recorder.Body.String())
	}
	if wantStatus == http.StatusNoContent {
		return apiResponse[T]{}
	}
	var response apiResponse[T]
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode %s %s response: %v, body=%s", method, path, err, recorder.Body.String())
	}
	if recorder.Header().Get("X-Request-ID") == "" || response.RequestID == "" {
		t.Fatalf("%s %s missing request id", method, path)
	}
	return response
}

func TestHTTPCommerceAcceptance(t *testing.T) {
	router, _ := newTestRouter(t)

	registered := requestJSON[commerce.TokenPair](t, router, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email": "buyer@example.com", "password": "password-123",
	}, nil, http.StatusCreated)
	access := registered.Data.AccessToken

	addressResponse := requestJSON[commerce.Address](t, router, http.MethodPost, "/api/v1/addresses", access, map[string]any{
		"recipient": "测试用户", "phone": "13800000000", "province": "浙江省",
		"city": "杭州市", "district": "西湖区", "detail": "测试路 1 号", "is_default": true,
	}, nil, http.StatusCreated)
	address := addressResponse.Data

	products := requestJSON[commerce.ProductPage](t, router, http.MethodGet, "/api/v1/products", "", nil, nil, http.StatusOK)
	if products.Data.Total != 1 || len(products.Data.Items[0].SKUs) != 1 {
		t.Fatalf("products = %+v", products.Data)
	}
	requestJSON[commerce.CartItem](t, router, http.MethodPost, "/api/v1/cart/items", access, map[string]any{
		"sku_id": 1, "quantity": 1,
	}, nil, http.StatusOK)
	preview := requestJSON[commerce.CheckoutPreview](t, router, http.MethodGet, "/api/v1/cart/checkout-preview", access, nil, nil, http.StatusOK)
	if !preview.Data.Available || preview.Data.TotalAmountCents != 699900 {
		t.Fatalf("checkout preview = %+v", preview.Data)
	}

	orderResponse := requestJSON[commerce.Order](t, router, http.MethodPost, "/api/v1/orders", access, map[string]any{
		"address_id": address.ID,
	}, map[string]string{"Idempotency-Key": "http-checkout-1"}, http.StatusCreated)
	order := orderResponse.Data
	duplicate := requestJSON[commerce.Order](t, router, http.MethodPost, "/api/v1/orders", access, map[string]any{
		"address_id": address.ID,
	}, map[string]string{"Idempotency-Key": "http-checkout-1"}, http.StatusOK)
	if duplicate.Data.OrderID != order.OrderID {
		t.Fatalf("idempotent order mismatch: %s != %s", duplicate.Data.OrderID, order.OrderID)
	}

	type paymentPayload struct {
		Payment           commerce.Payment `json:"payment"`
		CallbackSignature string           `json:"callback_signature"`
	}
	paymentResponse := requestJSON[paymentPayload](t, router, http.MethodPost, "/api/v1/payments/mock", access, map[string]any{
		"order_id": order.OrderID,
	}, nil, http.StatusCreated)
	paid := requestJSON[commerce.Order](t, router, http.MethodPost, "/api/v1/payments/mock/callback", "", map[string]any{
		"payment_no":   paymentResponse.Data.Payment.PaymentNo,
		"callback_ref": "http-callback-1",
		"signature":    paymentResponse.Data.CallbackSignature,
	}, nil, http.StatusOK)
	if paid.Data.Status != commerce.OrderStatusPaid {
		t.Fatalf("paid order status = %s", paid.Data.Status)
	}

	requestJSON[any](t, router, http.MethodPost, "/api/v1/admin/orders/"+order.OrderID+"/ship", access, map[string]any{
		"carrier": "SF", "tracking_no": "SF-HTTP-001",
	}, nil, http.StatusForbidden)
	admin := requestJSON[commerce.TokenPair](t, router, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
		"email": "admin@example.com", "password": "admin-password",
	}, nil, http.StatusOK)
	shipped := requestJSON[commerce.Order](t, router, http.MethodPost, "/api/v1/admin/orders/"+order.OrderID+"/ship", admin.Data.AccessToken, map[string]any{
		"carrier": "SF", "tracking_no": "SF-HTTP-001",
	}, nil, http.StatusOK)
	if shipped.Data.Status != commerce.OrderStatusShipped {
		t.Fatalf("shipped order status = %s", shipped.Data.Status)
	}
	completed := requestJSON[commerce.Order](t, router, http.MethodPost, "/api/v1/orders/"+order.OrderID+"/confirm", access, nil, nil, http.StatusOK)
	if completed.Data.Status != commerce.OrderStatusCompleted {
		t.Fatalf("completed order status = %s", completed.Data.Status)
	}
}

func TestHTTPRejectsUnauthorizedAndInvalidPaymentSignature(t *testing.T) {
	router, _ := newTestRouter(t)
	requestJSON[any](t, router, http.MethodGet, "/api/v1/cart/items", "", nil, nil, http.StatusUnauthorized)
	requestJSON[any](t, router, http.MethodPost, "/api/v1/payments/mock/callback", "", map[string]any{
		"payment_no": "pay_missing", "callback_ref": "callback", "signature": "invalid",
	}, nil, http.StatusUnauthorized)
}
