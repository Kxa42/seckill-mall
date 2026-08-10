# Payment Service

## 目的
独立维护支付单、回调幂等和退款单，并发布支付成功/退款事件。

## 规范
- 回调先在本地 Payment Repository 幂等收口，再通过 Order gRPC 或事件恢复订单状态。
- `payments`、`refunds` 和 `payment_*_events` 由 Payment 独占；不直接写 Order 表。
- 重复回调、重复退款和同一 callback_ref 冲突均由本地状态机处理。
- `payment.succeeded.v1`、`payment.refunded.v1` 与本地状态同事务提交。
- Order 调用只依赖 Payment 私有 `OrderClient` 的 `Get/ConfirmPayment/Refund`；gRPC 适配器位于 `services/payment/internal/app`，不依赖共享业务客户端。

## 入口
gRPC `Create/Callback/Refund`；Gateway `/api/v1/orders/:order_id/payments`、`/api/v1/payments/callback`、`/api/v1/orders/:order_id/refunds`；启动 `go run ./services/payment/cmd/payment-service`，实现位于 `services/payment/internal/app`。

## 变更历史
- [202608091438_fix_core_bugs](../../history/2026-08/202608091438_fix_core_bugs/) - 移除事件处理器只写不读的 `pending` 集合与 `PendingOrder()` 死代码，保留 payload 校验，`sync` import 清理。
- [202608070836_private_order_clients](../../history/2026-08/202608070836_private_order_clients/) - Order 客户端完全收敛到 Payment 私有目录，测试替身改为窄接口。
- [202608070804_service_first_monorepo](../../history/2026-08/202608070804_service_first_monorepo/) - Payment 的入口、实现、测试和配置收敛到服务自治目录。
- [202608070731_repository_layout_refactor](../../history/2026-08/202608070731_repository_layout_refactor/) - Payment 实现迁入统一内部服务目录。
