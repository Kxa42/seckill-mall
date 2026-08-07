# 变更提案: 微服务仓库结构收敛

## 需求背景

当前项目已经具备独立服务、gRPC 契约、消息可靠性和自动化测试，但代码目录仍保留迁移阶段形成的多套组织规则。服务实现同时位于顶层 `*_service` 和 `internal/order`，Gateway 入口位于 `api_gateway`，共享代码集中在宽泛的 `common` 目录，导致目录职责不一致、跨层依赖不易识别，降低新成员定位代码和后续拆分服务的效率。

## 变更内容

1. 统一所有服务实现至 `internal/<service>`，统一所有可执行程序至 `cmd/<service>/`。
2. 将 Gateway 拆为 `cmd/api-gateway` 启动入口和 `internal/gateway` 实现包。
3. 将共享代码按跨服务契约、生成代码和平台能力拆分，移除 `common` 作为总括目录。
4. 按服务职责整理数据库迁移目录，删除确认无引用的旧配置、旧入口和空遗留目录。
5. 更新构建、Compose、测试脚本、README 和 HelloAGENTS 知识库，保证路径与运行事实一致。

## 影响范围

- **模块:** Gateway、Identity、Catalog、Inventory、Cart、Order、Payment、Fulfillment、平台基础设施、消息契约。
- **文件:** Go 包路径、Dockerfile、docker-compose.yaml、tests、README、helloagents/wiki。
- **API:** 外部 HTTP 和 gRPC 接口保持不变，仅调整 Go 包路径和启动命令。
- **数据:** 不修改表结构和数据，不删除数据库中的历史表；仅调整 migration 文件的目录组织。

## 核心场景

### 需求: 按目录快速定位服务代码
**模块:** 全部服务
服务实现、测试和入口应使用统一的目录约定，读者无需记忆 Order 与其他服务的特殊路径。

#### 场景: 新增或修改服务
开发者可分别在 `cmd/<service>`、`internal/<service>` 和 `api/proto` 找到入口、实现和契约。

### 需求: 限制跨层依赖
**模块:** Gateway、平台、契约
Gateway 只依赖 Gateway 内部实现、gRPC 生成代码、契约和平台客户端，不直接引用 Order 领域内部包。

#### 场景: Gateway 生成内部调用元数据
Gateway 通过通用内部调用能力完成签名，Order 领域包不再作为工具包被 Gateway 引入。

## 风险评估

- **风险:** Go import path、Docker 构建路径和测试脚本同时变化，遗漏引用会导致构建失败。
- **缓解:** 先执行全仓引用扫描，再执行 `gofmt`、`go test ./...`、`go vet ./...` 和 Compose 静态路径审计；删除前保留 Git 可恢复历史。
