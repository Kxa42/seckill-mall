# 变更历史索引

本文件记录所有已完成变更的索引，便于追溯和查询。

## 索引

| 时间戳 | 功能名称 | 类型 | 状态 | 方案包路径 |
|--------|----------|------|------|------------|
| 202608051526 | backend_commerce_mvp | 功能 | ✅已完成（28完成/6跳过） | [方案包](2026-08/202608051526_backend_commerce_mvp/) |
| 202608060800 | microservice_mall_migration | 架构迁移 | ✅已完成（35完成） | [方案包](2026-08/202608060800_microservice_mall_migration/) |
| 202608060842 | catalog_inventory_services | 功能 | ✅已完成（17完成） | [方案包](2026-08/202608060842_catalog_inventory_services/) |
| 202608061041 | contract_config_hardening | 修复 | ✅已完成（19完成） | [方案包](2026-08/202608061041_contract_config_hardening/) |
| 202608061112 | order_service_orchestration | 功能 | ✅已完成（38完成/1跳过） | [方案包](2026-08/202608061112_order_service_orchestration/) |
| 202608061319 | stage4_domain_services | 功能 | ✅已完成（42完成，真实基础设施跳过） | [方案包](2026-08/202608061319_stage4_domain_services/) |
| 202608061539 | stage5_messaging | 方案清理 | [-]未执行（统一方案替代） | [方案包](2026-08/202608061539_stage5_messaging/) |
| 202608061551 | unified_stage5_messaging | 架构迁移 | ✅已完成（35完成/1跳过） | [方案包](2026-08/202608061551_unified_stage5_messaging/) |
| 202608070731 | repository_layout_refactor | 架构重构 | ✅已完成（11完成） | [方案包](2026-08/202608070731_repository_layout_refactor/) |
| 202608070804 | service_first_monorepo | 架构重构 | ✅已完成（14完成） | [方案包](2026-08/202608070804_service_first_monorepo/) |
| 202608070836 | private_order_clients | 架构重构 | ✅已完成（11完成） | [方案包](2026-08/202608070836_private_order_clients/) |
| 202608081459 | extract_appkit_scaffold | 重构 | ✅已完成（11完成） | [方案包](2026-08/202608081459_extract_appkit_scaffold/) |
| 202608091438 | fix_core_bugs | 修复 | ✅已完成（26完成） | [方案包](2026-08/202608091438_fix_core_bugs/) |
| 202608100829 | architecture_diagram | 文档 | ✅已完成（轻量迭代） | [方案包](2026-08/202608100829_architecture_diagram/) |
| 202608100839 | service_layout_cleanup | 重构 | ✅已完成（轻量迭代） | [方案包](2026-08/202608100839_service_layout_cleanup/) |
| 202608100911 | redis_rabbitmq_guide | 文档 | ✅已完成（轻量迭代） | [方案包](2026-08/202608100911_redis_rabbitmq_guide/) |
| 202608101524 | project_quality_hardening | 修复/重构 | ✅已完成（23完成/2备注） | [方案包](2026-08/202608101524_project_quality_hardening/) |

## 按月归档

### 2026-08

