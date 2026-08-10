# Seckill Mall 架构示意图

本文档用 Mermaid 图直观展示系统的**组件功能**与**组件间关联**。代码是唯一客观事实，
当本文档与代码冲突时以代码为准并更新本文档。

## 1. 总体架构

```mermaid
flowchart TB
    subgraph Client["外部客户端"]
        Browser["浏览器 / App"]
    end

    subgraph Access["接入层"]
        Gateway["API Gateway :8080<br/>HTTP /api/v1/* · JWT 认证<br/>HMAC 内部调用签名 · etcd 服务发现"]
    end

    subgraph Biz["业务服务层（gRPC）"]
        Identity["Identity :51001<br/>注册/登录/刷新令牌<br/>地址管理 · bcrypt + JWT"]
        Catalog["Catalog :51002<br/>商品列表/详情<br/>SKU 快照"]
        Inventory["Inventory :51003<br/>库存预占/确认/释放/回补<br/>秒杀准入 · Redis Lua 原子操作"]
        Cart["Cart :51004<br/>购物车增删查/预览<br/>SKU 快照校验"]
        Order["Order :51005<br/>订单状态机<br/>创建/取消/支付/发货/退款<br/>SQL Outbox + Inbox"]
        Payment["Payment :51006<br/>支付单/回调/退款<br/>SQL Outbox + Inbox"]
        Fulfillment["Fulfillment :51007<br/>发货/确认收货/查询<br/>SQL Outbox + Inbox"]
    end

    subgraph Infra["基础设施"]
        MySQL[("MySQL 8.0<br/>各服务自有表 · 禁止跨库写")]
        Redis[("Redis 7.4<br/>库存状态 · Stream Outbox/Inbox")]
        MQ[("RabbitMQ<br/>commerce.events.v1<br/>retry + DLQ")]
        ETCD[("etcd<br/>服务注册与发现")]
    end

    subgraph Observability["可观测性"]
        Prom["Prometheus + Grafana"]
        Jaeger["Jaeger OTLP"]
    end

    subgraph Shared["共享库（单 go.mod）"]
        Proto["shared/proto + gen<br/>7 个 gRPC 契约"]
        Contracts["shared/contracts<br/>事件类型 · 数据所有权边界"]
        Platform["shared/platform<br/>配置 · 发现 · 认证 · MQ · 遥测"]
    end

    Browser -->|"HTTP :8080 /api/v1/*"| Gateway

    Gateway -->|"gRPC"| Identity
    Gateway -->|"gRPC"| Catalog
    Gateway -->|"gRPC 秒杀准入"| Inventory
    Gateway -->|"gRPC"| Cart
    Gateway -->|"gRPC"| Order
    Gateway -->|"gRPC"| Payment
    Gateway -->|"gRPC"| Fulfillment

    Order -->|"GetSKUSnapshot"| Catalog
    Order -->|"GetAddressSnapshot"| Identity
    Order -->|"Reserve/Confirm/Release/Restock"| Inventory
    Cart -->|"GetSKUSnapshot"| Catalog
    Payment -->|"ConfirmPayment/Refund"| Order
    Fulfillment -->|"Ship/ConfirmReceipt"| Order

    Identity -->|"自有表读写"| MySQL
    Catalog -->|"自有表读写"| MySQL
    Cart -->|"自有表读写"| MySQL
    Order -->|"订单 + outbox/inbox"| MySQL
    Payment -->|"支付 + outbox/inbox"| MySQL
    Fulfillment -->|"履约 + outbox/inbox"| MySQL
    Inventory -->|"Lua 原子状态 + Stream"| Redis

    Inventory -->|"发布事件"| MQ
    Order -->|"发布事件"| MQ
    Payment -->|"发布事件"| MQ
    Fulfillment -->|"发布事件"| MQ
    MQ -.->|"订阅事件"| Order
    MQ -.->|"订阅事件"| Payment
    MQ -.->|"订阅事件"| Fulfillment
    MQ -.->|"订阅事件"| Inventory

    Gateway -.->|"服务发现"| ETCD
```

> 说明：所有服务启动时向 etcd 注册并定时续约（Gateway 通过 etcd 解析下游地址）；
> 每个服务暴露 Prometheus metrics 和 Jaeger OTLP 遥测端点；`shared/*` 被所有服务引用。

