# 变更提案: 抽取服务启动脚手架为 shared/platform/appkit

## 需求背景

八个服务中七个业务服务的 `main.go` 存在大量重复的启动装配代码，经逐字比对确认：

- `registerService`、`registerHealth`、`serveWithShutdown`、`validateContract` 在 cart/payment/fulfillment 中为**逐字重复**（脚本对比 body 长度 318/261/415/251 字符完全一致，仅日志中的服务名不同）。
- 其余服务形态不一：catalog/inventory 用 `validateServiceContract`（逻辑同源、命名不同），catalog/identity/inventory/order 把注册、健康检查、优雅停机逻辑**内联**在 `main` 中，identity 仅做单行边界校验。
- 同一逻辑"有的抽了函数、有的内联"，维护一处契约校验或启动行为变更需同步 7 个文件，容易漏改。

## 变更内容

1. 新建 `shared/platform/appkit` 包，承载 4 个零业务依赖的启动脚手架函数：
   `ValidateContract`、`RegisterService`、`RegisterHealth`、`ServeWithShutdown`。
2. 7 个业务服务（catalog、identity、inventory、order、cart、payment、fulfillment）统一删除本地副本/内联逻辑，改为调用 `appkit`。
3. 顺带统一 catalog/inventory 的 `validateServiceContract` 命名、identity 补齐 `ValidateServiceBoundaries` 校验、inventory 的停止错误判断由 `strings.Contains` 收敛为 `errors.Is`、catalog 获得与其余服务一致的信号优雅停机。

## 影响范围

- **模块:** shared/platform（新增 appkit）+ 7 个服务入口
- **文件:** `shared/platform/appkit/appkit.go`（新增）、7 个 `services/*/cmd/*-service/main.go`
- **API:** 无（纯内部重构，gRPC/HTTP 契约不变）
- **数据:** 无
- **知识库:** `helloagents/wiki/modules/platform.md`（视内容补充 appkit）、`CHANGELOG.md`、`history/index.md`

## 核心场景

### 需求: gofmt/vet 与行为一致性
**模块:** shared/platform/appkit

#### 场景: 全仓编译测试通过
前置: 改造完成
- 预期结果: `go build ./...`、`go vet ./...`、`go test ./...` 通过；未删除任何服务特有函数（`buildRepository`、`configureMessaging`、inventory store 系列、order worker 系列等）。

#### 场景: 启动行为等价
前置: 任意外部信号（SIGINT/SIGTERM）
- 预期结果: 每个服务均通过公共 `ServeWithShutdown` 优雅停机；etcd 不可用时经 `RegisterService` 降级跳过而非终止进程。

## 风险评估

- **风险:** 删除 `errors`/`health`/`discovery` 等 import 后残留未使用 import 导致编译失败
- **缓解:** 逐服务 `goimports` 风格核对 + 全仓 `go build` 门禁；任务按服务拆分，每步可编译。
- **风险:** catalog 从无优雅停机变为有优雅停机，行为增强影响部署脚本
- **缓解:** 与其余 6 个服务行为对齐，docker-compose 与测试脚本均以信号/进程终止交互，优雅停机为兼容超集；在 CHANGELOG 标注。
