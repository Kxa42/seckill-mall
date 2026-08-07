# 技术设计: 微服务仓库结构收敛

## 技术方案

### 目标目录

```text
cmd/
  api-gateway/
  identity-service/
  catalog-service/
  inventory-service/
  cart-service/
  order-service/
  payment-service/
  fulfillment-service/
  migrate/
internal/
  gateway/
  identity/
  catalog/
  inventory/
  cart/
  order/
  payment/
  fulfillment/
  contracts/
  gen/commerce/
  platform/
    config/
    discovery/
    internalcall/
    messaging/
    observability/
    orderclient/
    auth/
    httpx/
    migrate/
api/proto/
migrations/
deploy/
tests/
```

### 迁移规则

- 顶层 `*_service` 实现移动到对应 `internal/<service>`，保留现有包内 API 和业务行为。
- `api_gateway` 的实现移动到 `internal/gateway`，其 `main` 改为 `Run`；`cmd/api-gateway/main.go` 只负责调用入口。
- `common/contracts` 移动为 `internal/contracts`；`common/pb` 移动为 `internal/gen/commerce`，保持 Protobuf 类型和 gRPC 方法签名不变。
- `common/config`、`common/discovery`、`common/internalcall`、`common/messaging`、`common/orderclient`、`common/tracer`、`common/utils` 按平台职责移动到 `internal/platform/*`。
- `internal/platform/auth`、`httpx`、`migrate` 保持其现有职责，补齐为统一平台目录。
- migration 暂时保持平铺版本文件；文件名直接写入 `schema_migrations.version`，不改名、不拆目录，服务所有权通过 SQL 注释和知识库记录。
- `config.example.yaml`、未被入口加载的 `config/user.yaml`、旧 `test.http` 和旧 Protobuf 文件若无引用则删除；`config/*.yaml` 保留为按服务示例配置。
- `cmd/commerce-api`、`order_service`、`product_service`、`mq_consumer`、`dlq_consumer`、`outbox_worker` 等未跟踪空目录不纳入提交；若存在跟踪文件则先审计后删除。

## 架构决策 ADR

### ADR-004: 统一单仓库服务目录与平台层

- **状态:** ✅采纳
- **决策:** 可执行入口统一放在 `cmd`，服务实现统一放在 `internal/<service>`，跨服务协议进入 `internal/contracts` 和 `internal/gen`，基础设施进入 `internal/platform`。
- **原因:** 保持单 Go module 的低迁移成本，同时让路径直接表达运行边界和依赖方向。
- **取舍:** 当前仍是单仓库、单 module，不能像多 module 或多仓库那样提供编译级服务隔离；后续若需要独立发布，可在此布局上拆分 module。

## 安全与性能

- 本次不改认证算法、密钥来源、网络暴露端口、数据库表或消息协议。
- 不执行 `DROP TABLE`、`TRUNCATE` 或生产服务连接；只移动代码、配置示例和 migration 文件。
- 包路径变化只影响编译期，不增加运行时开销。
- 删除操作以 Git 可恢复历史为保护，执行前完成引用和 Git 跟踪范围核对。

## 验证方案

1. `go list ./...` 确认所有包可解析，且旧 import path 不再出现。
2. `gofmt -w` 后执行 `GOTMPDIR=/tmp go test ./...` 和 `GOTMPDIR=/tmp go vet ./...`。
3. 执行 `tests/stage5_memory_e2e.sh` 以及脚本路径审计。
4. 检查 Dockerfile、Compose、README、OpenAPI 和知识库中的旧路径引用。
