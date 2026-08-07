# Go-Seckill-Mall 技术约定

## 技术栈
- Go、Gin、gRPC/Protobuf、GORM/MySQL、Redis Lua、RabbitMQ、etcd。
- OpenTelemetry、Prometheus、Grafana、Jaeger；Docker Compose 和版本化 SQL migration。

## 架构约定
- `/api/v1/*` 是唯一业务 HTTP 入口；旧 `/order*`、`/product/*` 不保留适配层。
- Order 是订单状态唯一写入者，Inventory 是库存/reservation 唯一写入者；跨服务只使用 gRPC 或统一事件。
- Order、Payment、Fulfillment 使用服务级 SQL Outbox/Inbox；Inventory 使用 Redis Lua + Stream Outbox。
- `shared/contracts` 是事件和数据所有权 SSOT；`shared/platform/messaging` 只提供传输/存储抽象，不依赖业务 Repository。
- 每个服务统一位于 `services/<service>`，在自身目录内保存 `cmd`、私有 `internal`、`etc` 和可选 `testkit`。
- 跨服务生成代码和协议位于 `shared/gen`、`shared/proto`，通用平台能力位于 `shared/platform`；业务客户端适配器归属消费者服务，仓库暂时保留单一 `go.mod`。
- 生产代码不得导入其他服务的 `internal` 或任何 `testkit`；`shared` 不得反向依赖 `services`。
- 旧表 `orders`、`product`、`outbox_events` 只保留历史数据，代码不读写，migration 不 DROP/TRUNCATE。

## 配置与安全
- 服务可通过 `deploy/config/commerce-services.example.yaml` 按角色加载，也可使用各自 `services/<service>/etc` 配置；DSN、MQ URL、Redis 密码和密钥只能由环境变量注入。
- `SECKILL_MQ_URL` 为空时不连接 RabbitMQ；Memory/Fake 模式用于本地验收。
- 事件 payload 只携带跨服务最小事实，不记录 Token、密码、支付签名或地址明文。
- gRPC/MQ 处理使用 deadline、有限重试和 Inbox 幂等。

## 验证

```bash
make check
```

Docker 不可用时跳过真实 MySQL/Redis/RabbitMQ/Compose 联调，并在验收结果中明确记录。
