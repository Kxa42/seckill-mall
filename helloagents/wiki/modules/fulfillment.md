# Fulfillment Service

## 目的
独立保存物流单据，通过 Order gRPC 驱动管理员发货和用户收货状态。

## 模块概述
- **职责:** 运单创建、承运商/运单唯一约束、发货幂等、确认收货和物流查询。
- **状态:** 🚧第 4 阶段已独立接入
- **最后更新:** 2026-08-06

## 规范

### 需求: 管理员发货
**模块:** Fulfillment、Order

#### 场景: 首次发货和重复发货
- Gateway 和 Fulfillment 服务端均校验 `admin` 角色签名。
- 相同订单和相同运单重复请求返回 `reused`；同订单不同运单、不同订单复用运单号返回冲突。
- 本地物流唯一键先落库，再调用 Order；远端失败可由相同命令重试收口。

### 需求: 用户收货与查询
**模块:** Fulfillment、Order

#### 场景: 用户操作自己的订单
- ConfirmReceipt/Get 由服务端校验用户 HMAC，并通过 Order 再次校验订单归属。
- 重复收货保持幂等，其他用户只能得到资源不存在或权限错误。

## API接口
- gRPC `Ship/ConfirmReceipt/Get`。
- Gateway: `POST /api/v1/admin/orders/:order_id/ship`、`POST /api/v1/orders/:order_id/receipt`、`GET /api/v1/orders/:order_id/shipment`。
- 独立入口：`go run ./cmd/fulfillment-service`。

## 数据模型
- 只写 `shipments(order_id, carrier, tracking_no, status, shipped_at, delivered_at)`。

## 依赖
- Order gRPC、MySQL 或 Memory Repository、etcd 注册和内部 HMAC 密钥。

## 变更历史
- [202608061319_stage4_domain_services](../../history/2026-08/202608061319_stage4_domain_services/) - 拆分 Fulfillment 并接入 Gateway。
