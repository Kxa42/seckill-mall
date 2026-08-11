# RabbitMQ 消息投递全景图（投放表）

本文基于当前代码实现，用图表完整展示消息队列的**投递关系**：谁发布事件、事件如何经交换机
路由、投递给哪些消费者的哪些队列，以及失败后的重试/死信去向。代码是唯一客观事实，
本文与代码冲突时以代码为准并更新本文。

## 1. 总览：发布 → 交换机 → 队列 → 消费者

```mermaid
flowchart LR
    subgraph Producers["发布方（Outbox 侧）"]
        INV["inventory-service<br/>Redis Stream Outbox → MQ 桥"]
        ORD["order-service<br/>SQL Outbox"]
        PAY["payment-service<br/>SQL Outbox"]
        FUL["fulfillment-service<br/>SQL Outbox"]
    end

    EX["commerce.events.v1<br/>topic 交换机<br/>routing key = 事件类型"]

    subgraph Queues["消费者主队列（每消费者独立）"]
        QO["commerce.order-service.events"]
        QP["commerce.payment-service.events"]
        QI["commerce.inventory-service.events"]
        QF["commerce.fulfillment-service.events"]
    end

    subgraph Consumers["消费者服务（Inbox 幂等）"]
        CO["order-service"]
        CP["payment-service"]
        CI["inventory-service"]
        CF["fulfillment-service"]
    end

    INV -->|"库存 4 事件"| EX
    ORD -->|"订单 2 事件"| EX
    PAY -->|"支付 2 事件"| EX
    FUL -->|"履约 2 事件"| EX

    EX -->|"绑定 7 类事件"| QO
    EX -->|"绑定 2 类事件"| QP
    EX -->|"绑定 2 类事件"| QI
    EX -->|"绑定 1 类事件"| QF

    QO --> CO
    QP --> CP
    QI --> CI
    QF --> CF
```

**核心结论：** 全部 10 类事件统一发布到 `commerce.events.v1`（topic 交换机），
routing key 就是事件类型本身；每个消费者拥有**独立主队列**，按订阅表绑定事件，
实现一对多的广播投递（fan-out）。

## 2. 事件级投递明细图（Fan-out）

```mermaid
flowchart LR
    subgraph P1["order-service 发布"]
        O1["order.created.v1"]
        O2["order.cancelled.v1"]
    end
    subgraph P2["payment-service 发布"]
        A1["payment.succeeded.v1"]
        A2["payment.refunded.v1"]
    end
    subgraph P3["inventory-service 发布"]
        B1["seckill.accepted.v1"]
        B2["inventory.reserved.v1"]
        B3["inventory.released.v1"]
        B4["inventory.restocked.v1"]
    end
    subgraph P4["fulfillment-service 发布"]
        C1["shipment.created.v1"]
        C2["shipment.delivered.v1"]
    end

    QO["order-service 队列"]
    QP["payment-service 队列"]
    QI["inventory-service 队列"]
    QF["fulfillment-service 队列"]
    X["⚠️ 无绑定队列"]

    O1 --> QP
    O2 --> QP
    O2 --> QI
    A1 --> QO
    A1 --> QF
    A2 --> QO
    A2 --> QI
    B1 --> QO
    B2 --> QO
    B3 --> QO
    C1 --> QO
    C2 --> QO
    B4 -. "无人订阅" .-> X
```

## 3. 投放表（事件 → 发布者 → 消费者）

| 事件类型 | 发布者 | 投递队列 | 消费者 | 处理器行为 |
|---------|--------|---------|--------|-----------|
| `seckill.accepted.v1` | inventory-service | order.events | order-service | no-op（仅 Ack 幂等收敛） |
| `order.created.v1` | order-service | payment.events | payment-service | 创建支付单 |
| `order.cancelled.v1` | order-service | payment.events / inventory.events | payment、inventory | 取消支付单 / 回补库存 |
| `payment.succeeded.v1` | payment-service | order.events / fulfillment.events | order、fulfillment | 确认订单支付 / 创建发货单 |
| `payment.refunded.v1` | payment-service | order.events / inventory.events | order、inventory | 记录退款 / 回补库存 |
| `inventory.reserved.v1` | inventory-service | order.events | order-service | no-op（仅 Ack 幂等收敛） |
| `inventory.released.v1` | inventory-service | order.events | order-service | no-op（仅 Ack 幂等收敛） |
| `inventory.restocked.v1` | inventory-service | （无绑定） | — | ⚠️ 发布但无人订阅 |
| `shipment.created.v1` | fulfillment-service | order.events | order-service | 记录已发货 |
| `shipment.delivered.v1` | fulfillment-service | order.events | order-service | 记录已送达 |

## 4. 消费者订阅矩阵（队列三件套）

每个消费者有主队列 / retry 队列 / DLQ 队列三件套，主队列绑定 topic 交换机，
retry 队列绑定 `commerce.events.retry.v1`（direct，routing key = 消费者名），
DLQ 队列绑定 `commerce.events.dlx.v1`（direct，routing key = 消费者名）。

