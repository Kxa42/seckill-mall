# 变更提案: 第三阶段唯一 Order Service 订单编排

## 需求背景

当前商城存在两套订单实现：`commerce-api` 已经直接写入 `commerce_orders`、`order_items`、`order_status_history` 和库存相关表；旧 `order_service` 仍使用 `orders`、`outbox_events`、旧 Product Service 和 RabbitMQ 异步链路。Gateway 的新 `/api/v1` 请求大部分仍通过 `NoRoute` 代理到 `commerce-api`，新 Inventory 虽已提供 gRPC，但尚未成为订单创建的唯一库存入口。

这种过渡状态会带来三个实际问题：订单表没有唯一业务所有者，普通订单和秒杀订单的库存编排不一致，切换 Gateway 时可能出现重复扣库存或新旧服务同时写同一订单数据。第三阶段需要把“订单”从过渡 Commerce 模块中抽出成为可独立启动、可通过 gRPC 调用、可使用独立 DSN 的 Order Service，同时保留旧秒杀接口的兼容边界，避免一次性重写全部支付、履约和消息基础设施。

## 变更内容

1. 将 `commerce_orders`、`order_items`、`order_status_history` 及订单创建/补偿操作记录明确为 Order Service 的写入边界；`inventory_reservations` 继续由 Inventory Service 所有，Order 只能通过 gRPC 操作库存。
2. 将普通订单和秒杀订单统一到同一订单状态机，使用 Catalog SKU 快照确定价格和商品信息，使用 Identity 地址快照生成不可变收货地址快照，使用 Inventory 完成预占、确认和释放。
3. 扩展版本化 Order/Inventory gRPC 契约，支持创建、查询、列表、取消、超时处理、支付确认以及履约状态转换；使用确定性幂等键和可重试的本地操作记录处理跨服务失败补偿。
4. 将 `/api/v1/orders`、`/api/v1/seckill/orders`、查询、取消和超时任务切换到 Order Service；Gateway 不再先调用 Inventory 再调用 Order，Commerce 的订单写入入口在切流后关闭或只保留显式回退模式。
5. 为当前尚未独立运行的 Identity 增加临时地址快照 gRPC 适配边界，允许真实运行时和 Fake/Memory E2E 使用同一客户端契约；阶段 4 再将该适配器升级为完整 Identity Service。

## 影响范围

- **模块:** Order Service、Commerce API 兼容层、Gateway、Catalog、Inventory/Seckill、Identity 地址快照适配、配置和迁移。
- **文件:** `proto/commerce/commerce_order.proto`、`proto/commerce/inventory.proto`、`cmd/order-service`、`internal/order`、`api_gateway`、`internal/commerce`、`migrations`、`config/commerce-services.example.yaml` 及对应测试。
- **API:** 新增/扩展 `CommerceOrderService` 的订单创建、查询、列表、取消和状态操作；保持旧 `/order` 和旧 Order gRPC 仅作为兼容路径。
- **数据:** 新增订单创建意图、跨服务操作/补偿记录和订单项库存预占引用；不复制或继续写旧 `orders`，不让 Order Service 直接写 Catalog、Identity 或 Inventory 表。

## 不在本阶段范围内

- 不实现前端展示。
- 不在本阶段完成 Identity、Cart、Payment、Fulfillment 的完整独立服务迁移；支付单、退款单和物流表暂保留原有过渡实现，但所有订单状态变更必须经 Order Service。
- 不在本阶段接入完整 RabbitMQ Outbox/Inbox、DLQ 和 publisher confirm；跨服务补偿使用 gRPC 加本地可重试操作记录，消息化放在阶段 5。
- 不删除旧 `order_service`、旧 `orders`、`outbox_events` 或旧接口；它们保留到阶段 6 收口，但新 `/api/v1` 链路不得写入旧订单表。
- Docker 不可用时不阻塞方案验收，真实 MySQL、Redis、RabbitMQ 和 Compose 验收项保留为后续入口。

## 核心场景

### 需求: 订单数据唯一所有权
**模块:** Order Service

