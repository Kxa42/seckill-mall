// Gateway Catalog 路由测试使用 Catalog gRPC bufconn，验证查询已不再依赖 Commerce HTTP 代理。
package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"seckill-mall/catalog_service"
	"seckill-mall/common/config"
	"seckill-mall/common/pb"
)

func TestCatalogRoutesUseCatalogGRPC(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config.Conf = &config.Config{}
	catalogServer, err := catalogservice.NewServer(catalogservice.NewMemoryRepository())
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	pb.RegisterCatalogServiceServer(grpcServer, catalogServer)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = listener.Close()
	})

	conn, err := grpc.DialContext(context.Background(), "bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.DialContext() error = %v", err)
	}
	defer conn.Close()

	router := gin.New()
	registerRoutes(router, grpcClients{catalog: pb.NewCatalogServiceClient(conn)})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/products?limit=10", nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/products status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"spu"`) || !strings.Contains(body, `"price_cents":699900`) {
		t.Fatalf("Catalog response does not preserve HTTP shape: %s", body)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/v1/products/1", nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"id":1`) {
		t.Fatalf("GET /api/v1/products/1 status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