| 消费者 | 主队列 | 重试队列 | DLQ 队列 | 订阅事件 |
|--------|--------|---------|---------|---------|
| order-service | `commerce.order-service.events` | `.retry` | `.dlq` | seckill.accepted、payment.succeeded、payment.refunded、inventory.reserved、inventory.released、shipment.created、shipment.delivered |
| payment-service | `commerce.payment-service.events` | `.retry` | `.dlq` | order.created、order.cancelled |
| inventory-service | `commerce.inventory-service.events` | `.retry` | `.dlq` | order.cancelled、payment.refunded |
| fulfillment-service | `commerce.fulfillment-service.events` | `.retry` | `.dlq` | payment.succeeded |

## 5. 发送侧：Outbox 投递流程

```mermaid
flowchart LR
    TX["业务事务（同一数据库）<br/>1. 业务状态变更<br/>2. INSERT outbox 事件"] -->|"事务提交"| REL["Relayer 后台扫描<br/>每秒 ≤100 条 · claim 租约 30s"]
    REL --> PUB["发布到 commerce.events.v1<br/>Publisher Confirm + mandatory 回执"]
    PUB -->|"✅ Broker 确认"| PD["status = published"]
    PUB -->|"❌ 发布失败"| PR{"attempts < 5 ?"}
    PR -->|"是"| PPR["指数退避后重试<br/>1s → 2s → 4s → … ≤5min"]
    PR -->|"否"| PF["status = failed<br/>人工排查"]
    PPR --> PUB
```

- Order / Payment / Fulfillment：SQL Outbox，与业务状态变更同事务写入。
- Inventory：Redis Lua 在库存/秒杀状态变更的同一原子脚本中 `XADD` Stream Outbox，
  再由 `inventory-publisher` 消费组把 Stream 转发到 RabbitMQ（无 Redis 时用 MemoryEventStream）。

## 6. 消费侧：幂等、重试与死信流程

```mermaid
flowchart TD
    MAIN["主队列 commerce.{consumer}.events"] --> FILTER["消息过滤<br/>未知类型 / 版本>1 / 未注册 → Ack 丢弃"]
    FILTER --> CLAIM["Inbox 幂等抢占<br/>30s 租约 · (consumer, event_id)"]
    CLAIM -->|"已处理过"| DONE1["✅ Ack 跳过"]
    CLAIM -->|"租约被其他副本持有"| RETRY0["延迟重投 → retry 队列<br/>（永不 DLQ）"]
    CLAIM -->|"抢到租约"| HANDLER["执行业务 Handler"]
    HANDLER -->|"✅ 成功"| DONE2["MarkProcessed + Ack"]
    HANDLER -->|"❌ 失败"| JUDGE{"attempts < 5 ?"}
    JUDGE -->|"是"| RETRY["commerce.events.retry.v1 → retry 队列<br/>TTL 指数退避 2s → 5min"]
    RETRY -->|"TTL 过期转回主队列"| MAIN
    JUDGE -->|"否"| DLQ["Nack 不重投 → DLQ<br/>commerce.{consumer}.dlq"]
```

- 主队列死信策略：`x-dead-letter-exchange = commerce.events.dlx.v1`、routing key = 消费者名。
- retry 队列死信策略：`x-dead-letter-exchange = ""`（默认交换机）、routing key = 主队列名，
  即消息 TTL 过期后自动转回主队列再次消费。

## 7. 关键参数速查

| 参数 | 值 |
|------|-----|
| 事件交换机 | `commerce.events.v1`（topic） |
| 重试交换机 | `commerce.events.retry.v1`（direct） |
| 死信交换机 | `commerce.events.dlx.v1`（direct） |
| 消费重试上限 | 5 次（超过进 DLQ） |
| 消费重试 TTL | 指数退避 2s → 5min（`RetryDelay(attempt+1, 1s)`） |
| 发布重试上限 | 5 次（Outbox attempts） |
| 发布退避 | 指数 1s 基准，上限 5min |
| Outbox claim 租约 | 30s |
| Inbox 幂等租约 | 30s |
| Relayer 批量 | 100 条 / 秒 |
| QoS 预取 | 16 |
| 投递语义 | at-least-once + Inbox 幂等收敛 |
| 事件版本 | v1（EventVersion > 1 安全 Ack 丢弃） |

## 8. 已知边界（如实标注）

- `inventory.restocked.v1` 已发布但**无任何消费者订阅**，属于"记录型"事件。
- order-service 对 `seckill.accepted.v1`、`inventory.reserved.v1`、`inventory.released.v1`
  的 Handler 为 no-op（返回 nil 直接 Ack），不产生业务副作用。
- 不保证跨事件类型的全局顺序，正确性依赖状态机校验与幂等收敛。

## 9. 代码索引

| 内容 | 位置 |
|------|------|
| 事件类型与信封 | `shared/contracts/events.go` |
| 服务边界（发布/消费声明） | `shared/contracts/boundaries.go` |
| 拓扑声明、订阅路由表 `consumerEvents`、队列命名 | `shared/platform/messaging/rabbit.go` |
| 重试退避计算 | `shared/platform/messaging/types.go:135` |
| SQL Outbox / Inbox | `shared/platform/messaging/sql_store.go` |
| Outbox Relayer 轮询 | `shared/platform/messaging/worker.go` |
| 幂等消费公共流程 | `shared/platform/messaging/dispatch.go` |
| Outbox/Inbox 表结构 | `migrations/004_stage5_messaging.sql` |
| Inventory Redis Stream → RabbitMQ 桥 | `services/inventory/cmd/inventory-service/main.go` |
