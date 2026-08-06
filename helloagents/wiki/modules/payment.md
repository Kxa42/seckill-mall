# Payment Service

## 目的
独立维护支付单、回调幂等和退款单，并发布支付成功/退款事件。

## 规范
- 回调先在本地 Payment Repository 幂等收口，再通过 Order gRPC 或事件恢复订单状态。
- `payments`、`refunds` 和 `payment_*_events` 由 Payment 独占；不直接写 Order 表。
- 重复回调、重复退款和同一 callback_ref 冲突均由本地状态机处理。
- `payment.succeeded.v1`、`payment.refunded.v1` 与本地状态同事务提交。

## 入口
gRPC `Create/Callback/Refund`；Gateway `/api/v1/orders/:order_id/payments`、`/api/v1/payments/callback`、`/api/v1/orders/:order_id/refunds`；启动 `go run ./cmd/payment-service`。
