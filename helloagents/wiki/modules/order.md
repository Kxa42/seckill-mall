# Order Service

## 目的
统一编排普通订单和秒杀订单，维护订单状态唯一事实，并发布订单创建/取消事件。

## 规范
- `Create` 通过 Catalog、Identity、Inventory gRPC 获取快照和 reservation；同一幂等键复用稳定订单号。
- Order 只写 `commerce_orders`、`order_items`、状态历史、创建意图、补偿操作和 `order_*_events`。
- `pending_payment`、`paid`、`shipped`、`completed`、`canceled`、`refund_pending`、`refunded` 由本服务状态机维护。
- Payment/Inventory/Shipment 事件通过 Order Inbox 幂等处理；履约事件只应用本地状态，不回调履约服务。
- 取消和退款事件由本地状态转换与 Outbox 同事务提交，失败由操作记录有限重试。

## 入口
- gRPC: `CommerceOrderService`。
- Gateway: `/api/v1/orders*`、`/api/v1/seckill/orders`。
- 独立入口: `go run ./services/order/cmd/order-service`；实现位于 `services/order/internal/app`。

## 依赖
Catalog、Identity、Inventory gRPC；MySQL 或 Memory Repository；RabbitMQ 可选。

## 变更历史
- [202608091438_fix_core_bugs](../../history/2026-08/202608091438_fix_core_bugs/) - 修复 `shipped` 订单退款必然失败：`canTransition` 补充 `Shipped → RefundPending`，新增发货后退款全链路回归测试（根因：状态机漏记可退款状态）。
- [202608070804_service_first_monorepo](../../history/2026-08/202608070804_service_first_monorepo/) - Order 的入口、实现、测试和配置收敛到服务自治目录。
- [202608070731_repository_layout_refactor](../../history/2026-08/202608070731_repository_layout_refactor/) - Order 与其他领域服务统一采用内部服务目录约定。
