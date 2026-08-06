# API 手册

## 概述
新商城接口统一使用 `/api/v1`，可通过 Gateway `:8080` 或 Commerce API `:8081` 访问。完整字段、请求体和响应结构以 `api/openapi.yaml` 为准。

成功和失败响应统一为：

```json
{"code":"OK","message":"success","data":{},"request_id":"..."}
```

## 认证方式
- `POST /api/v1/auth/register` 和 `POST /api/v1/auth/login` 返回短期 Access Token 与可轮换 Refresh Token。
- 受保护接口使用 `Authorization: Bearer <access_token>`。
- 用户资源同时校验 Token 身份和 `user_id` 归属；`/admin/*` 要求 `admin` 角色。
- 旧 `/login` 仅在 Gateway `debug` 模式注册，禁止在 release 部署中使用。

## `/api/v1` 接口

| 方法 | 路径 | 认证 | 描述 |
|------|------|------|------|
| POST | `/auth/register` | 否 | 邮箱注册并签发 Token 对 |
| POST | `/auth/login` | 否 | 邮箱密码登录 |
| POST | `/auth/refresh` | Refresh Token | 轮换 Refresh Token |
| GET/POST | `/addresses` | 用户 | 列表或创建当前用户地址 |
| PUT/DELETE | `/addresses/:id` | 用户 | 更新或删除自己的地址 |
| GET | `/products` | 否 | 分页查询在售 SPU/SKU；Gateway 已切换到 Catalog gRPC |
| GET | `/products/:id` | 否 | 查询在售商品详情；Gateway 已切换到 Catalog gRPC |
| GET/POST | `/cart/items` | 用户 | 列表或设置购物车商品 |
| PUT/DELETE | `/cart/items/:sku_id` | 用户 | 修改或删除购物车商品 |
| GET | `/cart/checkout-preview` | 用户 | 重新计算实时价格与可售库存 |
| POST | `/orders` | 用户 | Gateway 调用 Order Service，使用 `Idempotency-Key` 从购物车创建普通订单 |
| POST | `/seckill/orders` | 用户 | Gateway 调用 Order Service，通过一次 `AdmitSeckill` 创建秒杀订单 |
| GET | `/orders` | 用户 | 分页查询自己的订单 |
| GET | `/orders/:order_id` | 用户 | 查询订单、状态历史、支付和物流 |
| POST | `/orders/:order_id/cancel` | 用户 | 取消待支付订单并释放库存 |
| POST | `/orders/:order_id/confirm` | 用户 | 确认已发货订单收货 |
| POST | `/orders/:order_id/refunds` | 用户 | 对已支付或已发货订单执行 Mock 退款；Order 先恢复 confirmed reservation，再完成退款状态 |
| POST | `/payments/mock` | 用户 | 创建 Mock 支付单与测试签名 |
| POST | `/payments/mock/callback` | 签名 | 幂等完成支付并确认库存 |
| POST | `/admin/products` | admin | 创建或更新分类、SPU、SKU 和可用库存 |
| POST | `/admin/orders/:order_id/ship` | admin | 录入承运商与运单并发货 |

## 关键契约
- 金额字段均为整数人民币分，例如 `699900` 表示 `6999.00` 元。
- 普通订单必须携带非空 `Idempotency-Key`，同一用户重复使用该键返回同一订单。
- `reserved_stock` 由库存预占状态机维护，后台商品更新不能覆盖该字段。
- Mock 回调签名无效返回 `401`；资源越权统一表现为 `403` 或不泄漏资源存在性的 `404`。

## 稳定错误码

| 错误码 | HTTP | 说明 |
|--------|------|------|
| `VALIDATION_ERROR` | 400 | 请求格式、数量、金额或字段不合法 |
| `UNAUTHORIZED` | 401 | Token、密码或 Mock 回调签名无效 |
| `FORBIDDEN` | 403 | 当前角色无权执行操作 |
| `NOT_FOUND` | 404 | 资源不存在或不属于当前用户 |
| `CONFLICT` | 409 | 唯一约束、回调或并发状态冲突 |
| `OUT_OF_STOCK` | 409 | SKU 可用库存不足 |
| `INVALID_TRANSITION` | 409 | 订单状态不允许该操作 |
| `INTERNAL_ERROR` | 500 | 已隐藏内部细节的服务错误 |

## 旧兼容接口
`/product/:id`、`/order`、`/order/:order_id` 继续服务原 Redis/MQ 秒杀链路。它们不使用新商城订单表，且不应被新客户端采用。

## 内部 gRPC 契约

| 服务 | 方法 | 当前运行时调用方 | 说明 |
|------|------|------------------|------|
| Catalog | `ListProducts` / `GetProduct` / `GetSKUSnapshot` | Gateway、后续 Order | 目录查询和 SKU 快照；默认过滤非 active 数据 |
| Inventory | `Reserve` / `Confirm` / `Release` / `Restock` | Order Service | 分别处理普通预占、支付确认、支付前释放和退款后恢复；Restock 不修改 Release 的状态语义 |
| Inventory | `AdmitSeckill` | Order Service | Redis Lua/Memory 秒杀准入，携带 Order ID，按活动+用户+SKU 限购且每个订单只调用一次 |

## Order Service gRPC

`CommerceOrderService` 提供 `Create`、`Get`、`List`、`Cancel`、`Expire`、`ConfirmPayment`、`Ship`、`ConfirmReceipt` 和 `Refund`。除本地 worker 外，所有调用都要求内部 HMAC metadata；`Expire` 额外要求 `system` 角色签名。

Order Service 只写订单表和 Order 自有操作表。Catalog、Identity Snapshot、Inventory 通过 gRPC 提供 SKU 快照、地址快照和库存命令，失败时返回稳定 gRPC status 并由 Order 操作记录有限重试。
