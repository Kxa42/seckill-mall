# 变更提案: 统一商城架构与阶段5可靠消息闭环

## 需求背景

当前仓库同时存在目标商城微服务链路和旧秒杀链路：新链路使用 Catalog、Inventory、Order、Payment、Fulfillment 等独立服务，旧链路则依赖旧 `/order` 入口、`product_service`、Legacy Order、旧 `orders` 表以及单独的 MQ Consumer/Outbox/DLQ。由于没有外部系统依赖旧队列，继续保留两套运行时只会造成双订单模型、双库存语义和双事件协议。

本次将阶段5调整为统一设计：新商城微服务成为唯一运行时和业务真源，所有业务事件使用 `common/contracts.EventEnvelope` 和统一 RabbitMQ Topic；旧 `/order` HTTP 入口、旧 Product/Order 服务、旧 MQ Worker 和兼容代理全部退出运行并从代码装配中移除。旧数据库表不在本阶段直接删除，仅停止读写，避免执行无备份的不可逆数据操作。

## 产品分析

### 目标用户与场景

- **用户群体:** 需要展示完整 Go 微服务商城、秒杀库存、事件驱动和故障恢复能力的开发者、面试官或项目评审者。
- **使用场景:** 客户只通过 `/api/v1` 完成商品、秒杀、订单、支付、履约和退款；服务重启或 RabbitMQ 短暂故障后，统一事件链路仍可恢复。
- **核心痛点:** 旧链路保留会让使用者无法判断哪个订单/库存/消息模型是真实架构，也会让维护和验收同时覆盖两套不兼容实现。

### 价值主张与成功指标

- **价值主张:** 用一套新服务、一个订单状态机、一套库存状态机和一套事件协议完成商城与秒杀闭环。
- **成功指标:** Gateway 不再注册旧 `/order`、`/product` 和 Commerce NoRoute 代理；Compose 不再启动旧服务；新服务只写自有数据和 Outbox/Inbox；重复消息、发布失败、DLQ 和恢复重放均能通过统一运行时验收。

### 人文关怀

事件仅携带业务 ID、状态和完成动作所需的最小数据，不复制密码、Token、支付签名或不必要的地址明文。移除旧入口时返回清晰的 404，不将旧接口请求静默转发到另一套语义不一致的服务。

## 变更内容

1. 以 Order、Inventory、Payment、Fulfillment 等新服务作为唯一业务运行时，统一秒杀和普通订单的状态机与库存 reservation。
2. 建立服务级 Outbox/Inbox、RabbitMQ Topic、发布确认、手动 Ack、有限重试、DLQ 和补偿机制；Inventory 使用 Redis Lua + Stream 保证热点状态与事件记录原子化。
3. 删除 Gateway 旧 `/order`、`/product` 路由及 Commerce NoRoute 代理，移除 `commerce-api`、Legacy Order、Product Service、旧 MQ Consumer、旧 Outbox Worker 和旧 DLQ Consumer 的启动与代码依赖。
4. 新增阶段5 migration 和统一 Compose 配置；现有旧表只保留为历史数据，不再由任何运行时读取或写入，不执行 DROP/TRUNCATE。
5. 更新测试、README、OpenAPI、架构图和知识库，增加“旧链路不可用、统一 `/api/v1` 可用”的回归门禁。

## 影响范围

- **模块:** Gateway、Catalog、Inventory/Seckill、Order、Payment、Fulfillment、Messaging、配置、部署、旧兼容模块和知识库。
- **文件:** `api_gateway`、`common/contracts`、新增 `common/messaging`、`internal/order`、`payment_service`、`fulfillment_service`、`inventory_service`、migration、Compose、README 和测试；删除或移除旧运行时文件。
- **API:** 保留 `/api/v1/*`；移除 `/order*`、`/product/:id` 旧入口和 Commerce 代理，不新增旧接口兼容行为。
- **数据:** 新增服务级 Outbox/Inbox；Inventory 事件使用 Redis Stream；旧 `orders`、`product`、`outbox_events` 表仅保留在已有数据库中，不再被代码访问。

## 核心场景

### 需求: 统一业务入口
**模块:** Gateway、Order、Catalog、Inventory、Payment、Fulfillment

#### 场景: 客户访问商城能力

- 商品、普通订单、秒杀、支付、取消、发货、收货和退款全部通过 `/api/v1` 调用目标服务。
- Gateway 只建立目标服务 gRPC 客户端，不再连接旧 Product/Order，也不配置 Commerce URL。
- 访问旧 `/order` 或 `/product` 路径直接返回 404，不产生旧订单或库存副作用。

### 需求: 统一事件可靠投递
**模块:** Messaging、Order、Payment、Inventory、Fulfillment

#### 场景: 本地状态和事件提交

- Order、Payment、Fulfillment 的业务状态与本服务 Outbox 在同一 MySQL 事务内提交。
- Inventory 的 Redis Lua 状态转换与 Outbox Stream 记录在同一脚本内完成。
- Publisher 使用持久化消息、Publisher Confirm 和 mandatory 路由检查；确认丢失允许重复发布，由 Inbox 收敛。

### 需求: 统一故障恢复
**模块:** Messaging、Order、Payment、Inventory、Fulfillment

#### 场景: 重复、宕机、发布失败和 DLQ

- 消费者按 `(consumer, event_id)` 唯一处理，业务成功和 Inbox 完成后才 Ack。
- 可恢复错误进入有限 retry queue；非法 payload、未知事件/不支持版本和超过上限的消息进入对应 DLQ。
- DLQ 重放使用原始事件 ID，恢复后的订单、库存、支付和履约命令保持幂等。

## 风险评估

- **风险:** 删除旧入口和旧进程会破坏仍在使用旧接口的客户端。
  **缓解:** 已确认没有外部系统依赖旧队列；本仓库测试和 README 全部迁移到 `/api/v1`，旧路径明确返回 404。
- **风险:** 删除旧代码与旧表清理混在一起会造成不可逆数据损失。
  **缓解:** 只删除运行时和代码装配，旧表不 DROP、不 TRUNCATE、不迁移覆盖；后续数据清理另行审批。
- **风险:** 新事件消费者与现有同步 gRPC 可能重复执行状态转换。
  **缓解:** Inbox 唯一键、业务状态前置检查和现有幂等命令共同防重；事件用于可靠通知和恢复，不创建第二套状态机。
- **风险:** Docker 当前不可用，无法在本轮完成真实 RabbitMQ 故障注入。
  **缓解:** Fake Broker、Memory E2E、Compose 静态解析和 migration 测试先作为门禁；真实 RabbitMQ 验收保留可重复入口。
