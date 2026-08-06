# Order Service

## 目的
统一编排新商城普通订单和秒杀订单；旧秒杀异步订单仍由独立 Legacy Order Service 兼容承载。

## 模块概述
- **职责:** 创建意图、SKU/地址快照、Inventory 预占、订单状态机、生命周期权限和有限补偿重试。
- **状态:** 🚧第 3 阶段已切流
- **最后更新:** 2026-08-06

## 规范

### 需求: 异步创建旧秒杀订单
**模块:** Legacy Order Service

#### 场景: Redis 扣库存成功
- 在同一 MySQL 事务中写入旧 Pending 订单和 `outbox_events`。
- 返回旧订单号供 `/order/:order_id` 查询。

### 需求: 统一商城订单
**模块:** Order Service

#### 场景: 创建普通或新秒杀订单
- `Create` 统一支持普通和秒杀订单；秒杀每个订单只调用一次 `AdmitSeckill`。
- 通过 Catalog/Identity Snapshot 获取商品和地址快照，Order 不直接读取下游表。
- 使用 `commerce_orders`、订单项、地址快照、状态历史、创建意图和操作记录。
- 统一支持待支付、取消、超时、支付、发货、收货和退款；退款先调用 Inventory `Restock`。

### 需求: 幂等与失败恢复
**模块:** Order Service

#### 场景: 重复请求、下游失败或进程中断
- `(user_id, Idempotency-Key, request_digest)` 决定稳定 Order ID 和 reservation ID。
- 部分预占、支付确认、取消释放和退款恢复失败会写入 `order_operations` 并有限退避。
- `order_create_intents` 由 worker 扫描恢复，载荷摘要不匹配或超过 8 次重试时进入可查询失败状态。
- 不同 `Idempotency-Key` 即使提交相同商品内容也必须生成不同 Order ID；相同键才允许幂等复用。
- 释放补偿恢复成功后，worker 会确认全部 reservation 已释放并完成 `pending_payment -> canceled` 收口。
- 订单已落库但意图仍为 `started` 时，幂等查询会将意图收口为 `done`，避免 worker 无限重复扫描。

## 依赖
- 旧服务依赖 Product Service、MySQL、etcd、gRPC 和 OpenTelemetry。
- 新交易模块只依赖 `Repository` 接口和 Catalog/Identity/Inventory gRPC 客户端；Commerce 通过 Order gRPC 调用支付、发货、收货和退款状态转换。

## 当前边界
- 旧订单仍使用三态和浮点兼容字段，不与新商城订单自动互转；新 `/api/v1` 链路不访问旧 `orders`。
- 旧接口标记为兼容路径，新客户端只使用 `/api/v1`。

## 变更历史
- [202608051526_backend_commerce_mvp](../../history/2026-08/202608051526_backend_commerce_mvp/) - 新增商城订单 MVP。
- [202608061112_order_service_orchestration](../../history/2026-08/202608061112_order_service_orchestration/) - Order Service 成为新商城订单唯一编排者。
