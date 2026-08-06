# Product Service

## 目的
保留旧 Product Service 的秒杀 Redis Lua 兼容链路；新商城目录与库存已经迁移到独立 Catalog、Inventory/Seckill 服务。

## 模块概述
- **职责:** 旧商品查询、Redis 库存、限购与回滚；不再承载新 Catalog/Inventory 的运行时实现。
- **状态:** ✅兼容（迁移中）
- **最后更新:** 2026-08-06

## 规范

### 需求: 旧秒杀库存防超卖
**模块:** Legacy Product Service

#### 场景: 用户请求扣减库存
- Redis Lua 在一次原子操作内校验库存和限购。
- 失败时不产生部分扣减，补偿通过 RollbackStock 恢复。

### 需求: 旧秒杀 SKU 库存
**模块:** Legacy Product Service

#### 场景: 用户创建订单
- 旧 Order Service 继续通过 Product gRPC 使用旧 Redis Lua 扣减和回滚。
- 旧 `product`、`orders`、`outbox_events` 表仍属于兼容链路，不被新 Catalog/Inventory 访问。

## 迁移边界
- Catalog Service 只读 `categories`、`spus`、`skus`、`product_images`；商品查询已由 Gateway 切换到 Catalog。
- Inventory/Seckill Service 负责新 reservation 状态机、秒杀准入和退款 Restock；新 `/api/v1/seckill/orders` 由 Order Service 统一调用它，旧 `/order` 仍使用本模块的兼容 Redis/MQ 链路。

## 依赖
- 旧服务依赖 MySQL、Redis、etcd、gRPC 和 Prometheus。
- 新目录与库存不再由本模块实现；Commerce 只保留未迁移交易链路。

## 当前边界
- 新 `/api/v1/seckill/orders` 不复用本模块旧 Redis Lua，而是由 Order Service 调用独立 Inventory；旧 `/order` 继续使用本模块 Redis Lua。
- `seckill_activities` 已建表，但活动运营 API 和时间窗校验尚未接入。

## 变更历史
- [202608051526_backend_commerce_mvp](../../history/2026-08/202608051526_backend_commerce_mvp/) - 增加 SPU/SKU 和库存 reservation。
- [202608060842_catalog_inventory_services](../../history/2026-08/202608060842_catalog_inventory_services/) - 建立 Catalog/Inventory 独立服务并划清旧 Product 边界。
