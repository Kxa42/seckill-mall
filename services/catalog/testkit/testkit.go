// Package testkit 暴露 Catalog 的内存测试组装入口。
package testkit

import catalogservice "seckill-mall/services/catalog/internal/app"

var (
	NewMemoryRepository = catalogservice.NewMemoryRepository
	NewServer           = catalogservice.NewServer
)
