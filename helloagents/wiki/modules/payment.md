# Payment Service

## 目的
独立保存支付单和退款单，通过同步 Order gRPC 驱动订单支付、退款状态转换。

## 模块概述
- **职责:** Mock 支付创建、渠道回调签名、支付流水幂等、退款单幂等和用户归属校验。
- **状态:** 🚧第 4 阶段已独立接入
- **最后更新:** 2026-08-06

## 规范

### 需求: 支付回调
**模块:** Payment、Order

#### 场景: 渠道重复通知或通知冲突
- `payment_no` 签名绑定支付单，非法签名返回未认证。
- `callback_ref` 全局唯一；相同支付单和流水重复通知返回 `reused`，不同流水或跨支付单冲突。
- 本地 Payment 单据先原子认领回调流水，再调用 Order；远端失败可使用相同回调重试收口。

### 需求: 退款授权
**模块:** Payment、Order、Inventory

#### 场景: 用户申请退款
- Payment 通过 Order 查询订单归属，越权请求不泄漏资源信息。
- 每个订单最多一个退款单；Order 是状态唯一写入者，Inventory 负责恢复已确认库存。

## API接口
- gRPC `Create/Callback/Refund`。
- Gateway: `POST /api/v1/orders/:order_id/payments`、`POST /api/v1/payments/callback`、`POST /api/v1/orders/:order_id/refunds`。
- 独立入口：`go run ./cmd/payment-service`。

## 数据模型
- 只写 `payments` 和 `refunds`；`payments.user_id` 由 `003_stage4_services.sql` 增加并建立用户索引。

## 依赖
- Order gRPC、MySQL 或 Memory Repository、至少 32 字节支付密钥和内部 HMAC 密钥。

## 变更历史
- [202608061319_stage4_domain_services](../../history/2026-08/202608061319_stage4_domain_services/) - 拆分 Payment 并接入 Gateway。
