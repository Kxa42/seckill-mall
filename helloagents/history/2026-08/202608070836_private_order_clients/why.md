# 变更提案: Order 客户端完全服务私有化

## 需求背景

服务优先目录已经将 Payment 和 Fulfillment 拆为独立服务，但两者仍共同依赖 `shared/clients/order`。该包同时承载两个消费者的业务模型、接口、gRPC 适配、超时、HMAC 签名和错误映射，导致服务边界被共享业务抽象反向耦合。

## 变更内容

1. 删除 `shared/clients/order`，不再提供跨服务共享的 Order 业务客户端。
2. 在 Payment 私有 `internal/app` 中定义仅覆盖支付场景的 Order 接口、模型和 gRPC 适配器。
3. 在 Fulfillment 私有 `internal/app` 中定义仅覆盖履约场景的 Order 接口、模型和 gRPC 适配器。
4. 仅共享 `shared/gen/commerce` 的 Protobuf 代码和 `shared/platform/internalcall` 的通用签名能力。
5. 通过各服务 `testkit` 为 Gateway Memory E2E 提供测试组装入口，不让 Gateway 生产代码依赖服务私有实现。

## 影响范围

- **模块:** Payment、Fulfillment、Gateway E2E、共享层、边界审计和知识库。
- **文件:** Order 客户端适配器、服务模型/入口/测试、两个服务 testkit、边界脚本和架构文档。
- **API:** HTTP、gRPC 方法、事件名称和错误语义保持不变。
- **数据:** 不修改数据库表、migration 或事件 payload。

## 核心场景

### 需求: Payment 只依赖支付所需的 Order 能力
**模块:** Payment Service

#### 场景: 创建支付、支付回调和退款
- Payment 的服务接口只依赖 `Get`、`ConfirmPayment` 和 `Refund`。
- Payment 的 gRPC 适配器在本服务私有目录中负责 3 秒 deadline、HMAC 用户身份签名和错误转换。
- Payment 测试替身不再实现 `Ship` 或 `ConfirmReceipt`。

### 需求: Fulfillment 只依赖履约所需的 Order 能力
**模块:** Fulfillment Service

#### 场景: 发货、收货和订单归属校验
- Fulfillment 的服务接口只依赖 `Get`、`Ship` 和 `ConfirmReceipt`。
- 仅用于归属校验的 `Get` 不暴露 Payment 的金额模型。
- Fulfillment 测试替身不再实现 `ConfirmPayment` 或 `Refund`。

### 需求: 共享层不保存业务消费者适配器
**模块:** Shared、Gateway

#### 场景: 编译和内存 E2E
- 生产代码仍可使用共享 Protobuf 和内部签名基础设施，但不能导入 `shared/clients/order`。
- Gateway E2E 通过 Payment/Fulfillment `testkit` 获取各自私有适配器的测试组装入口。
- API、签名字段、deadline、状态码映射和 Memory E2E 行为保持不变。

## 风险评估

- **风险:** 两个私有适配器会产生少量重复 gRPC 调用代码。
- **缓解:** 仅复制与各自业务能力相关的最小代码；通用 HMAC 逻辑继续复用 `shared/platform/internalcall`。
- **风险:** Gateway E2E 无法直接访问另一个服务的 `internal`。
- **缓解:** 扩展两个服务的 `testkit`，只暴露测试构造器，不进入生产依赖。
