# Changelog

本文件记录项目所有重要变更。
格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/)，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### 新增
- 新增 `Makefile`，统一 Protobuf 生成、格式检查、包枚举、单元测试、vet 和 Memory E2E 入口。
- 新增独立 Identity、Cart、Payment、Fulfillment gRPC 服务、Memory/MySQL Repository、服务入口和阶段 4 Gateway `/api/v1` 路由。
- 新增阶段 4 内部 HMAC 调用认证、商城 JWT 严格校验、Payment 回调/退款幂等、Fulfillment 发货/收货幂等和 `payments.user_id` migration。
- 新增 `tests/stage4_memory_e2e.sh`，覆盖注册到收货、退款和权限边界的 Memory/Fake/bufconn 验收。
- 新增唯一 `Order Service` 独立进程和 Gateway 订单路由，统一普通订单与秒杀订单创建、查询、取消、支付、履约和退款状态转换。
- 新增 Identity 地址快照适配器、Order 创建意图恢复 worker、`order_operations` 补偿记录和 Inventory `Restock` 退款恢复 RPC。
- 新增 Order/Inventory 退款恢复、创建意图恢复、系统 `Expire` 签名、幂等与有限重试测试。
- 补齐事件契约的未来版本测试和版本语义说明；Catalog、Inventory 启动时实际校验共享服务边界。
- 新增 `SECKILL_SERVICES_CONFIG` 按角色配置加载器，支持严格环境变量展开、Gateway/Catalog/Inventory 映射和旧配置回退。
- 建立商城微服务渐进式迁移方案，新增 Catalog、Inventory/Seckill、Identity、Cart、Order、Payment、Fulfillment 的版本化 gRPC 契约。
- 新增 `shared/contracts` 事件信封、事件类型、服务标识和独占数据所有权校验，覆盖后续 RabbitMQ Outbox/Inbox 迁移基线。
- 新增统一 `shared/platform/messaging` Outbox/Inbox、Fake Broker、RabbitMQ Topic、Publisher Confirm、mandatory return、手动 Ack、retry/DLQ 和断线重连。
- 新增 Order、Payment、Fulfillment 服务级 SQL Outbox/Inbox，以及 Inventory Redis Lua + Stream Outbox 到 RabbitMQ 的出站桥和 Redis Inbox。
- 新增统一事件消费者：订单状态恢复、支付订单事实、库存取消/退款恢复和履约待发货事实；重复事件、未知事件和未来版本均有隔离测试。
- 新增 `migrations/004_stage5_messaging.sql`、`tests/stage5_memory_e2e.sh` 和 `tests/stage5_rabbitmq_e2e.sh`。
- 新增 `deploy/config/commerce-services.example.yaml`，明确目标服务独立地址、DSN、RabbitMQ、etcd 和 Inventory Redis 配置约定。
- 新增独立 `services/catalog/cmd/catalog-service`、`services/inventory/cmd/inventory-service` 入口、Catalog Memory/MySQL Repository、Inventory Memory/Redis Lua Store、gRPC 健康检查和 etcd 注册适配。
- 新增 Catalog/Inventory 的 bufconn Fake E2E，覆盖商品查询、库存预占状态机、重复命令、秒杀限购与释放回滚。

