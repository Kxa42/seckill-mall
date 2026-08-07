# Fulfillment Service

## 目的
独立维护 shipment、承运商/运单唯一性、发货和收货事实。

## 规范
- `shipments` 和 `fulfillment_*_events` 由 Fulfillment 独占。
- `shipment.created.v1`、`shipment.delivered.v1` 与本地状态同事务提交。
- 支付成功事件只建立待履约事实，不伪造承运商或运单号。
- 重复发货、收货和重复消息均由本地唯一约束、Inbox 和状态机收敛。
- Order 调用只依赖 Fulfillment 私有 `OrderClient` 的 `Get/Ship/ConfirmReceipt`；归属校验不暴露 Payment 的订单金额模型。

## 入口
gRPC `Ship/ConfirmReceipt/Get`；Gateway `/api/v1/admin/orders/:order_id/ship`、`/api/v1/orders/:order_id/receipt`、`/api/v1/orders/:order_id/shipment`；启动 `go run ./services/fulfillment/cmd/fulfillment-service`，实现位于 `services/fulfillment/internal/app`。

## 变更历史
- [202608070836_private_order_clients](../../history/2026-08/202608070836_private_order_clients/) - Order 客户端完全收敛到 Fulfillment 私有目录，测试替身改为窄接口。
- [202608070804_service_first_monorepo](../../history/2026-08/202608070804_service_first_monorepo/) - Fulfillment 的入口、实现、测试和配置收敛到服务自治目录。
- [202608070731_repository_layout_refactor](../../history/2026-08/202608070731_repository_layout_refactor/) - Fulfillment 实现迁入统一内部服务目录。
