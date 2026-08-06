# Seckill Mall

统一商城微服务示例，当前运行时只有一套订单、库存和事件模型。外部客户端通过 API Gateway 的 `/api/v1/*` 访问 Catalog、Inventory、Order、Payment、Fulfillment、Identity 和 Cart；旧 `/order*`、`/product/*` 路径直接返回 404。

## 架构

```text
Gateway -> Identity / Catalog / Cart / Order / Payment / Fulfillment / Inventory (gRPC)
Order, Payment, Fulfillment -> service-owned MySQL state + Outbox/Inbox
Inventory -> Redis Lua state + Redis Stream Outbox
Outbox -> RabbitMQ commerce.events.v1 -> service-owned consumers/retry/DLQ
```

Order 是订单状态唯一维护者，Inventory 是 reservation/库存唯一维护者，Payment 只维护支付和退款单据，Fulfillment 只维护 shipment。服务之间禁止跨库写表。

统一事件包括 `seckill.accepted.v1`、`order.created.v1`、`order.cancelled.v1`、`payment.succeeded.v1`、`payment.refunded.v1`、`inventory.reserved.v1`、`inventory.released.v1`、`inventory.restocked.v1`、`shipment.created.v1` 和 `shipment.delivered.v1`。

## 本地验证

无 Docker 时各服务默认可以使用 Memory/Fake/bufconn：

```bash
GOTMPDIR=/tmp/seckill-go-build go test ./...
GOTMPDIR=/tmp/seckill-go-build go vet ./...
bash tests/stage5_memory_e2e.sh
```

Go 临时目录使用 `/tmp` 是当前受限环境要求。真实 RabbitMQ 验收入口为：

```bash
SECKILL_RABBITMQ_INTEGRATION=1 bash tests/stage5_rabbitmq_e2e.sh
```

Docker 不可用时脚本会明确跳过，不连接生产或外部真实服务。

## 启动

单服务本地启动示例：

```bash
go run ./cmd/catalog-service
go run ./cmd/inventory-service
go run ./cmd/order-service
go run ./cmd/payment-service
go run ./cmd/fulfillment-service
go run ./api_gateway
```

设置 `SECKILL_SERVICES_CONFIG=config/commerce-services.example.yaml` 可使用统一按角色配置。MySQL、Redis、etcd 和 RabbitMQ 未配置时，目标服务保持安全的 Memory/Fake 降级；配置 `SECKILL_MQ_URL` 后才启动 RabbitMQ Outbox Publisher/Consumer。

## 数据边界

`migrations/004_stage5_messaging.sql` 创建：

- `order_outbox_events`、`order_inbox_events`
- `payment_outbox_events`、`payment_inbox_events`
- `fulfillment_outbox_events`、`fulfillment_inbox_events`

旧 `orders`、`product`、`outbox_events` 等表不由新环境初始化，也不执行 `DROP` 或 `TRUNCATE`；已有实例中的旧表仅作为历史残留，代码不再读写。

## 部署

`docker-compose.yaml` 只包含新商城服务、MySQL migration、Redis、RabbitMQ、etcd、Gateway 和监控组件。RabbitMQ 拓扑由 `common/messaging` 声明：Topic 主交换机、每服务主队列、retry queue 和 DLQ，消费者使用手动 Ack、Inbox 幂等和有限重试。

## 目录

- `api_gateway/`: 版本化 HTTP Gateway
- `cmd/*-service/`: 新商城服务入口
- `common/contracts/`: 跨服务事件与边界契约
- `common/messaging/`: Outbox、Inbox、RabbitMQ、Fake Broker 和发布循环
- `internal/order/`: Order 状态机与 MySQL/Memory Repository
- `inventory_service/`: Redis Lua、Stream Outbox 和 Memory Store
- `payment_service/`: 支付/退款状态机与事件消费者
- `fulfillment_service/`: 发货/收货状态机与事件消费者
- `migrations/`: 版本化数据库迁移
- `helloagents/`: 项目知识库、方案包和历史归档
