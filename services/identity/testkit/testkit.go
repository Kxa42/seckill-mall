// Package testkit 暴露 Identity 的内存测试组装入口。
package testkit

import identityservice "seckill-mall/services/identity/internal/app"

type Address = identityservice.Address

var (
	NewMemoryRepository = identityservice.NewMemoryRepository
	NewServer           = identityservice.NewServer
)
