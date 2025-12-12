# Seckill Mall - 高并发微服务秒杀系统 

基于 Go 语言 (Gin + gRPC) 开发的微服务电商秒杀系统。采用 DDD 领域驱动设计，引入 RabbitMQ 实现异步削峰，使用 Redis + Lua 脚本彻底解决超卖问题。

## 🛠 技术栈
* **语言:** Golang (1.20+)
* **框架:** Gin (HTTP), gRPC (RPC), GORM (ORM)
* **服务治理:** Etcd (注册与发现)
* **中间件:**
    * **Redis:** 缓存预热 + Lua 原子脚本 (防超卖核心)
    * **RabbitMQ:** 消息队列 (异步下单/削峰填谷)
* **数据库:** MySQL 8.0
* **配置管理:** Viper

## 🏗 架构演进
**当前版本 (v2.0): 全链路异步削峰**

1.  **流量拦截:** API 网关统一鉴权与路由。
2.  **库存扣减:** 商品服务通过 Redis + Lua 脚本进行原子扣减，拦截 99% 无效流量。
3.  **异步下单:** 订单服务**不操作数据库**，仅发送订单消息至 RabbitMQ，实现毫秒级响应。
4.  **削峰填谷:** 消费者服务 (Consumer) 从 MQ 拉取消息，平滑写入 MySQL。

## 🚀 快速运行

### 1. 启动基础设施 (Docker)
确保以下端口未被占用：`3306`, `6379`, `2379`, `5672`。

    # 启动 MySQL, Redis, Etcd, RabbitMQ
    docker run -d --name mysql -p 3306:3306 -e MYSQL_ROOT_PASSWORD=123456 mysql:8.0
    docker run -d --name redis -p 6379:6379 redis
    docker run -d --name etcd -p 2379:2379 -e ALLOW_NONE_AUTHENTICATION=yes bitnami/etcd:3.5
    docker run -d --name rabbitmq -p 5672:5672 -p 15672:15672 rabbitmq:3-management

### 2. 初始化数据库
连接 MySQL，创建数据库 `seckill` 并导入 `orders` 表结构（详见 SQL 脚本）。

### 3. 启动微服务 (严格按顺序)
1.  **Product Service:** (预热库存到 Redis)
2.  **Order Service:** (连接 MQ 准备发信)
3.  **MQ Consumer:** (连接 MQ 准备收信)
4.  **API Gateway:** (启动 HTTP 入口)

### 4. 接口测试
**秒杀下单 (POST):**
`http://localhost:8080/order`

    {
        "user_id": 10086,
        "product_id": 1,
        "count": 1
    }

**预期返回:**

    {
        "code": 200,
        "message": "排队中，请稍后查询结果"
    }

## 📝 目录结构
* `/api_gateway`: HTTP 网关
* `/common`: 公共组件 (Proto, Config, Utils)
* `/order_service`: 订单服务 (Producer)
* `/product_service`: 商品服务 (Redis Logic)
* `/mq_consumer`: 消息队列消费者