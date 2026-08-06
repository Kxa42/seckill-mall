# 任务清单: 阶段5 Outbox/Inbox 与 RabbitMQ 可靠事件闭环

目录: `helloagents/plan/202608061539_stage5_messaging/`

> 目标: 在不破坏阶段4同步链路和旧秒杀兼容链路的前提下，建立新商城服务级消息可靠性和可重放闭环。

## 1. 事件契约与消息运行时

- [ ] 1.1 在 `common/contracts/events.go`、`common/contracts/boundaries.go`、`common/contracts/events_test.go` 中补齐 `inventory.restocked.v1`、事件 payload 校验、发布/消费矩阵和服务所有权校验。
- [ ] 1.2 在 `common/messaging` 新增 Outbox、Inbox、Broker、Publisher、Consumer 接口及 Fake Broker；确保公共运行时不依赖领域服务数据库模型。
- [ ] 1.3 在 `common/messaging` 新增 RabbitMQ Topic 拓扑声明、持久化发布、Publisher Confirm、mandatory return 和 OpenTelemetry headers 传播。
- [ ] 1.4 在 `common/messaging` 新增 retry queue、DLQ、手动 Ack、Prefetch、有限退避和断线重连；对非法事件、未来版本和毒丸消息提供隔离结果。
- [ ] 1.5 为消息运行时补充单元测试，覆盖发布成功、Nack、unroutable、连接重建、上下文超时、重复投递和 Fake Broker 重放。

## 2. 服务级 Outbox/Inbox 存储

- [ ] 2.1 新增 `migrations/004_stage5_messaging.sql`，创建 Order、Payment、Fulfillment 的独立 Outbox/Inbox 表和事件/处理状态索引；不修改旧 `outbox_events` 与 `inbox_events`。
- [ ] 2.2 在 `common/messaging` 新增 SQL Outbox/Inbox 存储适配器，支持批量 claim、状态更新、唯一 event_id、消费者 lease 和错误摘要截断。
- [ ] 2.3 为 `internal/order` 的 MySQL/Memory Repository 增加可选事件记录器，在订单创建、取消和超时收口的本地边界写入统一信封。
- [ ] 2.4 为 `payment_service` 的 MySQL/Memory Repository 增加支付成功和退款事件记录，保证支付/退款本地状态与 Outbox 同事务提交。
- [ ] 2.5 为 `fulfillment_service` 的 MySQL/Memory Repository 增加发货和收货事件记录，保证物流单据与 Outbox 同事务提交。
- [ ] 2.6 在 `inventory_service` 的 Redis Lua、MemoryStore 和事件适配器中增加原子 Stream Outbox，覆盖准入、预占、释放和退款恢复；为 Redis Stream pending 消息提供 claim/重放能力。
- [ ] 2.7 增加存储层测试，验证事务回滚无 Outbox、提交只写一次、重复 event_id 冲突、Inbox 并发唯一约束和 Redis Lua 状态/事件原子性。

## 3. 领域事件接入

- [ ] 3.1 在 `internal/order/service.go` 和 `internal/order/mysql_repository.go` 接入 `order.created.v1`、`order.cancelled.v1`，保持旧订单表和旧消息链路完全隔离。
- [ ] 3.2 在 `payment_service/model.go` 和 `payment_service/server_test.go` 接入 `payment.succeeded.v1`、`payment.refunded.v1`，重复回调只产生一个事件。
- [ ] 3.3 在 `fulfillment_service/model.go` 和 `fulfillment_service/server_test.go` 接入 `shipment.created.v1`、`shipment.delivered.v1`，重复发货/收货不重复发布。
- [ ] 3.4 在 `inventory_service/redis_store.go`、`inventory_service/memory_store.go` 和 `inventory_service/server_test.go` 接入库存事件，确保重复 Reserve/Release/Restock 不重复增加库存或重复写事件。
- [ ] 3.5 为事件 payload 建立版本化结构和脱敏测试，禁止出现密码、Token、支付签名和非必要个人信息。

## 4. Order 消费者与订单恢复

