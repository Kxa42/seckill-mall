# 任务清单: 项目质量加固与重复收敛

目录: `helloagents/plan/202608101524_project_quality_hardening/`

---

## 1. Outbox 正确性
- [√] 1.1 在 `shared/platform/messaging/types_test.go` 增加信封重建和发布回归测试，验证 why.md#需求-outbox-事件可以被可靠发布-场景-发布-sqlmemory-outbox-记录
- [√] 1.2 在 `shared/platform/messaging/types.go` 与 `shared/platform/messaging/worker.go` 实现 Outbox 信封重建和发布接入，依赖任务 1.1
- [√] 1.3 在 `shared/platform/messaging/types_test.go` 增加 claim 次数与批次顺序测试，验证 why.md#需求-outbox-事件可以被可靠发布-场景-批量-claim-多条内存事件
- [√] 1.4 在 `shared/platform/messaging/memory_store.go` 与 `shared/platform/messaging/sql_store.go` 修复确定性 claim、headers 隔离和 SQL 返回次数，依赖任务 1.3

## 2. RabbitMQ 重试可靠性
- [√] 2.1 在 `shared/platform/messaging/rabbit.go` 为 retry publish 启用并等待 deferred confirm，确认成功后才 Ack 原消息
- [√] 2.2 执行 `shared/platform/messaging` 单元测试与可用的 RabbitMQ 集成脚本，验证 why.md#需求-rabbitmq-重试不丢消息-场景-业务消费失败后延迟重试
> 备注: 单元测试通过；RabbitMQ 集成脚本未执行（Docker daemon 不可用，`SECKILL_RABBITMQ_INTEGRATION` 未设置时脚本按设计跳过并退出 0）。

## 3. 消息运行时重复收敛
- [√] 3.1 新增 `shared/platform/messaging/service_runtime.go` 与测试，统一 Memory/SQL Store、Publisher、worker 和关闭逻辑
- [√] 3.2 更新 `services/order/cmd/order-service/main.go` 与 `services/payment/cmd/payment-service/main.go` 使用共享运行时，验证 why.md#需求-服务运行时结构保持单一实现-场景-服务启用或禁用-mq
- [√] 3.3 更新 `services/fulfillment/cmd/fulfillment-service/main.go` 并删除三处本地 `messageRuntime` 重复实现，依赖任务 3.1

## 4. HTTP 与客户端生命周期
- [√] 4.1 在 `shared/platform/appkit` 增加有界 HTTP shutdown/metrics 脚手架及测试，验证 why.md#需求-进程可以有界优雅退出-场景-收到-sigterm-或上下文取消
- [√] 4.2 更新 `services/catalog/cmd/catalog-service/main.go` 与 `services/inventory/cmd/inventory-service/main.go` 复用 metrics 生命周期
- [√] 4.3 更新 `services/gateway/internal/app/server.go` 与 `main.go` 使用 signal context 和 HTTP shutdown
- [√] 4.4 更新 `services/gateway/internal/app/clients.go` 与测试，显式关闭 gRPC/etcd 客户端资源

## 5. 冗余配置与上下文语义
- [√] 5.1 更新 `shared/platform/config/services.go` 与测试，移除已废弃 debug login 的未使用配置字段并修正文案
- [√] 5.2 更新 `deploy/config/commerce-services.example.yaml` 与 Gateway middleware 注释，清除过时登录描述
- [√] 5.3 在相关 Memory Repository/Store 中传递调用方 context，避免事件落库绕过取消语义
> 备注: 5.3 实现后经 gofmt 修复 fulfillment/payment Memory Repository 缩进问题。

## 6. 安全检查
- [√] 6.1 审计本次改动不记录密钥、DSN、MQ URL、JWT、地址明文或支付签名，不增加跨服务 `internal` 依赖
- [√] 6.2 执行 `bash scripts/check_boundaries.sh` 与敏感信息/危险调用扫描

## 7. 质量验证
- [√] 7.1 执行 `gofmt`、`git diff --check`、`go test -race -count=1 ./...` 和 `go vet ./...`
- [√] 7.2 执行 `make check`；RabbitMQ/Docker 可用时执行真实集成脚本，否则记录跳过原因
> 备注: `make check` 全部通过；RabbitMQ 真实集成因 Docker daemon 不可用跳过（与任务 2.2 一致）。

## 8. 知识库与归档
- [√] 8.1 更新 `helloagents/CHANGELOG.md`、`wiki/modules/messaging.md`、`wiki/modules/platform.md` 与相关 Gateway 文档
- [√] 8.2 完成一致性审计，更新本任务状态并迁移方案包至 `helloagents/history/2026-08/`
- [√] 8.3 更新 `helloagents/history/index.md` 并扫描遗留方案包
