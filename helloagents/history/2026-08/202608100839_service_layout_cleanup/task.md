# 服务内部文件组织统一重构（轻量迭代）

类型：重构（纯文件移动，行为不变）

## 背景
Cart/Payment/Fulfillment/Identity 将模型、Repository 接口、Service 与两种存储实现堆在单文件，
与 Catalog/Order/Inventory 的分层结构不一致。

## 任务清单
- [√] Cart 拆分 model/service/repository/memory/mysql，CatalogClient 接口移入 clients.go
- [√] Payment 拆分 model/service/repository/memory/mysql，事件构造归属 service.go
- [√] Fulfillment 拆分 model/service/repository/memory/mysql，事务辅助归属 mysql_repository.go
- [√] Identity 拆分 model/repository/memory/mysql
- [√] 门禁验证：gofmt、vet、race 测试、边界脚本、Memory E2E、tidy
- [√] 更新 CHANGELOG 与历史索引
