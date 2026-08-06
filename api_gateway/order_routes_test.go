package main

import (
	"context"
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

	"seckill-mall/catalog_service"
	"seckill-mall/common/config"
	"seckill-mall/common/pb"
	"seckill-mall/identity_service"
	"seckill-mall/internal/order"
	platformauth "seckill-mall/internal/platform/auth"
	"seckill-mall/inventory_service"
)

func TestOrderRoutesUseOrderGRPCWithoutGatewayInventoryCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config.Conf = &config.Config{Server: config.ServerConfig{Mode: "release"}, JWT: config.JWTConfig{Secret: strings.Repeat("g", 32)}}
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	conn, err := grpc.DialContext(context.Background(), "bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.DialContext() error = %v", err)
	}

	catalogServer, _ := catalogservice.NewServer(catalogservice.NewMemoryRepository())
	inventoryStore := inventoryservice.NewMemoryStore(map[uint64]int32{1: 5}, 1)
	inventoryServer, _ := inventoryservice.NewServer(inventoryStore)
	identityServer, _ := identityservice.NewServer(identityservice.NewMemoryRepository(identityservice.Address{ID: 7, UserID: 9, Recipient: "测试用户", Phone: "13800000000", Detail: "测试地址"}))
	orderService, err := order.NewService(order.NewMemoryRepository(), order.NewGRPCCatalogClient(pb.NewCatalogServiceClient(conn)), order.NewGRPCIdentityClient(pb.NewIdentityServiceClient(conn)), order.NewGRPCInventoryClient(pb.NewInventoryServiceClient(conn)), time.Minute)
	if err != nil {
		t.Fatalf("order.NewService() error = %v", err)
	}
	orderServer, _ := order.NewGRPCServer(orderService)
	pb.RegisterCatalogServiceServer(grpcServer, catalogServer)
	pb.RegisterInventoryServiceServer(grpcServer, inventoryServer)
	pb.RegisterIdentityServiceServer(grpcServer, identityServer)
	pb.RegisterCommerceOrderServiceServer(grpcServer, orderServer)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = conn.Close()
		_ = listener.Close()
	})

	authManager, err := platformauth.NewManager(config.Conf.JWT.Secret, time.Hour, 24*time.Hour)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	token, _, err := authManager.IssueAccessToken(9, "customer")
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}
	router := gin.New()
	registerRoutes(router, grpcClients{commerceOrder: pb.NewCommerceOrderServiceClient(conn)})

	created := gatewayOrderRequest(t, router, http.MethodPost, "/api/v1/orders", token, `{"address_id":7,"items":[{"sku_id":1,"quantity":1}]}`, "gateway-order-1", http.StatusCreated)
	if !strings.Contains(created, `"status":"pending_payment"`) || inventoryStore.AvailableStock(1) != 4 {
		t.Fatalf("created response=%s stock=%d", created, inventoryStore.AvailableStock(1))
	}
	duplicate := gatewayOrderRequest(t, router, http.MethodPost, "/api/v1/orders", token, `{"address_id":7,"items":[{"sku_id":1,"quantity":1}]}`, "gateway-order-1", http.StatusOK)
	if !strings.Contains(duplicate, `"reused":true`) || inventoryStore.AvailableStock(1) != 4 {
		t.Fatalf("duplicate response=%s stock=%d", duplicate, inventoryStore.AvailableStock(1))
	}
	orderID := extractOrderID(t, created)
	gatewayOrderRequest(t, router, http.MethodGet, "/api/v1/orders/"+orderID, token, "", "", http.StatusOK)
	gatewayOrderRequest(t, router, http.MethodGet, "/api/v1/orders?limit=10", token, "", "", http.StatusOK)
	gatewayOrderRequest(t, router, http.MethodPost, "/api/v1/orders/"+orderID+"/cancel", token, `{"reason":"测试取消"}`, "", http.StatusOK)
	if inventoryStore.AvailableStock(1) != 5 {
		t.Fatalf("stock after cancellation = %d, want 5", inventoryStore.AvailableStock(1))
	}

	seckill := gatewayOrderRequest(t, router, http.MethodPost, "/api/v1/seckill/orders", token, `{"address_id":7,"sku_id":1,"quantity":1,"activity_id":100}`, "gateway-seckill-1", http.StatusCreated)
	if inventoryStore.AvailableStock(1) != 4 {
		t.Fatalf("seckill stock = %d, want one admission deduction", inventoryStore.AvailableStock(1))
	}
	gatewayOrderRequest(t, router, http.MethodPost, "/api/v1/seckill/orders", token, `{"address_id":7,"sku_id":1,"quantity":1,"activity_id":100}`, "gateway-seckill-1", http.StatusOK)
	if inventoryStore.AvailableStock(1) != 4 {
		t.Fatalf("duplicate seckill stock = %d, want unchanged", inventoryStore.AvailableStock(1))
	}
	seckillID := extractOrderID(t, seckill)
	otherToken, _, err := authManager.IssueAccessToken(10, "customer")
	if err != nil {
		t.Fatalf("GenerateToken(other) error = %v", err)
	}
	gatewayOrderRequest(t, router, http.MethodGet, "/api/v1/orders/"+seckillID, otherToken, "", "", http.StatusNotFound)
	gatewayOrderRequest(t, router, http.MethodPost, "/api/v1/orders/"+seckillID+"/cancel", token, `{}`, "", http.StatusOK)
	if inventoryStore.AvailableStock(1) != 5 {
		t.Fatalf("stock after seckill cancellation = %d, want 5", inventoryStore.AvailableStock(1))
	}
}

func gatewayOrderRequest(t *testing.T, handler http.Handler, method, path, token, body, idempotency string, wantStatus int) string {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if idempotency != "" {
		request.Header.Set("Idempotency-Key", idempotency)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != wantStatus {
		t.Fatalf("%s %s status=%d body=%s", method, path, recorder.Code, recorder.Body.String())
	}
	return recorder.Body.String()
}

func extractOrderID(t *testing.T, body string) string {
	t.Helper()
	marker := `"order_id":"`
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatalf("order_id missing from response: %s", body)
	}
	start += len(marker)
	end := strings.Index(body[start:], `"`)
	if end < 0 {
		t.Fatalf("order_id is malformed: %s", body)
	}
	return body[start : start+end]
}
