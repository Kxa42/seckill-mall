# 任务清单: 微服务仓库结构收敛

## 目录与包迁移

- [√] [P0] 移动服务实现与测试到 `internal/<service>`，更新所有 Go import path。
- [√] [P0] 将 Gateway 实现移动到 `internal/gateway`，新增 `cmd/api-gateway` 薄入口。
- [√] [P0] 将 `common` 子包拆分到 `internal/contracts`、`internal/gen/commerce` 和 `internal/platform/*`。
- [√] [P1] 保持 `migrations/` 的版本文件名和执行顺序，补充服务所有权说明，不改动 SQL 版本路径。

## 冗余清理与工程文件

- [√] [P0] 审计并移除确认无引用的旧配置、旧入口和冗余目录。
- [√] [P0] 更新 Dockerfile、docker-compose.yaml、测试脚本和 README 的路径。
- [√] [P1] 增加统一的目录/测试命令说明，避免重新引入旧路径。

## 验证与知识库

- [√] [P0] 执行 `gofmt`、`go list ./...`、`go test ./...` 和 `go vet ./...`。
- [√] [P0] 执行 Memory E2E，并审计旧路径、敏感配置和跨服务内部依赖。
- [√] [P0] 更新 `helloagents/project.md`、`wiki/overview.md`、`wiki/arch.md`、模块文档和 `CHANGELOG.md`。
- [√] [P0] 将方案包迁移到 `helloagents/history/2026-08/` 并更新索引。

## 执行摘要

- 11/11 项任务完成。
- `make generate`、`make check` 和 Compose 静态解析通过。
- migration 文件路径保持不变，避免已记录版本重复执行。
