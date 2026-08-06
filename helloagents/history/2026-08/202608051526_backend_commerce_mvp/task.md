# 任务清单: 后端完整商城 MVP

归档目录: `helloagents/history/2026-08/202608051526_backend_commerce_mvp/`

## 1. 架构与平台基础
- [√] 1.1 新增 `internal/platform/config`、`internal/platform/httpx` 和对应测试，实现显式配置、统一响应与错误模型。
- [√] 1.2 新增 `internal/platform/auth`、Token 与密码测试，实现 bcrypt、Access/Refresh Token 和角色声明，依赖任务 1.1。
- [-] 1.3 建立 `cmd/` 入口约定并为现有服务增加兼容启动包装，单任务迁移不超过三个文件。
  > 备注: 新进程已采用 `cmd/`；旧服务为避免大范围重构继续保留原目录入口，不再增加无逻辑的包装层。
- [√] 1.4 新增版本化 migration runner 和 `migrations/` 基线，验证初始化库与升级库均可执行。

## 2. Identity
- [√] 2.1 新增用户与刷新令牌领域模型、Repository 接口和 MySQL 实现，验证 why.md#需求-用户身份与地址-场景-用户注册并登录。
- [√] 2.2 实现注册、登录、刷新令牌轮换和基础 RBAC application service，并覆盖重复账号与错误密码测试。
- [√] 2.3 实现地址 CRUD 与资源归属校验，并覆盖越权访问测试。
- [√] 2.4 增加 Identity gRPC/HTTP transport 和 API Gateway `/api/v1/auth`、`/api/v1/addresses` 路由。
  > 备注: 按 ADR-001 以 Commerce HTTP 模块落地，未增加独立 Identity gRPC 部署单元。

## 3. Catalog、SKU 与库存
- [√] 3.1 扩展 Product Proto 和模型，增加分类、SPU、SKU、分页列表与详情，验证 why.md#需求-商品目录与购物车-场景-用户选择在售-sku。
  > 备注: 新模型通过版本化 HTTP/OpenAPI 提供，旧 Product Proto 保持兼容未扩展。
- [√] 3.2 实现库存 reservation 的预占、确认、释放与过期状态机，并覆盖并发条件更新测试。
- [-] 3.3 新增秒杀活动模型，将 Redis Lua 结果映射为 reservation，而非订单最终成功。
  > 备注: `seckill_activities` 表已建模，新入口使用 MySQL reservation；旧 Redis Lua 适配留待兼容链路迁移。
- [√] 3.4 增加商品与库存运营 API，并实施 admin 角色校验。

## 4. Cart 与结算
- [√] 4.1 新增 Cart 领域模型、Repository 和 service，实现添加、修改、删除、列表与数量校验。
- [√] 4.2 实现结算预览，批量读取 SKU 并返回实时价格、可售状态和总金额分值。
- [√] 4.3 增加 API Gateway `/api/v1/cart/items` 路由及认证上下文传播。

## 5. 统一订单状态机
- [√] 5.1 引入整数金额、订单项和地址快照模型，提供旧订单数据兼容迁移。
  > 备注: 通过新旧表隔离兼容，未对旧浮点订单执行破坏性原地转换。
- [√] 5.2 实现订单状态机与状态历史，覆盖非法转换、重复转换和所有终态测试。
- [√] 5.3 实现带 `Idempotency-Key` 的普通订单创建、库存预占和失败释放，验证 why.md#需求-普通订单闭环-场景-用户从购物车结算。
- [√] 5.4 实现订单列表、详情、待支付取消和超时关闭任务。

## 6. Mock 支付、履约与退款
- [√] 6.1 实现 Mock 支付单创建、签名回调和幂等支付成功处理。
- [√] 6.2 实现支付成功后的库存确认与订单已支付事件，失败时保留可补偿状态。
- [√] 6.3 实现 admin 发货、用户确认收货和 shipment 轨迹，验证完整履约链路。
- [√] 6.4 实现取消和 Mock 退款状态机，验证 why.md#需求-取消与退款-场景-用户取消或申请退款。

## 7. 消息一致性与秒杀迁移
- [√] 7.1 为 Outbox 增加事件版本，为消费者增加 Inbox 幂等记录和唯一约束。
- [-] 7.2 移除 MQ Consumer 跨订单与商品表写事务，改由各领域消费者处理自有数据。
  > 备注: 旧 Consumer 保留在兼容域且不写新商城表；Commerce Outbox 消费者尚未接入。
- [-] 7.3 将现有秒杀创建接入统一订单、支付和库存 reservation，验证 why.md#需求-秒杀统一交易-场景-秒杀资格通过后创建订单。
  > 备注: 已新增统一 `/api/v1/seckill/orders`，但旧 Redis/MQ 秒杀入口尚未切换到新交易域。
- [-] 7.4 验证发布失败、重复消息、DLQ、支付超时和库存补偿故障场景。
  > 备注: 支付超时与业务幂等已验证；RabbitMQ/Redis/MySQL 故障注入依赖本轮不可用的真实集成环境。

## 8. 部署、安全和文档
- [√] 8.1 为业务进程增加 Dockerfile、健康检查、优雅停机和完整 Compose 编排。
- [√] 8.2 增加 JWT Secret、Mock 回调签名、生产 Debug 路由和敏感日志安全检查。
- [√] 8.3 更新 README、OpenAPI、Proto 生成说明和 `helloagents/wiki/`。

## 9. 验收
- [√] 9.1 增加后端端到端验收，覆盖注册登录、地址、商品、购物车、下单、支付、发货和收货。
- [√] 9.2 增加取消、退款、重复请求、越权、库存不足和超时释放验收。
- [-] 9.3 执行 `gofmt`、`go test ./...`、`go vet ./...`、migration 重放和 Compose 健康检查。
  > 备注: 格式化、全量测试、race、vet、Compose 配置和 OpenAPI 解析通过；真实 migration 重放与 Compose 健康检查经用户确认因 Docker 环境不可用跳过。
- [√] 9.4 更新任务状态、CHANGELOG 和知识库，将方案包迁移至 `helloagents/history/2026-08/`。

## 执行总结
- 完成: 28 项。
- 跳过: 6 项，均在对应任务下记录原因与后续边界。
- 失败: 0 项。
- 前端: 按用户要求未实施。
