# Messaging

## 目的
为统一商城提供服务级 Outbox/Inbox、RabbitMQ Topic、重试/DLQ 和 Fake/Memory 验收能力。

## 当前实现
- `shared/contracts` 定义 `EventEnvelope`、事件 payload、事件版本和服务所有权。
- `shared/platform/messaging` 实现 Outbox/Inbox、RabbitMQ、Fake Broker 和发布/消费循环。
- Order、Payment、Fulfillment 使用各自 SQL Outbox/Inbox 表；状态变更和事件在同一 MySQL 事务中追加。
- Inventory 使用 Redis Lua 在库存/reservation 状态变更的同一脚本中 `XADD` Stream Outbox；无 Redis 时使用 MemoryEventStream。
- RabbitMQ 使用 `commerce.events.v1`、retry exchange、DLX、服务独立队列、Publisher Confirm、mandatory return、手动 Ack 和有限重试。
- `(consumer,event_id)` Inbox claim 保证重复投递收敛；未知事件和未来版本安全确认并隔离。
- Messaging 共享层只负责事件契约和传输运行时，不承载 Order 等业务客户端；业务客户端适配器由调用方服务私有维护。

## 事件
`seckill.accepted.v1`、`order.created.v1`、`order.cancelled.v1`、`payment.succeeded.v1`、`payment.refunded.v1`、`inventory.reserved.v1`、`inventory.released.v1`、`inventory.restocked.v1`、`shipment.created.v1`、`shipment.delivered.v1`。

## 运行边界
- `SECKILL_MQ_URL` 为空时不连接 RabbitMQ；服务仍可记录 Memory Outbox 并运行 Memory/Fake 测试。
- 旧 `orders`、`product`、`outbox_events` 和旧 Worker 不属于新消息运行时。
- RabbitMQ 真实故障注入需要 Docker；`tests/stage5_rabbitmq_e2e.sh` 会在基础设施不可用时明确跳过。

## 变更历史
- [202608101524_project_quality_hardening](../../history/2026-08/202608101524_project_quality_hardening/) - 修复 Outbox 发布链路（`OutboxEvent.Envelope()` 由分列元数据与业务 payload 重建信封并兼容历史完整信封记录）、SQL/Memory claim 计数与批次确定性、retry 转投 deferred confirm（确认成功才 Ack 原消息）；抽取服务级消息运行时 `shared/platform/messaging/service_runtime.go` 统一 Order/Payment/Fulfillment 装配。
- [202608091438_fix_core_bugs](../../history/2026-08/202608091438_fix_core_bugs/) - 修复 Redis Inbox attempts 恒为 1 与消费端忙循环：`markFailed` 续租保留计数、租约过期重投递增，`!claim.Claimed` 改为延迟重投（`requeueDelayed` 永不 DLQ），与 SQL Inbox 失败语义对齐。
- [202608070836_private_order_clients](../../history/2026-08/202608070836_private_order_clients/) - 明确共享消息层与业务客户端的边界。
- [202608070804_service_first_monorepo](../../history/2026-08/202608070804_service_first_monorepo/) - 消息契约和运行时迁入跨服务共享层。
- [202608070731_repository_layout_refactor](../../history/2026-08/202608070731_repository_layout_refactor/) - 消息运行时迁入统一平台目录。
