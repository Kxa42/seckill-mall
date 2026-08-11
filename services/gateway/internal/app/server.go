// 本文件封装 Gateway HTTP 服务器的启动与有界优雅停机。
package gateway

import (
	"context"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"seckill-mall/shared/platform/appkit"
	"seckill-mall/shared/platform/config"
)

func startHTTPServer(ctx context.Context, r *gin.Engine) error {
	port := config.Conf.Server.Port
	if port == "" {
		port = "8080"
	}

	addr := net.JoinHostPort("", port)
	log.Printf("api gateway started addr=%s", addr)
	server := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return appkit.ServeHTTPWithShutdown(ctx, server, appkit.DefaultHTTPShutdownTimeout)
}