## 2. 组件功能清单

| 组件 | 端口 | 职责 |
|------|------|------|
| API Gateway | 8080 | 唯一外部入口，`/api/v1/*` 路由、JWT 认证、内部调用 HMAC 签名注入、gRPC 客户端池 |
| Identity | 51001 | 注册/登录/刷新令牌（bcrypt 哈希 + JWT）、地址 CRUD 与地址快照 |
| Catalog | 51002 | 商品列表/详情、SKU 快照（供下单与购物车校验） |
| Inventory | 51003 | 库存预占/确认/释放/回补、秒杀准入；Redis Lua 原子操作 + Stream Outbox/Inbox |
| Cart | 51004 | 购物车项增删查、预览（调用 Catalog 校验 SKU） |
| Order | 51005 | 订单状态机唯一维护者（创建/取消/过期/支付/发货/收货/退款），SQL Outbox/Inbox |
| Payment | 51006 | 支付单创建/回调/退款，SQL Outbox/Inbox |
| Fulfillment | 51007 | 发货/确认收货/查询，SQL Outbox/Inbox |
| MySQL | 3306 | 各服务自有表存储，禁止跨库写 |
| Redis | 6379 | Inventory 库存状态与 Stream Outbox/Inbox（db 2） |
| RabbitMQ | 5672 | 事件总线 `commerce.events.v1`，每服务队列 + retry + DLQ |
| etcd | 2379 | 服务注册与发现 |
| Prometheus/Grafana | 9090/3000 | 指标采集与可视化 |
| Jaeger | 16686 | OTLP 分布式追踪 |

## 3. 组件间关联

### 3.1 同步调用（gRPC）

| 调用方 | 被调用方 | 接口 | 用途 |
|--------|----------|------|------|
| Gateway | Identity/Catalog/Inventory/Cart/Order/Payment/Fulfillment | 各服务 RPC | 转发 `/api/v1` 请求（注入 HMAC 身份） |
| Order | Catalog | `GetSKUSnapshot` | 下单时获取商品快照 |
| Order | Identity | `GetAddressSnapshot` | 下单时获取收货地址快照 |
| Order | Inventory | `Reserve`/`AdmitSeckill`/`Confirm`/`Release`/`Restock` | 库存生命周期管理 |
| Cart | Catalog | `GetSKUSnapshot` | 校验 SKU 并组装预览 |
| Payment | Order | `ConfirmPayment`/`Refund` | 支付结果回写订单状态 |
| Fulfillment | Order | `Ship`/`ConfirmReceipt` | 履约结果回写订单状态 |

### 3.2 异步事件（RabbitMQ `commerce.events.v1`）

```mermaid
flowchart LR
    MQ[("RabbitMQ<br/>commerce.events.v1")]

    Inv["Inventory"] -->|"seckill.accepted.v1<br/>inventory.reserved.v1<br/>inventory.released.v1<br/>inventory.restocked.v1"| MQ
    Ord["Order"] -->|"order.created.v1<br/>order.cancelled.v1"| MQ
    Pay["Payment"] -->|"payment.succeeded.v1<br/>payment.refunded.v1"| MQ
    Ful["Fulfillment"] -->|"shipment.created.v1<br/>shipment.delivered.v1"| MQ

    MQ -->|"payment.* / shipment.* / inventory.*"| Ord
    MQ -->|"order.created.v1 / order.cancelled.v1"| Pay
    MQ -->|"payment.succeeded.v1"| Ful
    MQ -->|"order.cancelled.v1 / payment.refunded.v1"| Inv
```

| 事件 | 发布者 | 消费者 | 用途 |
|------|--------|--------|------|
| `seckill.accepted.v1` | Inventory | Order(no-op) | 秒杀准入事实 |
| `inventory.reserved.v1` | Inventory | Order(no-op) | 预占成功事实 |
| `inventory.released.v1` | Inventory | Order(no-op) | 释放预占事实 |
| `inventory.restocked.v1` | Inventory | Order(no-op) | 退款回补事实 |
| `order.created.v1` | Order | Payment | 触发支付单创建 |
| `order.cancelled.v1` | Order | Payment、Inventory | 取消支付单、释放/回补库存 |
| `payment.succeeded.v1` | Payment | Order、Fulfillment | 确认订单支付、触发待发货 |
| `payment.refunded.v1` | Payment | Order、Inventory | 订单退款、库存回补 |
| `shipment.created.v1` | Fulfillment | Order | 订单进入已发货 |
| `shipment.delivered.v1` | Fulfillment | Order | 订单进入已完成 |

