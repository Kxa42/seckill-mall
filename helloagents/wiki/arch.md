# 架构设计

## 总体架构

当前处于微服务迁移第 4 阶段：Identity、Catalog、Inventory/Seckill、Cart、Order、Payment 和 Fulfillment 均具备独立 gRPC 运行边界；Gateway 已显式切换 `/api/v1` 的身份、购物车、订单、支付和履约路由，Commerce API 仅保留迁移期回退和旧兼容能力。

```mermaid
flowchart LR
    Client[API Client] --> Gateway[API Gateway]
    Gateway -->|商品查询 gRPC| Catalog[Catalog Service]
    Gateway -->|迁移期未切换路由| Commerce[Commerce API]
    Gateway -->|订单与秒杀订单 gRPC| Order[Order Service]
    Gateway -->|身份/地址 gRPC| Identity[Identity Service]
    Gateway -->|购物车 gRPC| Cart[Cart Service]
    Gateway -->|支付/退款 gRPC| Payment[Payment Service]
    Gateway -->|发货/收货/物流 gRPC| Fulfillment[Fulfillment Service]
    Order -->|SKU 快照 gRPC| Catalog[Catalog Service]
    Order -->|地址快照 gRPC| Identity
    Order -->|Reserve/Admit/Confirm/Release/Restock| Inventory[Inventory / Seckill Service]
    Cart -->|SKU 快照 gRPC| Catalog
    Payment -->|Get/ConfirmPayment/Refund| Order
    Fulfillment -->|Ship/ConfirmReceipt| Order
    Catalog --> CatalogDB[(Catalog-owned tables)]
    Inventory --> Redis[(Redis Lua)]
    Inventory --> InventoryMemory[Memory Store / bufconn]
    Commerce --> CommerceDB[(Commerce transition tables)]
    Order --> OrderDB[(Order-owned commerce_orders/order_items)]
    Payment --> PaymentDB[(Payment-owned payments/refunds)]
    Fulfillment --> FulfillmentDB[(Fulfillment-owned shipments)]

    Gateway --> LegacyOrder[Legacy Order Service]
    Gateway --> LegacyProduct[Legacy Product Service]
    LegacyOrder --> LegacyProduct
    LegacyOrder --> LegacyDB[(orders/outbox_events)]
    LegacyProduct --> Redis[(Redis Lua Admission)]
    LegacyWorker[Outbox/MQ/DLQ Workers] --> LegacyDB
    LegacyWorker --> RabbitMQ[(RabbitMQ)]
    LegacyWorker --> Redis
```

## 目标微服务架构

```mermaid
flowchart LR
    Client[HTTP Client] --> Gateway[API Gateway]
    Gateway --> Identity[Identity]
    Gateway --> Catalog[Catalog]
    Gateway --> Cart[Cart]
    Gateway --> Order[Order]
    Gateway --> Payment[Payment]
    Gateway --> Fulfillment[Fulfillment]
    Gateway --> Inventory[Inventory / Seckill]
    Order -->|gRPC| Catalog
    Order -->|gRPC| Identity
    Order -->|gRPC| Inventory
    Inventory --> Redis[(Redis Lua)]
    Identity --> IDDB[(Identity DB)]
    Catalog --> CatDB[(Catalog DB)]
    Cart --> CartDB[(Cart DB)]
    Order --> OrderDB[(Order DB)]
    Payment --> PayDB[(Payment DB)]
    Fulfillment --> ShipDB[(Fulfillment DB)]
    Inventory --> InvDB[(Inventory DB)]
    Identity --> MQ[(RabbitMQ)]
    Catalog --> MQ
    Inventory --> MQ
    Order --> MQ
    Payment --> MQ
    Fulfillment --> MQ
    MQ --> Consumers[Inbox Consumers / DLQ]
```

