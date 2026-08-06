# 任务清单: 事件契约与按服务配置落地

目录: `helloagents/plan/202608061041_contract_config_hardening/`

## 1. 事件契约语义与测试

- [√] 1.1 在 `common/contracts/events.go` 明确 `EventType`、`EventVersion` 的独立演进语义，并保持正数未来版本可接收。
- [√] 1.2 在 `common/contracts/events_test.go` 增加已知事件未来版本、版本号不一致和未知事件未来版本测试。
- [√] 1.3 在 `common/contracts/boundaries.go` 简化服务依赖校验，保持未知服务仍被拒绝。

## 2. 共享契约运行时接入

- [√] 2.1 在 `cmd/catalog-service/main.go` 启动时校验 `ServiceCatalog` 边界并使用契约服务名默认值。
- [√] 2.2 在 `cmd/inventory-service/main.go` 启动时校验 `ServiceInventory` 边界并使用契约服务名默认值。
- [√] 2.3 在 `api_gateway/clients.go` 使用 `common/contracts` 的 Catalog/Inventory 服务名作为默认发现键，并同步独立服务配置。

## 3. 按服务配置加载器

- [√] 3.1 新增 `common/config/services.go`，实现严格环境变量展开、统一模板解码、服务选择和运行配置映射。
- [√] 3.2 在 `common/config/conf.go` 接入 `SECKILL_SERVICES_CONFIG`，保留旧配置回退和现有 `SECKILL_*` 覆盖。
- [√] 3.3 新增 `common/config/services_test.go`，覆盖模板加载、环境变量展开、缺失变量、未知服务和 Gateway/Catalog/Inventory 映射。
- [√] 3.4 更新 `config/commerce-services.example.yaml`、`config/catalog.yaml`、`config/inventory.yaml`、`config/gateway.yaml` 的服务名和加载说明。

## 4. 安全、一致性与文档

- [√] 4.1 检查配置错误和启动日志不泄漏 DSN、RabbitMQ URL、Redis 密码、JWT 或支付密钥。
- [√] 4.2 更新 `README.md`、`helloagents/CHANGELOG.md`、`helloagents/project.md`、`helloagents/wiki/arch.md`、`helloagents/wiki/modules/messaging.md`、`helloagents/wiki/modules/platform.md`。
- [√] 4.3 修正阶段 1 任务描述中“未知版本已覆盖”的事实记录，并记录本次 ADR。

## 5. 测试与验收

- [√] 5.1 运行 `go test ./...`、`go vet ./...`、`go test -race ./...`、`git diff --check`。
- [√] 5.2 使用临时环境变量验证 `commerce-services.example.yaml` 可加载；Docker 不可用时记录真实基础设施集成跳过项。
- [√] 5.3 阶段验收：事件版本语义明确、服务实际消费共享契约、统一配置模板可运行、旧配置回退正常。

## 执行总结

- EventType/EventVersion 语义已固定，已知事件未来版本和版本独立演进测试通过。
- Catalog、Inventory 和 Gateway 已使用共享契约服务名；Catalog/Inventory 启动会校验自身服务边界。
- `SECKILL_SERVICES_CONFIG` 已接入，统一模板按角色加载并严格展开当前服务需要的环境变量，旧 YAML 回退保持兼容。
- `go test ./...`、`go vet ./...`、`go test -race ./...`、`git diff --check`、Compose 配置校验和内存 E2E 全部通过。
- Docker daemon 在当前环境不可用，真实 MySQL/Redis/RabbitMQ/Compose 联调按约定跳过。
