// 本文件负责 Gateway 进程级信号、追踪与客户端生命周期。
package gateway

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"seckill-mall/shared/platform/config"
	"seckill-mall/shared/platform/internalcall"
	"seckill-mall/shared/platform/tracer"
)

// Run 启动 API Gateway，并在进程信号到达时有界关闭 HTTP 和下游客户端。
func Run() (runErr error) {
	// 先加载配置
	config.InitConfig("gateway")
	if strings.EqualFold(config.Conf.Server.Mode, "release") {
		if err := internalcall.ValidateSecret(os.Getenv("SECKILL_INTERNAL_CALL_SECRET")); err != nil {
			return fmt.Errorf("gateway internal call config invalid: %w", err)
		}
	}

	//初始化链路追踪
	shutdown := tracer.InitTracer("api-gateway", tracer.EndpointFromEnv())
	defer shutdown(context.Background())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	clients, err := initGRPCClients()
	if err != nil {
		return err
	}
	defer func() {
		runErr = errors.Join(runErr, clients.Close())
	}()
	r := setupRouter(clients)
	return startHTTPServer(ctx, r)
}
