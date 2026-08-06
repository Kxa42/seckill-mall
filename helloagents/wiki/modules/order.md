# Order Service

## 目的
保留旧秒杀异步订单兼容链路；完整商城订单由 Commerce API 交易模块负责。

## 模块概述
- **职责:** 旧 Redis 扣库存调用、价格查询、排队订单、旧 Outbox、状态查询和失败补偿触发。
- **状态:** ✅兼容
- **最后更新:** 2026-08-05

## 规范

### 需求: 异步创建旧秒杀订单
**模块:** Legacy Order Service

#### 场景: Redis 扣库存成功
- 在同一 MySQL 事务中写入旧 Pending 订单和 `outbox_events`。
- 返回旧订单号供 `/order/:order_id` 查询。

### 需求: 完整商城订单
**模块:** Commerce Order

#### 场景: 创建普通或新秒杀订单
- 使用 `commerce_orders`、订单项、地址快照、状态历史和库存 reservation。
- 统一支持待支付、取消、超时、支付、发货、收货和退款。

## 依赖
- 旧服务依赖 Product Service、MySQL、etcd、gRPC 和 OpenTelemetry。
- 新交易模块依赖 Commerce Repository，详见 [commerce](commerce.md)。

## 当前边界
- 旧订单仍使用三态和浮点兼容字段，不与新商城订单自动互转。
- 旧接口标记为兼容路径，新客户端只使用 `/api/v1`。

## 变更历史
- [202608051526_backend_commerce_mvp](../../history/2026-08/202608051526_backend_commerce_mvp/) - 新增独立统一商城订单状态机。
