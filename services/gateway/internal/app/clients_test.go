// Gateway 客户端生命周期测试验证下游连接和发现客户端的关闭顺序。
package gateway

import (
	"errors"
	"io"
	"reflect"
	"testing"
)

type recordingCloser struct {
	name  string
	calls *[]string
	err   error
}

func (c recordingCloser) Close() error {
	*c.calls = append(*c.calls, c.name)
	return c.err
}

func TestGRPCClientsCloseResourcesInReverseOrder(t *testing.T) {
	firstErr := errors.New("first close failed")
	lastErr := errors.New("last close failed")
	var calls []string
	clients := grpcClients{closers: []io.Closer{
		recordingCloser{name: "etcd", calls: &calls, err: firstErr},
		recordingCloser{name: "catalog", calls: &calls},
		recordingCloser{name: "inventory", calls: &calls, err: lastErr},
	}}

	err := clients.Close()
	if !errors.Is(err, firstErr) || !errors.Is(err, lastErr) {
		t.Fatalf("Close() error = %v, want both resource errors", err)
	}
	if want := []string{"inventory", "catalog", "etcd"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("close order = %v, want %v", calls, want)
	}

	if err := clients.Close(); err != nil {
		t.Fatalf("second Close() error = %v, want nil", err)
	}
	if len(calls) != 3 {
		t.Fatalf("second Close() closed resources again: calls=%v", calls)
	}
}

func TestDialServiceRequiresDiscoveryForEmptyAddress(t *testing.T) {
	connection, err := dialService("catalog-service", "", nil)
	if err == nil {
		_ = connection.Close()
		t.Fatal("dialService() error = nil, want unavailable discovery error")
	}
}
