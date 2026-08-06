# Identity Service

## 目的
独立提供用户认证、Refresh Token 轮换、地址管理和订单地址快照 gRPC，不让其他服务直接访问身份数据表。

## 模块概述
- **职责:** 注册、登录、Refresh Token 轮换、管理员初始化、地址 CRUD、用户/地址归属校验和 `GetAddressSnapshot`。
- **状态:** 🚧第 4 阶段已拆分
- **最后更新:** 2026-08-06

## 规范

### 需求: 订单地址快照
**模块:** Identity、Order

#### 场景: 创建普通或秒杀订单
- 请求必须同时提供 `user_id` 和 `address_id`。
- 地址不属于用户或不存在时返回稳定错误，Order 不保存未校验地址。
- 响应仅作为订单快照输入，普通日志不得输出完整手机号和地址明文。

### 需求: 认证和地址管理
**模块:** Identity、Gateway

#### 场景: 注册、登录和刷新令牌
- 注册和登录使用 bcrypt 密码哈希，Refresh Token 只保存 SHA-256 哈希并在刷新时轮换。
- Gateway `/api/v1` 只接受 issuer 为 `seckill-commerce`、`token_type=access` 的商城 JWT，旧 JWT 不能伪造商城角色。
- 管理员初始化由显式环境变量控制，不在代码中硬编码凭据。

## API 接口
- `IdentityService.Register/Login/Refresh`。
- `IdentityService.CreateAddress/UpdateAddress/DeleteAddress/ListAddresses`。
- `IdentityService.GetAddressSnapshot(user_id, address_id)`。
- 服务入口：`go run ./cmd/identity-snapshot-service`（进程名兼容，服务职责已升级为完整 Identity）。

## 数据与依赖
- 生产适配只访问 `users`、`refresh_tokens`、`user_addresses`；无 DSN 时使用并发安全 Memory Repository。
- Order 只依赖 gRPC 客户端，不获取 Identity DSN。
- Gateway 已显式把 `/api/v1/auth/*` 和 `/api/v1/addresses*` 路由切到 Identity；Commerce 仅保留兼容回退。

## 当前边界
- 阶段 4 已完成身份能力的独立进程拆分；新商城 RabbitMQ Outbox/Inbox 运行时仍留待阶段 5。
- Docker 不可用时使用 Memory/bufconn 验收，不执行真实数据库联调。

## 变更历史
- [202608061112_order_service_orchestration](../../history/2026-08/202608061112_order_service_orchestration/) - 新增地址快照 gRPC 适配器。
- [202608061319_stage4_domain_services](../../history/2026-08/202608061319_stage4_domain_services/) - 升级为完整 Identity Service 并接入 Gateway。