## 迁移阶段形态
- **第 4 阶段运行态:** Gateway 显式注册 Identity、Address、Cart、Order、Payment、Refund 和 Fulfillment 的 `/api/v1` 路由，通过目标 gRPC 服务处理；未切换路由继续通过 `NoRoute` 兼容代理到 Commerce API。
- **Order 编排态:** Order 通过 Catalog 获取 SKU 快照、通过 Identity 获取地址快照、通过 Inventory 一次完成普通预占或秒杀准入，并只写自己的订单表、状态历史、创建意图和操作记录。
- **库存状态态:** Inventory/Seckill 提供 `Reserve`、`Confirm`、`Release`、`Restock` 和 `AdmitSeckill`；`Release` 只处理支付前 reserved，`Restock` 只处理退款前已确认库存。MemoryStore 用于无 Docker Fake E2E，RedisStore 用 Lua 保证库存、限购和恢复原子性。
- **Commerce 过渡态:** `commerce-api` 在 `order_service` 模式下关闭订单创建和超时 worker；阶段 4 新支付、发货、收货和退款请求不进入 Commerce Repository，Commerce 只保留回退和旧兼容 API。
- **契约基线:** `common/contracts` 只存放跨服务稳定事件信封、事件类型和服务标识；`proto/commerce` 存放新商城版本化 gRPC 契约。
- **目标边界:** Identity、Catalog、Inventory/Seckill、Cart、Order、Payment、Fulfillment 各自拥有数据和后续 Outbox/Inbox，禁止跨服务直接写表；阶段 4 先完成同步 gRPC，阶段 5 再接入新商城消息运行时。
- **切换原则:** 每阶段先完成 Fake/契约测试，再由 Gateway 将对应 `/api/v1` 路由切换到目标服务。
- **边界契约:** `common/contracts` 固化七个业务服务的数据集、同步依赖和事件发布/消费关系；返回值为副本，避免调用方修改全局定义。
- **配置契约:** `config/commerce-services.example.yaml` 为目标服务提供独立监听地址、DSN、RabbitMQ URL 和 etcd 地址；通过 `SECKILL_SERVICES_CONFIG` 按角色加载，Gateway 不读取业务 DSN，Inventory 额外配置 Redis。
- **事件版本契约:** `EventType` 负责路由和主版本，`EventVersion` 负责 Payload/信封演进；传输层接受正数未来版本，消费者必须按能力处理未知版本。
- **服务入口:** `go run ./cmd/catalog-service`、`go run ./cmd/inventory-service`、`go run ./cmd/identity-snapshot-service`、`go run ./cmd/cart-service`、`go run ./cmd/order-service`、`go run ./cmd/payment-service` 和 `go run ./cmd/fulfillment-service` 可在无真实基础设施时使用 Memory/直连 Fake 验收；配置和环境变量支持切换到 MySQL、Redis 与 etcd。

## 商城核心流程

```mermaid
sequenceDiagram
    participant User
    participant Gateway
    participant Order
    participant Identity
    participant Cart
    participant Payment
    participant Fulfillment
    participant Store
    User->>Gateway: 注册/登录并维护地址
    Gateway->>Identity: Register/Login/Address gRPC
    Identity->>Store: 保存用户、地址和 Refresh Token 哈希
    User->>Gateway: 购物车与结算预览
    Gateway->>Cart: Set/List/Preview gRPC
    Cart->>Store: 保存 cart_items
    User->>Gateway: Idempotency-Key 创建普通/秒杀订单
    Gateway->>Order: Create
    Order->>Store: SKU/地址快照 + Inventory reservation + OrderDB
    User->>Gateway: 创建 Mock 支付并提交签名回调
    Gateway->>Payment: Create/Callback gRPC
    Payment->>Order: ConfirmPayment
    Order->>Store: Confirm reservation -> paid
    User->>Gateway: admin 发货 / 用户确认收货 / 退款
    Gateway->>Fulfillment: Ship/ConfirmReceipt gRPC
    Gateway->>Payment: Refund gRPC
    Fulfillment->>Order: Ship / ConfirmReceipt
    Payment->>Order: Refund
    Order->>Store: 状态转换和退款 Restock
```

## 一致性与状态
- 创建订单通过同步 gRPC 编排下游，再在 Order Repository 本地事务提交订单、订单项、状态历史和创建意图；跨服务动作以确定性 ID 和操作记录补偿。
- 支付成功确认预占库存；待支付取消或超时调用 `Release`；已支付退款调用 `Restock` 恢复已确认库存。
- 订单和支付重复请求分别由 `(user_id, idempotency_key)`、`callback_ref` 和状态机保证幂等。
- Commerce Outbox 已持久化版本化事件，Inbox 表已建模；第一阶段已补充跨服务事件信封、事件类型和数据所有权，发布器和领域消费者仍待后续阶段接入。

## 已知边界
- 当前环境未完成真实 MySQL migration 重放和完整 Compose 健康检查，已用内存 E2E、SQL 解析和 Compose 配置校验替代。
- 新链路 `/api/v1/seckill/orders` 进入 Order Service，并只调用一次 Inventory `AdmitSeckill`；旧 `/order` 仍保留原 Redis/MQ 兼容链路。
- 订单创建意图恢复和库存操作都有最大 8 次重试；达到上限后保持可查询失败状态，不无限轰炸下游。
- 旧 MQ Consumer 仍跨写旧 `orders` 与 `product` 表；该行为被限制在兼容域，不属于新商城数据所有权模型。
- 前端明确暂缓，OpenAPI 是当前客户端契约。
- Identity、Cart、Payment、Fulfillment 已完成阶段 4 的独立进程和同步 gRPC 拆分，但尚未接入阶段 5 的新商城 RabbitMQ Outbox/Inbox；当前结果不能等同于真实基础设施和异步最终一致性验收。

