# 变更提案: Catalog 与 Inventory/Seckill 服务拆分

## 需求背景

第一阶段已经建立商城微服务的 gRPC、事件和数据所有权契约，但运行时仍由 `commerce-api` 统一承载商品目录和库存预占。第二阶段需要先把 Catalog 与 Inventory/Seckill 形成可独立启动、可通过 gRPC 验证的服务，逐步让 Gateway 使用目标服务。

## 变更内容

1. 从 `internal/commerce` 的领域模型中提取 Catalog 独立模型、内存 Repository、MySQL Repository 和 gRPC 服务端。
2. 新增 Inventory/Seckill 独立库存服务，统一实现 `Reserve`、`Confirm`、`Release` 和 Redis Lua 秒杀准入。
3. 保留旧 Product Service 兼容入口，禁止新 Catalog/Inventory 服务写旧 `product` 表或旧订单表。
4. Gateway 优先切换商品查询到 Catalog gRPC；秒杀订单在 Order Service 完成统一编排前保留兼容代理，新增目标 Inventory 的 Fake/契约验收入口。
5. 使用内存 Repository 和 bufconn 完成不依赖 Docker 的跨服务验收。

## 影响范围

- **模块:** Catalog、Inventory/Seckill、Gateway、Commerce 过渡层、Platform、知识库。
- **文件:** `catalog_service/`、`inventory_service/`、`cmd/catalog-service/`、`cmd/inventory-service/`、`api_gateway/`、`proto/commerce/catalog.proto`、配置和文档。
- **API:** 新增内部 Catalog/Inventory gRPC 实现；`GET /api/v1/products*` 由 Gateway 优先调用 Catalog。
- **数据:** Catalog 只拥有 `categories`、`spus`、`skus`、`product_images`；Inventory 只拥有库存、预占和秒杀限购数据；旧表只供兼容链路使用。

## 核心场景

### 需求: 商品目录服务
**模块:** Catalog Service

#### 场景: 查询在售商品和 SKU 快照

- Gateway 通过 gRPC 调用 Catalog，不再直接访问商城 Repository。
- Catalog 只返回在售 SPU/SKU；订单后续通过 SKU 快照获取名称、编码和整数分价格。
- 重复查询不修改库存或购物车数据。

### 需求: 统一库存服务
**模块:** Inventory/Seckill Service

#### 场景: 普通订单库存预占与释放

- `Reserve` 使用 reservation_id 幂等，库存不足不产生部分扣减。
- `Release` 可重复调用，已释放/已确认的预占不会再次增加库存。
- `Confirm` 只允许 reserved 状态转换，支付成功后不再回到可用库存。

### 需求: 秒杀准入
**模块:** Inventory/Seckill Service

#### 场景: Redis Lua 原子限购

- Redis Lua 在一次脚本执行中校验库存、用户限购并记录 admission_id。
- 重复 request_id 返回同一准入结果；失败只返回业务状态，不泄漏 Redis 错误细节。
- Docker/Redis 不可用时使用内存实现验证相同状态机，真实 Lua 集成留待环境可用后执行。

## 风险评估

- **风险:** 直接让秒杀订单同时写新 Inventory 和旧 Commerce 库存会造成双重扣减。
- **缓解:** 本阶段先切换商品查询并实现独立库存服务；秒杀订单保持过渡代理，待 Order Service 阶段采用统一库存命令后再切换完整下单链路。
- **风险:** 新服务与旧服务使用不同数据模型。
- **缓解:** 新服务只依赖 `proto/commerce` 和自己的 Repository，旧 Product Service 继续保留兼容边界。
