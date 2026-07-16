# 实验控制面基础说明

## 1. 当前阶段边界

阶段三建立实验控制面的数据一致性和后台执行基础，阶段四完成受限资源编排，阶段五已将
公开端点 `POST /api/v1/labs` 接入创建闭环。请求完成 Session 与 CSRF 校验后返回
`202 Accepted`，后台 Worker 负责调用编排器并持久化资源结果。

这种顺序避免平台在资源创建、补偿和核对尚未可用时，对外返回无法兑现的“已受理”结果。
课程、认证和学习进度接口不受实验编排器状态影响。

## 2. 对应需求

| 需求 | 阶段三落实内容 |
| --- | --- |
| R10 单活动实验 | 创建事务按用户行加锁，活动状态包含 Preparing、Running、Expiring 和 Terminating。 |
| R11 实验生命周期 | 固定六种状态和允许的状态转换，拒绝跨阶段跳转。 |
| R37 引导式操作 | 持久操作只保存后端定义的动作，Worker 只映射已知结构化命令。 |
| R40 资源准入 | 统一检查全局活动实验数、临时容器预留数和单实验实例上限。 |
| R41 幂等与串行执行 | operationId 全局唯一；同一 ID 返回原结果；同一实验操作通过持久队列串行领取。 |
| R43 持久与高频数据分离 | 实验会话和低频操作结果保存在 MySQL，不引入逐请求运行日志。 |
| R45 稳定错误 | UDS 拒绝和传输失败保存结构化错误码，不把内部错误正文作为接口契约。 |
| R46 故障恢复 | claimed 或 running 操作的租约过期后可被重新领取。 |

阶段三本身不实现 Docker、Nginx、实验数据库存储过程适配器、资源补偿和实验快照；这些能力已在阶段四和阶段五按边界逐步接入。MVP 仍不采用服务器推送事件。

## 3. 数据库并发控制

全局实验准入使用 MySQL 命名锁 `platform:global-admission`，
不为并发控制增加业务表。

创建实验会话时，事务依次执行：

1. 查询是否已经存在相同 operationId。
2. 锁定当前用户行，串行处理同一账号的创建请求。
3. 获取全局命名锁，串行计算全局活动实验和临时容器预留量。
4. 校验课程是否属于后端允许的实验场景。
5. 检查单活动实验和资源配额。
6. 同时写入 `lab_sessions`、`lab_operations` 和 `course_progress.last_lab_id`。
7. 提交事务并释放命名锁后，后台 Worker 才能领取该操作。

如果事务中任何一步失败，会话和操作都不会产生部分记录，
命名锁会在同一数据库连接上释放。

## 4. 状态机

状态集合为：

```text
Preparing -> Running | Failed | Terminating
Running -> Expiring | Failed | Terminating
Expiring -> Running | Failed | Terminating
Failed -> Terminating
Terminating -> Failed | Terminated
Terminated -> 无后续状态
```

`Failed` 不计入活动实验，因此失败清理完成后，用户可以使用新的 operationId 再次创建。
`Terminating` 仍占用活动实验名额，防止旧资源尚未清理时创建第二套资源。

## 5. 持久队列与租约

Worker 默认每 500 毫秒查询一次队列。领取操作时，会把状态改为 `claimed`，记录
`lease_owner`、`lease_expires_at` 并增加 `attempt_count`。开始调用编排器前，状态更新为
`running`。

如果进程在调用途中退出，操作会保留在 MySQL。租约过期后，新 Worker 可以重新领取
`claimed` 或 `running` 操作。该机制提供至少一次执行语义，因此后续编排命令还必须结合
operationId、资源标签和真实资源状态保证幂等。

Worker 当前支持把 `CREATE_LAB` 映射为 `PROVISION_LAB`。未知动作会以
`ACTION_NOT_SUPPORTED` 失败，不会被转换为任意命令。

## 6. UDS 调用链

```text
lab_operations
  -> platform-api Operation Worker
  -> HTTP/JSON over /run/platform/orchestrator.sock
  -> POST /v1/commands
  -> orchestrator 结构化响应
  -> 原子更新 lab_operations 与 lab_sessions
```

客户端限制请求目标、响应大小、内容类型、命令状态，并核对 commandId 和 operationId。
它不接受 TCP 地址，也不会向公网发送编排命令。

阶段四之前，orchestrator 会返回 `501 COMMAND_NOT_IMPLEMENTED`，并由 Worker 将拒绝保存为：

```text
lab_operations.status = failed
lab_operations.error_code = COMMAND_NOT_IMPLEMENTED
lab_sessions.status = Failed
```

该历史结果用于证明阶段三控制链已经真实接通，不表示实验资源已创建。
阶段四已经用结构化命令执行替换该占位响应，详见
[`secure-orchestrator-guide.md`](secure-orchestrator-guide.md)。

## 7. 配置

| 环境变量 | 默认值 | 作用 |
| --- | ---: | --- |
| `LAB_MAX_ACTIVE` | `10` | 全局活动实验上限 |
| `LAB_MAX_TEMP_CONTAINERS` | `40` | 全局临时容器预留上限 |
| `LAB_MAX_INSTANCES_PER_SESSION` | `4` | 单实验应用实例上限 |
| `LAB_OPERATION_WORKER_ID` | `platform-api-1` | 租约持有者标识 |
| `LAB_OPERATION_POLL_INTERVAL` | `500ms` | 空队列轮询间隔 |
| `LAB_OPERATION_LEASE_DURATION` | `30s` | 操作租约时长 |
| `LAB_ORCHESTRATOR_TIMEOUT` | `20s` | 单次 UDS 命令超时 |

租约时长必须大于编排命令超时，避免正常调用尚未结束时被另一 Worker 重复领取。

## 8. 下一阶段入口

阶段四已经在 orchestrator 内实现命令白名单、模板注册表、Docker 适配器、实验数据库适配器、
Nginx 配置管理、补偿和资源核对。
阶段五已经开放实验创建、快照、重置和主动结束接口，形成应用与数据分离实验的第一条端到端闭环。
下一阶段补充空闲/最长时限回收、重启恢复和周期性资源核对；商品批次处理归入阶段七。

阶段六快照中的 `idleExpiresAt` 和 `maximumExpiresAt` 由 `started_at` 与
`last_effective_action_at` 按平台配置推导，不重复写入数据库。
