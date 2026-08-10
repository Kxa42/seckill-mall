# 技术方案: 修复四个核心缺陷并移除死代码

目录: `helloagents/plan/202608091438_fix_core_bugs/`

## 一、Bug1 订单状态机补充发货后退款转换

### 方案
- `services/order/internal/app/model.go` 的 `canTransition`：
  `StatusShipped: {StatusCompleted: true}` → `StatusShipped: {StatusCompleted: true, StatusRefundPending: true}`。
- 保持 `StatusPaid/StatusShipped → RefundPending → Refunded` 单向流，`Restock` 恢复库存逻辑不变。

### 约束
- 状态转换仍为白名单制，不引入自动回退。
- 上游 Payment 已支持发货后退款，"先款后货可退"语义与支付网关现状一致。

## 二、Bug4 Redis 初始库存配置驱动播种

### 方案
1. `shared/platform/config/conf.go` 的 `InventoryConfig` 增加 `Stock map[uint64]int32`（`mapstructure:"stock"`）。
2. `applyEnvOverrides` 支持 `SECKILL_INVENTORY_STOCK`（JSON 字符串，如 `{"1":100}`），兼容 compose 环境变量注入。
3. `services/inventory/internal/app/redis_store.go` 新增 `SeedStock(ctx, map[uint64]int32) error`：
   - 逐 SKU 执行 `SETNX inventory:stock:<sku> <qty>`，已存在的键（含 0）一律不覆盖 → 重启幂等，保留人工调库结果。
4. `services/inventory/cmd/inventory-service/main.go` 的 `buildStore` redis 分支：
   - 播种 `config.Conf.Inventory.Stock`，为空时回退默认 `{1: 100}`（与 memory 分支默认一致，保证 compose 不配 stock 也能运行）。
   - 播种失败（Redis 连接异常）按既有风格 `log.Fatalf`。
5. 配置样例：`services/inventory/etc/inventory.yaml` 增加 `stock: {1: 100}`；`deploy/config/commerce-services.example.yaml` 增加注释说明 `SECKILL_INVENTORY_STOCK`。

### 边界
- memory 模式不受影响（构造时直接传 stock map）。
- Lua `EXISTS==0` 的缺货分支保留（应对误删键的兜底）。
- 不改 `ServiceDefinition`（部署清单不含库存语义）。

## 三、Bug2 已释放 reservation 拒绝幂等复用

### 方案（ADR: 采用"状态校验返回冲突"）
幂等命中（`code==10` / map 命中）且字段一致时，追加校验 `reservation.Status == ReservationReserved`，否则返回 `ErrConflict`。

- **方案 A（采用）**：命中但非 `reserved` → `ErrConflict`。诚实表达"该预占已失效"，客户端必须更换 Idempotency-Key / Reservation ID 重新下单。改动面小，不触碰 Lua 扣减。
- **方案 B（备选，弃用）**：对 `released` 的 reservation 重新预占（Lua 重置状态并重新扣减）。恢复性更好但引入"同键复活"语义，与订单取消补偿流程（Restock 已生效）冲突，且 Lua 改动面大。

### 落点
- `memory_store.go`：`Reserve` 命中分支、`AdmitSeckill` 命中分支各加状态校验。
- `redis_store.go`：`Reserve`、`AdmitSeckill` 的 `code==10` 分支各加状态校验。
- `confirmed/restocked` 等其他状态同样拒绝复用（幂等键只对 `reserved` 有效）。

## 四、Bug3 消息重试计数与忙循环

### 方案（对齐 SQL Inbox 语义）
SQL Inbox 语义（`sql_store.go`）：attempts 在**租约过期重新认领**时 +1；`MarkFailed` 只续租不递增；第 5 次处理失败 → DLQ。

#### 4.1 Redis Inbox attempts 持久化（`services/inventory/internal/app/redis_inbox.go`）
- value 格式从 `processing` 改为 `processing:N`（N=attempts）。
- `claimInboxScript`：
  - 无键 → `SET processing:1 PX lease`，返回 `{1, 1}`（claimed, attempts=1）。
  - `processed` → 返回 `{2, N}`。
  - 租约过期（`PTTL<=0`）→ `SET processing:N+1 PX lease`，返回 `{1, N+1}`。
  - 租约未到期 → 返回 `{0, N}`（未认领，携带当前 attempts）。
