package main

import (
	"context"

	"seckill-mall/common/config"
	"seckill-mall/common/tracer"
)

func main() {
	// 先加载配置
	config.InitConfig("gateway")

	//初始化链路追踪
	shutdown := tracer.InitTracer("api-gateway", tracer.EndpointFromEnv())
	defer shutdown(context.Background())

	// 初始化 Sentinel
	initSentinel()

	clients := initGRPCClients()
	r := setupRouter(clients)
	startHTTPServer(r)
}