- [ ] 4.1 在 `internal/order` 新增 Order 消费 Handler，消费支付成功/退款、库存预占/释放/恢复和履约发货/收货事件，所有动作调用现有幂等状态机。
- [ ] 4.2 在 `internal/order` 接入 Order Inbox 记录和处理 lease，重复事件直接 Ack，处理期间宕机后允许恢复，未知类型或不支持版本进入 Order DLQ。
- [ ] 4.3 在 `cmd/order-service/main.go` 增加可选消息 Publisher/Consumer 启动和优雅停止装配；未配置 MQ 时保持阶段4同步模式。
- [ ] 4.4 增加 Order Handler 单元测试和 Fake Broker E2E，覆盖支付成功、取消释放、退款恢复、重复消息和服务重启重放。

## 5. Payment、Inventory、Fulfillment 消费者

- [ ] 5.1 在 `payment_service` 新增订单创建/取消 Handler、Inbox 和可重放测试；不绕过 Payment Repository，不直接写 Order 表。
- [ ] 5.2 在 `inventory_service` 新增取消/退款 Handler、Inbox 和补偿测试；只调用本服务 Store 的 Release/Restock，不接受跨服务表写入。
- [ ] 5.3 在 `fulfillment_service` 新增支付成功 Handler、Inbox 和待发货恢复测试；不得自动伪造承运商或运单号。
- [ ] 5.4 在 `cmd/payment-service/main.go`、`cmd/inventory-service/main.go`、`cmd/fulfillment-service/main.go` 增加各自消息运行时装配和优雅关闭。
- [ ] 5.5 增加跨消费者路由测试，确认每条事件只进入声明的目标队列，重复消息不会跨消费者共享 Inbox 状态。

## 6. 重试、DLQ 与补偿运维

- [ ] 6.1 在 `common/messaging` 和各消费者中实现错误分类：可重试业务/基础设施错误、不可重试参数/版本错误和超过上限错误。
- [ ] 6.2 在 `common/messaging` 增加发布、消费、Inbox 重复、DLQ 和补偿 Prometheus 指标；禁止将 event_id 或 order_id 作为无限 cardinality label。
- [ ] 6.3 新增 DLQ 重放/补偿入口或可测试的重放函数，确保人工恢复只重新投递原始信封，不修改 event_id、不绕过 Inbox。
- [ ] 6.4 增加故障注入测试：RabbitMQ 不可达、Publisher Confirm Nack、消费者异常退出、数据库暂时失败、非法 JSON、未来事件版本和 DLQ 重放。

## 7. 配置、部署和文档

- [ ] 7.1 更新 `common/config/conf.go`、`common/config/services.go`、`config/commerce-services.example.yaml` 和各服务配置，增加 MQ 开关、重试上限、延迟、消费者名称并保持缺省可运行。
- [ ] 7.2 更新 `docker-compose.yaml`、`Dockerfile`、`deploy/rabbitmq/enabled_plugins` 和 Prometheus 配置，声明新 Topic、服务队列、retry/DLQ 和目标服务消息运行时。
- [ ] 7.3 更新 `helloagents/wiki/arch.md`、`helloagents/wiki/data.md`、`helloagents/wiki/modules/messaging.md`、`order.md`、`payment.md`、`inventory.md`、`fulfillment.md` 和 `helloagents/project.md`。
- [ ] 7.4 更新 `README.md`、`api/openapi.yaml`（如验收说明需要）和 `helloagents/CHANGELOG.md`，记录启动方式、故障注入、旧链路隔离和 Docker 跳过项。

## 8. 阶段验收与收口

- [ ] 8.1 运行 `gofmt -w`、`go test ./...`、`go vet ./...`、`go test -race ./common/... ./internal/... ./payment_service/... ./fulfillment_service/... ./inventory_service/...`。
- [ ] 8.2 运行 `tests/stage5_memory_e2e.sh`，验证 Fake Broker 下注册/下单/秒杀/支付/取消/发货/收货/退款完整事件链和重复消息。
- [ ] 8.3 运行 Compose 静态解析和 migration 分句测试；Docker 可用时执行 `SECKILL_RABBITMQ_INTEGRATION=1 tests/stage5_rabbitmq_e2e.sh`，否则记录明确跳过项。
- [ ] 8.4 完成安全检查和一致性审计，确认新消费者不访问旧订单/商品表、日志无敏感信息、所有跨服务调用带 deadline，并将方案包迁移至 `helloagents/history/2026-08/`。
