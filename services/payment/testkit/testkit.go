// Package testkit 暴露 Payment 的内存测试组装入口。
package testkit

import paymentservice "seckill-mall/services/payment/internal/app"

var (
	NewMemoryRepository = paymentservice.NewMemoryRepository
	NewService          = paymentservice.NewService
	NewServer           = paymentservice.NewServer
	NewGRPCOrderClient  = paymentservice.NewGRPCOrderClient
)
