# 变更提案: 服务优先单仓库

## 需求背景

当前仓库已经把入口、领域实现和共享平台分层，但仍以根级 `cmd`、`internal` 和 `config` 为中心。单个服务的入口、实现、测试和配置分散在多个根目录中，阅读或迁移一个服务时需要跨目录收集上下文，与成熟微服务常见的服务自治目录仍有差距。

## 变更内容

1. 为 Gateway、Identity、Catalog、Inventory、Cart、Order、Payment 和 Fulfillment 建立 `services/<service>` 自治目录。
2. 每个服务在自身目录中保存 `cmd`、`internal`、`etc` 和可选 `testkit`。
3. 将跨服务契约、Protobuf、生成代码、客户端和平台能力收敛到 `shared`。
4. 调整 Docker、Compose、Makefile、测试脚本和文档，使服务可从自身目录独立定位和构建。
5. 删除过时的旧路由压测程序和迁移后形成的空根目录。

## 影响范围

- **模块:** 全部业务服务、Gateway、Contracts、Messaging、Platform、构建和测试。
- **文件:** Go import path、服务入口、配置示例、Protobuf 生成路径、Docker/Compose、Makefile、知识库。
- **API:** HTTP、gRPC、事件名称和 payload 不变。
- **数据:** migration 文件名、SQL 内容和数据库表不变。

## 核心场景

### 需求: 单目录理解和构建服务
**模块:** 全部服务
开发者进入 `services/<service>` 后即可找到该服务入口、实现、单元测试和本地配置。

#### 场景: 修改 Order Service
- 入口位于 `services/order/cmd/order-service`。
- 实现和单元测试位于 `services/order/internal/app`。
- 本地配置位于 `services/order/etc`。

### 需求: 编译期保护领域实现
**模块:** 全部服务
服务实现位于服务私有 `internal` 中，其他服务生产代码不能直接导入。

#### 场景: 跨服务调用
- 生产代码只通过 `shared/gen/commerce`、`shared/contracts`、共享客户端或消息契约通信。
- 跨服务内存 E2E 只允许通过显式 `testkit` 使用测试夹具。

## 风险评估

- **风险:** Go `internal` 规则会阻断现有 Gateway E2E 对领域实现的直接导入。
- **缓解:** 为测试建立最小 `testkit` 门面，并增加边界审计，禁止生产文件导入其他服务的 `internal` 或 `testkit`。
- **风险:** Protobuf descriptor 和配置相对路径可能因移动失效。
- **缓解:** 从新 proto 位置重新生成代码，执行完整测试、生成幂等检查和 Compose 静态解析。
