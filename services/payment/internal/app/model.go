// Package paymentservice 定义支付与退款服务的模型和数据边界。
package paymentservice

import (
	"time"
)

const (
	StatusPending   = "pending"
	StatusSucceeded = "succeeded"
	RefundSucceeded = "succeeded"
)

type Payment struct {
	PaymentNo, OrderID, Provider, Status, CallbackRef string
	UserID                                            uint64
	AmountCents                                       int64
	PaidAt, CreatedAt, UpdatedAt                      time.Time
}

type Refund struct {
	RefundNo, OrderID, PaymentNo, Reason, Status string
	UserID                                       uint64
	AmountCents                                  int64
	CreatedAt, CompletedAt                       time.Time
}
