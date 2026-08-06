package main

import (
	"context"
	"log"
	"os"
	"strings"

	"seckill-mall/common/config"
	"seckill-mall/common/internalcall"
	"seckill-mall/common/tracer"
)

func main() {
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

	// 初始化 Sentinel
	initSentinel()

	clients := initGRPCClients()
	r := setupRouter(clients)
	startHTTPServer(r)
}
