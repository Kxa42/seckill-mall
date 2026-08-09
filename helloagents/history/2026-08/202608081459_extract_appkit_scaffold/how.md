# 技术设计: 抽取服务启动脚手架为 shared/platform/appkit

## 技术方案

### 核心技术
- Go（单一 module `seckill-mall`），依赖 `google.golang.org/grpc`、`go.etcd.io/etcd/client/v3`
- 现成能力: `shared/platform/discovery`、`shared/platform/config`、`shared/contracts`

### 实现要点
1. 新建包 `shared/platform/appkit`，四个函数签名与原副本一致：
   - `ValidateContract(service string)`：调 `contracts.ValidateServiceBoundaries()` + `contracts.ServiceBoundaryFor(service)`
   - `RegisterService(ctx context.Context, service, address, port string) *discovery.Registration`：空地址回退 `127.0.0.1:<port>`，调 `discovery.Register`，失败 log 并返回 nil（不终止进程）
   - `RegisterHealth(server *grpc.Server, service string)`：注册空服务名与具体服务名两个 SERVING 状态
   - `ServeWithShutdown(ctx context.Context, server *grpc.Server, listener net.Listener)`：Serve 分离 goroutine，`ctx.Done()` 时 `GracefulStop`
2. 日志统一为通用措辞（原 `cart etcd registration skipped` 等服务名前缀并入 `%s` 参数），消除各服务日志差异。
3. 七个服务入口统一调用；每个服务保留其特有函数（`buildRepository`/`buildStore`/`configureMessaging`/worker 系列）不动。

## 架构决策 ADR

### ADR-20260808-01: 新增 shared/platform/appkit 包承载启动脚手架
**上下文:** 4 个启动装配函数在 3 个服务中逐字重复，另有 4 个服务以不同命名/内联形态存在同一逻辑；不放公共包则无法收敛。
**决策:** 新建 `shared/platform/appkit`，仅放服务启动生命周期通用函数，不放业务仓储/消息装配（后者绑定各服务具体类型，保持各服务独立）。
**理由:** 四个函数零业务类型依赖，天然属于平台层；`shared` 反向依赖 `services` 被边界门禁禁止，而 appkit 只依赖 shared 内部与第三方，无循环。
**替代方案:** 泛型化 `buildRepository` → 拒绝原因: 引入抽象成本，且各服务仓储接口/类型各不相同，收益低于一致性风险。
**影响:** 主包 import 变更；日志文本统一；行为只增不减（identity 补边界校验、catalog 增优雅停机、inventory 停止判断规范化）。

## 安全与性能

- **安全:** 无权限、密钥、支付等 EHRB 信号；仅纯代码重构，不触碰配置解析与注册逻辑语义。
- **性能:** 无热路径变化；新增一层函数调用成本可忽略。

## 测试与部署

- **测试:** 全仓 `go build ./...`、`go vet ./...`、`go test ./...`；新增 `appkit` 轻量单测覆盖 `RegisterHealth`/`ServeWithShutdown` 优雅退出；`scripts/check_boundaries.sh` 确认边界门禁仍通过。
- **部署:** 无需数据库/消息/etcd 变更；docker-compose 无需改动；catalog 获得优雅停机为兼容增强。
