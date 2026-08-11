# 变更提案: 项目质量加固与重复收敛

## 需求背景
当前仓库的格式、编译、单元测试、`go vet`、架构边界和 Memory E2E 基线均通过，但静态审计发现这些门禁未覆盖的实现与维护性问题：

1. SQL/Memory Outbox 的 `payload` 保存业务 payload，发布 worker 却把它直接按完整 `EventEnvelope` 解码，导致 Outbox 事件无法发布。
2. SQL Outbox claim 在数据库中递增 `attempts`，返回给 worker 的记录仍是递增前值，与 Memory 实现不一致，有限重试存在一次偏差。
3. Memory Outbox 在截断批次后才排序，批次选择受 Go map 随机迭代顺序影响；消息头也以可变 map 引用保存。
4. RabbitMQ 消费失败转入 retry 队列后立即 Ack 原消息，但 retry 发布没有等待 Publisher Confirm，连接异常窗口存在消息丢失风险。
5. Order、Payment、Fulfillment 三个入口重复实现相同的 Outbox/Inbox/Publisher 装配和后台 worker 启停逻辑。
6. Gateway 与独立 metrics HTTP 服务缺少统一的优雅停机；Gateway 创建的 gRPC/etcd 客户端没有显式关闭。
7. Gateway 统一配置与部分注释仍描述已移除的 debug 登录能力，形成过时配置语义。

## 变更内容
1. 修复 Outbox 信封重建、SQL claim 计数和 Memory claim 确定性，并补齐回归测试。
2. 为 RabbitMQ retry 发布增加 broker confirm，只有确认成功后才 Ack 原消息。
3. 在 `shared/platform/messaging` 抽取服务级消息运行时，替换三处重复装配。
4. 在 `shared/platform/appkit` 增加 HTTP/metrics 生命周期脚手架，补齐 Gateway 客户端关闭。
5. 清理已失效的 Gateway debug 登录配置描述和未使用的统一配置字段。
6. 同步知识库、变更日志和方案历史，保持代码与文档一致。

## 影响范围
- **模块:** Messaging、Platform/Appkit、Gateway、Order、Payment、Fulfillment、Catalog、Inventory、Configuration
- **文件:** `shared/platform/*`、相关服务入口、消息测试、Gateway 测试与知识库文件
- **API:** 不修改 HTTP/gRPC 路径、请求响应或 Protobuf 契约
- **数据:** 不新增或修改 migration；兼容已有 Outbox 业务 payload 记录

## 核心场景

### 需求: Outbox 事件可以被可靠发布
**模块:** Messaging

#### 场景: 发布 SQL/Memory Outbox 记录
Outbox 记录保存分离的事件元数据和业务 payload。
- worker 能重建并校验完整 `EventEnvelope`
- Publisher 收到正确的事件 ID、类型、版本、聚合信息和 payload
- 发布成功后记录进入 `published`，失败按当前次数有限重试

#### 场景: 批量 claim 多条内存事件
待发布事件数量超过批次上限。
- 按 `next_retry_at`、创建时间和 ID 稳定选择最早记录
- 调用方后续修改原消息头不会改变已保存的 Outbox 记录

### 需求: RabbitMQ 重试不丢消息
**模块:** Messaging

#### 场景: 业务消费失败后延迟重试
消费者需要把消息发布到 retry exchange。
- broker Ack retry 发布后才 Ack 原消息
- publish/confirm 失败时保留原消息供重新投递
- 原有 Inbox 幂等、TTL 退避和 DLQ 上限语义不变

### 需求: 服务运行时结构保持单一实现
**模块:** Platform、Order、Payment、Fulfillment

#### 场景: 服务启用或禁用 MQ
服务根据自身 Repository 和 `SECKILL_MQ_URL` 装配消息运行时。
- Memory/SQL Store 选择保持不变
- MQ URL 为空时不建立 RabbitMQ 连接
- 三个服务共享同一套启动、停止和关闭逻辑

### 需求: 进程可以有界优雅退出
**模块:** Platform、Gateway、Catalog、Inventory

#### 场景: 收到 SIGTERM 或上下文取消
- HTTP 服务停止接收新请求并在期限内完成在途请求
- metrics 服务同步退出
- Gateway 关闭所有 gRPC 连接和 etcd 客户端
- gRPC 服务现有停机行为保持不变

## 风险评估
- **风险:** 消息运行时属于跨服务共享路径，错误会影响 Order、Payment、Fulfillment 的事件协作。
- **缓解:** 先增加可复现回归测试，再做小步实现；保留 Outbox 旧记录兼容路径，并执行全仓 race、边界和 Memory E2E。
- **风险:** HTTP 生命周期调整可能改变启动失败和关闭日志。
- **缓解:** 保持监听地址与对外路由不变，统一忽略正常的 `http.ErrServerClosed`，异常仍明确返回或记录。
- **EHRB:** 未检测到；不连接生产环境、不变更数据库结构、不处理真实 PII/支付数据。
