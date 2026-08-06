# API Gateway

## 目的
为新商城 API 和旧秒杀兼容 API 提供统一 HTTP 入口。

## 模块概述
- **职责:** Identity、Catalog、Cart、Order、Payment、Fulfillment gRPC HTTP 适配、未切换 `/api/v1` 的过渡代理、旧 gRPC 协议转换、商城/旧 JWT、Sentinel 限流、追踪和健康检查。
- **状态:** 🚧第 4 阶段切流中
- **最后更新:** 2026-08-06

## 规范

### 需求: 商城版本化入口
**模块:** API Gateway

#### 场景: 客户端访问 `/api/v1`
- 阶段 4 已显式注册的领域路由通过目标 gRPC 服务处理；未切换路由才保留 Authorization、Idempotency-Key、请求体和查询参数并代理到 Commerce API。
- 上游不可用返回 `502 UPSTREAM_UNAVAILABLE`，不泄漏内部连接信息。

### 需求: 旧秒杀兼容
**模块:** API Gateway

#### 场景: 使用旧订单接口
- `/product/:id`、`/order`、`/order/:order_id` 保持兼容。
- 任意用户 ID 的模拟 `/login` 只在 `debug` 模式注册，release 模式返回 404。

### 需求: 领域服务路由切换
**模块:** API Gateway、Identity、Catalog、Cart、Order、Payment、Fulfillment

#### 场景: 访问商城领域 API
- Identity、Catalog、Cart、Order、Payment 和 Fulfillment 路由通过服务发现或直连地址调用对应 gRPC。
- Gateway 将 Catalog 商品模型转换为既有 `spu/skus/images` JSON 形状，并为下游调用设置 2 秒 deadline。
- 其余未显式注册的 `/api/v1` 路由通过 `NoRoute` 代理到 Commerce，避免通配路由覆盖显式领域路由。

### 需求: Order 路由切换
**模块:** API Gateway、Order Service

#### 场景: 普通/秒杀订单统一编排
- `POST /api/v1/orders`、`POST /api/v1/seckill/orders`、订单查询和取消显式调用 Order gRPC。
- Gateway 不预先调用 Inventory，不把同一秒杀请求拆成两次扣减。
- Order Service 不可用时返回 `UPSTREAM_UNAVAILABLE`，不得静默回退为 Commerce 写订单。

### 需求: 商城认证边界
**模块:** API Gateway、Identity

#### 场景: 访问阶段 4 受保护路由
- `/api/v1` 领域路由严格校验商城 JWT 的 issuer、token_type、user_id、role 和过期时间；旧 JWT 只能访问旧兼容路径。
- Gateway 通过覆盖 method、user、role 和时间戳的内部 HMAC metadata 调用下游服务。

## API 接口
- `GET /api/v1/products`: Catalog gRPC 商品分页查询。
- `GET /api/v1/products/:id`: Catalog gRPC 商品详情查询。
- `/api/v1/auth/*`、`/api/v1/addresses*`: Identity gRPC。
- `/api/v1/cart*`: Cart gRPC。
- `POST/GET /api/v1/orders*`、`POST /api/v1/seckill/orders`: Order gRPC。
- `/api/v1/orders/:order_id/payments`、`/api/v1/payments/callback`、`/api/v1/orders/:order_id/refunds`: Payment gRPC。
- `/api/v1/admin/orders/:order_id/ship`、`/api/v1/orders/:order_id/receipt`、`/api/v1/orders/:order_id/shipment`: Fulfillment gRPC。
- 其他未切换的 `/api/v1/*`: 通过 NoRoute 代理 Commerce API。
- `GET /healthz`: Gateway 存活检查。
- 旧路径详见 `wiki/api.md`。

## 依赖
- Identity、Catalog、Cart、Order、Payment、Fulfillment、Commerce API、Product Service、Inventory/Seckill、etcd、Sentinel、OpenTelemetry。

## 变更历史
- [202608051526_backend_commerce_mvp](../../history/2026-08/202608051526_backend_commerce_mvp/) - 增加商城代理、健康检查和 release 登录保护。
- [202608060842_catalog_inventory_services](../../history/2026-08/202608060842_catalog_inventory_services/) - 切换 Catalog 商品查询并建立 Inventory 客户端边界。
- [202608061319_stage4_domain_services](../../history/2026-08/202608061319_stage4_domain_services/) - 显式切换 Identity、Cart、Payment、Fulfillment 路由。