- `markProcessedInboxScript`：匹配 `processing:` 前缀 → `SET processed EX retention`。
- `markFailedInboxScript`：**不再 DEL**，匹配 `processing:` 前缀 → 同值续租 `PX lease`（与 SQL `updated_at=now` 续租一致）。
- Go `Claim` 改用 `Int64Slice()` 一次解析状态与 attempts，消除二次 GET 竞态。

#### 4.2 消费端消除忙循环（`shared/platform/messaging/rabbit.go`）
- `!claim.Claimed` 分支由 `Nack(false,true)` 立即重排改为**延迟重投**：提取 `publishRetry`（publish 到 RetryExchange + Ack 原消息，沿用 `x-event-attempt` 头与指数退避）。
- 新增 `requeueDelayed`：仅延迟重投、**永不 DLQ**（另一副本可能正在成功处理，DLQ 会误伤；租约过期后自然由 `claim` 递增 attempts 收敛）。
- `retryOrDeadLetter` 复用 `publishRetry`，DLQ 判定逻辑不变。
- 收敛性推演：处理失败 → MarkFailed 续租 30s → 重投遇租约未到期 → 延迟重投循环（指数退避）→ 30s 后租约过期 → attempts+1 重新处理 → 第 5 次失败 `retryOrDeadLetter(5)` Nack 进 DLQ。与 SQL 完全一致，无忙循环。

### 测试边界
- 仓库无 Redis/RabbitMQ 依赖且无 miniredis（网络受限不可新增依赖），Redis Lua / AMQP 行为按现有基线不做自动化测试；通过 `go build/vet/test` 与 memory E2E 回归，脚本改动在方案包判读时逐字复核。

## 五、死代码移除

| 位置 | 动作 | 说明 |
|---|---|---|
| `services/payment/internal/app/events.go` | 删 `pending` map、`PendingOrder()`、`mu` | handler 保留 payload 校验（消费侧哨兵） |
| `services/fulfillment/internal/app/events.go` | 同上 | 同上 |
| `services/gateway/internal/middleware/auth.go` | 删整个文件 | `JWTAuth` 无路由使用 |
| `shared/platform/utils/jwt.go` | 删整个文件 | 仅被 auth/debug `/login`/1 个测试引用 |
| `services/gateway/internal/app/routes.go` | 删 debug `POST /login` 块 | 可给任意 userID 签发 token；顺带清 `time/config/utils` import |
| `services/gateway/internal/middleware/sentinel.go` + `app/sentinel.go` + `main.go` 的 `initSentinel` 调用 | 删除 | 规则从不被中间件消费 |
| `rabbit.go` 的 `attemptFromHeaders` | 删除 | 无引用 |
| `stage4_routes_test.go:156` | `utils.GenerateToken` → 无效 token 字符串 | 测试意图（旧格式 token 必须 401）不变 |

- `go.mod` 中 `github.com/alibaba/sentinel-golang` 依赖：删除引用后执行 `go mod tidy`（优先离线 cache）；若网络受限导致 tidy 失败，保留 require 并在知识库记录待清理。
- `routes_test.go` 的 `POST /login → 404` 断言删除后依然成立（未注册路由 Gin 返回 404），无需修改。
- `order_routes_test.go` 未引用 `utils`（错误消息文案而已），无需修改。

## 六、安全与性能

- 删除 debug `/login` 消除"任意 userID 签发旧格式 token"的越权面；旧格式 token 走 `CommerceJWTAuth` 一律 401。
- Bug3 修复降低消费者无效重投（忙循环 × prefetch 竞争），延迟重投使用既有指数退避，无新增热点。
- Bug4 播种仅启动时执行、SetNX 幂等，不引入运行时锁竞争。
- 无密钥/配置向日志输出；错误信息不携带配置值。

## 七、验证计划

1. `go build ./...`、`go vet ./...`。
2. `go test -race -count=1 ./...`（含新增 Bug1/Bug2 回归测试）。
3. `bash scripts/check_boundaries.sh`（服务边界门禁）。
4. `bash tests/stage5_memory_e2e.sh`（内存端到端回归）。
5. `gofmt -l` 相关文件为空；`go mod tidy` 状态确认（sentinel 依赖清理情况按实际网络记录）。
6. 人工复核：`rg "JWTAuth|PendingOrder|SentinelLimit|attemptFromHeaders|GenerateToken"` 无残留。
