# 任务清单: 修复四个核心缺陷并移除死代码

目录: `helloagents/plan/202608091438_fix_core_bugs/`

---

## 1. Bug1 已发货订单退款失败
- [√] 1.1 `services/order/internal/app/model.go`：`canTransition` 的 `StatusShipped` 增加 `StatusRefundPending: true`
- [√] 1.2 `services/order/internal/app/service_test.go`：新增 `TestRefundShippedOrder`（发货后退款 → RefundPending → Refunded 全链路）

## 2. Bug4 Redis 初始库存永不写入
- [√] 2.1 `shared/platform/config/conf.go`：`InventoryConfig` 增加 `Stock map[uint64]int32`（mapstructure `stock`）；`applyEnvOverrides` 解析 `SECKILL_INVENTORY_STOCK`（JSON）
- [√] 2.2 `services/inventory/internal/app/redis_store.go`：新增 `SeedStock(ctx, map[uint64]int32)`（SetNX 幂等，不覆盖已有键）
- [√] 2.3 `services/inventory/cmd/inventory-service/main.go`：`buildStore` redis 分支启动时播种（配置为空回退 `{1:100}`）
- [√] 2.4 配置样例：`services/inventory/etc/inventory.yaml` 加 `stock: {1: 100}`；`deploy/config/commerce-services.example.yaml` 加 `SECKILL_INVENTORY_STOCK` 注释说明

## 3. Bug2 已释放 reservation 被幂等重试复用
- [√] 3.1 `services/inventory/internal/app/memory_store.go`：`Reserve`/`AdmitSeckill` 命中分支校验 `status == ReservationReserved`，否则 `ErrConflict`
- [√] 3.2 `services/inventory/internal/app/redis_store.go`：`Reserve`/`AdmitSeckill` 的 `code==10` 分支追加相同状态校验
- [√] 3.3 `services/inventory/internal/app/server_test.go`：新增 released 后同 Reservation ID 重试返回 `ErrConflict` 的回归测试

## 4. Bug3 消息重试计数与忙循环
- [√] 4.1 `services/inventory/internal/app/redis_inbox.go`：value 改 `processing:N`；claim 脚本新键=1/租约过期+1/返回 attempts；markProcessed 前缀匹配；markFailed 不再 DEL 改为续租；`Claim` 用 `Int64Slice` 解析
- [√] 4.2 `shared/platform/messaging/rabbit.go`：`!claim.Claimed` 分支改为延迟重投（提取 `publishRetry`，新增 `requeueDelayed` 永不 DLQ）；删除 `attemptFromHeaders`
- [√] 4.3 `shared/platform/messaging/types_test.go`：补充 `RetryDelay` 边界相关断言（如有增量）

## 5. 死代码移除
- [√] 5.1 `services/payment/internal/app/events.go`：删 `pending`/`PendingOrder`/`mu`，handler 保留校验；清理 `sync` import
- [√] 5.2 `services/fulfillment/internal/app/events.go`：同上
- [√] 5.3 删除 `services/gateway/internal/middleware/auth.go`、`shared/platform/utils/jwt.go`（utils 包仅剩 jwt.go 则整体删除）
- [√] 5.4 `services/gateway/internal/app/routes.go`：删 debug `POST /login` 块并清理 `time/config/utils` import
- [√] 5.5 删除 `services/gateway/internal/middleware/sentinel.go`、`services/gateway/internal/app/sentinel.go` 及 `main.go` 中 `initSentinel` 调用；`go.mod` 清理 sentinel-golang（按网络情况）
- [√] 5.6 `services/gateway/internal/app/stage4_routes_test.go`：`utils.GenerateToken` 改用无效 token 字符串，测试意图不变

## 6. 安全检查与回归
- [√] 6.1 `go build ./...`、`go vet ./...` 通过
- [√] 6.2 `go test -race -count=1 ./...` 通过（含新增测试）
- [√] 6.3 `bash scripts/check_boundaries.sh`、`bash tests/stage5_memory_e2e.sh` 通过
- [√] 6.4 `rg "JWTAuth|PendingOrder|SentinelLimit|attemptFromHeaders|GenerateToken|/login"` 无残留（测试断言除外）；`gofmt -l` 为空

## 7. 知识库同步
- [√] 7.1 `helloagents/CHANGELOG.md`：按 G7 推断版本，补充本次修复与清理条目
- [√] 7.2 `helloagents/wiki/arch.md`：ADR 表追加（Bug2 方案 A 决策、Bug3 对齐 SQL Inbox 语义）
- [√] 7.3 `helloagents/wiki/modules/*`：缺陷复盘（根因/修复/预防），涉及 order、inventory、platform(messaging)、gateway、payment、fulfillment
- [√] 7.4 迁移方案包至 `helloagents/history/2026-08/` 并更新 `history/index.md`
