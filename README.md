# Seckill Mall

统一商城微服务示例，当前运行时只有一套订单、库存和事件模型。外部客户端通过 API Gateway 的 `/api/v1/*` 访问 Catalog、Inventory、Order、Payment、Fulfillment、Identity 和 Cart；旧 `/order*`、`/product/*` 路径直接返回 404。

## 架构

> 组件功能与关联的完整示意图见 [docs/architecture.md](docs/architecture.md)。
> Redis 与 RabbitMQ 的实现与工业化设计解读见 [docs/redis-rabbitmq.md](docs/redis-rabbitmq.md)。

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
make check
```

Go 临时目录使用 `/tmp` 是当前受限环境要求。真实 RabbitMQ 验收入口为：

```bash
SECKILL_RABBITMQ_INTEGRATION=1 bash tests/stage5_rabbitmq_e2e.sh
```

Docker 不可用时脚本会明确跳过，不连接生产或外部真实服务。

## 启动

单服务本地启动示例：

```bash
go run ./services/catalog/cmd/catalog-service
go run ./services/inventory/cmd/inventory-service
go run ./services/identity/cmd/identity-service
go run ./services/cart/cmd/cart-service
go run ./services/order/cmd/order-service
go run ./services/payment/cmd/payment-service
go run ./services/fulfillment/cmd/fulfillment-service
go run ./services/gateway/cmd/api-gateway
```

设置 `SECKILL_SERVICES_CONFIG=deploy/config/commerce-services.example.yaml` 可使用统一按角色配置。各服务的本地配置位于自身 `etc/`；MySQL、Redis、etcd 和 RabbitMQ 未配置时，目标服务保持安全的 Memory/Fake 降级。

## 数据边界

`migrations/004_stage5_messaging.sql` 创建：

- `order_outbox_events`、`order_inbox_events`
- `payment_outbox_events`、`payment_inbox_events`
- `fulfillment_outbox_events`、`fulfillment_inbox_events`

旧 `orders`、`product`、`outbox_events` 等表不由新环境初始化，也不执行 `DROP` 或 `TRUNCATE`；已有实例中的旧表仅作为历史残留，代码不再读写。

## 部署

`docker-compose.yaml` 只包含新商城服务、MySQL migration、Redis、RabbitMQ、etcd、Gateway 和监控组件。RabbitMQ 拓扑由 `shared/platform/messaging` 声明：Topic 主交换机、每服务主队列、retry queue 和 DLQ，消费者使用手动 Ack、Inbox 幂等和有限重试。

## 目录

- `services/<service>/cmd/`: 服务可执行入口
- `services/<service>/internal/`: 服务私有实现与单元测试，其他服务不可导入
- `services/<service>/etc/`: 服务本地配置
- `services/<service>/testkit/`: 仅供跨服务内存 E2E 使用的最小测试门面
- `shared/contracts/`: 跨服务事件与数据所有权契约
- `shared/proto/commerce/`、`shared/gen/commerce/`: Protobuf 源文件与生成代码
- `shared/platform/`: 配置、服务发现、内部认证、消息和可观测性能力
- 业务客户端适配器位于消费者服务的 `services/<service>/internal/app`；共享层不保存领域客户端
- `tools/migrate/`: 全局 migration 执行工具
- `migrations/`: 版本化数据库迁移
- `helloagents/`: 项目知识库、方案包和历史归档
