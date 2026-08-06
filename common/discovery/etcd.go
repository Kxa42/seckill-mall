// Package discovery 提供基于 etcd endpoint manager 的服务注册能力。
// 注册失败由调用方决定是否降级，便于本地无 etcd 时使用内存验收实现。
package discovery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/naming/endpoints"
)

// Registration 表示一个带租约的 etcd 服务注册。
type Registration struct {
	client      *clientv3.Client
	manager     endpoints.Manager
	endpointKey string
	leaseID     clientv3.LeaseID
}

// Register 将 address 注册到 etcd 的 serviceName 下，并持续保活租约。
func Register(ctx context.Context, etcdAddr, serviceName, address string) (*Registration, error) {
	etcdAddr = strings.TrimSpace(etcdAddr)
	serviceName = strings.Trim(strings.TrimSpace(serviceName), "/")
	address = strings.TrimSpace(address)
	if etcdAddr == "" || serviceName == "" || address == "" {
		return nil, errors.New("etcd address, service name and endpoint are required")
	}
	client, err := clientv3.New(clientv3.Config{Endpoints: []string{etcdAddr}, DialTimeout: 3 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("create etcd client: %w", err)
	}
	manager, err := endpoints.NewManager(client, serviceName)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("create endpoint manager: %w", err)
	}
	grantCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	lease, err := client.Grant(grantCtx, 10)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("grant etcd lease: %w", err)
	}
	endpointKey := serviceName + "/" + address
	if err := manager.AddEndpoint(grantCtx, endpointKey, endpoints.Endpoint{Addr: address}, clientv3.WithLease(lease.ID)); err != nil {
		_, _ = client.Revoke(context.Background(), lease.ID)
		_ = client.Close()
		return nil, fmt.Errorf("add etcd endpoint: %w", err)
	}
	keepAlive, err := client.KeepAlive(ctx, lease.ID)
	if err != nil {
		_ = manager.DeleteEndpoint(context.Background(), endpointKey)
		_, _ = client.Revoke(context.Background(), lease.ID)
		_ = client.Close()
		return nil, fmt.Errorf("keep etcd lease alive: %w", err)
	}
	go func() {
		for range keepAlive {
		}
	}()
	return &Registration{client: client, manager: manager, endpointKey: endpointKey, leaseID: lease.ID}, nil
}

// Close 移除服务 endpoint、撤销租约并关闭 etcd 客户端。
func (r *Registration) Close(ctx context.Context) error {
	if r == nil || r.client == nil {
		return nil
	}
	var firstErr error
	if err := r.manager.DeleteEndpoint(ctx, r.endpointKey); err != nil && !errors.Is(err, context.Canceled) {
		firstErr = err
	}
	if _, err := r.client.Revoke(ctx, r.leaseID); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := r.client.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}