- [202608051526_backend_commerce_mvp](2026-08/202608051526_backend_commerce_mvp/) - 完成商城后端 MVP；真实 Docker/MySQL 集成与旧秒杀最终迁移按记录跳过。
- [202608060800_microservice_mall_migration](2026-08/202608060800_microservice_mall_migration/) - 完成从阶段1到阶段5的统一商城微服务迁移，归档总任务表及各阶段验收记录。
- [202608060842_catalog_inventory_services](2026-08/202608060842_catalog_inventory_services/) - 拆分 Catalog 与 Inventory/Seckill 服务，切换商品查询并完成内存 Fake 验收；秒杀订单完整切换留待 Order Service 阶段。
- [202608061041_contract_config_hardening](2026-08/202608061041_contract_config_hardening/) - 补齐事件版本前向兼容、共享契约运行时接入和按服务配置加载；真实基础设施联调因 Docker 不可用跳过。
- [202608061112_order_service_orchestration](2026-08/202608061112_order_service_orchestration/) - 建立唯一 Order Service，切换普通/秒杀订单 gRPC 编排，补齐退款恢复、创建意图恢复和 Memory/bufconn 验收；真实基础设施因 Docker 不可用跳过。
- [202608061319_stage4_domain_services](2026-08/202608061319_stage4_domain_services/) - 拆分 Identity、Cart、Payment、Fulfillment，Gateway 显式切换商城领域路由，完成 Memory/Fake/bufconn 验收；真实基础设施因 Docker daemon 不可用跳过。
- [202608061539_stage5_messaging](2026-08/202608061539_stage5_messaging/) - 原阶段5草案未执行，因统一设计方案已覆盖其目标而归档清理。
- [202608061551_unified_stage5_messaging](2026-08/202608061551_unified_stage5_messaging/) - 统一商城事件协议和服务级 Outbox/Inbox，移除旧运行时与旧 HTTP 入口，补齐 Inventory Redis Stream 到 RabbitMQ 桥接和事件消费者；Memory/race/vet/Compose 静态验收通过，真实 RabbitMQ、Compose 运行和 migration 因 Docker daemon 不可用跳过。
- [202608070731_repository_layout_refactor](2026-08/202608070731_repository_layout_refactor/) - 统一 `cmd`、`internal`、契约、生成代码和平台目录，移除无引用遗留文件并通过完整工程门禁。
- [202608070804_service_first_monorepo](2026-08/202608070804_service_first_monorepo/) - 八个服务收敛为自治目录，共享协议与平台能力迁入 `shared`，保留单 Go module 并通过完整工程、边界和 Compose 静态验收。
- [202608070836_private_order_clients](2026-08/202608070836_private_order_clients/) - 删除共享 Order 业务客户端，Payment/Fulfillment 各自维护窄接口和 gRPC 适配器，并通过签名、deadline、边界和 Memory E2E 验证。
- [202608081459_extract_appkit_scaffold](2026-08/202608081459_extract_appkit_scaffold/) - 抽取服务启动脚手架至 `shared/platform/appkit`，七个服务统一契约校验、etcd 注册、健康检查与优雅停机。
- [202608091438_fix_core_bugs](2026-08/202608091438_fix_core_bugs/) - 修复四个核心缺陷（发货后退款、Redis 库存播种、reservation 幂等状态校验、Inbox 重试计数/忙循环）并按序移除无引用死代码；全仓 build/vet/race/边界/Memory E2E 通过。
- [202608100829_architecture_diagram](2026-08/202608100829_architecture_diagram/) - 新增架构示意图文档，覆盖组件功能、gRPC/事件关联、数据所有权与秒杀链路时序。
- [202608100839_service_layout_cleanup](2026-08/202608100839_service_layout_cleanup/) - 统一 Cart/Payment/Fulfillment/Identity 内部文件组织为模型/接口/服务/存储分层，行为不变，全仓门禁通过。
- [202608100911_redis_rabbitmq_guide](2026-08/202608100911_redis_rabbitmq_guide/) - 新增 Redis 与 RabbitMQ 深度解读文档，覆盖 Outbox/Inbox、Publisher Confirm、重试/DLQ 与 Redis Lua 热路径。
- [202608101524_project_quality_hardening](2026-08/202608101524_project_quality_hardening/) - 质量加固：修复 Outbox 信封重建发布链路、SQL/Memory claim 确定性与 retry deferred confirm；抽取服务级消息运行时，appkit 增加 HTTP/metrics 生命周期，Gateway 显式关闭 gRPC/etcd 客户端并清理 debug 登录配置；全仓 race/vet/边界/Memory E2E 通过，RabbitMQ 真实集成因 Docker 不可用跳过。
