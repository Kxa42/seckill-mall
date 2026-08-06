-- 仅创建数据库。所有新运行时表由 migrations/ 按版本事务创建。
-- 历史 product/orders/outbox_events 表不会在新环境初始化，也不会被 migration 删除。
CREATE DATABASE IF NOT EXISTS seckill DEFAULT CHARACTER SET utf8mb4;
USE seckill;
