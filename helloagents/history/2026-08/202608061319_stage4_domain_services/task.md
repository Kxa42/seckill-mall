# 任务清单: 阶段 4 领域服务拆分

目录: `helloagents/history/2026-08/202608061319_stage4_domain_services/`

> **执行边界:** 暂缓前端；本阶段不实现新商城 RabbitMQ Outbox/Inbox，不删除 Commerce 和旧秒杀兼容链路。Docker 不可用时使用 Memory/Fake/bufconn 验收并记录跳过项。

## 1. 环境与共享安全边界

- [√] 1.1 将 Go 用户级 `GOTMPDIR` 和 shell 默认值设置为 `/tmp`，验证移除继承变量后 `go env GOTMPDIR` 返回 `/tmp`。
- [√] 1.2 新增通用内部 HMAC metadata 包并让 Order 保持兼容包装，覆盖用户、角色、过期时间和签名篡改测试。
- [√] 1.3 新增商城 JWT Gateway 中间件，严格校验 issuer、token_type 和 role，保留旧接口 JWT 中间件。

## 2. gRPC 契约

- [√] 2.1 扩展 `proto/commerce/identity.proto` 的认证和地址 CRUD，保持 `GetAddressSnapshot` 兼容。
- [√] 2.2 扩展 `proto/commerce/cart.proto` 的 Set/Delete/List/Preview 和 SKU 快照响应。
- [√] 2.3 扩展 `proto/commerce/payment.proto` 的 Create/Callback/Refund 幂等契约。
- [√] 2.4 扩展 `proto/commerce/fulfillment.proto` 的 Ship/ConfirmReceipt/Get 契约。
- [√] 2.5 重新生成 `common/pb/*` 并运行 Proto 编译与字段兼容测试。

## 3. Identity Service

- [√] 3.1 扩展 `identity_service` 领域模型和 Repository，Memory/MySQL 只访问 `users/refresh_tokens/user_addresses`。
- [√] 3.2 实现注册、登录、Refresh Token 轮换、管理员初始化和地址 CRUD，复用地址快照 RPC。
- [√] 3.3 将 `cmd/identity-snapshot-service` 升级为完整 Identity 进程，加载 JWT、DSN、etcd 和内部签名配置。
- [√] 3.4 增加密码、Token 轮换、邮箱冲突、地址越权和 gRPC 权限测试。

## 4. Cart Service

- [√] 4.1 新增 `cart_service` 领域模型、Memory/MySQL Repository，只访问 `cart_items`。
- [√] 4.2 实现 Catalog gRPC 客户端和 Set/Delete/List/Preview，校验 SKU、数量、实时价格与金额溢出。
- [√] 4.3 新增 `cmd/cart-service` 独立入口、配置、服务发现、健康检查和优雅停机。
- [√] 4.4 增加并发设置、用户隔离、下架 SKU、空购物车和 Fake Catalog 测试。

## 5. Payment Service

- [√] 5.1 新增 `payment_service` 领域模型、Memory/MySQL Repository，只访问 `payments/refunds`。
- [√] 5.2 实现 Order gRPC 客户端、Mock 支付创建、签名回调和退款，保证 payment/order/callback/refund 幂等。
- [√] 5.3 新增 `cmd/payment-service` 独立入口、配置、服务发现、健康检查和优雅停机。
- [√] 5.4 增加无效签名、重复回调、回调流水冲突、越权退款和远端状态已完成后的恢复测试。

## 6. Fulfillment Service

- [√] 6.1 新增 `fulfillment_service` 领域模型、Memory/MySQL Repository，只访问 `shipments`。
- [√] 6.2 实现 Order gRPC 客户端、管理员发货、用户确认收货和物流查询，保证重复命令幂等。
- [√] 6.3 新增 `cmd/fulfillment-service` 独立入口、配置、服务发现、健康检查和优雅停机。
- [√] 6.4 增加角色伪造、不同运单冲突、用户越权、重复发货和重复收货测试。

## 7. Gateway 切流

- [√] 7.1 扩展 Gateway gRPC Clients 和服务配置，接入 Identity、Cart、Payment、Fulfillment。
- [√] 7.2 新增 Identity/Address HTTP 路由，保持注册、登录、刷新和地址 JSON 契约。
- [√] 7.3 新增 Cart HTTP 路由，并在普通订单 items 为空时读取 Cart 后调用 Order。
- [√] 7.4 新增 Payment/Refund/Fulfillment HTTP 路由，统一 gRPC 错误到稳定 HTTP 错误码。
- [√] 7.5 验证显式路由优先于 Commerce NoRoute，阶段 4 新链路不调用 Commerce Repository。

## 8. 配置、数据和部署

- [√] 8.1 扩展 `common/config` 的 Cart/Payment/Fulfillment 配置与按角色依赖加载测试。
- [√] 8.2 新增阶段 4 独立 YAML 和 `migrations/003_stage4_services.sql`，迁移不得包含 DROP/TRUNCATE。
- [√] 8.3 更新 Docker Compose，增加三个新进程并升级 Identity，保持 Commerce 回退和健康依赖。
- [√] 8.4 更新 `api/openapi.yaml` 的运行边界和阶段 4 响应字段。

## 9. 阶段验收

- [√] 9.1 增加 Gateway + 八个目标服务的 Memory/Fake/bufconn E2E，覆盖注册、地址、购物车、下单、支付、发货、收货和退款。
- [√] 9.2 验证重复注册/Refresh/支付回调/退款/发货/收货以及用户和管理员权限边界。
- [√] 9.3 运行 `go test ./...`、`go vet ./...`、`go test -race ./...`、`gofmt`、`git diff --check` 和 Compose 静态解析。
- [√] 9.4 检查 Docker daemon；不可用时记录真实 MySQL/Redis/etcd/RabbitMQ/Compose 跳过项。
> 备注: Docker client/Compose 可用，但 daemon socket 无权限；已用临时测试变量完成 Compose 静态解析，并以 Memory/Fake/bufconn 完成验收。真实 migration、MySQL/Redis/etcd/RabbitMQ、Compose 多进程和真实多进程 gRPC E2E 留待可用环境执行。

## 10. 安全与一致性审计

- [√] 10.1 检查服务端 user_id/role 二次授权、JWT/HMAC 重放、PII/Token/支付签名日志和配置密钥。
- [√] 10.2 检查跨服务 deadline、幂等唯一键、同步中断恢复、数据表所有权和 Commerce 双写风险。

## 11. 文档与方案迁移

- [√] 11.1 更新 README、CHANGELOG、project、overview、arch、api、data 和 Identity/Cart/Payment/Fulfillment/Gateway/Commerce 模块文档。
- [√] 11.2 更新总迁移任务阶段 4 状态和历史索引。
- [√] 11.3 将本方案包迁移到 `helloagents/history/2026-08/202608061319_stage4_domain_services/`。

## 执行总结

- 完成 42 项，无代码任务失败；Docker 真实集成按环境约束跳过。
- 通过 `env -u GOTMPDIR go test ./...`、`go vet ./...`、`go test -race ./...`、`gofmt -l`、`git diff --check`、阶段 4 Memory/Fake/bufconn E2E、OpenAPI YAML 解析和 Compose 静态解析。
- Identity、Cart、Payment、Fulfillment 已有独立 gRPC 入口、数据 Repository、服务发现配置和 Gateway `/api/v1` 显式路由；Commerce 仅保留迁移期 NoRoute/兼容回退。
- 新商城 RabbitMQ Outbox/Inbox 仍留待阶段 5；现有旧秒杀 RabbitMQ 链路未删除。
