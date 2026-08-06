-- 第四阶段领域服务拆分。
-- Payment 需要在本地单据中持久化用户归属，以便退款前做二次授权。

ALTER TABLE payments
    ADD COLUMN user_id BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER order_id,
    ADD KEY idx_payments_user (user_id, created_at);
