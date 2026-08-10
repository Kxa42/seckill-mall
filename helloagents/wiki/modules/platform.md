# Platform

## 目的
提供安全配置、认证、统一 HTTP 响应、数据库迁移、容器编排和可观测基础。

## 模块概述
- **职责:** 环境配置、bcrypt/JWT、内部 HMAC、请求 ID、统一响应、migration runner、Dockerfile、Compose 和 Prometheus 抓取。
- **状态:** ✅阶段 5 统一运行时
- **最后更新:** 2026-08-07

## 规范

### 需求: 后端可部署与验收
**模块:** Platform

#### 场景: 启动完整后端
- Compose 要求显式提供 MySQL、Redis、RabbitMQ、JWT 和支付签名密钥。
- migration 成功后才启动各领域服务；服务提供存活、就绪或 metrics 健康检查。
- 容器使用非 root 用户，服务支持 SIGTERM 优雅停机。

#### 场景: 无 Docker 的本地验收
- `tests/stage5_memory_e2e.sh` 使用 Memory/Fake 测试统一事件、幂等和旧路径 404。
- `make check` 统一执行格式、包枚举、单元测试、vet 和 Memory E2E。
- Catalog/Inventory 在 `debug` 或 memory 模式下不连接真实 MySQL/Redis；bufconn E2E 验证 gRPC 契约和状态机。

## 依赖
- Go 标准库、Gin、GORM MySQL driver、Docker Compose、Prometheus、OpenTelemetry。

## 当前边界
- 当前环境无法访问 Docker daemon，真实 migration 重放、Compose 健康检查和 RabbitMQ 故障注入待有 Docker 环境后执行。
- Catalog/Inventory 的 etcd 注册失败会记录 warning 并继续启动，适合无 etcd 的本地代码验收；生产部署仍需配置健康的 etcd。
- Go 用户级 `GOTMPDIR` 固定为 `/tmp`，`GOCACHE` 固定为 `/tmp/go-build-cache`；当前 shell 若继承旧变量，测试使用 `env -u GOTMPDIR`。
- 平台实现统一位于 `shared/platform`；跨服务协议和生成代码分别位于 `shared/proto` 与 `shared/gen`，平台层不得反向依赖服务实现。

### 需求: 按服务配置模板加载
**模块:** Platform Configuration

#### 场景: 使用统一商城配置模板启动服务
- 设置 `SECKILL_SERVICES_CONFIG` 后，Gateway、Identity、Catalog、Inventory、Cart、Order、Payment 和 Fulfillment 按自身角色读取 `services`/`gateway` 配置。
- `${VAR}` 只展开当前进程需要的环境变量；缺失变量在启动前报错，错误信息不包含变量值。
- 未设置 `SECKILL_SERVICES_CONFIG` 时，各服务使用自身 `services/<service>/etc/<service>.yaml` 配置；统一模板位于 `deploy/config/commerce-services.example.yaml`。

## 变更历史
- [202608091438_fix_core_bugs](../../history/2026-08/202608091438_fix_core_bugs/) - `InventoryConfig` 新增 `Stock map[uint64]int32`（`SECKILL_INVENTORY_STOCK` JSON 解析），支撑 Redis 库存启动播种；`rabbit.go` 租约未到期改为延迟重投并移除无引用的 `attemptFromHeaders`；`shared/platform/utils` JWT 工具随 Gateway 死代码整体移除。
- [202608070804_service_first_monorepo](../../history/2026-08/202608070804_service_first_monorepo/) - 平台能力迁入共享层，配置按服务自治并增加生产依赖边界审计。
- [202608070731_repository_layout_refactor](../../history/2026-08/202608070731_repository_layout_refactor/) - 收敛平台包目录并新增统一工程检查入口。
- [202608051526_backend_commerce_mvp](../../history/2026-08/202608051526_backend_commerce_mvp/) - 增加商城平台基础与完整编排。
