# Commerce API

## 目的
提供不依赖前端的完整商城后端 MVP，并统一普通订单和新秒杀订单的交易状态。

## 模块概述
- **职责:** 用户认证、地址、商品目录、购物车、库存预占、订单、Mock 支付、履约、退款和超时关单。
- **状态:** 🚧开发中（MVP 已实现，外部集成待补）
- **最后更新:** 2026-08-05

## 规范

### 需求: 用户身份与地址
**模块:** Commerce Identity

#### 场景: 用户注册并登录
- 密码使用 bcrypt 哈希，重复邮箱返回冲突。
- Access Token 包含用户、角色、发行者、类型和过期时间。
- Refresh Token 只保存哈希且每次刷新撤销旧 Token。
- 地址读取、更新和删除必须校验当前用户归属。

### 需求: 商品目录与购物车
**模块:** Commerce Catalog、Cart

#### 场景: 用户选择在售 SKU
- 公共列表只返回上架 SPU/SKU，支持 offset/limit 分页。
- 购物车限制单 SKU 数量为 1 到 99，结算时重新读取价格与库存。
- 后台商品更新不能覆盖系统维护的 `reserved_stock`。

### 需求: 普通订单闭环
**模块:** Commerce Order、Inventory、Payment、Fulfillment

#### 场景: 用户从购物车结算
- `Idempotency-Key` 在用户范围内唯一，重复请求返回同一订单。
- 订单保存商品、价格和地址快照，并在事务中预占库存。
- Mock 支付确认库存，admin 发货，订单所有者确认收货。

### 需求: 取消与退款
**模块:** Commerce Order、Payment、Inventory

#### 场景: 用户取消或申请退款
- 待支付订单取消或超时后释放预占库存。
- 已支付未发货订单可执行幂等 Mock 退款并恢复可用库存。
- 非法状态转换返回稳定冲突错误并保留状态历史。

### 需求: 秒杀统一交易
**模块:** Commerce Inventory、Order

#### 场景: 创建秒杀订单
- `/api/v1/seckill/orders` 与普通订单共用支付、超时、履约和退款状态机。
- 当前通过 MySQL 条件更新防止超卖；旧 Redis Lua 准入尚未接入该入口。

## API 接口
- 身份与地址: `/api/v1/auth/*`、`/api/v1/addresses*`。
- 目录与购物车: `/api/v1/products*`、`/api/v1/cart/*`。
- 交易: `/api/v1/orders*`、`/api/v1/seckill/orders`、`/api/v1/payments/mock*`。
- 运营: `/api/v1/admin/products`、`/api/v1/admin/orders/:order_id/ship`。

## 数据模型
- Identity: `users`、`refresh_tokens`、`user_addresses`。
- Catalog/Inventory: `categories`、`spus`、`skus`、`inventory_reservations`、`seckill_activities`。
- Trade: `cart_items`、`commerce_orders`、`order_items`、`order_status_history`、`payments`、`refunds`、`shipments`。
- Messaging: `commerce_outbox_events`、`inbox_events`。

## 依赖
- MySQL；内存 Repository 仅用于开发和验收。
- Gin、GORM、bcrypt、JWT、Prometheus。

## 变更历史
- [202608051526_backend_commerce_mvp](../../history/2026-08/202608051526_backend_commerce_mvp/) - 新增完整商城后端 MVP。
