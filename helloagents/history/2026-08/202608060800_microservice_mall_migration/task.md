# 任务清单: 微服务商城与秒杀统一架构迁移

目录: `helloagents/history/2026-08/202608060800_microservice_mall_migration/`

## 1. 阶段1：契约与边界基线

- [√] 1.1 新增服务边界、事件类型和数据所有权的共享契约包，禁止依赖具体数据库实现。
- [√] 1.2 新增 Catalog、Inventory/Seckill、Order、Identity、Cart、Payment、Fulfillment 的版本化 gRPC proto；保留旧 proto 兼容。
- [√] 1.3 新增事件信封、事件版本和幂等键测试，覆盖未知事件类型、已知事件未来 `EventVersion` 和重复事件。
  > 备注: 初始阶段仅覆盖未知事件类型和重复事件；已在 `202608061041_contract_config_hardening` 中补充已知事件未来版本与版本独立演进测试。
- [√] 1.4 拆分配置约定，明确每个服务独立地址、DSN、RabbitMQ 和 etcd 配置。

## 2. 阶段2：Catalog 与 Inventory/Seckill 服务

- [√] 2.1 从 `internal/commerce` 提取 Catalog Repository 和服务端，建立商品快照 gRPC。
- [√] 2.2 改造现有 Product Service，明确 Catalog 与 Inventory 数据所有权，保留 Redis Lua 秒杀能力。
- [√] 2.3 实现统一库存 `Reserve/Confirm/Release` gRPC，并增加普通库存并发测试。
- [√] 2.4 将 `/api/v1/products` 和 `/api/v1/seckill/orders` 切换到目标服务。
  > 备注: 商品接口在阶段 2 切换；秒杀订单接口在阶段 3 统一 Order Service 编排后切换，旧 `/order` 兼容链路仍保留。
- [√] 2.5 阶段验收：商品查询、库存预占/释放、Redis Lua 限购、Gateway gRPC 调用和 Fake E2E。
  > 备注: 已完成 Memory/Fake/bufconn 验收；Docker 不可用导致真实 Redis/MySQL/Compose 集成留待后续环境验收。

## 3. 阶段3：唯一 Order Service

- [√] 3.1 将 `commerce_orders`、订单项和状态历史迁移为 Order Service 所有表。
- [√] 3.2 改造现有 Order Service，使普通订单和秒杀订单共用新的订单状态机。
- [√] 3.3 增加 Catalog、Identity、Inventory gRPC 客户端和失败补偿。
- [√] 3.4 将 `/api/v1/orders`、查询、取消和超时任务切换到 Order Service。
- [√] 3.5 阶段验收：幂等创建、库存失败回滚、状态转换、权限边界和旧订单表不再被新链路写入。
  > 备注: 已完成全仓测试、race、vet 和 Order Memory/Fake/bufconn 验收；真实基础设施集成因 Docker daemon 不可用跳过。

## 4. 阶段4：Identity、Cart、Payment、Fulfillment 服务

- [√] 4.1 提取 Identity Service，迁移用户、Token 和地址能力。
- [√] 4.2 提取 Cart Service，迁移购物车和结算预览能力。
- [√] 4.3 提取 Payment Service，迁移 Mock 支付、回调幂等和退款能力。
- [√] 4.4 提取 Fulfillment Service，迁移发货、收货和物流数据。
- [√] 4.5 阶段验收：注册到收货完整链路、支付回调重复、退款幂等和服务独立启动。
  > 备注: 已完成独立入口、配置和 Memory/Fake/bufconn 验收；Docker daemon 不可用，真实多进程与基础设施集成留待可用环境。

## 5. 阶段5：Outbox/Inbox 与 RabbitMQ

- [√] 5.1 为各服务增加本地 Outbox Publisher 和统一事件信封。
  > 备注: Order、Payment、Fulfillment 使用服务级 SQL Outbox；Inventory 使用 Redis Lua 原子 Stream Outbox，并由 `common/messaging` 统一发布。
- [√] 5.2 增加 Order、Payment、Inventory、Fulfillment 消费者和 Inbox 唯一约束。
  > 备注: Order、Payment、Fulfillment 使用独立 Inbox 表；Inventory 使用 `inventory:inbox:<consumer>:<event_id>` Redis 租约键和已处理标记。
