# 任务清单: Catalog 与 Inventory/Seckill 服务拆分

目录: `helloagents/plan/202608060842_catalog_inventory_services/`

## 1. Catalog Service

- [√] 1.1 扩展 `proto/commerce/catalog.proto` 的商品分页和详情 RPC，重新生成 Go 代码。
- [√] 1.2 在 `catalog_service/` 实现独立领域模型、MemoryRepository、MySQLRepository 和 gRPC server。
- [√] 1.3 在 `cmd/catalog-service/` 增加独立启动入口、配置加载、健康检查和 etcd 注册。

## 2. Inventory/Seckill Service

- [√] 2.1 在 `inventory_service/` 实现 reservation 状态机和并发安全 MemoryStore。
- [√] 2.2 实现 Redis Lua Store，覆盖 Reserve、Confirm、Release、AdmitSeckill 和重复请求。
- [√] 2.3 增加独立 gRPC server 与 `cmd/inventory-service/` 启动入口。

## 3. Gateway 切换

- [√] 3.1 增加 Catalog/Inventory gRPC 客户端和服务发现配置。
- [√] 3.2 将 `GET /api/v1/products`、`GET /api/v1/products/:id` 优先切换到 Catalog，Commerce 保留 NoRoute 过渡代理。
- [√] 3.3 保留 `/api/v1/seckill/orders` 兼容代理并明确其完整 Inventory 切换依赖 Order Service 阶段，禁止双重扣减。

## 4. 安全与文档

- [√] 4.1 检查服务端参数校验、gRPC deadline、错误信息、Redis/DSN/Token 日志和旧表访问边界。
- [√] 4.2 更新 README、架构、数据、Catalog/Product/Messaging 模块文档和 CHANGELOG。

## 5. 测试与验收

- [√] 5.1 增加 Catalog/Inventory 单元测试和 bufconn Fake E2E，覆盖库存不足、重复预占、重复释放、确认后释放和秒杀限购。
- [√] 5.2 运行 `go test ./...`、`go vet ./...`、`go test -race ./...`、`git diff --check`；Docker 不可用时记录真实集成跳过项。
- [√] 5.3 阶段验收：Catalog gRPC 查询已切换，Inventory gRPC 状态机可独立验证，秒杀完整订单切换边界清晰。

## 执行总结

- Catalog 查询已由 Gateway 通过 gRPC 调用独立 Catalog Service。
- Inventory/Seckill 已具备 MemoryStore、Redis Lua Store、状态机和独立 gRPC 入口。
- `/api/v1/seckill/orders` 仍保留 Commerce 兼容代理，待 Order Service 统一编排后再切换，避免双重扣库存。
- Docker daemon 在当前环境不可用，真实 MySQL/Redis/RabbitMQ/Compose 集成跳过；内存、bufconn、静态检查和受控端到端验收通过。
