package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	cartservice "seckill-mall/services/cart/testkit"
	catalogservice "seckill-mall/services/catalog/testkit"
	fulfillmentservice "seckill-mall/services/fulfillment/testkit"
	identityservice "seckill-mall/services/identity/testkit"
	inventoryservice "seckill-mall/services/inventory/testkit"
	order "seckill-mall/services/order/testkit"
	paymentservice "seckill-mall/services/payment/testkit"
	"seckill-mall/shared/gen/commerce"
	platformauth "seckill-mall/shared/platform/auth"
	"seckill-mall/shared/platform/config"
)

func TestStage4CommerceFlowUsesDomainServices(t *testing.T) {
	gin.SetMode(gin.TestMode)
	jwtSecret := strings.Repeat("j", 32)
	internalSecret := strings.Repeat("h", 32)
	t.Setenv("SECKILL_INTERNAL_CALL_SECRET", internalSecret)
	config.Conf = &config.Config{Server: config.ServerConfig{Mode: "release"}, JWT: config.JWTConfig{Secret: jwtSecret}}

	listener := bufconn.Listen(4 * 1024 * 1024)
	grpcServer := grpc.NewServer()
	conn, err := grpc.DialContext(context.Background(), "bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.DialContext() error = %v", err)
	}
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = conn.Close()
		_ = listener.Close()
	})

	authManager, err := platformauth.NewManager(jwtSecret, time.Hour, 24*time.Hour)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	catalogServer, _ := catalogservice.NewServer(catalogservice.NewMemoryRepository())
	identityServer, _ := identityservice.NewServer(identityservice.NewMemoryRepository(), authManager)
	inventoryStore := inventoryservice.NewMemoryStore(map[uint64]int32{1: 10}, 5)
	inventoryServer, _ := inventoryservice.NewServer(inventoryStore)
	cartService, _ := cartservice.NewService(cartservice.NewMemoryRepository(), cartservice.NewGRPCCatalogClient(pb.NewCatalogServiceClient(conn)))
	cartServer, _ := cartservice.NewServer(cartService)
	orderService, err := order.NewService(order.NewMemoryRepository(), order.NewGRPCCatalogClient(pb.NewCatalogServiceClient(conn)), order.NewGRPCIdentityClient(pb.NewIdentityServiceClient(conn)), order.NewGRPCInventoryClient(pb.NewInventoryServiceClient(conn)), time.Minute)
	if err != nil {
		t.Fatalf("order.NewService() error = %v", err)
	}
	orderServer, _ := order.NewGRPCServer(orderService)
	paymentOrderClient := paymentservice.NewGRPCOrderClient(pb.NewCommerceOrderServiceClient(conn), internalSecret)
	paymentService, err := paymentservice.NewService(paymentservice.NewMemoryRepository(), paymentOrderClient, strings.Repeat("p", 32))
	if err != nil {
		t.Fatalf("paymentservice.NewService() error = %v", err)
	}
	paymentServer, _ := paymentservice.NewServer(paymentService)
	fulfillmentOrderClient := fulfillmentservice.NewGRPCOrderClient(pb.NewCommerceOrderServiceClient(conn), internalSecret)
	fulfillmentService, _ := fulfillmentservice.NewService(fulfillmentservice.NewMemoryRepository(), fulfillmentOrderClient)
	fulfillmentServer, _ := fulfillmentservice.NewServer(fulfillmentService)

	pb.RegisterCatalogServiceServer(grpcServer, catalogServer)
	pb.RegisterIdentityServiceServer(grpcServer, identityServer)
	pb.RegisterInventoryServiceServer(grpcServer, inventoryServer)
	pb.RegisterCartServiceServer(grpcServer, cartServer)
	pb.RegisterCommerceOrderServiceServer(grpcServer, orderServer)
	pb.RegisterPaymentServiceServer(grpcServer, paymentServer)
	pb.RegisterFulfillmentServiceServer(grpcServer, fulfillmentServer)
	go func() { _ = grpcServer.Serve(listener) }()

	router := gin.New()
	registerRoutes(router, grpcClients{
		catalog:       pb.NewCatalogServiceClient(conn),
		identity:      pb.NewIdentityServiceClient(conn),
		cart:          pb.NewCartServiceClient(conn),
		commerceOrder: pb.NewCommerceOrderServiceClient(conn),
		payment:       pb.NewPaymentServiceClient(conn),
		fulfillment:   pb.NewFulfillmentServiceClient(conn),
	})

	registered := stage4Request(t, router, http.MethodPost, "/api/v1/auth/register", "", map[string]any{"email": "buyer@example.com", "password": "correct-password"}, "", http.StatusCreated)
	token := stage4String(t, registered, "access_token")
	refreshToken := stage4String(t, registered, "refresh_token")
	if refreshToken == "" {
		t.Fatal("register response did not include refresh_token")
	}
	rotated := stage4Request(t, router, http.MethodPost, "/api/v1/auth/refresh", "", map[string]any{"refresh_token": refreshToken}, "", http.StatusOK)
	if stage4String(t, rotated, "refresh_token") == refreshToken {
		t.Fatal("refresh token was not rotated")
	}
	stage4Request(t, router, http.MethodPost, "/api/v1/auth/refresh", "", map[string]any{"refresh_token": refreshToken}, "", http.StatusUnauthorized)

	address := stage4Request(t, router, http.MethodPost, "/api/v1/addresses", token, map[string]any{
		"recipient": "测试用户", "phone": "13800000000", "province": "浙江省", "city": "杭州市", "district": "西湖区", "detail": "测试地址 1 号", "is_default": true,
	}, "", http.StatusCreated)
	addressID := stage4Uint(t, address, "id")
	stage4Request(t, router, http.MethodPost, "/api/v1/cart/items", token, map[string]any{"sku_id": 1, "quantity": 1}, "", http.StatusOK)
	preview := stage4Request(t, router, http.MethodGet, "/api/v1/cart/preview", token, nil, "", http.StatusOK)
	if stage4Uint(t, preview, "total_amount_cents") != 699900 {
		t.Fatalf("cart preview total = %v", preview["total_amount_cents"])
	}

	firstOrder := stage4Request(t, router, http.MethodPost, "/api/v1/orders", token, map[string]any{"address_id": addressID}, "stage4-order-1", http.StatusCreated)
	firstOrderID := stage4String(t, firstOrder, "order_id")
	payment := stage4Request(t, router, http.MethodPost, "/api/v1/orders/"+firstOrderID+"/payments", token, nil, "", http.StatusCreated)
	paymentNo := stage4String(t, payment, "payment_no")
	callbackSignature := stage4String(t, payment, "callback_signature")
	duplicatePayment := stage4Request(t, router, http.MethodPost, "/api/v1/orders/"+firstOrderID+"/payments", token, nil, "", http.StatusOK)
	if stage4String(t, duplicatePayment, "payment_no") != paymentNo {
		t.Fatalf("duplicate payment changed payment_no: %+v", duplicatePayment)
	}
	stage4Request(t, router, http.MethodPost, "/api/v1/payments/callback", "", map[string]any{"payment_no": paymentNo, "callback_ref": "callback-stage4-1", "signature": "invalid"}, "", http.StatusUnauthorized)
	stage4Request(t, router, http.MethodPost, "/api/v1/payments/callback", "", map[string]any{"payment_no": paymentNo, "callback_ref": "callback-stage4-1", "signature": callbackSignature}, "", http.StatusOK)
	stage4Request(t, router, http.MethodPost, "/api/v1/payments/callback", "", map[string]any{"payment_no": paymentNo, "callback_ref": "callback-stage4-1", "signature": callbackSignature}, "", http.StatusOK)

	customerOnAdmin := stage4Request(t, router, http.MethodPost, "/api/v1/admin/orders/"+firstOrderID+"/ship", token, map[string]any{"carrier": "SF", "tracking_no": "SF-1"}, "", http.StatusForbidden)
	if len(customerOnAdmin) != 0 {
		t.Fatalf("forbidden response unexpectedly returned data: %+v", customerOnAdmin)
	}
	adminToken, _, err := authManager.IssueAccessToken(99, "admin")
	if err != nil {
		t.Fatalf("IssueAccessToken(admin) error = %v", err)
	}
	stage4Request(t, router, http.MethodPost, "/api/v1/admin/orders/"+firstOrderID+"/ship", adminToken, map[string]any{"carrier": "SF", "tracking_no": "SF-1"}, "", http.StatusOK)
	stage4Request(t, router, http.MethodPost, "/api/v1/orders/"+firstOrderID+"/receipt", token, nil, "", http.StatusOK)
	completed := stage4Request(t, router, http.MethodGet, "/api/v1/orders/"+firstOrderID, token, nil, "", http.StatusOK)
	if stage4String(t, completed, "status") != "completed" {
		t.Fatalf("completed order status = %q", completed["status"])
	}

	secondOrder := stage4Request(t, router, http.MethodPost, "/api/v1/orders", token, map[string]any{"address_id": addressID}, "stage4-order-2", http.StatusCreated)
	secondOrderID := stage4String(t, secondOrder, "order_id")
	secondPayment := stage4Request(t, router, http.MethodPost, "/api/v1/orders/"+secondOrderID+"/payments", token, nil, "", http.StatusCreated)
	stage4Request(t, router, http.MethodPost, "/api/v1/payments/callback", "", map[string]any{"payment_no": stage4String(t, secondPayment, "payment_no"), "callback_ref": "callback-stage4-2", "signature": stage4String(t, secondPayment, "callback_signature")}, "", http.StatusOK)
	refund := stage4Request(t, router, http.MethodPost, "/api/v1/orders/"+secondOrderID+"/refunds", token, map[string]any{"reason": "测试退款"}, "", http.StatusOK)
	if stage4String(t, refund, "status") != "succeeded" || stage4String(t, refund, "order_status") != "refunded" {
		t.Fatalf("refund response = %+v", refund)
	}

	// 旧格式/任意签发的令牌必须被 CommerceJWTAuth 拒绝。
	stage4Request(t, router, http.MethodGet, "/api/v1/addresses", "invalid.token.value", nil, "", http.StatusUnauthorized)
}

func stage4Request(t *testing.T, handler http.Handler, method, path, token string, body any, idempotencyKey string, wantStatus int) map[string]any {
	t.Helper()
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != wantStatus {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, path, recorder.Code, wantStatus, recorder.Body.String())
	}
	if recorder.Body.Len() == 0 {
		return map[string]any{}
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope body=%s err=%v", recorder.Body.String(), err)
	}
	if len(envelope.Data) == 0 {
		return map[string]any{}
	}
	var data map[string]any
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatalf("decode data body=%s err=%v", recorder.Body.String(), err)
	}
	return data
}

func stage4String(t *testing.T, data map[string]any, name string) string {
	t.Helper()
	value, ok := data[name].(string)
	if !ok || value == "" {
		t.Fatalf("field %q missing from %+v", name, data)
	}
	return value
}

func stage4Uint(t *testing.T, data map[string]any, name string) uint64 {
	t.Helper()
	value, ok := data[name].(float64)
	if !ok || value <= 0 {
		t.Fatalf("field %q missing from %+v", name, data)
	}
	return uint64(value)
}
