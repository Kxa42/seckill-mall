# Cart Service

## 目的
独立保存用户购物车数量，并通过 Catalog 获取实时 SKU 快照和结算预览。

## 模块概述
- **职责:** `cart_items` 所有权、Set/Delete/List/Preview、价格与金额溢出校验。
- **状态:** 🚧第 4 阶段已独立接入
- **最后更新:** 2026-08-06

## 规范

### 需求: 购物车隔离
**模块:** Cart Service

#### 场景: 用户维护购物车
- 每次请求由服务端 HMAC 校验用户身份，不能通过请求体切换用户。
- `(user_id, sku_id)` 是唯一键，MySQL 使用原生 upsert，内存实现使用并发安全 Map。
- 下架或不存在的 SKU 不会写入购物车；实时价格来自 Catalog，不信任购物车历史价格。

### 需求: 结算预览
**模块:** Cart、Catalog

#### 场景: 读取当前可结算金额
- Preview 重新读取 SKU 价格和库存，返回小计、总金额和 `available`。
- 整数分金额做负数与 int64 溢出校验；空购物车返回空列表和零金额。

## API接口
- gRPC `Set/Delete/List/Preview`。
- Gateway: `GET /api/v1/cart`、`POST /api/v1/cart/items`、`DELETE /api/v1/cart/items/:sku_id`、`GET /api/v1/cart/preview`。
- 独立入口：`go run ./cmd/cart-service`。

## 数据模型
- 只写 `cart_items(user_id, sku_id, quantity, created_at, updated_at)`。

## 依赖
- Catalog gRPC、MySQL 或 Memory Repository、etcd 注册。

## 变更历史
- [202608061319_stage4_domain_services](../../history/2026-08/202608061319_stage4_domain_services/) - 拆分 Cart 并接入 Gateway。
