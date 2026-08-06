# 数据模型

## 新商城数据

| 服务 | 自有数据 |
|------|----------|
| Identity | `users`、`refresh_tokens`、`user_addresses` |
| Catalog | `categories`、`spus`、`skus`、`product_images` |
| Inventory | Redis `inventory:stock:*`、`inventory:reservation:*`、Stream `inventory:events` |
| Cart | `cart_items` |
| Order | `commerce_orders`、`order_items`、`order_status_history`、`order_create_intents`、`order_operations`、`order_outbox_events`、`order_inbox_events` |
| Payment | `payments`、`refunds`、`payment_outbox_events`、`payment_inbox_events` |
| Fulfillment | `shipments`、`fulfillment_outbox_events`、`fulfillment_inbox_events` |

`migrations/004_stage5_messaging.sql` 只创建服务级消息表，不修改旧 `orders`、`product` 或 `outbox_events`。

## 事件约束
- `event_id` 在每个发布服务 Outbox 中唯一；事件正文是 `EventEnvelope` JSON。
- Inbox 以 `(consumer,event_id)` 唯一，记录 processing/processed、attempts、错误和 lease 时间。
- payload 只包含订单、支付、reservation、shipment 的最小事实，不包含凭据或敏感认证材料。

## 历史数据边界
已有实例可能仍有 `orders`、`product`、`outbox_events`、`commerce_outbox_events` 等历史表。当前代码和新 migration 不读取、写入、DROP 或 TRUNCATE 这些表；清理需要独立备份和审批。
