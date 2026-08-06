# 变更提案: 事件契约与按服务配置落地

## 需求背景

当前微服务迁移基线已经定义了 `common/contracts`、事件信封和按服务配置模板，但存在四类落差：事件测试没有覆盖已知事件类型的未来版本；`EventType` 与 `EventVersion` 的语义未定义；Catalog/Inventory 运行时没有消费服务边界契约；`commerce-services.example.yaml` 尚未被任何服务加载。此外，服务边界校验中存在可简化的局部映射逻辑。

## 变更内容

1. 明确 `EventType` 是事件路由与主版本标识，`EventVersion` 是同一事件类型的 Payload/信封演进版本；只要为正数，信封层接受未来版本，消费者自行按能力忽略或隔离未知版本。
2. 增加已知事件类型未来版本、版本独立演进和未知事件类型的契约测试，并完善代码注释。
3. Catalog/Inventory 启动时校验并读取 `common/contracts` 的服务边界；服务事件发布必须使用 `NewEventEnvelope`，不复制事件字符串或边界定义。
4. 新增严格环境变量展开的按服务配置加载器，使 `commerce-services.example.yaml` 可被 Gateway、Catalog 和 Inventory 入口消费；保留现有独立 YAML 作为兼容回退。
5. 简化 `ValidateServiceBoundaries` 的冗余依赖检查，并同步知识库和阶段任务记录。

## 影响范围

- **模块:** common/contracts、common/config、Catalog Service、Inventory/Seckill Service、API Gateway、知识库。
- **文件:** `common/contracts/events.go`、`common/contracts/events_test.go`、`common/contracts/boundaries.go`、`common/config/conf.go`、新增按服务配置加载器及测试、三个服务入口、配置模板和文档。
- **API:** 不修改对外 HTTP API；内部事件契约和配置加载行为增强。
- **数据:** 不修改数据库表；只影响配置读取和事件信封校验语义。

## 核心场景

### 需求: 事件版本前向兼容
**模块:** Common Contracts

#### 场景: 已知事件类型收到未来版本

- `order.created.v1` 携带 `EventVersion=99` 时，信封结构校验成功。
- 消费者不得把“事件类型已知”误认为“Payload 版本一定可解析”；未知版本由消费者能力检查后忽略、隔离或进入重试/DLQ策略。

#### 场景: 事件类型与 Payload 版本独立演进

- `order.created.v1` 携带 `EventVersion=2` 时，契约层接受。
- `EventType` 后缀用于路由和主版本兼容边界，`EventVersion` 用于 Payload/信封版本，不在传输层强制数值一致。

### 需求: 服务实际消费共享边界
**模块:** Catalog/Inventory Service

#### 场景: 服务启动校验边界

- Catalog 和 Inventory 入口通过 `ServiceBoundaryFor` 获取自身边界，并调用全局边界校验。
- 服务实现不新增本地服务名、数据所有权或事件类型副本。

### 需求: 按服务配置加载
**模块:** Platform Configuration

#### 场景: 加载目标商城配置模板

- 通过 `SECKILL_SERVICES_CONFIG` 指定 `commerce-services.example.yaml`。
- `${CATALOG_MYSQL_DSN}` 等变量在 YAML 解码前严格展开，缺失变量时返回不包含密钥值的错误。
- Gateway、Catalog 和 Inventory 从同一模板读取各自地址、服务名、DSN、RabbitMQ、etcd 和 Redis 配置；未指定模板时继续使用现有独立配置文件。

## 风险评估

- **风险:** 未来事件版本被接受后，旧消费者可能无法解析 Payload。
  - **缓解:** 信封层只负责结构校验；消费者必须先检查支持的事件版本，未知版本进入忽略/隔离路径，并在后续 Inbox/DLQ 阶段补充行为测试。
- **风险:** 切换配置入口导致已有本地启动命令失效。
  - **缓解:** `SECKILL_SERVICES_CONFIG` 为显式选择，未设置时保持现有 Viper 配置路径和 `SECKILL_*` 覆盖逻辑。
- **风险:** 配置模板包含敏感 DSN 占位符。
  - **缓解:** 只展开环境变量，不在日志和错误中输出变量值；模板继续不保存真实密钥。
