# Messaging Workers

## 目的
维护旧秒杀链路的可靠消息投递与死信补偿，并为新商城事件演进提供 Outbox/Inbox 模型。

## 模块概述
- **职责:** 旧 `outbox_events` 扫描、publisher confirm、主队列消费、DLQ 和 Redis 补偿；维护新商城跨服务事件契约和后续 Outbox/Inbox 运行时。
- **状态:** 🚧迁移中，第一阶段契约已完成
- **最后更新:** 2026-08-06

## 规范

### 需求: 旧订单消息可靠处理
**模块:** Messaging Workers

#### 场景: 发布或消费失败
- 发布失败按退避策略重试，达到上限后执行补偿。
- 消费失败进入死信队列，由 DLQ Consumer 判断旧订单状态后补偿 Redis。

### 需求: 新商城事件幂等基础
**模块:** Commerce Messaging

#### 场景: 商城状态事务提交
- 与业务状态同事务写入 `commerce_outbox_events`。
- 事件包含确定性 `event_id`、`event_type`、`event_version` 和 JSON payload。
- `inbox_events` 使用 `(consumer, event_id)` 唯一约束预留消费幂等边界。

## 依赖
- MySQL、Redis、RabbitMQ、OpenTelemetry、Prometheus。

## 当前边界
- 现有 Outbox Worker 只扫描旧 `outbox_events`，尚未发布 `commerce_outbox_events`。
- 旧 MQ Consumer 仍在一个事务内写旧 `orders` 与 `product` 表；新商城表不受该跨写影响。
- `common/contracts` 已定义统一事件信封、版本化事件类型、服务标识和数据所有权；订单取消事件的规范名称为 `order.cancelled.v1`，兼容代码别名不会产生第二种线上事件类型。
- `proto/commerce` 已定义 Catalog、Inventory、Identity、Cart、Order、Payment、Fulfillment 的内部 gRPC 契约，对应服务仍按阶段拆分。
- 发布失败、重复消息和 DLQ 的真实集成验收需要 RabbitMQ/Redis/MySQL 环境。

## 变更历史
- [202608051526_backend_commerce_mvp](../../history/2026-08/202608051526_backend_commerce_mvp/) - 增加版本化 Commerce Outbox 和 Inbox 表。
