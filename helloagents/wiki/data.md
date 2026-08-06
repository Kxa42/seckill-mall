# 数据模型

## 概述
MySQL `seckill` 库同时承载新商城表和旧秒杀兼容表。新表由 `migrations/001_commerce_mvp.sql`、`migrations/002_order_service.sql` 与 `migrations/003_stage4_services.sql` 管理；旧表由 `deploy/mysql/init.sql` 初始化。两组表共享实例但保持写入边界。Catalog Repository 只读取目录表，Inventory 的运行时库存状态由 Redis 或内存 Store 管理。

## 新商城表

| 领域 | 表 | 关键约束与用途 |
|------|----|----------------|
| Identity | `users` | `email` 唯一，保存 bcrypt 密码哈希、角色和状态 |
| Identity | `refresh_tokens` | `token_hash` 唯一，只保存 SHA-256 哈希、过期和撤销时间 |
| Identity | `user_addresses` | 按 `user_id` 查询，订单创建时复制地址快照 |
| Catalog | `categories` | `slug` 唯一，支持父分类与启用状态 |
| Catalog | `spus` | 关联分类，维护商品描述和上架状态 |
| Catalog | `skus` | `code` 唯一，金额为 `price_cents`，库存分为 available/reserved |
| Catalog | `product_images` | 按 SPU 和顺序保存图片 URL |
| Inventory | `inventory_reservations` | 旧/过渡库存 reservation 数据集；新 Order 只能通过 Inventory gRPC 操作，状态为 reserved/confirmed/released/restocked |
| Inventory | `seckill_activities` | 秒杀价格、限购、起止时间和活动状态的预留模型 |
| Cart | `cart_items` | `(user_id, sku_id)` 唯一，数量必须大于 0 |
| Order | `commerce_orders` | `order_id` 唯一，`(user_id, idempotency_key)` 唯一，保存地址 JSON 快照 |
| Order | `order_items` | 保存 SKU 名称、编码、单价、数量和小计快照 |
| Order | `order_status_history` | 记录每次状态转换、原因、操作方和时间 |
| Order | `order_create_intents` | `(user_id, idempotency_key)` 唯一，保存创建载荷摘要、确定性订单号、恢复状态和最大重试次数 |
| Order | `order_operations` | 记录 release/confirm/restock 补偿命令、尝试次数、退避时间和最终失败 |
| Payment | `payments` | `payment_no`、`order_id`、`callback_ref` 分别唯一，`user_id` 用于服务端归属校验 |
| Payment | `refunds` | 每个订单最多一条 Mock 退款，保存退款金额与原因 |
| Fulfillment | `shipments` | 每订单一条物流，`(carrier, tracking_no)` 唯一 |
| Messaging | `commerce_outbox_events` | 64 位哈希 `event_id` 唯一，包含事件类型、版本、payload 和重试状态 |
| Messaging | `inbox_events` | `(consumer, event_id)` 唯一，为后续消费者幂等预留 |

## 关键不变量
- 新商城所有金额使用 `BIGINT` 整数分，禁止浮点金额参与计算。
- 创建订单时由 Order Service 按 SKU ID 稳定排序调用 Inventory；Order 本地事务保存 SKU、地址和 reservation 引用快照。
- 支付成功调用 `Confirm`；取消/超时调用 `Release`；退款调用独立 `Restock`，只允许 `confirmed -> restocked`，避免支付前取消误恢复已确认库存。
- Order ID 和创建意图 ID 由用户、`Idempotency-Key` 与请求摘要共同派生，避免不同幂等键的相同订单内容发生主键碰撞。
- 订单状态只允许 `pending_payment -> paid/canceled`、`paid -> shipped/refund_pending`、`shipped -> completed`、`refund_pending -> refunded`。
- Outbox `event_id` 由事件类型和聚合 ID 的 SHA-256 生成，事务重试保持确定性且不超过 64 字符。

## 旧秒杀兼容数据
- `product`: 单层商品、定点数据库价格和最终库存。
- `orders`: 排队/成功/失败三态的异步秒杀订单，Go/Protobuf 仍使用兼容 `float32` 金额。
- `outbox_events`: 旧 Order Service 到 RabbitMQ 的事件表，由现有 Outbox Worker 消费。
- Redis `product:stock:{product_id}` 与用户购买记录由 Product Service Lua 脚本维护。

## Catalog/Inventory 运行时状态

### Catalog Service
- `categories`、`spus`、`skus`、`product_images` 是 Catalog 的目录数据集；商品响应中的 SKU 库存字段属于目录快照，不作为秒杀扣减真源。
- `MemoryRepository` 提供 SKU `1` 的演示商品；配置 `catalog.mysql_dsn` 后使用只读 `MySQLRepository`。

### Inventory/Seckill Service
- `Reservation` 状态允许 `reserved -> confirmed/released` 和 `confirmed -> restocked`；重复确认、释放或恢复保持幂等，确认后 `Release` 仍被拒绝。
- Redis key 空间默认使用 `inventory:stock:{sku_id}`、`inventory:users:{activity_id}:{sku_id}` 和 `inventory:reservation:{reservation_id}`。
- 秒杀限购按 `(activity_id, user_id, sku_id)` 隔离；释放 reserved 秒杀预占时，同时恢复库存并回滚活动限购计数。
- `MemoryStore` 与 Redis Lua 保持同一状态机不变量；秒杀订单由 Order Service 唯一调用 `AdmitSeckill`，退款恢复时同时回滚活动限购计数。

## 当前边界
- `commerce_outbox_events` 与 `inbox_events` 已建表，但现有 Outbox Worker 只处理旧 `outbox_events`。
- 目标服务的数据集、同步依赖和事件边界以 `common/contracts/boundaries.go` 为代码契约；共享 MySQL 实例不等于允许跨服务写表。
- 真实 MySQL migration 重放本轮因 Docker daemon 不可用未执行；SQL 文件、代码测试和 Compose 静态配置已验证，不能将 Memory/Fake 结果等同于真实基础设施验收。
- 阶段 4 的 Identity、Cart、Payment、Fulfillment 通过独立 Repository 维护各自表；Payment/Fulfillment 只通过 Order gRPC 改订单状态，不直接写 `commerce_orders`。
