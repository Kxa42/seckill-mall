# 变更历史索引

本文件记录所有已完成变更的索引，便于追溯和查询。

## 索引

| 时间戳 | 功能名称 | 类型 | 状态 | 方案包路径 |
|--------|----------|------|------|------------|
| 202608051526 | backend_commerce_mvp | 功能 | ✅已完成（28完成/6跳过） | [方案包](2026-08/202608051526_backend_commerce_mvp/) |
| 202608060842 | catalog_inventory_services | 功能 | ✅已完成（17完成） | [方案包](2026-08/202608060842_catalog_inventory_services/) |
| 202608061041 | contract_config_hardening | 修复 | ✅已完成（19完成） | [方案包](2026-08/202608061041_contract_config_hardening/) |

## 按月归档

### 2026-08

- [202608051526_backend_commerce_mvp](2026-08/202608051526_backend_commerce_mvp/) - 完成商城后端 MVP；真实 Docker/MySQL 集成与旧秒杀最终迁移按记录跳过。
- [202608060842_catalog_inventory_services](2026-08/202608060842_catalog_inventory_services/) - 拆分 Catalog 与 Inventory/Seckill 服务，切换商品查询并完成内存 Fake 验收；秒杀订单完整切换留待 Order Service 阶段。
- [202608061041_contract_config_hardening](2026-08/202608061041_contract_config_hardening/) - 补齐事件版本前向兼容、共享契约运行时接入和按服务配置加载；真实基础设施联调因 Docker 不可用跳过。
