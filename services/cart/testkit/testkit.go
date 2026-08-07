// Package testkit 暴露 Cart 的内存测试组装入口。
package testkit

import cartservice "seckill-mall/services/cart/internal/app"

var (
	NewGRPCCatalogClient = cartservice.NewGRPCCatalogClient
	NewMemoryRepository  = cartservice.NewMemoryRepository
	NewService           = cartservice.NewService
	NewServer            = cartservice.NewServer
)
