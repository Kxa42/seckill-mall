// Catalog gRPC 契约测试不依赖 MySQL，通过内存 Repository 和 bufconn 验证服务边界。
package catalogservice

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"seckill-mall/shared/gen/commerce"
)

func TestCatalogServerOverBufconn(t *testing.T) {
	server, err := NewServer(NewMemoryRepository())
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	pb.RegisterCatalogServiceServer(grpcServer, server)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = listener.Close()
	})

	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.DialContext() error = %v", err)
	}
	defer conn.Close()
	client := pb.NewCatalogServiceClient(conn)

	page, err := client.ListProducts(ctx, &pb.CatalogListProductsRequest{Offset: 0, Limit: 20})
	if err != nil {
		t.Fatalf("ListProducts() error = %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || len(page.Items[0].Skus) != 1 {
		t.Fatalf("unexpected page = %+v", page)
	}
	if page.Items[0].Skus[0].AvailableStock != 100 || page.Items[0].Skus[0].PriceCents != 699900 {
		t.Fatalf("unexpected sku snapshot = %+v", page.Items[0].Skus[0])
	}

	product, err := client.GetProduct(ctx, &pb.CatalogGetProductRequest{SpuId: 1})
	if err != nil || product.GetSpuId() != 1 {
		t.Fatalf("GetProduct() product=%+v error=%v", product, err)
	}

	_, err = client.GetProduct(ctx, &pb.CatalogGetProductRequest{SpuId: 999})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("GetProduct(not found) code = %s, want %s", status.Code(err), codes.NotFound)
	}
}

func TestCatalogServerRejectsInvalidRequest(t *testing.T) {
	server, err := NewServer(NewMemoryRepository())
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	_, err = server.GetSKUSnapshot(context.Background(), &pb.CatalogGetSKUSnapshotRequest{SkuIds: []uint64{0}})
	if status.Code(err) != codes.NotFound && status.Code(err) != codes.InvalidArgument {
		t.Fatalf("GetSKUSnapshot(invalid) code = %s", status.Code(err))
	}
	_, err = server.ListProducts(context.Background(), &pb.CatalogListProductsRequest{Offset: -1, Limit: 20})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ListProducts(invalid) code = %s", status.Code(err))
	}
}