## 重大架构决策

完整 ADR 存储在已归档方案的 `how.md` 中。

| adr_id | title | date | status | affected_modules | details |
|--------|-------|------|--------|------------------|---------|
| ADR-001 | 采用增量式受控微服务 | 2026-08-05 | ✅已采纳 | Gateway、Commerce、Legacy Services | [详情](../history/2026-08/202608051526_backend_commerce_mvp/how.md#adr-001-采用增量式受控微服务) |
| ADR-002 | 同一 MySQL 实例内实施领域数据所有权 | 2026-08-05 | ✅已采纳 | Commerce、Product、Order | [详情](../history/2026-08/202608051526_backend_commerce_mvp/how.md#adr-002-同一-mysql-实例内实施领域数据所有权) |
| ADR-003 | 金额使用最小货币单位整数 | 2026-08-05 | ✅已采纳 | Commerce API、Trade | [详情](../history/2026-08/202608051526_backend_commerce_mvp/how.md#adr-003-金额使用最小货币单位整数) |
| ADR-004 | 本地事务加 Outbox/Inbox | 2026-08-05 | ✅已采纳 | Commerce、Messaging | [详情](../history/2026-08/202608051526_backend_commerce_mvp/how.md#adr-004-本地事务加-outboxinbox) |
| ADR-005 | 采用渐进式微服务拆分 | 2026-08-06 | ✅已采纳 | Gateway、Commerce、Catalog、Inventory、Order、Messaging | [详情](../plan/202608060800_microservice_mall_migration/how.md#adr-001-采用渐进式微服务拆分) |
| ADR-006 | 订单状态只由 Order Service 维护 | 2026-08-06 | ✅已采纳 | Order、Inventory、Payment、Fulfillment | [详情](../plan/202608060800_microservice_mall_migration/how.md#adr-002-订单状态只由-order-service-维护) |
| ADR-007 | 服务数据库按所有权隔离 | 2026-08-06 | ✅已采纳 | All Services、Platform | [详情](../plan/202608060800_microservice_mall_migration/how.md#adr-003-服务数据库按所有权隔离) |
| ADR-008 | 先切换 Catalog 查询，延后秒杀订单完整切换 | 2026-08-06 | ✅已采纳 | Gateway、Catalog、Inventory、Commerce | [详情](../history/2026-08/202608060842_catalog_inventory_services/how.md#adr-001-先切换-catalog-查询延后秒杀订单完整切换) |
| ADR-009 | EventType 与 EventVersion 独立演进 | 2026-08-06 | ✅已采纳 | Contracts、Messaging、Catalog、Inventory | [详情](../history/2026-08/202608061041_contract_config_hardening/how.md#adr-001-eventtype-与-eventversion-独立演进) |
| ADR-010 | 显式选择按服务配置并保留旧配置回退 | 2026-08-06 | ✅已采纳 | Platform、Gateway、Catalog、Inventory | [详情](../history/2026-08/202608061041_contract_config_hardening/how.md#adr-002-显式选择按服务配置保留旧配置回退) |
| ADR-011 | Order Service 成为商城订单唯一写入者 | 2026-08-06 | ✅已采纳 | Order、Gateway、Commerce | [详情](../history/2026-08/202608061112_order_service_orchestration/how.md#adr-011-order-service-成为商城订单唯一写入者) |
| ADR-012 | 使用同步 gRPC 编排加本地可重试操作记录 | 2026-08-06 | ✅已采纳 | Order、Inventory、Commerce | [详情](../history/2026-08/202608061112_order_service_orchestration/how.md#adr-012-使用同步-grpc-编排加本地可重试操作记录) |
| ADR-013 | Identity 使用地址快照 gRPC 兼容适配 | 2026-08-06 | ✅已采纳 | Identity、Order | [详情](../history/2026-08/202608061112_order_service_orchestration/how.md#adr-013-identity-使用地址快照-grpc-兼容适配) |
| ADR-014 | 秒杀只走一次 Inventory 预占并在预占时绑定订单 | 2026-08-06 | ✅已采纳 | Gateway、Order、Inventory | [详情](../history/2026-08/202608061112_order_service_orchestration/how.md#adr-014-秒杀只走一次-inventory-预占并在预占时绑定订单) |
| ADR-015 | 通过确定性创建意图处理跨服务重试 | 2026-08-06 | ✅已采纳 | Order、Inventory | [详情](../history/2026-08/202608061112_order_service_orchestration/how.md#adr-015-通过确定性创建意图处理跨服务重试) |
