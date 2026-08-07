# 技术设计: Order 客户端完全服务私有化

## 技术方案

### Payment 私有 Order 客户端

在 `services/payment/internal/app/order_client.go` 定义：

- `OrderSummary`：只保留 `OrderID`、`UserID`、`Status`、`TotalAmountCents`。
- `OrderTransition`：只保留 Payment 对返回结果需要的订单号、状态和幂等复用标记。
- `OrderClient`：只声明 `Get`、`ConfirmPayment`、`Refund`。
- `GRPCOrderClient`：持有生成的 `pb.CommerceOrderServiceClient` 和内部调用密钥。

Payment 的 `model.go`、服务入口和测试全部改用本包类型；`testkit` 仅转出 `NewGRPCOrderClient` 供 Gateway Memory E2E 使用。

### Fulfillment 私有 Order 客户端

在 `services/fulfillment/internal/app/order_client.go` 定义：

- `OrderClient`：只声明 `Get(ctx, userID, orderID) error`、`Ship` 和 `ConfirmReceipt`。
- `OrderTransition`：只保留履约响应需要的订单号、状态和幂等复用标记。
- `GRPCOrderClient`：使用 `pb.CommerceOrderServiceClient`，复用 `shared/platform/internalcall` 完成用户/管理员签名。

Fulfillment 的 `Get` 不返回 Payment 所需的金额和订单汇总模型，因为当前调用方只关心订单归属校验。服务入口、模型、Server、测试和 `testkit` 同步切换到私有类型。

### 共享职责边界

```text
services/payment/internal/app/order_client.go
  ├─ Payment OrderClient
  ├─ Payment gRPC 适配器
  └─ Payment 错误映射

services/fulfillment/internal/app/order_client.go
  ├─ Fulfillment OrderClient
  ├─ Fulfillment gRPC 适配器
  └─ Fulfillment 错误映射

shared/gen/commerce       -> 仅共享协议与生成客户端
shared/platform/internalcall -> 仅共享 HMAC metadata 能力
```

不新增通用 `shared` Order 领域模型，不把两个适配器重新抽到一个基础包中；少量重复代码是服务自治边界的有意成本。

## 架构决策 ADR

### ADR-006: Order 客户端适配器归属消费者服务

**上下文:** `shared/clients/order` 同时服务 Payment 与 Fulfillment，接口包含两个消费者的联合能力，测试替身被迫实现无关方法。
**决策:** 删除共享 Order 客户端；Payment 和 Fulfillment 在各自 `internal/app` 定义窄接口、业务模型和 gRPC 适配器。共享层只保留 Protobuf 生成代码和通用内部签名工具。
**理由:** 接口由消费者需求决定，降低跨服务业务耦合，允许两个服务独立演进和测试。
**替代方案:** 保留共享客户端并拆成 `PaymentOrderClient`/`FulfillmentOrderClient` 两个接口 → 拒绝原因: 仍会让共享层持有 Order 消费者业务模型和适配器，服务边界没有完全收敛。
**影响:** Payment/Fulfillment 各增加少量适配器代码；Gateway E2E 需要通过各服务 `testkit` 获取测试构造器；运行时协议和安全策略不变。

## 安全与性能

- 每个私有 gRPC 适配器必须继续绑定完整方法名、调用用户/角色、时间戳和内部 HMAC，不能退化为无签名调用。
- 保持 3 秒客户端 deadline、现有错误码映射和空客户端防护。
- 不复制密钥，不在客户端结构或日志中输出内部密钥。
- 适配器拆分只改变编译边界，不改变网络调用次数和运行时复杂度。

## 测试与部署

- 更新 Payment/Fulfillment 单元测试，使 fake 只实现本服务窄接口。
- 为两个私有 gRPC 适配器增加签名、deadline/错误映射和关键方法调用测试；Gateway bufconn E2E 验证完整业务链路不变。
- 更新 `scripts/check_boundaries.sh`，明确禁止生产代码依赖 `shared/clients/order` 和服务间私有 Order 适配器。
- 运行 `make fmt-check`、`make boundaries`、`go test ./...`、`go vet ./...`、`go build ./services/... ./tools/...` 和 `bash tests/stage5_memory_e2e.sh`。
