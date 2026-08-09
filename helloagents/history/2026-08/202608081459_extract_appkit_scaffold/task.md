# 任务清单: 抽取服务启动脚手架为 shared/platform/appkit

目录: `helloagents/plan/202608081459_extract_appkit_scaffold/`

---

## 1. 公共包
- [√] 1.1 新建 `shared/platform/appkit/appkit.go`，实现 `ValidateContract`/`RegisterService`/`RegisterHealth`/`ServeWithShutdown`，依赖 `shared/contracts`、`shared/platform/config`、`shared/platform/discovery` 与 grpc health 库

## 2. 服务入口改造
- [√] 2.1 改造 `services/cart/cmd/cart-service/main.go`，删除本地 validateContract/registerService/registerHealth/serveWithShutdown 并改用 appkit，清理残留 import
- [√] 2.2 改造 `services/payment/cmd/payment-service/main.go`，同上
- [√] 2.3 改造 `services/fulfillment/cmd/fulfillment-service/main.go`，同上
- [√] 2.4 改造 `services/catalog/cmd/catalog-service/main.go`：validateServiceContract→appkit.ValidateContract，注册/health/serve 内联→appkit，新增信号 ctx（行为增强：获得优雅停机）
- [√] 2.5 改造 `services/identity/cmd/identity-service/main.go`：单行边界校验→appkit.ValidateContract，注册/health/serve 内联→appkit
- [√] 2.6 改造 `services/inventory/cmd/inventory-service/main.go`：validateServiceContract→appkit.ValidateContract，注册/health/serve 内联→appkit（停止判断收敛为 errors.Is）
- [√] 2.7 改造 `services/order/cmd/order-service/main.go`：内联边界校验→appkit.ValidateContract，注册/health/serve 内联→appkit
- [√] 2.8 每个服务保留特有函数（buildRepository/buildStore/configureMessaging/worker 系列），不得误删

## 3. 测试与安全
- [√] 3.1 新增 `shared/platform/appkit/appkit_test.go`，覆盖 RegisterHealth 与 ServeWithShutdown 优雅退出
- [√] 3.2 安全检查：全仓 `go build ./...`、`go vet ./...`、`go test ./...`、`scripts/check_boundaries.sh` 通过
- [√] 3.3 确认未改动 gateway 与各服务内部业务代码，grep 无残留本地副本函数

## 4. 知识库
- [√] 4.1 更新 `helloagents/CHANGELOG.md`（变更条目）
- [√] 4.2 迁移本方案包至 `helloagents/history/2026-08/` 并更新 `history/index.md`