### 3.3 数据所有权（禁止跨库写）

| 服务 | 自有数据 |
|------|----------|
| Identity | `users`、`refresh_tokens`、`user_addresses` |
| Catalog | `categories`、`spus`、`skus`、`product_images` |
| Inventory | `inventory_stock`、`inventory_reservations`、`seckill_activities`、`seckill_user_limits` |
| Cart | `cart_items` |
| Order | `commerce_orders`、`order_items`、`order_status_history`、`order_create_intents`、`order_operations`、`order_outbox_events`、`order_inbox_events` |
| Payment | `payments`、`refunds`、`payment_outbox_events`、`payment_inbox_events` |
| Fulfillment | `shipments`、`fulfillment_outbox_events`、`fulfillment_inbox_events` |

所有权契约在 `shared/contracts` 中定义并通过 `ValidateServiceBoundaries` 启动校验。

## 4. 秒杀核心链路（时序）

```mermaid
sequenceDiagram
    autonumber
    participant C as 客户端
    participant G as API Gateway
    participant O as Order
    participant I as Inventory
    participant M as RabbitMQ
    participant P as Payment
    participant F as Fulfillment

    C->>G: POST /api/v1/seckill/orders（JWT）
    G->>O: Order.Create（gRPC + HMAC）
    O->>I: AdmitSeckill（Redis Lua 原子扣减）
    I-->>O: 预占成功 reservation_id
    O-->>G: 订单创建成功（待支付）
    O-->>M: 发布 order.created.v1
    M-->>P: 消费 order.created.v1

    C->>G: POST /api/v1/orders/:id/payments
    G->>P: Payment.Create
    P-->>M: 发布 payment.succeeded.v1
    M-->>O: 消费 payment.succeeded.v1
    O->>I: Confirm（提交预占）
    O-->>G: 订单已支付

    C->>G: POST /api/v1/admin/orders/:id/ship
    G->>F: Fulfillment.Ship
    F-->>M: 发布 shipment.created.v1
    M-->>O: 消费 shipment.created.v1 → 订单已发货

    C->>G: POST /api/v1/orders/:id/receipt
    G->>F: Fulfillment.ConfirmReceipt
    F-->>M: 发布 shipment.delivered.v1
    M-->>O: 消费 shipment.delivered.v1 → 订单已完成
```

> 取消/退款链路：`POST /orders/:id/cancel` 经 Gateway → Order.Cancel，Order 同步调用
> Inventory `Release`/`Restock` 并发布 `order.cancelled.v1`；Payment 消费后同步退款。

## 5. 可靠性机制

- **Outbox**：业务状态与事件在同一数据库事务写入（Order/Payment/Fulfillment 用 SQL，
  Inventory 用 Redis Lua + Stream），由 Relayer 发布到 RabbitMQ。
- **Inbox**：消费端幂等去重，重复事件不产生重复事实。
- **有限重试 + DLQ**：手动 Ack，可恢复错误进入 retry 队列，attempt ≥ 5 进入死信队列。
- **内部调用认证**：Gateway 注入 HMAC 签名（`x-internal-*` header），下游校验方法名、
  身份、角色和时间窗（±30s），防止绕过 Gateway 直连。
- **幂等**：确定性 request/event ID + 状态机校验，同步 gRPC 与异步事件重复执行安全。

## 6. 代码位置索引

| 内容 | 位置 |
|------|------|
| 服务入口 | `services/<service>/cmd/<service>-service/main.go` |
| 服务私有实现 | `services/<service>/internal/app/` |
| 本地配置 | `services/<service>/etc/` |
| gRPC 契约与生成代码 | `shared/proto/commerce/`、`shared/gen/commerce/` |
| 事件与数据所有权契约 | `shared/contracts/` |
| 平台能力（配置/发现/认证/MQ/遥测） | `shared/platform/` |
| 数据库迁移 | `migrations/` |
| 编排与监控 | `docker-compose.yaml`、`deploy/` |