#### 场景: 新订单表只由 Order Service 写入
订单服务可以在独立 DSN 下创建、读取和更新商城订单；服务代码不直接访问 `skus`、`user_addresses`、`inventory_reservations` 或旧 `orders` 表。迁移完成后，Commerce 订单写入口被关闭或显式置于回退模式。

### 需求: 统一订单创建
**模块:** Order Service、Catalog、Identity、Inventory

#### 场景: 普通订单创建成功
Order Service 获取 Catalog SKU 快照和 Identity 地址快照，校验整数金额和用户边界，按 SKU 稳定顺序调用 Inventory 预占，成功后一次性保存订单、订单项、状态历史和预占引用，初始状态为 `pending_payment`。

#### 场景: 秒杀订单创建成功
秒杀订单只经过 Order Service 编排，Inventory 的秒杀准入/预占只执行一次，订单与普通订单共用 `pending_payment` 到 `paid/canceled` 的状态机，不再由 Gateway 或 Commerce 额外扣库存。

### 需求: 幂等与失败补偿
**模块:** Order Service、Inventory

#### 场景: 重复请求和参数冲突
相同用户、相同 `Idempotency-Key` 和相同请求摘要返回原订单且不重复调用库存；相同幂等键对应不同商品、数量、地址或订单类型时返回冲突，并且不执行新的库存动作。

#### 场景: 部分预占失败
多个 SKU 中任一预占失败时，Order Service 按已记录的预占引用逆序释放已成功预占；释放失败会留下可重试操作，不能返回成功订单，也不能把库存失败伪装成订单创建成功。

### 需求: 订单生命周期和权限
**模块:** Order Service、Gateway、Commerce 兼容层

#### 场景: 取消和超时关单
用户只能取消自己的 `pending_payment` 订单；超时任务由 Order Service 执行，先幂等释放所有预占，再写入 `canceled` 和状态历史。重复取消、重复超时和重复释放不会产生新的库存变化。

#### 场景: 查询和管理员操作
普通用户只能读取和操作自己的订单；发货等管理员动作校验内部调用方角色，并由 Order Service 执行合法状态转换。Gateway 的 JWT 用户身份不能被请求体中的 `user_id` 覆盖。

### 需求: 受控切流与回退
**模块:** API Gateway、Commerce API、Order Service

#### 场景: 新链路切换
先完成数据库迁移、下游健康检查和 Fake E2E，再关闭 Commerce 订单写入/超时任务，最后启用 Gateway 的 Order gRPC 路由；同一时刻只能有一个商城订单写入者。

#### 场景: 回退
发生故障时先停止或隔离 Order Service 的写入，再将 Gateway 切回明确开启的 Commerce 兼容模式；不允许两个服务同时写 `commerce_orders`，回退不触碰旧 `orders` 的数据迁移。

## 风险评估

- **风险:** 跨 Catalog、Identity、Inventory 和 Order 数据库的同步调用不是单个分布式事务，任一步骤失败都可能留下库存预占或地址/价格快照不完整。
  **缓解:** 使用确定性订单/预占 ID、创建意图和本地操作记录；补偿动作幂等、有限重试、记录最后错误，订单只在所有预占成功且本地事务提交后返回成功。
- **风险:** 当前 Identity 没有独立运行时，直接切 Gateway 会使订单无法取得地址快照。
  **缓解:** 提供临时 Identity 地址快照 gRPC 适配器和内存 Fake；Order 不直接访问身份表，阶段 4 复用同一契约替换实现。
- **风险:** 现有支付、发货和退款代码仍可能直接更新订单状态。
  **缓解:** 将这些入口改为 Order Service 的显式状态操作客户端，Commerce 只保留支付/物流过渡数据能力；增加“非 Order 写入者”检查和回归测试。
- **风险:** 订单切流和支付相关操作涉及交易数据，属于高风险变更。
  **缓解:** 只使用开发/验收 Mock 支付，不连接生产服务；迁移采用向后兼容方式，不执行删除或清空；切流前设置单写者检查和可逆配置开关。
