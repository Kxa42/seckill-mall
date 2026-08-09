package appkit

import (
	"context"
	"net"
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
