package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"seckill-mall/common/config"
)

// registerCommerceProxy 将版本化商城 API 转发到 commerce-api。
func registerCommerceProxy(router *gin.Engine) {
	targetValue := strings.TrimSpace(config.Conf.Commerce.URL)
	if targetValue == "" {
		log.Println("commerce-api proxy disabled: commerce.url is empty")
		return
	}
	target, err := url.Parse(targetValue)
	if err != nil || target.Scheme == "" || target.Host == "" {
		log.Printf("commerce-api proxy disabled: invalid target=%q", targetValue)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(writer http.ResponseWriter, request *http.Request, proxyErr error) {
		log.Printf("commerce-api proxy failed path=%s err=%v", request.URL.Path, proxyErr)
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write([]byte(`{"code":"UPSTREAM_UNAVAILABLE","message":"商城服务暂时不可用"}`))
	}
	router.Any("/api/v1/*path", gin.WrapH(proxy))
}
