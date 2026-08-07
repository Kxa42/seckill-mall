// Package testkit 暴露 Inventory 的内存测试组装入口。
package testkit

import inventoryservice "seckill-mall/services/inventory/internal/app"

type MemoryStore = inventoryservice.MemoryStore

var (
	NewMemoryStore = inventoryservice.NewMemoryStore
	NewServer      = inventoryservice.NewServer
)
