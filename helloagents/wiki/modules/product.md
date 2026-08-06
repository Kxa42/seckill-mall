# Product Service

## 目的
保留旧秒杀 Redis Lua 准入，同时由 Commerce Catalog 提供新商城 SPU/SKU 和库存预占模型。

## 模块概述
- **职责:** 旧商品查询、Redis 库存、限购与回滚；新商城分类、SPU、SKU 和 reservation。
- **状态:** ✅兼容
- **最后更新:** 2026-08-05

## 规范

### 需求: 旧秒杀库存防超卖
**模块:** Legacy Product Service

#### 场景: 用户请求扣减库存
- Redis Lua 在一次原子操作内校验库存和限购。
- 失败时不产生部分扣减，补偿通过 RollbackStock 恢复。

### 需求: 商城 SKU 库存
**模块:** Commerce Catalog、Inventory

#### 场景: 用户创建订单
- 订单事务按 SKU ID 排序加锁并条件扣减可用库存。
- reservation 在支付、取消、超时和退款时转换状态。
- admin 更新 SKU 时不能覆盖系统维护的预占库存。

## 依赖
- 旧服务依赖 MySQL、Redis、etcd、gRPC 和 Prometheus。
- 新目录与库存位于 Commerce Repository。

## 当前边界
- `/api/v1/seckill/orders` 尚未复用旧 Redis Lua 准入，当前使用 MySQL reservation 防超卖。
- `seckill_activities` 已建表，但活动运营 API 和时间窗校验尚未接入。

## 变更历史
- [202608051526_backend_commerce_mvp](../../history/2026-08/202608051526_backend_commerce_mvp/) - 增加 SPU/SKU 和库存 reservation。