### 变更
- 仓库演进为服务优先单仓库：八个服务分别拥有 `services/<service>/cmd`、私有 `internal`、`etc` 和可选 `testkit`，同时保留单一 `go.mod`。
- 跨服务契约、Protobuf、生成代码、客户端和平台能力收敛到 `shared`；migration 工具迁入 `tools/migrate`，SQL migration 版本路径保持不变。
- 新增编译与脚本边界门禁，禁止生产代码跨服务导入 `internal`/`testkit`，并禁止 `shared` 反向依赖 `services`。
- 在服务优先迁移前先完成标准 Go 过渡布局，拆分 Gateway 薄入口与内部实现并消除其对 Order 实现包的直接依赖。
- Gateway 内部调用签名现使用 `shared/platform/internalcall`；Identity 入口统一命名为 `services/identity/cmd/identity-service`。
- Protobuf `go_package`、Compose 构建路径、测试脚本、README 和知识库已同步服务优先目录；migration 文件名保持稳定以兼容已记录版本。
- Gateway 已将 `/api/v1/*` 业务路由显式切换到目标领域服务；未注册路径直接返回 404，不再保留 Commerce NoRoute/兼容回退。
- 阶段 5 统一 RabbitMQ `commerce.events.v1` 事件拓扑；旧秒杀 Outbox、MQ、DLQ Worker、旧 Product/Order 服务和旧 HTTP 入口已移除运行时装配。
- `/order*`、`/product/*` 仅返回 404；新运行时只通过 `/api/v1/*` 访问，旧 `orders`、`product`、`outbox_events` 表只保留历史数据且停止读写。
- Go 用户级 `GOTMPDIR` 设置为 `/tmp`，`GOCACHE` 设置为 `/tmp/go-build-cache`。
- `/api/v1/orders*` 和 `/api/v1/seckill/orders` 统一由 Order Service 编排，Inventory 是唯一 reservation/库存写入者。
- Inventory 将支付前 `Release` 与退款后的 `Restock` 分离；秒杀退款同时回滚活动限购计数。
- 本轮通过 Memory/Fake/bufconn、全仓测试、vet、全仓 race 和 Compose 静态校验；Docker daemon 不可用，真实基础设施联调跳过。
- 修复不同 `Idempotency-Key` 相同请求摘要复用订单号的问题；稳定订单号和创建意图 ID 现在同时绑定用户、幂等键和请求摘要。
- 修复库存释放补偿成功后订单仍停留在 `pending_payment` 的问题，补偿 worker 会在全部 reservation 释放后完成取消。
- 修复订单事务成功但创建意图尚未标记完成时的重复恢复扫描，幂等命中已有订单会收口对应意图。

- 统一 Catalog/Inventory 的服务发现键为 `catalog-service`、`inventory-service`，避免配置名与共享契约不一致。
- 明确 `EventType` 与 `EventVersion` 独立演进，未知正数未来版本由消费者能力检查处理。
- 知识库已同步统一微服务运行时、服务级消息边界和旧表停用策略，未将历史方案的过渡状态误写为当前运行时。
- 统一订单取消事件名称为 `order.cancelled.v1`，保留旧拼写的代码别名但不增加新的事件类型。
- Gateway 的 `/api/v1/products`、`/api/v1/products/:id` 由 Catalog gRPC 提供，其他 `/api/v1` 路由同样显式调用目标服务。
- Inventory Stream 出站事件使用 pending claim 重放，入站取消/退款事件使用 Redis Inbox 租约与已处理标记；MQ 未配置时服务安全降级，不连接任何旧队列。
- 归档完整微服务迁移总方案和已执行阶段5方案；未执行的重复阶段5草案标记为统一方案替代并移入历史记录。

### 移除
- 移除服务优先迁移后的根级 `cmd`、`internal`、`config`、`proto`、`api` 空目录和过时 `stress_test` 程序。
- 移除已被按服务配置替代的 `config.example.yaml`、未被入口加载的 `config/user.yaml` 和旧 `/order` HTTP 示例 `test.http`。
- 移除已被 `shared/proto/commerce/*` 替代的旧 `proto/order.proto`、`proto/product.proto`，以及阶段 5 已覆盖的旧 Memory E2E 脚本。
- 移除不含跟踪文件的旧服务、集中 Worker 和过渡模块空目录。

## [0.1.0] - 2026-08-05

### 新增
- 建立 HelloAGENTS 项目知识库与变更方案管理目录。
- 新增 `/api/v1` 商城后端，覆盖身份、地址、SPU/SKU、购物车、统一订单、Mock 支付、履约和退款。
- 新增整数分金额、库存 reservation、订单状态历史、幂等下单、支付回调和超时关单。
- 新增 MySQL Repository、内存验收 Repository、版本化 migration、OpenAPI、Dockerfile 和完整 Compose 编排。
- 新增商城领域/HTTP/平台测试与不依赖 Docker 的 `tests/e2e_memory.sh` 主链路验收。

### 变更
- Gateway 增加 `/api/v1` 反向代理和健康检查，旧模拟登录仅在 `debug` 模式注册。
- 旧业务进程支持容器可配置的 etcd 注册地址和 OTLP endpoint。
- 敏感连接信息与 Redis 密码改为通过环境变量注入。

### 修复
- 修复 Catalog 内存商品克隆丢失 SKU、秒杀释放不回滚限购计数、活动限购键缺少 activity_id 和 Redis 重复命令参数未校验问题。
- 修复商城 Outbox 确定性事件 ID 超过数据库 64 字符限制的问题。
- 防止后台商品更新覆盖库存预占状态，并统一内存/MySQL 幂等订单详情语义。
- 增加订单总金额累加溢出保护。
