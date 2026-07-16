# 阶段六生命周期与恢复设计

## 目标

阶段六补齐实验倒计时、自动回收、重启恢复和周期性资源核对。
实验快照仍是页面恢复的事实来源，不增加 SSE、Event Hub、有界事件环或后端流量生成器。

## 截止时间

平台不重复持久化截止时间，而是根据现有事实字段计算：

* `idleExpiresAt = last_effective_action_at + LAB_IDLE_TIMEOUT`，默认 10 分钟；
* `maximumExpiresAt = started_at + LAB_MAX_DURATION`，默认 30 分钟。

两个字段作为可空 UTC 时间加入实验快照。创建尚未完成、没有开始时间的实验返回 `null`。
页面刷新、快照轮询、课程阅读和自动流量批次不修改 `last_effective_action_at`。
当前阶段只有创建成功和重置成功属于有效操作；后续阶段的扩缩容、性能和权重操作成功后
复用同一刷新规则。

## 状态推进

Lifecycle Scheduler 默认每 5 秒扫描 `Running` 和 `Expiring` 实验：

1. 取空闲截止和最长截止中的较早者；
2. 距离截止 1 分钟时，把 `Running` 原子更新为 `Expiring`；
3. 用户有效操作刷新空闲时间且不再处于提醒窗口时，可恢复为 `Running`；
4. 到达截止时间后，在同一事务中把实验更新为 `Terminating`，记录
   `idle_timeout` 或 `maximum_duration`，并创建系统 `DESTROY_LAB` 操作；
5. 若同一实验已有未完成控制操作，本轮不插入销毁操作，下一轮重新判断。

系统销毁操作使用由 `labId` 派生的稳定 `operationId`。平台在插入操作或状态更新之间崩溃时，
事务会整体回滚；操作 Worker 继续使用现有租约机制，在平台重启后重新领取过期任务。

## 资源核对

Scheduler 默认每分钟调用一次 `RECONCILE_RESOURCES`，期望集合包括
`Preparing`、`Running`、`Expiring` 和 `Terminating` 实验。
调用明确设置 `cleanup=true`，编排器只清理带平台管理标签的容器、网络、Nginx 片段，
以及满足固定 `lab_[a-z0-9]` 命名规则的实验数据库和账号。

编排器通过只授予 `EXECUTE` 的存储过程列出实验数据库，不向命令 payload 开放原始 SQL、
数据库名或账号。核对结果区分资源孤儿、数据库孤儿和缺失数据库，便于日志审计。

## 配置

| 配置 | 默认值 |
|---|---:|
| `LAB_IDLE_TIMEOUT` | `10m` |
| `LAB_MAX_DURATION` | `30m` |
| `LAB_EXPIRING_LEAD` | `1m` |
| `LAB_LIFECYCLE_POLL_INTERVAL` | `5s` |
| `LAB_RECONCILE_INTERVAL` | `1m` |
| `LAB_RECONCILE_CLEANUP` | `true` |

提醒时间必须小于空闲和最长时限。核对周期与生命周期轮询独立，任一轮失败只记录错误，
不会停止 HTTP 服务、操作 Worker 或后续调度。

## 验收

* 快照返回准确的两个截止时间；
* 进入提醒窗口时状态变为 `Expiring`；
* 空闲 10 分钟或总时长 30 分钟时只创建一个系统销毁操作；
* 自动批次和快照查询不延长实验；
* 平台重启后过期租约和超时实验继续处理；
* 周期核对能够发现并清理受管 Docker、Nginx 和实验数据库孤儿；
* 固定资源以外的数据库、容器和网络不受影响。
