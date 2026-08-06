# Commerce 旧运行时

该模块已从构建和 Compose 中移除，不是当前运行时模块。旧数据库表只作为历史残留保留，未执行 DROP/TRUNCATE；新代码不读取或写入旧订单、商品和旧 Outbox 表。
