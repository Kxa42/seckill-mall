# Fulfillment Service

## 目的
独立维护 shipment、承运商/运单唯一性、发货和收货事实。

## 规范
- `shipments` 和 `fulfillment_*_events` 由 Fulfillment 独占。
- `shipment.created.v1`、`shipment.delivered.v1` 与本地状态同事务提交。
- 支付成功事件只建立待履约事实，不伪造承运商或运单号。
- 重复发货、收货和重复消息均由本地唯一约束、Inbox 和状态机收敛。

## 入口
gRPC `Ship/ConfirmReceipt/Get`；Gateway `/api/v1/admin/orders/:order_id/ship`、`/api/v1/orders/:order_id/receipt`、`/api/v1/orders/:order_id/shipment`；启动 `go run ./cmd/fulfillment-service`。
