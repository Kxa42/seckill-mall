# 项目技术约定

## 技术栈
- **核心:** Go 1.25 / Gin / gRPC / Protobuf / GORM。
- **数据:** MySQL 8 / Redis / RabbitMQ / etcd。
- **可观测性:** OpenTelemetry / Prometheus / Grafana / Jaeger。
- **交付:** Docker Compose、版本化 SQL migration、OpenAPI 3.0。

## 架构与兼容约定
- 新商城当前由 Gateway、Identity、Catalog、Inventory/Seckill、Cart、Order、Payment 和 Fulfillment 协同承载；Order Service 是普通/秒杀订单唯一写入者，Commerce API 只保留迁移期回退和旧兼容能力。跨服务稳定契约位于 `common/contracts` 和 `proto/commerce`。
- 旧秒杀服务继续保留兼容接口；其 `product`、`orders`、`outbox_events` 与新商城表相互隔离。
- 新客户端只使用 `/api/v1`；旧 `/login` 仅在 Gateway `debug` 模式注册。
- 每个领域只写自己拥有的表；跨领域功能优先使用 API 或版本化事件。
- `common/` 只能保存稳定的跨服务契约、配置、追踪和工具，不得保存业务 Repository 或跨服务数据库访问。
- 服务边界与数据所有权以 `common/contracts` 为契约基线；每个业务服务只能写入自己的数据集。
- 目标商城服务的地址、独立 DSN、RabbitMQ 和 etcd 配置约定见 `config/commerce-services.example.yaml`；设置 `SECKILL_SERVICES_CONFIG` 后由按角色配置加载器消费，模板不包含真实密钥。
- 事件信封的 `EventType` 与 `EventVersion` 独立演进；事件发布统一使用 `common/contracts.NewEventEnvelope`，消费者自行处理未知未来版本。
- Catalog/Inventory/Identity/Cart/Order/Payment/Fulfillment 可分别通过 `go run ./cmd/catalog-service`、`go run ./cmd/inventory-service`、`go run ./cmd/identity-snapshot-service`、`go run ./cmd/cart-service`、`go run ./cmd/order-service`、`go run ./cmd/payment-service`、`go run ./cmd/fulfillment-service` 启动；Gateway 对跨服务 gRPC 调用设置 deadline，阶段 4 `/api/v1` 路由显式调用目标服务，其他未迁移路由继续使用 NoRoute 过渡代理。Order/Commerce 的切流由 `SECKILL_COMMERCE_ORDER_MODE` 控制，但两个写入者不能同时启用。
- Go 用户级构建临时目录固定为 `/tmp`，构建缓存固定为 `/tmp/go-build-cache`；当前进程若继承旧 `GOTMPDIR`，验证命令使用 `env -u GOTMPDIR`。

## 开发约定
- **代码规范:** Go 代码必须通过 `gofmt`、`go test ./...`、`go vet ./...` 和 `git diff --check`。
- **目录约定:** 新进程放入 `cmd/`，业务逻辑放入 `internal/`，`common/` 只存放稳定共享能力。
- **依赖方向:** HTTP transport 依赖应用服务，应用服务依赖 Repository 接口，领域模型不依赖 Gin 或 GORM。
- **金额:** 新商城 API、模型和表统一使用 `int64 amount_cents`；旧兼容 Protobuf 的 `float32` 不得进入新交易域。
- **编码:** UTF-8 无 BOM。

## 安全、错误与日志
- 密码使用 bcrypt；Access Token 短期有效，Refresh Token 只保存 SHA-256 哈希并在刷新时轮换。
- HTTP API 使用稳定错误码和统一响应信封；未知内部错误不得返回 SQL、Redis 或 gRPC 原始信息。
- 用户地址按 `user_id` 校验归属，运营接口要求 `admin` 角色。
- 密钥、DSN 和中间件密码只能通过环境变量注入；禁止记录密码、Token、地址明文请求体和支付签名。
- Mock 支付只用于开发与验收，不接收真实支付凭证。

## 测试与流程
- **单元测试:** 覆盖状态机、整数金额、库存预占、Token 轮换、幂等和权限边界。
- **内存 E2E:** `bash tests/e2e_memory.sh`，无需 Docker，覆盖注册到确认收货主链路。
- **Order Memory E2E:** `bash tests/order_service_memory_e2e.sh`，通过 gRPC/bufconn 验证普通/秒杀订单、幂等、查询、取消、退款恢复和权限边界。
- **Stage 4 Memory E2E:** `bash tests/stage4_memory_e2e.sh`，通过 Memory/Fake/bufconn 验证身份、购物车、订单、支付和履约完整链路。
- **集成测试:** 有可用 Docker/MySQL 时执行 migration 重放、MySQL Repository、Compose 健康检查和旧 MQ 补偿链路。
- **环境限制:** Docker daemon 不可用时跳过真实 MySQL/Redis/etcd/RabbitMQ/Compose 联调；Memory/Fake/bufconn 结果不能等同于真实基础设施验收。
- **提交:** 建议使用 Conventional Commits。