- [√] 5.3 将秒杀准入、支付成功、取消、释放、发货和退款接入 RabbitMQ。
  > 备注: 统一使用 `commerce.events.v1` Topic；Inventory Redis Stream 出站事件由 `inventory-publisher` consumer group 桥接到 RabbitMQ。
- [√] 5.4 增加发布重试、publisher confirm、手动 Ack、DLQ 和补偿逻辑。
  > 备注: RabbitMQ Publisher 使用 confirm/mandatory，Consumer 使用手动 Ack、有限 retry/DLQ；领域消费者调用现有幂等状态机完成补偿和恢复。
- [√] 5.5 阶段验收：重复消息、服务宕机、发布失败、DLQ 和恢复后重放。
  > 备注: Memory/Fake、race、vet、Compose 静态解析和旧入口 404 验收通过；真实 RabbitMQ 故障注入、Compose 启动和 migration 因 Docker daemon 不可用跳过，入口为 `tests/stage5_rabbitmq_e2e.sh`。

## 6. 阶段6：Gateway、部署与收口

- [√] 6.1 Gateway 按 `/api/v1` 领域路由调用目标服务，不再代理 `commerce-api` 业务 Repository。
  > 备注: 已移除 Commerce NoRoute 代理和旧 Product/Order 客户端，未注册路径直接返回 404。
- [√] 6.2 逐步下线旧 `/order`、旧 `orders/product` 跨表 MQ Consumer 和重复表写入。
  > 备注: 旧 HTTP 入口、旧服务、旧 Worker 和旧 protobuf 已从构建/Compose 移除；旧数据库表仅保留历史数据，不再读写。
- [√] 6.3 更新 Docker Compose、migration、OpenAPI、README 和知识库架构图。
  > 备注: 已增加阶段5消息 migration、统一 RabbitMQ 拓扑、服务级 Outbox/Inbox 说明和启动/验收入口。
- [√] 6.4 完成全部自动化测试、静态检查和可用 Docker 环境下的真实集成验收。
  > 备注: 自动化测试、race、vet、格式和 Compose 静态检查通过；真实 Docker 集成按 6.5 记录跳过。
- [√] 6.5 记录 Docker 不可用时被跳过的项目和后续验收入口。
  > 备注: 当前已记录真实 migration、Redis/etcd/RabbitMQ、Compose 启动和真实 gRPC E2E 的跳过原因及替代验收入口。

## 7. 安全检查

- [√] 7.1 检查服务端授权、PII/Token/支付签名日志、配置密钥和事件 payload。
- [√] 7.2 检查跨服务调用 deadline、幂等键、重试上限、DLQ 毒丸消息和库存补偿。
  > 备注: 已完成同步 gRPC 与 RabbitMQ Consumer 的 deadline、幂等、有限重试、DLQ 和库存补偿检查；真实 broker 故障注入因 Docker 不可用跳过。

## 8. 文档更新

- [√] 8.1 更新 `helloagents/wiki/arch.md`、`data.md`、`api.md` 和各模块文档。
  > 备注: 已同步阶段 4 四个独立领域服务、Gateway 显式切流、数据所有权和阶段 5 消息边界。
- [√] 8.2 更新 `README.md`、`helloagents/project.md`、`helloagents/CHANGELOG.md` 和历史索引。
  > 备注: 已同步阶段 4 启动方式、验收入口、Go 临时目录和 Docker 跳过项。

## 9. 测试

- [√] 9.1 每个阶段运行 `go test ./...`、`go vet ./...`、必要范围的 `go test -race`。
  > 备注: 阶段 4 已通过全仓测试、vet、race、格式检查和差异检查；后续阶段继续复用该门禁。
- [√] 9.2 每个阶段运行 Fake/Memory E2E；Docker 可用时追加 MySQL、Redis、RabbitMQ 和 Compose 验收。
  > 备注: 阶段 4 已完成 Memory/Fake/bufconn E2E 和 Compose 静态解析；真实基础设施因 Docker daemon 不可用跳过。
