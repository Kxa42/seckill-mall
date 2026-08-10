// Package fulfillmentservice 定义履约服务模型和数据所有权。
// 本包只访问 shipments，订单状态转换通过 Order Service 完成。
package fulfillmentservice

import (
	"fmt"
	"time"
)

const (
	StatusShipped  = "shipped"
	StatusReceived = "received"
)

type Shipment struct {
	ID          uint64
	OrderID     string
	Carrier     string
	TrackingNo  string
	Status      string
	ShippedAt   time.Time
	DeliveredAt time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func PublicID(value Shipment) string { return fmt.Sprintf("ship_%d", value.ID) }
