# 变更提案: 微服务商城与秒杀统一架构迁移

## 需求背景

当前仓库同时存在两条业务链路：旧秒杀域使用 gRPC、Redis Lua、RabbitMQ 和独立 Worker；新商城域由 `commerce-api` 以模块化单体方式实现。两条链路拥有不同的订单表、库存模型和消息边界，导致新商城无法展示微服务间调用、异步消息和最终一致性，也增加了后续维护成本。

本次采用渐进式拆分方案，在保留可回滚能力的前提下，将新商城逐步迁移到统一的微服务领域模型，最终让商城普通订单和秒杀订单共用 Order Service、库存服务、支付服务和履约服务的边界。

## 产品分析

### 目标用户与场景

- **用户群体:** 需要展示 Go 微服务、gRPC、RabbitMQ、Redis 高并发库存和完整商城交易闭环的开发者、面试官或项目评审者。
- **使用场景:** 通过 Gateway 调用商城 API，观察普通订单、秒杀、支付、发货和异常补偿的同步与异步链路。
- **核心痛点:** 当前新商城功能完整但以单体实现，旧微服务链路与新商城割裂，无法用一条业务链路体现完整架构能力。

### 价值主张与成功指标

- **价值主张:** 以统一订单和库存模型承载商城与秒杀，并清晰展示 gRPC 同步调用、RabbitMQ 异步事件、Outbox/Inbox、Redis Lua 和最终一致性。
- **成功指标:** 新商城 `/api/v1` 不再直接访问总 Repository；订单只由一个 Order Service 负责；秒杀请求经过 Redis Lua 与 RabbitMQ；核心服务具备独立进程、独立数据所有权和可验证的幂等/补偿测试。

### 人文关怀

认证、地址和支付数据继续按最小暴露原则处理；服务拆分不得复制明文密码、Refresh Token、支付签名或地址快照以外的个人信息；Mock 支付仍只用于开发和验收。

## 变更内容

1. 建立 Identity、Catalog、Inventory/Seckill、Cart、Order、Payment、Fulfillment 七个业务服务边界。
2. 为服务间同步调用增加版本化 gRPC 契约，为异步协作增加版本化 RabbitMQ 事件契约。
3. 按服务拆分数据库所有权，开发环境允许共用 MySQL 实例但禁止跨服务写表。
4. 将 `commerce-api` 逐步降级为过渡适配层，最终移除其业务 Repository 和跨域事务。
5. 迁移普通订单和秒杀订单到同一 Order Service 状态机，并接入库存预占、支付、履约和退款事件。
6. 对每个服务建立 Outbox Publisher、Inbox 幂等边界、重试和死信处理。

## 影响范围

- **模块:** Gateway、Identity、Catalog、Inventory/Seckill、Cart、Order、Payment、Fulfillment、Messaging、Platform。
- **文件:** `cmd/commerce-api`、`internal/commerce`、`product_service`、`order_service`、`api_gateway`、`proto`、`common/pb`、数据库 migration、Compose 和知识库。
- **API:** `/api/v1` 保持外部契约稳定，内部新增版本化 gRPC 服务和 RabbitMQ 事件。
- **数据:** `commerce_*` 表按服务迁移至各自所有权边界，旧 `orders`、`product`、`outbox_events` 仅在兼容阶段保留。

## 核心场景

### 需求: 普通商城订单微服务闭环
**模块:** Order、Catalog、Inventory、Payment、Fulfillment

#### 场景: 用户创建并完成普通订单

- Gateway 将请求路由到 Order Service。
- Order Service 通过 gRPC 获取商品快照并调用 Inventory Service 预占库存。
- Order Service 本地事务写订单、订单项、状态历史和 `order.created.v1` Outbox 事件。
- 支付成功由 Payment Service 发布 `payment.succeeded.v1`，Order Service 通过 Inbox 幂等更新订单状态。
- Fulfillment Service 消费已支付订单事件并发布发货/收货事件，Order Service 完成最终状态转换。

### 需求: 秒杀异步下单
**模块:** Inventory/Seckill、Order、Messaging

#### 场景: 高并发用户参与秒杀

- Inventory/Seckill Service 使用 Redis Lua 校验活动时间、库存和用户限购。
- 通过 RabbitMQ 发布版本化秒杀准入事件，Order Service 异步创建统一商城订单。
- 订单创建失败或超时由补偿事件释放 Redis/数据库库存，重复消息由业务唯一键和 Inbox 拒绝重复处理。

### 需求: 服务故障与重复消息
**模块:** Messaging、Inventory、Order、Payment

#### 场景: 发布失败、消费重复或消费者不可用

- Outbox Publisher 按退避策略重试，RabbitMQ 使用 publisher confirm、持久化消息和 DLQ。
- 每个消费者按 `(consumer, event_id)` 记录 Inbox，重复消息返回已处理结果。
- 达到重试上限后进入 DLQ，并由补偿消费者执行库存释放或订单失败收敛。

## 风险评估

- **风险:** 一次性迁移会破坏当前已通过的商城 MVP 和旧秒杀链路。
- **缓解:** 按服务逐阶段迁移，先建立契约和数据所有权；每个阶段保留内存 Fake、契约测试和旧链路回归，验收通过后再切换 Gateway。
- **风险:** Redis、MySQL 和 RabbitMQ 之间无法使用分布式事务。
- **缓解:** 使用本地事务 + Outbox/Inbox、明确 Saga 补偿和幂等键，不允许跨服务直接写数据库。
- **风险:** 当前环境可能无法运行 Docker。
- **缓解:** 优先完成纯 Go 单元测试、进程级 Fake E2E 和 SQL/Compose 静态校验；真实中间件故障注入标记为待环境可用后验收。
