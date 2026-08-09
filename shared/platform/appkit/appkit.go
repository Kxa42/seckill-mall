// Package appkit 提供服务进程启动所需的通用脚手架：契约校验、etcd 注册、
// gRPC 健康检查与优雅停机，供各业务服务入口统一调用。
package appkit

import (
	"context"
	"errors"
	"log"
	"net"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	"seckill-mall/shared/contracts"
	"seckill-mall/shared/platform/config"
	"seckill-mall/shared/platform/discovery"
)

// ValidateContract 校验服务契约边界定义有效，校验失败时终止进程。
func ValidateContract(service string) {
	if err := contracts.ValidateServiceBoundaries(); err != nil {
		log.Fatalf("service contract validation failed: %v", err)
	}
	if _, ok := contracts.ServiceBoundaryFor(service); !ok {
		log.Fatalf("service contract is not defined: %s", service)
	}
}

// RegisterService 将服务注册到 etcd；注册失败时记录日志并返回 nil，进程降级继续。
func RegisterService(ctx context.Context, service, address, port string) *discovery.Registration {
	if strings.TrimSpace(address) == "" {
		address = "127.0.0.1:" + port
	}
	registration, err := discovery.Register(ctx, config.Conf.Etcd.Addr, service, config.AdvertiseAddr(address))
	if err != nil {
		log.Printf("etcd registration skipped service=%s err=%v", service, err)
		return nil
	}
	return registration
}

// RegisterHealth 注册 gRPC 健康检查服务，空服务名与具体服务名均置为 SERVING。
func RegisterHealth(server *grpc.Server, service string) {
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus(service, grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(server, healthServer)
}

// ServeWithShutdown 启动 gRPC 服务；ctx 取消时优雅停机。
func ServeWithShutdown(ctx context.Context, server *grpc.Server, listener net.Listener) {
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Fatalf("grpc service stopped: %v", err)
		}
	case <-ctx.Done():
		server.GracefulStop()
		if err := <-serveErr; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Printf("grpc service graceful stop: %v", err)
		}
	}
}
