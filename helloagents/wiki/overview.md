# Go-Seckill-Mall

> 本文件描述代码当前已经实现的能力；前端和后续兼容迁移不计入已完成范围。

## 1. 项目概述

### 目标与背景
项目在原有高并发秒杀、Outbox、死信补偿和可观测能力之上，新增可独立运行的完整商城后端 MVP。新 API 覆盖身份、地址、商品目录、购物车、结算、统一订单、Mock 支付、履约和退款。

### 范围
- **当前范围内:** `/api/v1` 商城 API、旧秒杀兼容 API、MySQL 持久化、内存验收模式、版本化 migration、容器编排和可观测性。
- **当前范围外:** 商城前端、真实支付渠道、营销优惠、复杂售后审批，以及旧 Redis/MQ 秒杀消息到新交易域的最终切换。

### 干系人
- **负责人:** 项目维护者。

## 2. 模块索引

| 模块名称 | 职责 | 状态 | 文档 |
|---------|------|------|------|
| Commerce API | 身份、地址、目录、购物车、订单、支付、履约与退款 | 🚧 MVP已实现 | [commerce](modules/commerce.md) |
| API Gateway | `/api/v1` 反向代理、旧接口兼容、限流和追踪 | ✅稳定 | [gateway](modules/gateway.md) |
| Product Service | 旧秒杀商品读取、Redis 库存和限购 | ✅兼容 | [product](modules/product.md) |
| Order Service | 旧秒杀下单编排、排队订单与 Outbox | ✅兼容 | [order](modules/order.md) |
| Messaging Workers | 旧链路 Outbox 发布、主队列消费和死信补偿 | ✅兼容 | [messaging](modules/messaging.md) |
| Platform | 配置、认证、响应、migration、Compose 和可观测性 | ✅稳定 | [platform](modules/platform.md) |

## 3. 快速链接
- [技术约定](../project.md)
- [架构设计](arch.md)
- [API 手册](api.md)
- [数据模型](data.md)
- [OpenAPI 契约](../../api/openapi.yaml)
- [变更历史](../history/index.md)
