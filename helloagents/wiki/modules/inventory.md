# Inventory/Seckill Service

## 目的
集中管理新商城库存预占状态机和秒杀热点准入，提供可替换的内存与 Redis Lua 实现。

## 模块概述
- **职责:** Reserve、Confirm、Release、Restock、AdmitSeckill、幂等状态转换、活动限购和 gRPC 服务。
- **状态:** 🚧第 3 阶段已接入 Order
- **最后更新:** 2026-08-06

## 规范

### 需求: 普通库存预占
**模块:** Inventory Service

#### 场景: 预占、确认或释放
- `reservation_id` 重复提交同参数返回原结果，参数不一致返回冲突。
- 状态允许 `reserved -> confirmed/released`、`confirmed -> restocked`；释放和恢复分别服务支付前取消与退款，不可互相替代。
- Redis 版本使用 Lua 原子执行库存变更和状态转换。

### 需求: 秒杀准入
**模块:** Seckill Service

#### 场景: 高并发活动准入
- `request_id` 重复提交同一活动、用户、SKU、数量返回同一 admission。
- 库存与 `(activity_id, user_id, sku_id)` 限购计数在同一 Lua 脚本内校验和更新。
- 释放秒杀预占时回滚库存和限购计数，避免用户被错误永久限购。

### 需求: 退款恢复库存
**模块:** Inventory Service、Order Service

#### 场景: 已确认订单退款
- Order 调用 `Restock(reservation_id, order_id)`，只有 `confirmed` reservation 可以恢复。
- Restock 成功增加可用库存，并回滚秒杀 `(activity_id, user_id, sku_id)` 限购计数。
- 重复 Restock 返回 `restocked`，不重复增加库存；失败由 Order `order_operations` 有限重试。

## API 接口
- `Reserve(reservation_id, order_id, user_id, sku_id, quantity)`。
- `Confirm(reservation_id, order_id)`。
- `Release(reservation_id, order_id)`。
- `Restock(reservation_id, order_id)`。
- `AdmitSeckill(request_id, activity_id, user_id, sku_id, quantity)`。

## 数据模型
- `Reservation`：`reserved`、`confirmed`、`released`、`restocked`、`expired`。
- Redis 默认 key：`inventory:stock:*`、`inventory:users:{activity_id}:{sku_id}`、`inventory:reservation:*`。

## 依赖
- gRPC、Protobuf、Redis；debug 或无 Redis 环境使用并发安全 MemoryStore。

## 变更历史
- [202608060842_catalog_inventory_services](../../history/2026-08/202608060842_catalog_inventory_services/) - 建立独立 Inventory/Seckill 服务、Lua 状态机和 Fake E2E。
