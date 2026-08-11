// appkit 测试覆盖 gRPC/HTTP 启停、健康检查与 metrics 生命周期。
package appkit

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"

	"seckill-mall/shared/contracts"
)

const bufSize = 1024 * 1024

func dialBufconn(lis *bufconn.Listener) (*grpc.ClientConn, error) {
	return grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
}

func TestValidateContract(t *testing.T) {
	ValidateContract(contracts.ServiceCatalog)
}

func TestRegisterHealth(t *testing.T) {
	lis := bufconn.Listen(bufSize)
	server := grpc.NewServer()
	RegisterHealth(server, "commerce.catalog.v1.CatalogService")
	go func() { _ = server.Serve(lis) }()
	defer server.Stop()

	conn, err := dialBufconn(lis)
	if err != nil {
		t.Fatalf("grpc client create failed: %v", err)
	}
	defer conn.Close()
	client := grpc_health_v1.NewHealthClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	resp, err := client.Check(ctx, &grpc_health_v1.HealthCheckRequest{Service: "commerce.catalog.v1.CatalogService"})
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	if resp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("status = %v, want SERVING", resp.Status)
	}
}

func TestServeWithShutdown(t *testing.T) {
	lis := bufconn.Listen(bufSize)
	server := grpc.NewServer()
	RegisterHealth(server, "commerce.catalog.v1.CatalogService")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		ServeWithShutdown(ctx, server, lis)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServeWithShutdown did not return after cancel")
	}
}

func TestServeHTTPWithShutdownWaitsForActiveRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("http listen failed: %v", err)
	}
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		<-releaseRequest
		writer.WriteHeader(http.StatusNoContent)
	})}
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- serveHTTPWithShutdown(ctx, server, func() error {
			return server.Serve(listener)
		}, time.Second)
	}()

	requestDone := make(chan error, 1)
	client := &http.Client{Timeout: 2 * time.Second}
	go func() {
		response, requestErr := client.Get("http://" + listener.Addr().String())
		if requestErr == nil {
			_ = response.Body.Close()
		}
		requestDone <- requestErr
	}()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("http request did not reach handler")
	}

	cancel()
	select {
	case err := <-serveDone:
		t.Fatalf("server returned before active request completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseRequest)

	select {
	case err := <-requestDone:
		if err != nil {
			t.Fatalf("active request failed during graceful shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("active request did not complete")
	}
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("ServeHTTPWithShutdown() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ServeHTTPWithShutdown did not return")
	}
}

func TestServeHTTPWithShutdownForcesCloseAtDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("http listen failed: %v", err)
	}
	requestStarted := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(requestStarted)
		<-request.Context().Done()
	})}
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- serveHTTPWithShutdown(ctx, server, func() error {
			return server.Serve(listener)
		}, 25*time.Millisecond)
	}()
	requestDone := make(chan error, 1)
	client := &http.Client{Timeout: 2 * time.Second}
	go func() {
		response, requestErr := client.Get("http://" + listener.Addr().String())
		if requestErr == nil {
			_ = response.Body.Close()
		}
		requestDone <- requestErr
	}()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("http request did not reach handler")
	}

	cancel()
	select {
	case err := <-serveDone:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("ServeHTTPWithShutdown() error = %v, want deadline exceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ServeHTTPWithShutdown exceeded its shutdown deadline")
	}
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("forced shutdown did not release active request")
	}
}

func TestStartMetricsServerStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := StartMetricsServer(ctx, "0")
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("metrics server shutdown failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("metrics server did not stop after context cancellation")
	}
}

func TestStartMetricsServerSkipsEmptyPort(t *testing.T) {
	done := StartMetricsServer(context.Background(), "  ")
	select {
	case err, ok := <-done:
		if ok || err != nil {
			t.Fatalf("disabled metrics result = (%v, %t), want closed channel", err, ok)
		}
	case <-time.After(time.Second):
		t.Fatal("disabled metrics server did not complete immediately")
	}
}
