CREATE TABLE IF NOT EXISTS users (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    email VARCHAR(255) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    role VARCHAR(32) NOT NULL DEFAULT 'customer',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_users_email (email)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL,
    token_hash CHAR(64) NOT NULL,
    expires_at DATETIME NOT NULL,
    revoked_at DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_refresh_tokens_hash (token_hash),
    KEY idx_refresh_tokens_user (user_id, expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS user_addresses (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL,
    recipient VARCHAR(64) NOT NULL,
    phone VARCHAR(32) NOT NULL,
    province VARCHAR(64) NOT NULL,
    city VARCHAR(64) NOT NULL,
    district VARCHAR(64) NOT NULL,
    detail VARCHAR(255) NOT NULL,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    KEY idx_addresses_user (user_id, is_default)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS categories (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    parent_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
    name VARCHAR(128) NOT NULL,
    slug VARCHAR(128) NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_categories_slug (slug),
    KEY idx_categories_parent (parent_id, active)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS spus (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    category_id BIGINT UNSIGNED NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    KEY idx_spus_category (category_id, active)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS skus (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    spu_id BIGINT UNSIGNED NOT NULL,
    code VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    price_cents BIGINT NOT NULL,
    available_stock INT NOT NULL DEFAULT 0,
    reserved_stock INT NOT NULL DEFAULT 0,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_skus_code (code),
    KEY idx_skus_spu (spu_id, active),
    CHECK (price_cents >= 0),
    CHECK (available_stock >= 0),
    CHECK (reserved_stock >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS product_images (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    spu_id BIGINT UNSIGNED NOT NULL,
    url VARCHAR(512) NOT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    KEY idx_product_images_spu (spu_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS seckill_activities (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    sku_id BIGINT UNSIGNED NOT NULL,
    name VARCHAR(255) NOT NULL,
    price_cents BIGINT NOT NULL,
    purchase_limit INT NOT NULL DEFAULT 1,
    starts_at DATETIME NOT NULL,
    ends_at DATETIME NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'draft',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    KEY idx_seckill_sku_time (sku_id, starts_at, ends_at),
    CHECK (price_cents >= 0),
    CHECK (purchase_limit > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS cart_items (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL,
    sku_id BIGINT UNSIGNED NOT NULL,
    quantity INT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_cart_user_sku (user_id, sku_id),
    KEY idx_cart_user (user_id),
    CHECK (quantity > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS commerce_orders (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    order_id VARCHAR(64) NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    order_type VARCHAR(32) NOT NULL DEFAULT 'normal',
    status VARCHAR(32) NOT NULL,
    total_amount_cents BIGINT NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    address_snapshot JSON NOT NULL,
    expires_at DATETIME NOT NULL,
    paid_at DATETIME NULL,
    shipped_at DATETIME NULL,
    completed_at DATETIME NULL,
    canceled_at DATETIME NULL,
    refunded_at DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_commerce_orders_order_id (order_id),
    UNIQUE KEY uk_commerce_orders_idempotency (user_id, idempotency_key),
    KEY idx_commerce_orders_user_time (user_id, created_at),
    KEY idx_commerce_orders_status_expiry (status, expires_at),
    CHECK (total_amount_cents >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS order_items (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    order_id VARCHAR(64) NOT NULL,
    sku_id BIGINT UNSIGNED NOT NULL,
    sku_code VARCHAR(64) NOT NULL,
    sku_name VARCHAR(255) NOT NULL,
    unit_price_cents BIGINT NOT NULL,
    quantity INT NOT NULL,
    subtotal_cents BIGINT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    KEY idx_order_items_order (order_id),
    CHECK (unit_price_cents >= 0),
    CHECK (quantity > 0),
    CHECK (subtotal_cents >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS inventory_reservations (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    reservation_id VARCHAR(64) NOT NULL,
    order_id VARCHAR(64) NOT NULL,
    sku_id BIGINT UNSIGNED NOT NULL,
    quantity INT NOT NULL,
    status VARCHAR(32) NOT NULL,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_inventory_reservation (reservation_id),
    UNIQUE KEY uk_inventory_order_sku (order_id, sku_id),
    KEY idx_inventory_status_expiry (status, expires_at),
    CHECK (quantity > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS order_status_history (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    order_id VARCHAR(64) NOT NULL,
    from_status VARCHAR(32) NOT NULL,
    to_status VARCHAR(32) NOT NULL,
    reason VARCHAR(255) NOT NULL DEFAULT '',
    actor_type VARCHAR(32) NOT NULL,
    actor_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    KEY idx_order_history_order (order_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS payments (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    payment_no VARCHAR(64) NOT NULL,
    order_id VARCHAR(64) NOT NULL,
    amount_cents BIGINT NOT NULL,
    provider VARCHAR(32) NOT NULL DEFAULT 'mock',
    status VARCHAR(32) NOT NULL,
    callback_ref VARCHAR(128) NULL,
    paid_at DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_payments_no (payment_no),
    UNIQUE KEY uk_payments_order (order_id),
    UNIQUE KEY uk_payments_callback (callback_ref),
    CHECK (amount_cents >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS refunds (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    refund_no VARCHAR(64) NOT NULL,
    order_id VARCHAR(64) NOT NULL,
    payment_no VARCHAR(64) NOT NULL,
    amount_cents BIGINT NOT NULL,
    reason VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME NULL,
    UNIQUE KEY uk_refunds_no (refund_no),
    UNIQUE KEY uk_refunds_order (order_id),
    CHECK (amount_cents >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS shipments (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    order_id VARCHAR(64) NOT NULL,
    carrier VARCHAR(64) NOT NULL,
    tracking_no VARCHAR(128) NOT NULL,
    status VARCHAR(32) NOT NULL,
    shipped_at DATETIME NOT NULL,
    delivered_at DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_shipments_order (order_id),
    UNIQUE KEY uk_shipments_tracking (carrier, tracking_no)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS commerce_outbox_events (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    event_id VARCHAR(64) NOT NULL,
    aggregate_type VARCHAR(64) NOT NULL,
    aggregate_id VARCHAR(64) NOT NULL,
    event_type VARCHAR(128) NOT NULL,
    event_version INT NOT NULL DEFAULT 1,
    payload JSON NOT NULL,
    headers JSON NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    retry_count INT NOT NULL DEFAULT 0,
    next_retry_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_commerce_outbox_event (event_id),
    KEY idx_commerce_outbox_pending (status, next_retry_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS inbox_events (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    consumer VARCHAR(128) NOT NULL,
    event_id VARCHAR(64) NOT NULL,
    event_type VARCHAR(128) NOT NULL,
    event_version INT NOT NULL,
    processed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_inbox_consumer_event (consumer, event_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT INTO categories (id, parent_id, name, slug, active)
VALUES (1, 0, '数码产品', 'digital', TRUE)
ON DUPLICATE KEY UPDATE name = VALUES(name), active = VALUES(active);

INSERT INTO spus (id, category_id, name, description, active)
VALUES (1, 1, 'iPhone 15', '秒杀与普通购买演示商品', TRUE)
ON DUPLICATE KEY UPDATE name = VALUES(name), description = VALUES(description), active = VALUES(active);

INSERT INTO skus (id, spu_id, code, name, price_cents, available_stock, reserved_stock, active)
VALUES (1, 1, 'IPHONE15-128-BLACK', 'iPhone 15 128GB 黑色', 699900, 100, 0, TRUE)
ON DUPLICATE KEY UPDATE name = VALUES(name), price_cents = VALUES(price_cents), active = VALUES(active);

