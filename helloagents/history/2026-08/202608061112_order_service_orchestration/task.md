# 任务清单: 第三阶段唯一 Order Service 订单编排

目录: `helloagents/history/2026-08/202608061112_order_service_orchestration/`

> **执行边界:** 本方案按增量式微服务迁移执行。阶段 3 不实现前端，不删除旧秒杀兼容服务，不接入完整 RabbitMQ Outbox/Inbox；Docker 不可用时以 Memory/Fake/bufconn 完成可重复验收。

## 1. gRPC 契约与服务配置

- [√] 1.1 在 `proto/commerce/commerce_order.proto` 中以向后兼容方式补齐 `List`、`Cancel`、支付确认、发货、确认收货和退款过渡操作，定义统一错误和幂等响应，验证 [统一订单创建](why.md#需求-统一订单创建) 与 [订单生命周期和权限](why.md#需求-订单生命周期和权限)。
- [√] 1.2 在 `proto/commerce/inventory.proto` 中补充秒杀请求的 `order_id`/活动绑定语义或等价绑定 RPC，明确重复调用、库存数量和 reservation 状态不变的契约，验证 [秒杀订单创建成功](why.md#场景-秒杀订单创建成功)。
- [√] 1.3 重新生成 `common/pb/commerce_order*.go`、`common/pb/inventory*.go` 并补充 proto 合约测试，验证旧客户端字段兼容、未知可选字段忽略和新客户端不重复扣库存，依赖任务 1.1、1.2。
- [√] 1.4 使 `cmd/order-service` 消费 `SECKILL_SERVICES_CONFIG` 的 `order` 配置，并加载 Order、Catalog、Identity、Inventory 的独立地址/DSN/etcd 配置；配置缺失时必须 not-ready，验证 [新订单表只由 Order Service 写入](why.md#场景-新订单表只由-order-service-写入)。

## 2. Order 数据模型与所有权迁移

- [√] 2.1 新增 `migrations/002_order_service.sql`，以非破坏方式补充 `order_items.reservation_id`、`order_create_intents`、`order_operations` 及查询/重试索引，禁止删除旧表、清空数据或自动迁移，验证 [迁移策略](how.md#服务边界与数据模型)。
- [√] 2.2 在 `internal/order/model.go`、`internal/order/repository.go`、`internal/order/mysql_repository.go` 中实现只访问 Order 自有表的模型和事务 Repository，验证 [订单数据唯一所有权](why.md#需求-订单数据唯一所有权)，依赖任务 2.1。
- [√] 2.3 在 `internal/order/memory_repository.go`、`internal/order/memory_repository_test.go` 中实现内存幂等、订单项快照、操作记录和并发安全行为，验证 [重复请求和参数冲突](why.md#场景-重复请求和参数冲突)。
- [√] 2.4 对 `internal/commerce/mysql_repository.go`、`internal/commerce/repository.go` 的订单写路径做隔离改造或删除其新链路入口，确保新 Order Service 不调用旧 Repository，也不直接写 `skus`、`user_addresses`、`inventory_reservations` 和旧 `orders`，验证 [新订单表只由 Order Service 写入](why.md#场景-新订单表只由-order-service-写入)。

## 3. Order 领域与跨服务编排

- [√] 3.1 在 `internal/order/domain.go`、`internal/order/domain_test.go` 中提取 `pending_payment`、`paid`、`shipped`、`completed`、`canceled`、退款相关状态及统一转换规则，覆盖普通和秒杀两种订单类型，验证 [普通订单创建成功](why.md#场景-普通订单创建成功) 与 [秒杀订单创建成功](why.md#场景-秒杀订单创建成功)。
- [√] 3.2 在 `internal/order/clients.go`、`internal/order/fakes.go`、`internal/order/clients_test.go` 中定义 Catalog/Identity/Inventory 客户端接口、deadline、错误映射和 Fake 调用计数，验证下游超时、NotFound、OutOfStock 和重复命令行为。
- [√] 3.3 在 `internal/order/service.go`、`internal/order/service_test.go` 中实现统一订单创建：校验请求摘要、获取 SKU/地址快照、按 SKU 稳定排序预占、保存快照和状态历史，验证 [普通订单创建成功](why.md#场景-普通订单创建成功) 与 [秒杀订单创建成功](why.md#场景-秒杀订单创建成功)，依赖任务 2.2、3.1、3.2。
- [√] 3.4 在 `internal/order/compensation.go`、`internal/order/compensation_test.go`、`internal/order/operation_worker.go` 中实现部分预占释放、支付确认重试、有限退避和 `compensation_pending` 状态，验证 [部分预占失败](why.md#场景-部分预占失败)。
- [√] 3.5 增加创建意图恢复逻辑，确保同一 `(user_id, Idempotency-Key, request_digest)` 使用同一 Order ID 和 reservation ID；不同摘要返回冲突且不调用 Inventory，验证 [重复请求和参数冲突](why.md#场景-重复请求和参数冲突)。

## 4. Identity 地址快照适配

- [√] 4.1 在 `internal/order/identity_client.go`、`internal/order/identity_client_test.go` 中实现 `IdentityService.GetAddressSnapshot` 客户端和内存 Fake，禁止暴露完整地址到普通日志，验证 [普通订单创建成功](why.md#场景-普通订单创建成功)。
- [√] 4.2 新增 `identity_service/server.go`、`identity_service/repository.go` 和 `cmd/identity-snapshot-service/main.go`，提供阶段 3 最小地址快照 gRPC 适配器；真实实现只能读取身份数据，Order 不获得身份 DSN，验证 [Identity 使用地址快照 gRPC 兼容适配](how.md#adr-013-identity-使用地址快照-grpc-兼容适配)。
- [√] 4.3 为 Identity 适配器增加用户归属校验、地址不存在错误和 Memory 启动模式，验证 [查询和管理员操作](why.md#场景-查询和管理员操作)。

## 5. Order gRPC 服务与独立进程

- [√] 5.1 在 `internal/order/grpc_server.go`、`internal/order/grpc_server_test.go`、`internal/order/error_mapping.go` 中实现新契约服务端和稳定 gRPC 错误映射，验证 [统一订单创建](why.md#需求-统一订单创建) 与 [订单生命周期和权限](why.md#需求-订单生命周期和权限)。
- [√] 5.2 新增 `cmd/order-service/main.go`、`cmd/order-service/dependencies.go`、`cmd/order-service/expiry_worker.go`，实现独立监听、服务发现、readiness、优雅停机和超时关单 worker，验证 [超时关单](why.md#场景-取消和超时关单)。
- [√] 5.3 在 `internal/order/internal_auth.go`、`internal/order/internal_auth_test.go`、`api_gateway/internal_auth.go` 中实现 Gateway 到 Order 的签名 metadata 校验，覆盖过期签名、身份不一致、普通用户伪造 admin 和缺少签名拒绝。

## 6. 订单生命周期与过渡支付/履约

- [√] 6.1 在 `internal/order/service.go`、`internal/order/lifecycle_test.go`、`internal/order/repository.go` 中实现 List/Get/Cancel/Expire，先幂等释放 reservation 再提交订单状态，验证 [取消和超时关单](why.md#场景-取消和超时关单)。
- [√] 6.2 在 `internal/order/payment_transition.go`、`internal/order/payment_transition_test.go`、`internal/commerce/httpapi/router.go` 中将 Mock 支付成功后的订单状态确认改为 Order Service 调用，保留支付表过渡能力并增加失败重试，禁止 Commerce 直接修改订单状态。
- [√] 6.3 在 `internal/order/fulfillment_transition.go`、`internal/order/fulfillment_transition_test.go`、`internal/commerce/httpapi/router.go` 中将发货、确认收货和退款相关订单状态转换改为 Order Service 显式操作，验证用户/管理员权限和非法状态转换。
- [√] 6.4 在 `cmd/commerce-api/main.go`、`internal/platform/config/commerce.go`、`internal/commerce/httpapi/router.go` 中增加订单写入兼容模式开关；新链路启用时关闭 Commerce 订单写入和 expiry worker，回退时要求先停止 Order 写入，验证 [新链路切换](why.md#场景-新链路切换) 与 [回退](why.md#场景-回退)。

## 7. Gateway 路由切换

- [√] 7.1 在 `api_gateway/clients.go`、`api_gateway/order_client.go`、`api_gateway/server.go` 中接入 Order gRPC Client、服务发现/direct address 和 deadline，验证 [受控切流与回退](why.md#需求-受控切流与回退)。
- [√] 7.2 新增 `api_gateway/order_routes.go`、`api_gateway/order_routes_test.go` 和 `api_gateway/routes.go`，显式注册 `/api/v1/orders`、`/api/v1/orders/:order_id`、取消、列表和 `/api/v1/seckill/orders`，确保显式路由优先于 Commerce `NoRoute` 代理。
- [√] 7.3 在 `api_gateway/commerce_proxy.go`、`api_gateway/routes_test.go`、`api_gateway/clients_test.go` 中验证 Gateway 不再为秒杀请求单独调用 Inventory，旧 `/order` 仍进入 Legacy Order，新增 `/api/v1` 链路不访问旧 Order/Commerce Repository。
- [√] 7.4 增加 Gateway 响应映射，统一处理订单创建、幂等复用、库存不足、权限不足、下游不可用和状态冲突，验证 [查询和管理员操作](why.md#场景-查询和管理员操作)。

## 8. 阶段验收测试

- [√] 8.1 通过 `tests/order_service_memory_e2e.sh` 与 `api_gateway/order_routes_test.go` 的 Memory/Fake/bufconn 串联 Gateway、Order、Catalog、Identity 和 Inventory，覆盖普通/秒杀创建、查询、取消和超时。
> 备注: 未另建重复的 `tests/order_service_memory_e2e_test.go`，等价 Go 验收落在 Gateway 包，脚本作为统一入口调用。
- [√] 8.2 增加回归测试，验证相同幂等请求不新增订单/预占、不同摘要冲突、部分预占失败会释放已成功 reservation、释放失败进入可重试状态，验证 [幂等与失败补偿](why.md#需求-幂等与失败补偿)。
- [√] 8.3 增加权限和状态机测试，验证用户隔离、admin 发货、非法转换、重复支付确认、重复释放和旧表零写入，验证 [订单生命周期和权限](why.md#需求-订单生命周期和权限)。
- [√] 8.4 运行 `go test ./...`、`go vet ./...`、`go test -race ./...`、`gofmt` 和 `git diff --check`；记录所有失败项和修复结果。
- [-] 8.5 检查 Docker daemon；可用时执行 MySQL migration 重放、Redis/etcd/Compose 健康检查和真实 gRPC E2E；不可用时明确跳过真实基础设施项，不把 Fake 结果等同于真实集成验收。
> 备注: Docker daemon socket 无权限，真实 MySQL migration、Redis/etcd/RabbitMQ、Compose 启动和真实 gRPC E2E 按用户约束跳过；仅完成 Compose 静态配置校验。

## 9. 安全检查

- [√] 9.1 检查 Order gRPC 的内部签名、用户归属、admin 角色、请求体 `user_id` 覆盖、deadline、重试上限和服务发现配置，验证 [内部调用身份](how.md#api-设计)。
- [√] 9.2 检查地址快照、Token、密钥、DSN、支付签名和 gRPC metadata 的日志脱敏；确认迁移脚本无 `DROP`、`TRUNCATE`、清空或生产连接行为。
- [√] 9.3 检查库存补偿毒丸操作、孤儿创建意图、重复回调和操作记录清理策略，确保失败后可查询、可重试且不会无限循环。
> 备注: 创建意图和补偿操作最多尝试 8 次，最终失败记录保留供查询；本阶段采用保留策略，物理归档/清理留到可观测与运维阶段。

## 10. 文档更新

- [√] 10.1 更新 `helloagents/wiki/arch.md`、`helloagents/wiki/data.md`、`helloagents/wiki/api.md`，记录 Order 唯一写入者、Identity 过渡适配、Inventory 调用关系和切流/回退顺序。
- [√] 10.2 更新 `helloagents/wiki/modules/order.md`、`helloagents/wiki/modules/commerce.md`、`helloagents/wiki/modules/gateway.md`、`helloagents/wiki/modules/inventory.md`，同步新订单状态机、服务依赖和已知边界。
- [√] 10.3 更新 `README.md`、`helloagents/project.md`、`helloagents/CHANGELOG.md`，注明新增启动方式、Fake/Memory 验收入口和 Docker 不可用时的跳过项。
- [√] 10.4 开发实施完成后，将本方案包任务状态更新为 `[√]` 或实际失败状态，迁移至 `helloagents/history/2026-08/202608061112_order_service_orchestration/` 并更新 `helloagents/history/index.md`。

## 执行总结

- 完成 38 项，跳过 1 项，无失败项。
- 代码验收通过：`go test ./...`、`go vet ./...`、`go test -race ./...`、`gofmt -l`、`git diff --check`、Order Memory/bufconn E2E。
- 追加回归修复：不同幂等键的相同请求生成独立订单；释放补偿成功后自动完成订单取消收口。
- 追加恢复窗口修复：订单已落库但创建意图未完成时，幂等命中会将意图收口为 `done`。
- 原商城进程级 Memory E2E 因当前沙箱禁止监听本地 TCP 端口未执行完成；其领域、HTTP 和 gRPC 路由测试均已由 `go test ./...` 覆盖。
- Docker daemon 不可用，真实基础设施集成按约束跳过。
