// Gateway 路由测试验证旧兼容接口不会泄漏到生产模式。
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"seckill-mall/common/config"
)

func TestLegacyLoginIsDisabledInReleaseMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config.Conf = &config.Config{
		Server: config.ServerConfig{Mode: "release"},
		JWT:    config.JWTConfig{Secret: strings.Repeat("j", 32)},
	}
	router := gin.New()
	registerRoutes(router, grpcClients{})

	request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"user_id":1}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("POST /login status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}
