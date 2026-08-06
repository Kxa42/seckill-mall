# 任务清单: 统一商城架构与阶段5可靠消息闭环

目录: `helloagents/history/2026-08/202608061551_unified_stage5_messaging/`

> 目标: 删除旧秒杀运行时和旧 HTTP 入口，统一到新商城微服务、统一事件协议和服务级 Outbox/Inbox。

## 1. 统一契约与消息运行时

- [√] 1.1 在 `common/contracts/events.go`、`common/contracts/boundaries.go`、`common/contracts/events_test.go` 中补齐 `inventory.restocked.v1`、payload 校验、发布/消费矩阵和服务所有权校验。
- [√] 1.2 在 `common/messaging` 新增 Outbox、Inbox、Broker、Publisher、Consumer 接口及 Fake Broker，禁止依赖旧订单/商品模型。
- [√] 1.3 在 `common/messaging` 新增 Topic Exchange、主队列、retry queue、DLQ、Publisher Confirm、mandatory return、手动 Ack 和断线重连。
- [√] 1.4 增加消息运行时单元测试，覆盖确认失败、不可路由、超时、重连、重复消息、未知事件和未来版本隔离。

## 2. 新商城 Outbox/Inbox 存储

- [√] 2.1 新增 `migrations/004_stage5_messaging.sql`，创建 Order、Payment、Fulfillment 独立 Outbox/Inbox 表；不创建或修改旧 `orders`、`product`、`outbox_events` 运行时表。
- [√] 2.2 在 `common/messaging` 新增 SQL Outbox/Inbox 适配器，支持批量 claim、唯一 event_id、消费者 lease、状态更新和错误截断。
- [√] 2.3 为 `internal/order` MySQL/Memory Repository 增加事件记录器，在创建、取消和超时收口写入统一事件。
- [√] 2.4 为 `payment_service` MySQL/Memory Repository 增加支付成功/退款事件，重复回调只产生一个事件。
- [√] 2.5 为 `fulfillment_service` MySQL/Memory Repository 增加发货/收货事件，重复操作不重复发布。
- [√] 2.6 为 `inventory_service` Redis Lua、MemoryStore 和事件适配器增加原子 Stream Outbox，覆盖准入、预占、释放和 Restock。
- [√] 2.7 增加 SQL/Redis 存储测试，验证事务回滚无事件、提交只写一次、Inbox 并发唯一和 Stream pending 重放。

## 3. 新服务事件消费者

- [√] 3.1 在 `internal/order` 新增 Handler 和 Inbox，消费支付、库存和履约事件，调用现有幂等状态机完成恢复。
- [√] 3.2 在 `payment_service` 新增订单创建/取消 Handler 和 Inbox，不绕过 Payment Repository，不直接写 Order 表。
- [√] 3.3 在 `inventory_service` 新增取消/退款 Handler 和 Inbox，只调用本地 Store 的 Release/Restock。
- [√] 3.4 在 `fulfillment_service` 新增支付成功 Handler 和 Inbox，只维护待发货事实，不伪造承运商和运单号。
- [√] 3.5 为四类消费者增加路由、重复消息、服务重启、未知事件、未来版本和 DLQ 测试。

## 4. 服务启动与统一事件链路

- [√] 4.1 在 `cmd/order-service/main.go`、`cmd/payment-service/main.go` 和 `cmd/fulfillment-service/main.go` 装配本服务 Publisher/Consumer，并实现优雅停机。
- [√] 4.2 在 `cmd/inventory-service/main.go` 装配 Redis Stream 出站 Publisher 和 RabbitMQ 入站 Consumer，并实现 pending claim、重试和优雅停机。
- [√] 4.3 在四个目标服务中增加 MQ 未配置时的安全降级：保留 Memory/Fake 测试能力，不连接或写入旧 MQ/旧表。
- [√] 4.4 增加统一事件链 Fake E2E，覆盖秒杀准入、订单创建、支付成功、取消释放、发货收货和退款恢复。

## 5. Gateway 与旧链路移除

