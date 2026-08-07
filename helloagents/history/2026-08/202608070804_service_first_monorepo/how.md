# 技术设计: 服务优先单仓库

## 技术方案

### 目标布局

```text
services/
  gateway/
    cmd/api-gateway/
    internal/app/
    internal/middleware/
    api/openapi.yaml
    etc/gateway.yaml
  order/
    cmd/order-service/
    internal/app/
    etc/order.yaml
    testkit/
  identity|catalog|inventory|cart|payment|fulfillment/
    cmd/<service>/
    internal/app/
    etc/<service>.yaml
    testkit/
shared/
  contracts/
  clients/order/
  gen/commerce/
  proto/commerce/
  platform/
tools/migrate/
deploy/config/commerce-services.example.yaml
migrations/
tests/
```

### 实现要点

- 保持根 `go.mod`，避免一次变更同时引入多 module 版本治理。
- 服务实现下沉到各自 `internal/app`，服务入口是唯一生产导入者。
- Gateway E2E 改为导入各服务 `testkit`；`testkit` 只暴露内存组装所需构造器。
- `shared` 只保存跨服务协议、生成代码、平台基础设施和明确的 Order 客户端，不保存领域 Repository。
- 服务配置移动到自身 `etc`；配置加载器同时支持仓库内服务路径和容器中的 `/app/config`。
- `migrations` 保持根级全局版本序列，因为现有数据库以文件名记录 migration 版本；按服务拆库和迁移版本属于后续数据架构变更。

## 架构决策 ADR

### ADR-005: 采用服务优先单仓库并保留单 Go module
**上下文:** 当前服务入口、实现和配置分散在根级目录，服务边界主要依赖约定。
**决策:** 每个服务拥有独立目录和私有 `internal`，共享代码进入 `shared`，仓库暂时保留单 `go.mod`。
**理由:** 获得服务自治和编译边界，同时避免多 module 带来的依赖版本、工作区和发布流程复杂度。
**替代方案:** 立即拆分多个 Go module 或多个仓库 → 拒绝原因: 当前服务仍共享发布节奏、集成测试和平台代码，收益不足以覆盖迁移成本。
**影响:** 生产代码不能导入其他服务内部实现；跨服务集成测试需要显式测试门面或进程级测试。

## 安全与性能

- 不修改认证、签名、密钥来源、端口、数据库或消息协议。
- `testkit` 不包含默认凭据，不被生产入口导入。
- 目录和 import path 调整不增加运行时开销。
- 删除操作仅针对已确认过时的压测程序和迁移后空目录。

## 测试与部署

- 运行 `make generate` 并确认生成结果幂等。
- 运行 `make check`，覆盖格式、包枚举、单元测试、vet、边界审计和 Memory E2E。
- 使用临时占位环境变量执行 `docker compose config --quiet`，不启动外部服务。
- 构建所有 `services/*/cmd/*` 与 `tools/migrate`。
