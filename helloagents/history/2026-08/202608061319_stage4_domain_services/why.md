# 变更提案: 阶段 4 领域服务拆分

## 需求背景

阶段 3 已建立唯一 Order Service，但 Identity 仅提供地址快照适配，Cart、Payment、Fulfillment 仍由 `commerce-api` 的总 Repository 承载。Gateway 对这些 `/api/v1` 路由继续使用 HTTP 代理，导致数据所有权和独立部署边界尚未真正落地。

本阶段在暂缓前端和 RabbitMQ 新商城事件运行时的前提下，提取四个独立 gRPC 服务，并由 Gateway 显式切流。`commerce-api` 暂时保留为回退入口，但新链路不再访问它的身份、购物车、支付和履约 Repository。

## 产品分析

### 目标用户与场景
- **用户群体:** 需要通过 API 展示完整 Go 微服务商城交易闭环的开发者、面试官和项目评审者。
- **使用场景:** 从注册登录、维护地址、购物车、创建订单、Mock 支付到发货收货和退款，观察各领域服务独立调用和数据所有权。
- **核心痛点:** 当前订单已微服务化，但交易前后能力仍集中在 Commerce，无法展示完整的服务边界和独立启动能力。

### 价值主张与成功指标
- **价值主张:** 保持外部 `/api/v1` 契约稳定，同时让 Identity、Cart、Payment、Fulfillment 成为可独立部署、可独立测试的服务。
- **成功指标:** Gateway 不再代理阶段 4 领域路由；四个服务只访问自身表；注册到收货与退款可通过 Memory/bufconn 端到端验证；重复回调、重复发货和越权请求保持幂等或被拒绝。

### 人文关怀

密码只保存 bcrypt 哈希，Refresh Token 只保存 SHA-256 哈希；内部日志不输出令牌、地址详情、支付签名和数据库凭据；用户资源必须校验归属，管理员能力必须校验角色。

## 变更内容

1. 将 Identity 地址快照适配器扩展为完整身份、令牌和地址服务。
2. 新增 Cart Service，通过 Catalog gRPC 获取实时 SKU 快照，不读取 Catalog 表。
3. 新增 Payment Service，独占支付和退款单据，通过 Order gRPC 驱动订单支付与退款状态。
4. 新增 Fulfillment Service，独占物流单据，通过 Order gRPC 驱动发货和收货状态。
5. Gateway 显式承接身份、地址、购物车、支付、退款和履约 HTTP 路由，并统一验证商城 JWT 与内部调用签名。
6. 增加阶段 4 migration、独立配置、Compose 服务和 Memory/bufconn 验收。

## 影响范围

- **模块:** Gateway、Identity、Cart、Payment、Fulfillment、Order、Catalog、Commerce、Platform。
- **文件:** `proto/commerce`、`common/pb`、四个服务包与 `cmd` 入口、Gateway 路由、配置、migration、Compose、测试和知识库。
- **API:** 保持 `/api/v1` 路径和 JSON 语义；扩展内部 gRPC 契约。
- **数据:** Identity 只写 `users/refresh_tokens/user_addresses`；Cart 只写 `cart_items`；Payment 只写 `payments/refunds`；Fulfillment 只写 `shipments`。

## 核心场景

### 需求: 完整身份与地址服务
**模块:** Identity

#### 场景: 注册登录和刷新令牌
- 注册创建 bcrypt 密码哈希并签发 Access/Refresh Token。
- 登录统一隐藏邮箱是否存在，旧 Refresh Token 轮换后不能再次使用。
- Gateway 使用商城 Access Token 获取用户和角色，不接受伪造管理员角色。

#### 场景: 地址归属与订单快照
- 用户只能增删改查自己的地址。
- Order 继续通过同一 Identity gRPC 服务读取地址快照。
- 普通日志不记录地址详情、手机号或令牌。

### 需求: 独立购物车与结算预览
**模块:** Cart、Catalog

#### 场景: 维护购物车并创建普通订单
- Cart 只保存用户、SKU 和数量，通过 Catalog 获取实时名称、价格和销售状态。
- 结算预览重新计算总金额并标记不可售 SKU。
- Gateway 在订单请求未显式提交 items 时读取当前购物车并调用 Order。

### 需求: 独立支付与退款
**模块:** Payment、Order

#### 场景: Mock 支付回调和退款幂等
- 创建支付单前通过 Order 查询用户归属、金额和可支付状态。
- 回调验证 HMAC 签名和唯一流水，重复相同回调返回同一结果，不同流水冲突。
- 退款只允许订单所有者发起，重复退款返回同一退款单，Order 仍是订单状态唯一写入者。

### 需求: 独立履约服务
**模块:** Fulfillment、Order

#### 场景: 管理员发货和用户收货
- 管理员角色签名通过后才能发货。
- 相同承运商和运单重复发货幂等，不同物流信息冲突。
- 用户只能确认自己的订单收货，物流状态和 Order 状态同步收敛。

### 需求: 受控切流与回退
**模块:** Gateway、Commerce

#### 场景: Gateway 显式路由优先
- 阶段 4 `/api/v1` 路由优先调用目标 gRPC 服务，不进入 Commerce `NoRoute` 代理。
- `commerce-api` 保留回退能力，阶段 4 不删除旧表和旧秒杀 MQ 链路。
- Docker 不可用时以纯 Go、Memory、Fake 和 bufconn 验收，不把替代验收声明为真实集成。

## 风险评估

- **风险:** Payment/Fulfillment 与 Order 的同步调用可能在“远端状态已变、本地单据未写”窗口中断。
- **缓解:** 所有命令使用业务唯一键，Order 转换幂等，本地 Repository 对同一业务键重复执行可收口；阶段 5 再使用 Outbox/Inbox 替代同步过渡。
- **风险:** Gateway 同时存在旧 JWT 与商城 JWT，可能混淆角色语义。
- **缓解:** `/api/v1` 显式路由使用带 issuer、token_type 和 role 校验的商城 JWT；旧 JWT 只保留旧兼容路由。
- **风险:** 一次拆分四个服务影响范围大。
- **缓解:** 按 Identity、Cart、Payment、Fulfillment 四个检查点实现，每个服务先完成 Memory/bufconn 测试再接入 Gateway。
