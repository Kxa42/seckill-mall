# API Gateway

## 目的
为统一商城 `/api/v1/*` 提供认证、限流、追踪和 gRPC HTTP 适配。

## 规范
- 只连接 Identity、Catalog、Inventory、Cart、Order、Payment 和 Fulfillment 目标服务。
- `GET/POST /order*`、`/product/*` 和 Commerce NoRoute 代理不存在，旧路径直接 404 且无业务副作用。
- 订单、秒杀、支付、退款和履约全部使用显式版本化路由，不因下游不可用回退到另一套写模型。
- Gateway 继续校验商城 JWT 和内部 HMAC metadata，并为下游调用设置 deadline。
- 入口位于 `services/gateway/cmd/api-gateway`，实现和 middleware 位于 `services/gateway/internal`，OpenAPI 位于 `services/gateway/api`。
- 内部签名统一使用 `shared/platform/internalcall`；生产代码只能依赖共享协议和平台能力，不引用领域服务实现或 `testkit`。跨服务内存 E2E 通过目标服务 `testkit` 组装。

## 依赖
目标 gRPC 服务、etcd 或直连地址、OpenTelemetry、Prometheus；不读取业务 DSN，不配置 Commerce URL。

## 变更历史
- [202608070804_service_first_monorepo](../../history/2026-08/202608070804_service_first_monorepo/) - Gateway 入口、实现、middleware、OpenAPI 和配置收敛到服务自治目录。
- [202608070731_repository_layout_refactor](../../history/2026-08/202608070731_repository_layout_refactor/) - 统一 Gateway 入口、实现目录与平台依赖边界。
