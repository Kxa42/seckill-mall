# 任务清单: 服务优先单仓库

## 服务自治目录

- [√] [P0] 迁移 Gateway 的入口、实现、middleware、OpenAPI 和配置到 `services/gateway`。
- [√] [P0] 迁移 Identity、Catalog 和 Inventory 的入口、实现、测试与配置到各自服务目录。
- [√] [P0] 迁移 Cart、Order、Payment 和 Fulfillment 的入口、实现、测试与配置到各自服务目录。
- [√] [P0] 为跨服务 Gateway E2E 增加最小 `testkit` 门面并更新测试导入。

## 共享层与工程入口

- [√] [P0] 将 Contracts、Protobuf、生成代码和平台包迁移到 `shared`。
- [√] [P0] 将 Order gRPC 客户端从平台层迁移到 `shared/clients/order`。
- [√] [P1] 将 migration 工具迁入 `tools/migrate`，保持 SQL 版本目录不变。
- [√] [P0] 更新配置加载路径、Dockerfile、Compose、Makefile 和测试脚本。
- [√] [P1] 删除旧压测程序和迁移后的空根目录。

## 验证与文档

- [√] [P0] 增加并通过生产依赖边界审计。
- [√] [P0] 执行 Protobuf 生成、格式、包枚举、单元测试、vet、Memory E2E 和二进制构建。
- [√] [P0] 执行 Compose 静态解析和旧路径/敏感配置审计。
- [√] [P0] 更新 README、知识库、CHANGELOG 和 ADR-005。
- [√] [P0] 归档方案包并更新历史索引。

## 执行摘要

- 14/14 项任务完成，无失败或跳过项。
- 八个服务已迁入 `services/<service>`，共享协议与基础设施已迁入 `shared`，仓库保留单一 `go.mod`。
- `make generate` 幂等；`make check`、Compose 静态解析、旧路径审计、敏感配置审计和 `git diff --check` 均通过。
