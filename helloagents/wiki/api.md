# API 手册

## 入口

统一入口是 Gateway 的 `/api/v1`：

| 方法 | 路径 | 认证 | 服务 |
|------|------|------|------|
| POST | `/auth/register`、`/auth/login`、`/auth/refresh` | 按接口 | Identity |
| GET/POST/PUT/DELETE | `/addresses*` | 用户 | Identity |
| GET | `/products`、`/products/:id` | 否 | Catalog |
| GET/POST/DELETE | `/cart*` | 用户 | Cart |
| POST | `/orders`、`/seckill/orders` | 用户 | Order |
| GET | `/orders`、`/orders/:order_id` | 用户 | Order |
| POST | `/orders/:order_id/cancel` | 用户 | Order |
| POST | `/orders/:order_id/payments`、`/payments/callback` | 用户/内部 | Payment |
| POST | `/orders/:order_id/refunds` | 用户 | Payment |
| POST | `/admin/orders/:order_id/ship` | admin | Fulfillment |
| POST | `/orders/:order_id/receipt` | 用户 | Fulfillment |
| GET | `/orders/:order_id/shipment` | 用户 | Fulfillment |
| GET | `/healthz` | 否 | Gateway |

金额使用整数分，订单创建必须使用 `Idempotency-Key`。内部 gRPC 调用使用 HMAC metadata，具体字段以 `api/openapi.yaml` 和 `common/pb` 为准。

## 已移除入口

`GET/POST /order*`、`GET /product/*` 和 Commerce NoRoute 代理不再注册，Gateway 对这些路径返回 404，不访问旧服务或旧表。
