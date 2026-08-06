# Changelog

本文件记录项目所有重要变更。
格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/)，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### 新增
- 补齐事件契约的未来版本测试和版本语义说明；Catalog、Inventory 启动时实际校验共享服务边界。
- 新增 `SECKILL_SERVICES_CONFIG` 按角色配置加载器，支持严格环境变量展开、Gateway/Catalog/Inventory 映射和旧配置回退。
- 建立商城微服务渐进式迁移方案，新增 Catalog、Inventory/Seckill、Identity、Cart、Order、Payment、Fulfillment 的版本化 gRPC 契约。
- 新增 `common/contracts` 事件信封、事件类型、服务标识和独占数据所有权校验，覆盖后续 RabbitMQ Outbox/Inbox 迁移基线。
- 新增 `config/commerce-services.example.yaml`，明确目标服务独立地址、DSN、RabbitMQ、etcd 和 Inventory Redis 配置约定。
- 新增独立 `cmd/catalog-service`、`cmd/inventory-service` 入口、Catalog Memory/MySQL Repository、Inventory Memory/Redis Lua Store、gRPC 健康检查和 etcd 注册适配。
- 新增 Catalog/Inventory 的 bufconn Fake E2E，覆盖商品查询、库存预占状态机、重复命令、秒杀限购与释放回滚。

### 变更
- 统一 Catalog/Inventory 的服务发现键为 `catalog-service`、`inventory-service`，避免配置名与共享契约不一致。
- 明确 `EventType` 与 `EventVersion` 独立演进，未知正数未来版本由消费者能力检查处理。
- 知识库明确新商城目标为统一微服务架构，当前仍处于契约基线阶段，未宣称服务拆分已完成。
- 统一订单取消事件名称为 `order.cancelled.v1`，保留旧拼写的代码别名但不增加新的事件类型。
- Gateway 的 `/api/v1/products`、`/api/v1/products/:id` 已切换到 Catalog gRPC；其他 `/api/v1` 路由通过 NoRoute 保留 Commerce 过渡代理。
- `/api/v1/seckill/orders` 暂不切换到新 Inventory，避免旧 Commerce 和新库存服务双重扣减。

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
