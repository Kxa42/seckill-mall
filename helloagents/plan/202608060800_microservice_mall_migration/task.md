# 任务清单: 微服务商城与秒杀统一架构迁移

目录: `helloagents/plan/202608060800_microservice_mall_migration/`

## 1. 阶段1：契约与边界基线

- [√] 1.1 新增服务边界、事件类型和数据所有权的共享契约包，禁止依赖具体数据库实现。
- [√] 1.2 新增 Catalog、Inventory/Seckill、Order、Identity、Cart、Payment、Fulfillment 的版本化 gRPC proto；保留旧 proto 兼容。
- [√] 1.3 新增事件信封、事件版本和幂等键测试，覆盖未知事件类型、已知事件未来 `EventVersion` 和重复事件。
  > 备注: 初始阶段仅覆盖未知事件类型和重复事件；已在 `202608061041_contract_config_hardening` 中补充已知事件未来版本与版本独立演进测试。
- [√] 1.4 拆分配置约定，明确每个服务独立地址、DSN、RabbitMQ 和 etcd 配置。

## 2. 阶段2：Catalog 与 Inventory/Seckill 服务

- [ ] 2.1 从 `internal/commerce` 提取 Catalog Repository 和服务端，建立商品快照 gRPC。
- [ ] 2.2 改造现有 Product Service，明确 Catalog 与 Inventory 数据所有权，保留 Redis Lua 秒杀能力。
- [ ] 2.3 实现统一库存 `Reserve/Confirm/Release` gRPC，并增加普通库存并发测试。
- [ ] 2.4 将 `/api/v1/products` 和 `/api/v1/seckill/orders` 切换到目标服务。
- [ ] 2.5 阶段验收：商品查询、库存预占/释放、Redis Lua 限购、Gateway gRPC 调用和 Fake E2E。

## 3. 阶段3：唯一 Order Service

- [ ] 3.1 将 `commerce_orders`、订单项和状态历史迁移为 Order Service 所有表。
- [ ] 3.2 改造现有 Order Service，使普通订单和秒杀订单共用新的订单状态机。
- [ ] 3.3 增加 Catalog、Identity、Inventory gRPC 客户端和失败补偿。
- [ ] 3.4 将 `/api/v1/orders`、查询、取消和超时任务切换到 Order Service。
- [ ] 3.5 阶段验收：幂等创建、库存失败回滚、状态转换、权限边界和旧订单表不再被新链路写入。

## 4. 阶段4：Identity、Cart、Payment、Fulfillment 服务

- [ ] 4.1 提取 Identity Service，迁移用户、Token 和地址能力。
- [ ] 4.2 提取 Cart Service，迁移购物车和结算预览能力。
- [ ] 4.3 提取 Payment Service，迁移 Mock 支付、回调幂等和退款能力。
- [ ] 4.4 提取 Fulfillment Service，迁移发货、收货和物流数据。
- [ ] 4.5 阶段验收：注册到收货完整链路、支付回调重复、退款幂等和服务独立启动。

## 5. 阶段5：Outbox/Inbox 与 RabbitMQ

- [ ] 5.1 为各服务增加本地 Outbox Publisher 和统一事件信封。
- [ ] 5.2 增加 Order、Payment、Inventory、Fulfillment 消费者和 Inbox 唯一约束。
- [ ] 5.3 将秒杀准入、支付成功、取消、释放、发货和退款接入 RabbitMQ。
- [ ] 5.4 增加发布重试、publisher confirm、手动 Ack、DLQ 和补偿逻辑。
- [ ] 5.5 阶段验收：重复消息、服务宕机、发布失败、DLQ 和恢复后重放。

## 6. 阶段6：Gateway、部署与收口

- [ ] 6.1 Gateway 按 `/api/v1` 领域路由调用目标服务，不再代理 `commerce-api` 业务 Repository。
- [ ] 6.2 逐步下线旧 `/order`、旧 `orders/product` 跨表 MQ Consumer 和重复表写入。
- [ ] 6.3 更新 Docker Compose、migration、OpenAPI、README 和知识库架构图。
- [ ] 6.4 完成全部自动化测试、静态检查和可用 Docker 环境下的真实集成验收。
- [ ] 6.5 记录 Docker 不可用时被跳过的项目和后续验收入口。

## 7. 安全检查

- [ ] 7.1 检查服务端授权、PII/Token/支付签名日志、配置密钥和事件 payload。
- [ ] 7.2 检查跨服务调用 deadline、幂等键、重试上限、DLQ 毒丸消息和库存补偿。

## 8. 文档更新

- [ ] 8.1 更新 `helloagents/wiki/arch.md`、`data.md`、`api.md` 和各模块文档。
- [ ] 8.2 更新 `README.md`、`helloagents/project.md`、`helloagents/CHANGELOG.md` 和历史索引。

## 9. 测试

- [ ] 9.1 每个阶段运行 `go test ./...`、`go vet ./...`、必要范围的 `go test -race`。
- [ ] 9.2 每个阶段运行 Fake/Memory E2E；Docker 可用时追加 MySQL、Redis、RabbitMQ 和 Compose 验收。
