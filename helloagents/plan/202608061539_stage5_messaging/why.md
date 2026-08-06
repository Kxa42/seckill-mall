# 变更提案: 阶段5 Outbox/Inbox 与 RabbitMQ 可靠事件闭环

## 需求背景

阶段4已经完成 Identity、Cart、Payment、Fulfillment 的独立进程和 Gateway 切流，但新商城跨服务协作仍主要依赖同步 gRPC。仓库中现有 `outbox_worker`、`mq_consumer` 和 `dlq_consumer` 只服务旧秒杀链路，读取旧 `orders`/`outbox_events` 表，也没有使用 `common/contracts.EventEnvelope`。

阶段5需要建立目标微服务自己的异步消息边界，使本地业务状态与事件记录在同一事务内提交，RabbitMQ 负责可靠传输，各消费者通过 Inbox 保证重复投递不会重复执行。旧秒杀消息链路在本阶段继续保留并隔离，避免把已验收的兼容能力与新商城事件迁移混为一体。

## 产品分析

### 目标用户与场景

- **用户群体:** 需要验证 Go 微服务、RabbitMQ、最终一致性和故障补偿能力的开发者、面试官或项目评审者。
- **使用场景:** 用户创建秒杀/普通订单、支付、取消、发货和退款时，系统在服务重启、消息重复或 RabbitMQ 短暂不可用后仍能恢复到一致状态。
- **核心痛点:** 当前同步调用存在“本地状态已提交但下游调用失败”的恢复窗口，新商城没有可重放的事件记录和统一的消费幂等边界。

### 价值主张与成功指标

- **价值主张:** 用服务级 Outbox/Inbox、Publisher Confirm、手动 Ack 和 DLQ 将阶段4的同步领域服务扩展为可观测、可恢复的异步协作系统。
- **成功指标:** 同一个 `event_id` 对同一个消费者只产生一次业务效果；发布失败可有限重试；消费者进程重启后能继续处理；毒丸消息进入对应 DLQ；恢复后可重放且不会重复扣减库存、重复支付或重复发货。

### 人文关怀

事件只携带跨服务完成动作所需的业务 ID、金额和状态，不携带密码、Token、支付签名或不必要的地址明文。日志和监控使用事件 ID、聚合 ID 与结果标签，不输出支付凭据和认证材料。

## 变更内容

1. 在 `common/messaging` 建立 RabbitMQ Topic Exchange、可靠发布、重试、手动 Ack、DLQ 和链路传播运行时；所有消息正文统一使用 `common/contracts.EventEnvelope`。
2. 为 Order、Payment、Fulfillment 增加服务自有 SQL Outbox/Inbox 表；Inventory/Seckill 使用与 Redis Lua 状态变更同一原子脚本写入 Redis Outbox，并为消费幂等保留唯一键。
3. 将秒杀准入、订单创建/取消、库存释放、支付成功/退款、发货/收货事件接入目标服务消费者；同步 gRPC 仍作为阶段5的权威命令入口，事件用于通知和失败恢复，阶段6再做旧链路收口。
4. 增加事件发布重试、Publisher Confirm、mandatory 路由检查、消费失败重试、毒丸消息 DLQ 和业务补偿；不修改旧 `orders`、旧 `outbox_events` 的兼容链路。
5. 增加 Fake Broker、Memory/Redis 测试、RabbitMQ 集成验收入口和知识库文档，记录 Docker 不可用时的跳过项。

## 影响范围

- **模块:** `common/contracts`、`common/messaging`、Order、Payment、Inventory/Seckill、Fulfillment、Messaging、配置、migration、Compose、测试和知识库。
- **文件:** 预计新增消息运行时/适配器/测试文件，修改四个领域服务仓储与启动装配、`migrations/004_stage5_messaging.sql`、`docker-compose.yaml`、配置示例和文档。
- **API:** 外部 `/api/v1` 和现有 gRPC 方法保持兼容；新增的是内部 RabbitMQ 事件路由和消费者能力，不暴露公网 HTTP 接口。
- **数据:** 新增 `order_outbox_events`、`payment_outbox_events`、`fulfillment_outbox_events` 及各服务 Inbox 表；Inventory 的事件日志使用 Redis Stream/Hash 状态，不跨写其他服务表。

## 核心场景

### 需求: 服务级 Outbox 发布
**模块:** Messaging、Order、Payment、Inventory、Fulfillment

#### 场景: 本地事务提交后发布事件

- 业务数据和本服务 Outbox 事件在同一个本地事务中提交。
- Publisher 使用持久化消息、Publisher Confirm 和 mandatory 路由检查；确认成功后才标记 Outbox 已发布。
- 发布进程崩溃或确认丢失时允许同一事件再次投递，消费者依靠 Inbox 幂等收敛。

### 需求: 事件消费幂等
**模块:** Order、Payment、Inventory、Fulfillment

#### 场景: 重复消息和服务重启

- 消费者以 `(consumer, event_id)` 作为唯一幂等键，重复消息不再次调用业务命令。
- 业务处理成功并完成 Inbox 记录后才 Ack；进程在处理期间退出时，消息可重新投递。
- 已知事件的未来版本、未知事件类型和非法 payload 不得造成无限重试，按策略进入隔离或 DLQ。

### 需求: 交易事件恢复
**模块:** Order、Payment、Inventory、Fulfillment

#### 场景: 支付、取消、发货或退款的下游调用失败

- Payment 成功、Order 取消、Inventory 释放、Fulfillment 发货/收货和退款事件均可重放。
- 消费者调用现有幂等 gRPC 命令并设置 deadline；失败消息进入有限重试队列，超过上限进入对应 DLQ。
- 取消/退款消息最终触发库存 Release/Restock，支付成功最终触发订单支付确认，履约事件最终收敛订单状态。

## 风险评估

- **风险:** RabbitMQ 与 MySQL/Redis 之间不存在分布式事务。
  **缓解:** MySQL 业务状态与 SQL Outbox 同事务；Inventory 在 Redis Lua 内同时写状态和 Stream；发布确认失败只重投，不回滚已提交业务状态。
- **风险:** 阶段4已有同步调用，新增消费者可能导致重复确认、释放或状态转换。
  **缓解:** Inbox 唯一键、业务命令幂等和状态前置检查共同防重；阶段5不删除同步权威路径。
- **风险:** 旧 Worker 与新 Topic 误消费或误写表。
  **缓解:** 新旧交换机、队列、表名和 Compose 服务明确隔离；新消费者禁止访问旧 `orders`/`product` 表。
- **风险:** Docker daemon 当前不可用，无法完成真实 RabbitMQ 故障注入。
  **缓解:** 提供 Fake Broker 和静态 Compose/migration 校验，真实 RabbitMQ 验收设置为环境恢复后的可重复入口。
