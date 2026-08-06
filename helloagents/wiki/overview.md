# Go-Seckill-Mall

> 本文件描述代码当前已经实现的能力；前端和后续兼容迁移不计入已完成范围。

## 1. 项目概述

### 目标与背景
项目在原有高并发秒杀、Outbox、死信补偿和可观测能力之上，正在将完整商城后端 MVP 渐进式迁移为统一微服务架构。新 API 覆盖身份、地址、商品目录、购物车、结算、统一订单、Mock 支付、履约和退款。

### 范围
- **当前范围内:** `/api/v1` 商城 API、旧秒杀兼容 API、MySQL 持久化、内存验收模式、版本化 migration、容器编排和可观测性。
- **当前范围外:** 商城前端、真实支付渠道、营销优惠、复杂售后审批，以及旧 Redis/MQ 秒杀消息到新交易域的最终切换。

### 干系人
- **负责人:** 项目维护者。

## 2. 模块索引

| 模块名称 | 职责 | 状态 | 文档 |
|---------|------|------|------|
| Commerce API | 迁移期回退和旧兼容适配层 | 🚧 第 4 阶段回退 | [commerce](modules/commerce.md) |
| Identity Service | 注册、登录、Token、地址和订单地址快照 gRPC | 🚧 第 4 阶段已拆分 | [identity](modules/identity.md) |
| Catalog Service | 分类、SPU、SKU 快照和商品查询 gRPC | 🚧 第 4 阶段已接入 | [catalog](modules/catalog.md) |
| Inventory/Seckill Service | reservation 状态机、Redis Lua、秒杀准入和退款恢复 | 🚧 第 4 阶段已接入 | [inventory](modules/inventory.md) |
| Cart Service | 购物车和结算预览 | 🚧 第 4 阶段已拆分 | [cart](modules/cart.md) |
| Payment Service | Mock 支付、签名回调和退款 | 🚧 第 4 阶段已拆分 | [payment](modules/payment.md) |
| Fulfillment Service | 发货、物流和收货 | 🚧 第 4 阶段已拆分 | [fulfillment](modules/fulfillment.md) |
| API Gateway | 领域路由、过渡代理、旧接口兼容、限流和追踪 | 🚧 第 4 阶段切流中 | [gateway](modules/gateway.md) |
| Product Service | 旧秒杀商品读取、Redis 库存和限购 | ✅兼容 | [product](modules/product.md) |
| Order Service | 新商城普通/秒杀订单唯一编排、状态机和补偿 | 🚧 第 3 阶段已切流 | [order](modules/order.md) |
| Messaging Workers | 旧链路消息处理和新商城事件契约 | 🚧 迁移中 | [messaging](modules/messaging.md) |
| Platform | 配置、认证、响应、migration、Compose 和可观测性 | ✅稳定 | [platform](modules/platform.md) |

## 3. 快速链接
- [技术约定](../project.md)
- [架构设计](arch.md)
- [API 手册](api.md)
- [数据模型](data.md)
- [OpenAPI 契约](../../api/openapi.yaml)
- [变更历史](../history/index.md)
- [阶段 4 归档方案](../history/2026-08/202608061319_stage4_domain_services/)
- [第三阶段方案包](../history/2026-08/202608061112_order_service_orchestration/)