- [√] 5.1 在 `api_gateway/routes.go`、`api_gateway/clients.go` 和相关测试中移除旧 `/order`、`/product` 路由及旧 Product/Order gRPC 客户端。
- [√] 5.2 删除 `api_gateway/commerce_proxy.go` 的 Commerce NoRoute 代理和 `CommerceURL` 运行时依赖，确认未匹配路径直接返回 404。
- [√] 5.3 删除或移出构建的 `cmd/commerce-api`、`internal/commerce/httpapi`、`internal/commerce` 和 `internal/platform` 旧商城单体依赖；保留仍被目标服务使用的通用平台代码。
- [√] 5.4 删除旧 `order_service`、`product_service`、`mq_consumer`、`outbox_worker`、`dlq_consumer` 代码、旧配置和无引用的旧 protobuf；先用 `rg` 验证无目标服务引用。
- [√] 5.5 更新 Gateway 路由测试，断言 `/api/v1/*` 正常且旧 `/order*`、`/product/*` 返回 404、无库存/订单副作用。

## 6. Compose、配置和数据边界收口

- [√] 6.1 更新 `common/config/conf.go`、`common/config/services.go`、服务配置和 `config/commerce-services.example.yaml`，移除 Commerce/Legacy 配置，加入统一 MQ 开关与重试配置。
- [√] 6.2 更新 `docker-compose.yaml`、`Dockerfile`、`deploy/rabbitmq/enabled_plugins` 和 Prometheus，移除旧服务容器，声明新服务消息运行时和队列拓扑。
- [√] 6.3 更新 `deploy/mysql/init.sql` 和 migration 流程，使全新环境不再创建旧 Product/Order/MQ 依赖；已有旧表不执行 DROP/TRUNCATE。
- [√] 6.4 增加 Compose 静态解析和旧组件扫描测试，禁止旧容器、旧入口和旧交换机重新进入部署文件。

## 7. 文档和安全审计

- [√] 7.1 更新 `README.md`、`api/openapi.yaml`、`helloagents/wiki/arch.md` 和 `helloagents/wiki/data.md`，删除旧入口/旧运行时说明并补充统一事件拓扑。
- [√] 7.2 更新 `helloagents/wiki/modules/messaging.md`、`order.md`、`inventory.md`、`payment.md`、`fulfillment.md`、`project.md` 和 `CHANGELOG.md`。
- [√] 7.3 执行安全检查，确认无旧服务跨表写入、无敏感事件 payload、无明文凭据、所有 gRPC/MQ 处理带 deadline 和有限重试。

## 8. 阶段验收与方案迁移

- [√] 8.1 运行 `gofmt -w`、`go test ./...`、`go vet ./...` 和目标范围 `go test -race`。
- [√] 8.2 运行 `tests/stage5_memory_e2e.sh`，验证统一新服务链路和重复消息重放。
- [-] 8.3 Docker 可用时运行 `SECKILL_RABBITMQ_INTEGRATION=1 tests/stage5_rabbitmq_e2e.sh`；不可用时记录 RabbitMQ、Compose 和真实 migration 的跳过项。
  > 备注: Docker CLI 可用但 Docker daemon socket 无权限，RabbitMQ、Compose 实际渲染和真实 migration 联调跳过；脚本已提供可重复入口。
- [√] 8.4 完成一致性审计，确认代码只保留统一运行时、旧表仅历史残留，并将本方案包迁移至 `helloagents/history/2026-08/`，更新历史索引。

## 执行记录

- 目标服务仅保留统一 `/api/v1` 与 gRPC 运行时；旧 `/order`、`/product` 路径由 Gateway 返回 404。
- Inventory Redis Stream 使用 `inventory-publisher` group 桥接到统一 RabbitMQ，入站事件使用 Redis Inbox 租约和已处理标记。
- `GOTMPDIR=/tmp/seckill-go-build go test -race ./...`、内存 E2E、`go vet ./...` 和 `git diff --check` 通过；Docker 相关验收按 8.3 跳过。
