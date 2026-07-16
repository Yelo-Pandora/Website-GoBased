# E-R 模型与迁移修正设计

## 1. 背景

当前平台 E-R 图定义八张业务表。

`002_lab_control_queue.sql` 新增了 `lab_control_locks`，
`003_lab_database_reconciliation.sql` 新增了
`orchestrator_lab_databases`。

这两张表不属于已批准的数据模型。
开发环境允许清除 MySQL 数据卷并重新初始化，因此不保留旧迁移兼容路径。

## 2. 迁移判断

删除 `002_lab_control_queue.sql` 和
`003_lab_database_reconciliation.sql`。

两者均不继续保留：

- `002` 只用于创建额外锁表，替代方案不需要数据库实体。
- `003` 的有效存储过程定义已存在于初始化脚本中，清卷后重复执行没有价值。
- 空迁移会制造不存在的历史兼容承诺，因此不保留空壳。

保留 `001_foundation_baseline.sql` 作为当前开发基线标记。
后续真实增量变更可以从 `002` 重新编号。

## 3. 数据模型修正

从平台初始化 Schema 删除 `orchestrator_lab_databases`。

实验数据库和数据库账号仍属于 `lab_resources` 的资源类型，
不建立第二个事实源。

实验数据库初始化脚本只保留以下三个受限存储过程：

- `provision_lab_database`
- `reset_lab_database`
- `destroy_lab_database`

删除登记表写入、删除和枚举逻辑。

## 4. 全局准入锁

实验创建事务使用 MySQL 命名锁替代 `lab_control_locks` 行锁。

命名锁必须满足以下约束：

- 使用固定、带平台命名空间的锁名。
- 在同一数据库连接上获取、执行事务并释放。
- 获取超时、业务失败、提交失败和上下文取消路径都不得泄漏锁。
- 用户行锁继续用于同一用户的并发创建保护。

## 5. 资源核对精简

删除数据库适配器的 `List` 和 `ExpectedLabIDs` 方法。

`RECONCILE_RESOURCES` 命令载荷显式携带 `expectedLabIds`。
编排器只负责比较调用方提供的期望集合与其可观察的 Docker、Nginx 资源。

阶段五已将编排结果写入 `lab_resources`；数据库单独遗留时的孤儿枚举仍不在本阶段实现，
后续资源核对任务可直接以该表作为期望资源事实源。
已知实验的显式销毁和创建失败补偿仍按确定性名称删除数据库及账号。

## 6. HTTP 框架审计

所有应用路由、中间件和 Handler 已使用 Gin。

以下标准库用法属于 Gin 的正常底层依赖，不构成框架混用：

- `http.Server` 承载 Gin Engine。
- `http.Client` 和 `http.Transport` 用于外部或 UDS HTTP 调用。
- `net/http` 状态码用于响应语义。
- `httptest` 和测试服务器用于协议级测试。

因此不进行无意义的标准库替换。

## 7. 验证

实施后执行：

- `gofmt`。
- `go test ./...`。
- `go vet ./...`。
- `docker compose config --quiet`。
- 搜索确认额外表、已删除过程和旧 Go 接口没有残留。
- 检查所有生产 HTTP Handler 仍以 `*gin.Context` 为入口。
