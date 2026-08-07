// Package testkit 暴露 Order 的内存测试组装入口。
package testkit

import order "seckill-mall/services/order/internal/app"

var (
	NewGRPCCatalogClient   = order.NewGRPCCatalogClient
	NewGRPCIdentityClient  = order.NewGRPCIdentityClient
	NewGRPCInventoryClient = order.NewGRPCInventoryClient
	NewGRPCServer          = order.NewGRPCServer
	NewMemoryRepository    = order.NewMemoryRepository
	NewService             = order.NewService
)
