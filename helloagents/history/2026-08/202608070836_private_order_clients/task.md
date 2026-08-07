# 任务清单: Order 客户端完全服务私有化

## 设计与边界

- [√] [P0] 在 Payment 私有包定义窄 `OrderClient`、Payment 专用模型和 gRPC 适配器。
- [√] [P0] 在 Fulfillment 私有包定义窄 `OrderClient`、履约专用模型和 gRPC 适配器。
- [√] [P0] 删除 `shared/clients/order`，迁移两个服务及入口的构造调用。
- [√] [P0] 更新 Payment、Fulfillment 模型、Server、单元测试和 Gateway E2E 测试组装。
- [√] [P1] 更新 Payment/Fulfillment `testkit`，只暴露测试所需构造器。

## 安全与质量

- [√] [P0] 保持 deadline、HMAC 用户/角色签名、错误码映射和空客户端防护。
- [√] [P0] 增加两个私有适配器的 gRPC 签名与错误行为测试。
- [√] [P0] 更新生产依赖边界审计，禁止重新引入共享 Order 客户端或跨服务 `internal` 依赖。

## 验证与文档

- [√] [P0] 运行格式、边界、全量测试、vet、构建和 Memory E2E。
- [√] [P0] 更新 `project.md`、`wiki/arch.md`、Payment/Fulfillment/Messaging 模块文档、CHANGELOG 和 ADR-006。
- [√] [P0] 迁移方案包至 `helloagents/history/2026-08` 并更新 `history/index.md`。

## 执行摘要

- 11/11 项任务完成，无失败或跳过项。
- Payment 和 Fulfillment 已分别持有窄 Order 接口与私有 gRPC 适配器，`shared/clients/order` 及空目录已移除。
- `make check` 全部通过；新增适配器测试确认 HMAC 用户/管理员签名、deadline、返回模型和错误码映射保持正确。
