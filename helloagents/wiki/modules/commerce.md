# Commerce API

## 目的
提供迁移期回退和旧兼容 API；阶段 4 的身份、购物车、支付和履约显式路由由目标领域服务承担，新商城订单的唯一创建与状态转换由 Order Service 承担。

## 模块概述
- **职责:** 未迁移 API 回退、旧秒杀兼容、历史 Commerce 数据适配，以及 `order_service` 模式下的兼容状态协作。
- **状态:** 🚧第 4 阶段回退中
- **最后更新:** 2026-08-06

## 规范

### 需求: 用户身份与地址
**模块:** Commerce Identity 兼容

#### 场景: 用户注册并登录
- 密码使用 bcrypt 哈希，重复邮箱返回冲突。
- Access Token 包含用户、角色、发行者、类型和过期时间。
- Refresh Token 只保存哈希且每次刷新撤销旧 Token。
- 地址读取、更新和删除必须校验当前用户归属。

### 需求: 商品目录与购物车
**模块:** Commerce Catalog、Cart 兼容

#### 场景: 用户选择在售 SKU
- 公共列表只返回上架 SPU/SKU，支持 offset/limit 分页。
- 购物车限制单 SKU 数量为 1 到 99，结算时重新读取价格与库存。
- 后台商品更新不能覆盖系统维护的 `reserved_stock`。

### 需求: 普通订单闭环
**模块:** Commerce Order 兼容

#### 场景: 用户从购物车结算
- `legacy` 模式仍可由 Commerce Repository 处理历史兼容订单；`order_service` 模式关闭 Commerce 创建订单和超时 worker。
- 新模式通过 Order gRPC 确认支付、发货、收货和退款，Commerce 不直接修改新订单状态。
- 阶段 4 Gateway 的 Payment/Fulfillment 路由不进入 Commerce Repository；Commerce 中的过渡实现只用于兼容和回退。

### 需求: 取消与退款
**模块:** Commerce Order、Payment、Inventory 兼容

#### 场景: 用户取消或申请退款
- 待支付订单取消或超时后释放预占库存。
- 已支付或已发货订单可执行幂等 Mock 退款；Order 先通过 Inventory `Restock` 恢复已确认库存。
- 非法状态转换返回稳定冲突错误并保留状态历史。

### 需求: 秒杀统一交易
**模块:** Commerce Inventory、Order 兼容

#### 场景: 创建秒杀订单
- `/api/v1/seckill/orders` 与普通订单共用 Order 状态机，并由 Order 一次调用带 `order_id` 的 `AdmitSeckill`。
- 旧 Commerce 秒杀写路径只在 `legacy` 模式保留，不能与 Order Service 同时启用。

### 需求: 迁移过渡边界
**模块:** Commerce API、Gateway、Catalog、Inventory、Identity、Cart、Payment、Fulfillment

#### 场景: 未完成服务拆分时保持单一扣减路径
- 商品列表和详情由 Gateway 调用 Catalog；Commerce API 仍保留直连兼容接口供过渡使用。
- `/api/v1/auth/*`、`/api/v1/addresses*`、`/api/v1/cart*`、`/api/v1/orders*`、支付和履约路由已由 Gateway 显式切换到目标 gRPC 服务。
- Commerce 只保留过渡数据；回退前必须停止目标服务写入，禁止双写 `commerce_orders`、`payments`、`refunds` 和 `shipments`。

## API 接口
- 身份与地址兼容: `/api/v1/auth/*`、`/api/v1/addresses*`。
- 目录与购物车兼容: `/api/v1/products*`、`/api/v1/cart/*`。
- 交易兼容: `/api/v1/orders*`、`/api/v1/seckill/orders`、`/api/v1/payments/mock*`。
- 运营: `/api/v1/admin/products`、`/api/v1/admin/orders/:order_id/ship`。

## 数据模型
- Identity: `users`、`refresh_tokens`、`user_addresses`。
- Catalog/Inventory: `categories`、`spus`、`skus`、`inventory_reservations`、`seckill_activities`。
- Trade: `cart_items`、`commerce_orders`、`order_items`、`order_status_history`、`payments`、`refunds`、`shipments`。
- Messaging: `commerce_outbox_events`、`inbox_events`。

## 依赖
- MySQL；内存 Repository 仅用于开发和验收。
- Catalog、Identity、Inventory、Cart、Order、Payment、Fulfillment gRPC；无 Docker 验收使用 Memory/Fake/bufconn。
- Gin、GORM、bcrypt、JWT、Prometheus。

## 当前边界
- Docker daemon 不可用时不执行 MySQL migration 重放、Redis/etcd 健康检查、Compose 启动和真实 gRPC E2E；对应结果必须单独标记为跳过。
- 新商城 RabbitMQ Outbox/Inbox 仍属于阶段 5；阶段 4 使用同步 gRPC 和本地幂等单据，不把现有旧秒杀 MQ 链路从项目中删除。

## 变更历史
- [202608051526_backend_commerce_mvp](../../history/2026-08/202608051526_backend_commerce_mvp/) - 新增完整商城后端 MVP。
- [202608061319_stage4_domain_services](../../history/2026-08/202608061319_stage4_domain_services/) - 阶段 4 路由切流后保留 Commerce 兼容回退。
