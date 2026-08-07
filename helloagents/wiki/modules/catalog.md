# Catalog Service

## 目的
独立提供商品目录、SPU/SKU 快照和在售商品查询，隔离 Commerce 过渡 Repository。

## 模块概述
- **职责:** 目录模型、商品分页、商品详情、SKU 快照、Memory/MySQL Repository 和 Catalog gRPC。
- **状态:** ✅独立服务
- **最后更新:** 2026-08-07

## 规范

### 需求: 商品查询
**模块:** Catalog Service

#### 场景: 查询在售商品和 SKU
- `ListProducts`、`GetProduct` 默认只返回 active SPU/SKU。
- `GetSKUSnapshot` 按请求 SKU 顺序返回整数分价格、编码、名称和目录快照字段。
- Gateway 将 gRPC 模型转换为既有 `/api/v1/products*` 的 `spu/skus/images` JSON 结构。

## API 接口
- `ListProducts(offset, limit)`：商品分页。
- `GetProduct(spu_id)`：商品详情。
- `GetSKUSnapshot(sku_ids)`：订单/购物车所需的 SKU 快照。

## 数据模型
- 只访问 `categories`、`spus`、`skus`、`product_images`。
- `available_stock` 和 `reserved_stock` 在本阶段仅作为目录快照，不是秒杀扣减真源。

## 依赖
- gRPC、Protobuf、GORM、MySQL、etcd；无 DSN 时使用 MemoryRepository。入口和实现位于 `services/catalog/cmd/catalog-service` 与 `services/catalog/internal/app`。

## 变更历史
- [202608070804_service_first_monorepo](../../history/2026-08/202608070804_service_first_monorepo/) - Catalog 的入口、实现、测试和配置收敛到服务自治目录。
- [202608070731_repository_layout_refactor](../../history/2026-08/202608070731_repository_layout_refactor/) - Catalog 实现迁入统一内部服务目录。
- [202608060842_catalog_inventory_services](../../history/2026-08/202608060842_catalog_inventory_services/) - 建立独立 Catalog 服务并切换 Gateway 商品查询。
