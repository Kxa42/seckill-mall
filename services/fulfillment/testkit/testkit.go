// Package testkit 暴露 Fulfillment 的内存测试组装入口。
package testkit

import fulfillmentservice "seckill-mall/services/fulfillment/internal/app"

var (
	NewMemoryRepository = fulfillmentservice.NewMemoryRepository
	NewService          = fulfillmentservice.NewService
	NewServer           = fulfillmentservice.NewServer
	NewGRPCOrderClient  = fulfillmentservice.NewGRPCOrderClient
)
