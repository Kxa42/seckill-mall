# 变更提案: 修复四个核心缺陷并移除死代码

## 需求背景

对当前仓库（Go 1.25 微服务秒杀商城）进行全量缺陷检查后，确认 10 项问题。其中 4 项为可复现的运行时缺陷（均未被既有测试覆盖），其余为无引用的死代码。本次按用户指定顺序逐一修复：**Bug1（已发货退款失败）→ Bug4（Redis 库存永不写入）→ Bug2（已释放 reservation 复重试复用）→ Bug3（消息重试计数失效与忙循环）→ 死代码移除**。

验证基线（修复前全部通过）：`go build ./...`、`go vet ./...`、`go test -race -count=1 ./...`、`bash scripts/check_boundaries.sh`、`bash tests/stage5_memory_e2e.sh`。

## 变更内容

1. **Bug1 订单状态机缺少 `Shipped → RefundPending` 转换**：`canTransition` 中 `StatusShipped` 仅允许 `StatusCompleted`，而 `Refund()` 允许 `StatusShipped` 订单发起退款 → 已发货订单退款必然返回 `CodeInvalidTransition`。
2. **Bug4 Redis 初始库存永不写入**：`reserveLua`/`seckillLua` 遇到 `EXISTS stockKey == 0` 直接返回 0（缺货），全代码库无任何播种路径；`docker-compose.yaml` 固定 `SECKILL_INVENTORY_STORE: redis` → 部署后所有 SKU 恒为缺货、秒杀全部失败。
3. **Bug2 已释放 reservation 被幂等重试复用**：内存/Redis Store 的幂等命中分支仅比较 orderID/userID/skuID/quantity/mode，不校验 `status`。取消订单后 reservation 已 `released`，用同一 Reservation ID 重试会静默返回已释放的预占，导致订单在无库存保障下继续流转。
4. **Bug3 消息重试计数失效与忙循环**：
   - `markFailedInboxScript` 直接 `DEL` Redis Inbox 键 → attempts 恒为 1，失败事件永不进入 DLQ（与 SQL Inbox 语义不一致）。
   - `rabbit.go` 中 `!claim.Claimed`（租约未到期）分支 `Nack(false,true)` 立即重新入队 → 约 30 秒忙循环，prefetch=16 时可能饿死其他消息。
5. **死代码移除**（引用面已逐一 grep 确认）：
   - payment/fulfillment `EventHandler.pending` map 与 `PendingOrder()`（只写不读，无调用者）。
   - Gateway `middleware/auth.go` 的 `JWTAuth`（无任何路由使用，全部 `/api/v1` 路由走 `CommerceJWTAuth`）。
   - `shared/platform/utils/jwt.go`（`GenerateToken`/`ParseToken`/`UserClaims`，仅被上述 auth、debug `/login` 与一个测试引用）。
   - Gateway debug `POST /login` 路由（可给任意 userID 签发旧格式 token，存在越权风险）。
   - `rabbit.go` 的 `attemptFromHeaders`（无引用）。
   - Gateway Sentinel 限流（`middleware/sentinel.go` 的 `SentinelLimit` 无路由挂载；`initSentinel` 只初始化规则从不被消费）。

## 影响范围

- **模块:** 订单状态机、库存存储（内存/Redis）、统一消息 Inbox/消费、Gateway 路由与认证、支付/履约事件消费
- **文件:** 约 15 个源码/配置/测试文件（详见 task.md）
- **API:** 无破坏性变更；新增 `SECKILL_INVENTORY_STOCK` 环境变量与 `inventory.stock` 配置项
- **数据:** 无迁移；Redis 库存播种幂等（SetNX，不覆盖已有库存）
- **已知限制（本期不处理）:** 支付密钥三套命名不一致、支付回调与超时取消竞态、秒杀 ActivityID 回退 1、Catalog 库存双写

## 核心场景

### 场景一: 已发货订单申请退款
**模块:** services/order
用户在已发货订单上发起退款 → `Refund()` 将状态置为 `StatusRefundPending` 时 `repository.Transition` 走 `canTransition` 校验 → 当前 `StatusShipped` 不在退款白名单 → 返回 `CodeInvalidTransition`（HTTP 409 类错误）。修复后允许 `Shipped → RefundPending`，与支付/履约既有发货后退款流程保持一致。

### 场景二: Redis 模式部署后全部缺货
**模块:** services/inventory + docker-compose
`inventory-service` 以 `store=redis` 启动 → 库存键从未写入 → `Reserve`/`AdmitSeckill` 的 Lua 遇 `EXISTS==0` 返回缺货 → 秒杀订单全部失败且无日志告警。修复后服务启动时按配置（或默认 `{1:100}`）播种初始库存，重复启动不覆盖存量。

### 场景三: 取消订单后同幂等键重试
**模块:** services/inventory（memory/redis）
订单取消 → reservation 状态 `released` → 客户端用同一 Reservation ID/Idempotency-Key 重试创建 → 命中幂等分支但 `status != reserved` → 当前实现静默返回已释放预占。修复后返回 `ErrConflict`，调用方明确感知失败并换新幂等键。

### 场景四: 事件消费失败永不进 DLQ
**模块:** shared/platform/messaging + services/inventory
消费者处理事件抛错 → `MarkFailed` 删除 Redis Inbox 键 → 重投消息被当作全新事件（attempts=1）→ 无限重试；若另一副本持有租约，当前消息 `Nack(false,true)` 立即重排 → 30 秒忙循环。修复后 attempts 持久化（与 SQL Inbox 一致），5 次失败进 DLQ；租约占用期间延迟重投而非立即重排。
