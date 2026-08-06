# Inventory/Seckill Service

## 目的
集中管理库存 reservation、秒杀准入、限购和退款恢复。

## 规范
- `Reserve`、`Confirm`、`Release`、`Restock` 和 `AdmitSeckill` 使用幂等 reservation ID。
- Redis Lua 同时更新库存、限购、reservation 和 Stream Outbox；MemoryStore 用同一状态机替代。
- `Release` 只处理支付前 `reserved`；`Restock` 只处理支付后 `confirmed`，重复操作不重复增加库存。
- 消费 `order.cancelled.v1` 和 `payment.refunded.v1` 时只调用本地 Store，不读取或写入 Order/Payment 表。

## 事件
发布 `seckill.accepted.v1`、`inventory.reserved.v1`、`inventory.released.v1`、`inventory.restocked.v1`。

## 入口与依赖
- gRPC: `InventoryService`。
- Gateway/Order: `/api/v1/seckill/orders` 由 Order 统一编排。
- `go run ./cmd/inventory-service`；Redis 可选，Stream 使用 consumer group 和 pending claim。
