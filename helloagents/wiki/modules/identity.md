# Identity Snapshot Service

## 目的
为 Order Service 提供带用户归属校验的地址快照 gRPC，不让订单服务直接访问身份数据表。

## 模块概述
- **职责:** `GetAddressSnapshot`、用户与地址归属校验、地址不存在错误和 Memory/MySQL 只读适配。
- **状态:** 🚧第 3 阶段最小适配
- **最后更新:** 2026-08-06

## 规范

### 需求: 订单地址快照
**模块:** Identity Snapshot、Order

#### 场景: 创建普通或秒杀订单
- 请求必须同时提供 `user_id` 和 `address_id`。
- 地址不属于用户或不存在时返回稳定错误，Order 不保存未校验地址。
- 响应仅作为订单快照输入，普通日志不得输出完整手机号和地址明文。

## API 接口
- `IdentityService.GetAddressSnapshot(user_id, address_id)`。
- 服务入口：`go run ./cmd/identity-snapshot-service`。

## 数据与依赖
- 生产适配可只读 `users`/`user_addresses`；无 DSN 时使用 Memory Repository。
- Order 只依赖 gRPC 客户端，不获取 Identity DSN。
- 完整注册、登录和地址写接口在当前阶段仍由 Commerce API 过渡承载。

## 当前边界
- 这是阶段 3 的兼容适配器，不代表 Identity 全部领域能力已拆分完成。
- Docker 不可用时使用 Memory/bufconn 验收，不执行真实数据库联调。

## 变更历史
- [202608061112_order_service_orchestration](../../history/2026-08/202608061112_order_service_orchestration/) - 新增地址快照 gRPC 适配器。
