# Platform

## 目的
提供安全配置、认证、统一 HTTP 响应、数据库迁移、容器编排和可观测基础。

## 模块概述
- **职责:** 环境配置、bcrypt/JWT、请求 ID、统一响应、migration runner、Dockerfile、Compose 和 Prometheus 抓取。
- **状态:** ✅稳定
- **最后更新:** 2026-08-05

## 规范

### 需求: 后端可部署与验收
**模块:** Platform

#### 场景: 启动完整后端
- Compose 要求显式提供 MySQL、Redis、RabbitMQ、JWT 和支付签名密钥。
- migration 成功后才启动 Commerce API；服务提供存活、就绪或 metrics 健康检查。
- 容器使用非 root 用户，服务支持 SIGTERM 优雅停机。

#### 场景: 无 Docker 的本地验收
- `SECKILL_COMMERCE_STORE=memory` 不要求 DSN，仅用于临时演示。
- `tests/e2e_memory.sh` 启动临时二进制并在结束后清理进程。

## 依赖
- Go 标准库、Gin、GORM MySQL driver、Docker Compose、Prometheus、OpenTelemetry。

## 当前边界
- 当前环境无法拉取 MySQL 镜像，真实 migration 重放和 Compose 健康检查待有 Docker 环境后执行。

## 变更历史
- [202608051526_backend_commerce_mvp](../../history/2026-08/202608051526_backend_commerce_mvp/) - 增加商城平台基础与完整编排。
