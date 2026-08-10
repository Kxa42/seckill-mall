package gateway

import (
	"context"
	"log"
	"os"
	"strings"

	"seckill-mall/shared/platform/config"
	"seckill-mall/shared/platform/internalcall"
	"seckill-mall/shared/platform/tracer"
)

// Run 启动 API Gateway。
func Run() {
	// 先加载配置
	config.InitConfig("gateway")
	if strings.EqualFold(config.Conf.Server.Mode, "release") {
		if err := internalcall.ValidateSecret(os.Getenv("SECKILL_INTERNAL_CALL_SECRET")); err != nil {
			log.Fatalf("gateway internal call config invalid: %v", err)
		}
	}

	//初始化链路追踪
	shutdown := tracer.InitTracer("api-gateway", tracer.EndpointFromEnv())
	defer shutdown(context.Background())

	clients := initGRPCClients()
	r := setupRouter(clients)
	startHTTPServer(r)
}
