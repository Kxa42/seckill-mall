# API Gateway

## 目的
为新商城 API 和旧秒杀兼容 API 提供统一 HTTP 入口。

## 模块概述
- **职责:** `/api/v1` 反向代理、旧 gRPC 协议转换、JWT、Sentinel 限流、追踪和健康检查。
- **状态:** ✅稳定
- **最后更新:** 2026-08-05

## 规范

### 需求: 商城版本化入口
**模块:** API Gateway

#### 场景: 客户端访问 `/api/v1`
- 保留 Authorization、Idempotency-Key、请求体和查询参数并代理到 Commerce API。
- 上游不可用返回 `502 UPSTREAM_UNAVAILABLE`，不泄漏内部连接信息。

### 需求: 旧秒杀兼容
**模块:** API Gateway

#### 场景: 使用旧订单接口
- `/product/:id`、`/order`、`/order/:order_id` 保持兼容。
- 任意用户 ID 的模拟 `/login` 只在 `debug` 模式注册，release 模式返回 404。

## API 接口
- `ANY /api/v1/*`: 代理 Commerce API。
- `GET /healthz`: Gateway 存活检查。
- 旧路径详见 `wiki/api.md`。

## 依赖
- Commerce API、Product Service、Order Service、etcd、Sentinel、OpenTelemetry。

## 变更历史
- [202608051526_backend_commerce_mvp](../../history/2026-08/202608051526_backend_commerce_mvp/) - 增加商城代理、健康检查和 release 登录保护。
