# 项目概述

## 模块索引

| 模块 | 职责 | 状态 | 文档 |
|------|------|------|------|
| Identity | 注册、登录、Token、地址快照 | ✅独立服务 | [identity](modules/identity.md) |
| Catalog | 分类、SPU、SKU 查询和快照 | ✅独立服务 | [catalog](modules/catalog.md) |
| Inventory/Seckill | reservation、Redis Lua、秒杀准入、Stream Outbox | ✅统一运行时 | [inventory](modules/inventory.md) |
| Cart | 购物车与结算预览 | ✅独立服务 | [cart](modules/cart.md) |
| Order | 普通/秒杀订单唯一状态机 | ✅统一运行时 | [order](modules/order.md) |
| Payment | 支付、回调、退款与事件 | ✅独立服务 | [payment](modules/payment.md) |
| Fulfillment | 发货、收货、物流与事件 | ✅独立服务 | [fulfillment](modules/fulfillment.md) |
| Messaging | Outbox、Inbox、RabbitMQ、Fake/Memory | ✅阶段5完成 | [messaging](modules/messaging.md) |
| API Gateway | `/api/v1` HTTP 入口 | ✅统一入口 | [gateway](modules/gateway.md) |
| Platform | 配置、认证、migration、可观测性 | ✅稳定 | [platform](modules/platform.md) |

## 规则

代码是运行时事实；服务只写自己的数据集；旧表仅历史保留。完整 API 以 [api.md](api.md) 和 [OpenAPI](../../services/gateway/api/openapi.yaml) 为准。

工程采用服务优先单仓库：服务入口、私有实现、配置、业务客户端适配器和测试门面位于 `services/<service>`，跨服务契约、生成代码、协议和平台能力位于 `shared`，全仓暂时共享单一 `go.mod`。
