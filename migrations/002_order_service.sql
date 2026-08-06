-- 第三阶段 Order Service 所有权迁移。
-- 该迁移只增加兼容字段和 Order 自有操作表，不删除旧订单/秒杀表。

ALTER TABLE order_items
    ADD COLUMN reservation_id VARCHAR(128) NULL AFTER subtotal_cents,
    ADD KEY idx_order_items_reservation (order_id, reservation_id);

ALTER TABLE commerce_orders
    ADD COLUMN request_digest CHAR(64) NULL AFTER idempotency_key,
    ADD KEY idx_commerce_orders_digest (user_id, request_digest);

CREATE TABLE IF NOT EXISTS order_create_intents (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    intent_id VARCHAR(64) NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    request_digest CHAR(64) NOT NULL,
    order_id VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL,
    attempts INT NOT NULL DEFAULT 0,
    payload JSON NOT NULL,
    last_error VARCHAR(255) NOT NULL DEFAULT '',
    next_retry_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_order_intent_id (intent_id),
    UNIQUE KEY uk_order_intent_idempotency (user_id, idempotency_key),
    UNIQUE KEY uk_order_intent_order (order_id),
    KEY idx_order_intent_retry (status, next_retry_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS order_operations (
    id VARCHAR(192) PRIMARY KEY,
    order_id VARCHAR(64) NOT NULL,
    kind VARCHAR(32) NOT NULL,
    reservation_id VARCHAR(128) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL,
    attempts INT NOT NULL DEFAULT 0,
    last_error VARCHAR(255) NOT NULL DEFAULT '',
    next_retry_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    KEY idx_order_operations_retry (status, next_retry_at),
    KEY idx_order_operations_order (order_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
