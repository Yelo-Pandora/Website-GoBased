# 处理速度与聚合负载模型设计

## 状态

本设计于 2026-07-17 经用户批准。

它替换阶段七和阶段八中的容量窗口模型，并保留现有固定权重与
自适应权重功能。

## 目标

* 每个 App 实例使用处理速度与最大负载描述教学负载。
* 请求进入实例后立即丢弃请求数据，只保留聚合订单数量。
* 实例负载按真实经过时间持续下降，不再按容量窗口清零。
* 超过最大负载的订单继续产生可观察的丢弃结果。
* 自适应权重尽量维持各运行实例的 `loadRatio` 一致。
* 页面在没有新请求时仍持续显示负载下降。
* 保持现有真实 Nginx 路由、串行操作和安全边界。

## 非目标

* 不模拟单个订单或请求的完成时间。
* 不保存请求队列、订单明细或到达顺序。
* 不模拟并发槽位、服务时间或排队等待时间。
* 不把聚合运行时负载写入 MySQL。
* 不把一个请求批次拆分到多个 App 实例。
* 不增加预测控制、机器学习调度或用户可调控制器参数。

## 已确认决策

* 每个实例分别配置 `processingSpeed` 和 `maxLoad`。
* `loadRatio = currentLoad / maxLoad`。
* 超出 `maxLoad` 的订单被丢弃。
* `processedUnits` 改名为 `acceptedUnits`。
* 负载使用经过时间惰性结算，不创建后台每秒定时器。
* 页面与控制器根据最近权威状态自行推算当前负载。
* 性能百分比只改变 `processingSpeed`，不改变 `maxLoad`。
* 一个小球只进入一个实例。
* 默认小球代表 10 个订单，默认生成间隔为 250 毫秒。
* 自适应控制使用处理速度基线加负载比例反馈。
* 公开控制器状态仍为 `converging`、`stable` 和 `degraded`。
* 成功调整后不增加冷却时间。

## 架构

### 数据流

一次教学请求仍只产生一个真实 HTTP 请求。

```text
Browser
  -> Platform API
  -> Lab Nginx
  -> one App instance
  -> accepted and dropped aggregate result
  -> Platform API runtime tracker and adaptive controller
  -> Browser animation and live load display
```

Nginx 继续按实例权重为完整请求选择一台 App 实例。

Nginx 不拆分请求体，Platform API 也不定向拆分批次。

响应可以把一个小球表现为接纳部分与丢弃部分，但接纳部分只能进入
同一个目标实例。

### 组件边界

App 实例拥有其 `currentLoad` 和 `lastUpdatedAt` 的权威状态。

Platform API 负责验证 App 响应、保存最近观察值、推算只读状态并运行
自适应控制器。

Orchestrator 负责从场景配置生成容器环境变量，并在性能调整时替换实例。

前端只使用后端提供的权威基准和固定公式推算展示值，不决定目标实例或
目标权重。

## App 实例负载模型

### 配置与运行时状态

每个实例保存以下配置：

```text
processingSpeed  每秒可消减的教学等效订单数
maxLoad          可保存的最大聚合负载数值
```

每个实例只保存以下可变运行时状态：

```text
currentLoad      当前聚合负载，可包含小数
lastUpdatedAt    上次权威结算时间
```

`processingSpeed` 和 `maxLoad` 必须为正数。

`currentLoad` 始终位于闭区间 `[0, maxLoad]`。

### 批次结算

App 在验证批次和商品后，在同一实例互斥区中执行以下计算：

```text
elapsedSeconds = max(0, now - lastUpdatedAt)
settledLoad = max(0, currentLoad - processingSpeed * elapsedSeconds)
available = max(0, maxLoad - settledLoad)
acceptedUnits = min(requestUnits, floor(available))
droppedUnits = requestUnits - acceptedUnits
currentLoad = settledLoad + acceptedUnits
loadRatio = currentLoad / maxLoad
lastUpdatedAt = now
```

`requestUnits`、`acceptedUnits` 和 `droppedUnits` 是整数。

内部 `currentLoad` 使用浮点数保留连续结算产生的小数。

API 中的 `currentLoad` 和 `loadRatio` 取三位小数，但实例内部不使用
舍入值继续计算。

实例不保存批次对象、订单对象、到达顺序或完成时间。

### 批次状态

批次响应使用以下状态：

* `accepted`：全部订单被接纳。
* `partially_accepted`：部分订单被接纳，部分被丢弃。
* `dropped`：全部订单被丢弃。

所有成功响应必须满足：

```text
acceptedUnits + droppedUnits = receivedUnits
```

### 负载状态

App 和前端使用相同的负载状态边界：

* `0 <= loadRatio <= 0.3`：`idle`。
* `0.3 < loadRatio <= 0.7`：`normal`。
* `0.7 < loadRatio < 1`：`high`。
* `loadRatio = 1`：`overloaded`。
* 实例健康检查失败：`unavailable`。

发生丢弃时，结算后的实例负载必然达到 `maxLoad`，因此状态为
`overloaded`。

### 统计聚合

`order_stats` 继续保存按商品、实例和时间桶聚合的教学统计。

时间桶只用于统计聚合，不参与负载结算或容量判断。

统计字段为 `received_orders`、`accepted_orders` 和 `dropped_orders`。

无效批次、无效商品或统计写入失败时，不提交新的运行时负载状态。

## 场景与实例配置

应用集群场景使用以下配置：

```json
{
  "loadModel": {
    "baseProcessingSpeed": 20,
    "maxLoad": 100,
    "initialPerformancePercent": 100,
    "minPerformancePercent": 20,
    "maxPerformancePercent": 100
  },
  "orderSimulation": {
    "defaultBatchSize": 10,
    "defaultGenerationIntervalMs": 250,
    "overloadPolicy": "drop_excess",
    "concurrencyControl": "mutex"
  }
}
```

以下旧配置被删除：

* `capacity.baseCapacity`。
* `capacity.capacityWindowMs`。
* `orderSimulation.processingDelayMs`。
* `orderSimulation.queueMode`。
* `orderSimulation.preserveArrivalOrder`。

App 容器环境变量使用：

```text
PROCESSING_SPEED
MAX_LOAD
```

以下旧环境变量被删除：

```text
EFFECTIVE_CAPACITY
CAPACITY_WINDOW_MS
```

实例处理速度按以下方式计算：

```text
processingSpeed = baseProcessingSpeed * performancePercent / 100
```

默认性能范围为 20% 至 100%，因此默认处理速度范围为每秒 4 至 20 个
订单。

性能调整继续替换 App 容器，以同时更新真实 CPU 限制与教学处理速度。

替换后的新实例保留相同 `maxLoad`，运行时 `currentLoad` 从零开始。

## 协议与公开状态

### 批次响应

`TrafficBatchResult` 使用以下核心字段：

```json
{
  "batchId": "batch-example",
  "labId": "lab-example",
  "status": "partially_accepted",
  "occurredAt": "2026-07-17T10:00:00Z",
  "path": ["user-pool", "lab-gateway", "app-1"],
  "targetInstanceId": "app-1",
  "receivedUnits": 10,
  "acceptedUnits": 6,
  "droppedUnits": 4,
  "instanceState": {
    "processingSpeed": 20,
    "maxLoad": 100,
    "currentLoad": 100,
    "loadRatio": 1,
    "loadState": "overloaded",
    "observedAt": "2026-07-17T10:00:00Z"
  }
}
```

`effectiveCapacity`、`remainingCapacity`、`capacityWindowStartedAt` 和
`capacityWindowEndsAt` 被删除。

### 实验快照

拓扑中的每个 App 实例公开：

```text
processingSpeed
maxLoad
currentLoad
loadRatio
loadState
observedAt
```

固定模式和自适应模式都可以获得最近观察状态的推算值。

自适应快照继续使用 `capacityNotice` 表达集群整体处理能力提示。

提示值仍为 `cluster_overloaded` 和 `cluster_underused`。

前端中的容量比例文案改为处理速度比例。

### 响应验证

Platform API 验证以下条件：

* 批次、实验和目标实例标识与请求及快照一致。
* `acceptedUnits` 与 `droppedUnits` 都是非负整数。
* 接纳量与丢弃量之和等于接收量。
* `processingSpeed` 与 `maxLoad` 为正数并与实例配置一致。
* `currentLoad` 位于 `[0, maxLoad]`。
* `loadRatio` 与 `currentLoad / maxLoad` 在舍入容差内一致。
* `observedAt` 有效且负载状态属于允许集合。

不满足契约的内部响应继续对外映射为稳定的不可用错误。

## 连续负载展示

App 响应中的实例状态是一次权威观察。

前端保存该观察，并按当前时间推算：

```text
displayLoad = max(
  0,
  observedCurrentLoad - processingSpeed * (now - observedAt)
)
displayLoadRatio = displayLoad / maxLoad
```

界面约每 100 毫秒刷新显示值。

收到同一实例的新响应时，前端用新的权威观察替换旧基准。

停止生成请求后，百分比与负载状态仍持续下降并最终显示为零和
`idle`。

前端累计统计名称从“已处理”改为“已接纳”。

Platform API 保存相同的最近观察字段，并使用同一公式装饰实验快照。

运行时观察只保存在进程内存中。

Platform API 重启后按零负载开始，直到收到新的批次观察。

App 实例重启后其权威负载也从零开始。

## 自适应权重算法

### 采样与平滑

控制器每 2 秒采样一次运行中的自适应实验。

每次采样先根据最近权威观察推算每台实例的当前 `loadRatio`。

控制器随后更新 EWMA：

```text
smoothedRatio = 0.5 * currentRatio + 0.5 * previousSmoothedRatio
```

新实例的第一个 EWMA 值等于当前推算比例。

推算负载达到零后，当前比例保持为零，不把最后一次非零观察视为过期
负载。

### 均衡区间

控制器计算所有运行实例平滑比例的算术平均值。

当最高与最低平滑比例之差不超过 `0.05` 时，实例被视为基本均衡。

基本均衡时不应用负载修正，目标分数等于实例处理速度。

### 负载反馈

比例差超过 `0.05` 时，每个实例按以下公式计算：

```text
correction = clamp(
  1 + (averageRatio - instanceSmoothedRatio),
  0.25,
  1.75
)
score = processingSpeed * correction
```

高于平均负载的实例获得较小分数，低于平均负载的实例获得较大分数。

所有实例达到相同比例时，目标权重自然回到处理速度比例。

全部实例同时满载时，权重仍按处理速度分配，并返回集群过载提示。

### 整数权重

控制器把所有正数分数按最大分数缩放到 Nginx 的 `1` 至 `100` 范围，
四舍五入为整数，再使用最大公约数约简。

相同负载下，处理速度 `20:20:6` 产生目标权重 `10:10:3`。

只有一台实例时，目标权重可以约简为 `1`。

### 调整阈值与状态

控制器把当前整数权重和目标整数权重转换为各自的流量份额。

所有实例的当前份额与目标份额差值都小于 3 个百分点时，不创建 Nginx
调整操作。

连续 3 次采样无需调整后，公开状态进入 `stable`。

需要调整、等待调整或执行调整时，公开状态为 `converging`。

调整成功并持久化权重后立即进入 `stable`，不增加成功冷却时间。

下一次 2 秒采样仍会正常重新评估负载。

Nginx 校验、reload 或编排器调用失败时进入 `degraded`。

失败重试继续使用 4 秒、8 秒、16 秒和 30 秒的上限退避。

重试前重新校验实验模式、实例集合、`processingSpeed`、`maxLoad` 和当前
权重。

切回固定模式后停止创建新的自适应调整，并保留最后成功权重。

## 持久化变更

平台实例持久化字段调整如下：

```text
effective_capacity -> processing_speed
add max_load
```

`performance_percent` 继续保存用户选择的性能百分比。

`processing_speed` 保存根据场景基准与性能百分比计算出的整数速度。

`max_load` 保存该实例独立的最大负载配置。

实验数据库的聚合统计字段调整如下：

```text
processed_orders -> accepted_orders
```

公开 Go 类型、仓库查询、操作结果、测试夹具、OpenAPI 和前端集成文档
同步改名。

旧字段不提供兼容别名，避免继续传播容量窗口语义。

## 并发与一致性

同一 App 实例的负载结算由单个互斥锁串行化。

并发批次不会使 `currentLoad` 超过 `maxLoad`。

不同 App 实例保持完全独立的运行时负载状态。

控制器不把数据库外的推算负载当作持久化事实。

自适应操作仍与扩缩容、性能调整、固定权重和模式切换共享现有的每实验
串行操作边界。

操作拓扑指纹包含实例标识、运行状态、处理速度、最大负载和当前权重。

负载在操作等待期间继续变化是允许的，下一次采样会继续纠正目标。

## 错误处理

* 无效 App 配置导致实例启动失败。
* 无效批次或商品不改变负载。
* 时间倒退按零经过时间处理。
* 统计写入失败不提交新的负载状态，并返回内部错误。
* Platform API 拒绝字段不一致或越界的 App 响应。
* 单个实验的控制失败不停止全局控制器。
* 配置应用失败保留上一份有效 Nginx 配置和数据库权重。
* 实例或 Platform API 重启后不尝试恢复内存负载。

## 测试

### App 单元测试

* 使用假时钟验证连续负载下降与归零。
* 验证同一秒内的多个批次不会触发窗口清零。
* 验证部分接纳、全部丢弃和全部接纳。
* 验证小数负载与整数接纳量向下取整。
* 验证时间倒退不会增加或错误消减负载。
* 验证并发批次不会突破 `maxLoad`。
* 验证统计写入失败不提交新状态。

### Platform API 与控制器测试

* 验证新批次响应契约与舍入容差。
* 验证固定和自适应快照都能推算负载下降。
* 验证 EWMA 在每个控制采样周期更新。
* 验证 `0.05` 均衡区间返回处理速度比例。
* 验证高负载实例降低目标权重。
* 验证修正系数限制为 `0.25` 至 `1.75`。
* 验证 `20:20:6` 在均衡状态下得到 `10:10:3`。
* 验证 3 个百分点份额阈值避免无效 reload。
* 验证连续 3 次无需调整后进入 `stable`。
* 验证成功后没有冷却，失败仍按上限退避。
* 验证固定模式不会自动修改权重。

### 前端测试

* 验证 `accepted`、`partially_accepted` 和 `dropped` 动画。
* 验证一个批次的接纳部分只进入一个目标实例。
* 使用假定时器验证负载显示连续下降到零。
* 验证负载状态随推算比例变化。
* 验证默认批次为 10，默认间隔为 250 毫秒。
* 验证“已接纳”和“已丢弃”累计统计。
* 验证处理速度比例、目标权重和三种控制器状态文案。

### 集成验证

* 运行 Go 单元测试、集成测试和静态检查。
* 运行前端单元测试与生产构建。
* 运行 Compose 配置校验。
* 验证真实 Nginx 仍为每个请求选择一台 App 实例。
* 验证性能调整后的容器使用新处理速度且负载归零。
* 验证平台和 App 重启后的内存负载恢复边界。

## 手动验收

1. 创建应用集群实验并确认两台实例的 `processingSpeed` 为 20，
   `maxLoad` 为 100。
1. 使用默认参数开始流量，确认每 250 毫秒产生一个代表 10 个订单的小球。
1. 确认每个小球的接纳部分只进入 Nginx 选中的一台实例。
1. 停止流量，确认实例负载百分比继续下降，并在最多 5 秒内从满载降至零。
1. 连续发送大批次使实例接近满载，确认仅剩余空间被接纳，超出部分显示
   为丢弃。
1. 增加第三台实例，将其性能调整为 30%，确认其处理速度为 6 且最大负载
   仍为 100。
1. 使用固定等权并运行默认流量，确认慢实例的负载比例明显高于两台快实例。
1. 切换到自适应模式，确认状态先为 `converging`，慢实例目标权重下降，
   各实例平滑负载比例差距逐步缩小。
1. 当负载比例进入均衡区间后，确认处理速度比例对应的目标权重为
   `10:10:3`。
1. 停止流量，确认所有实例最终回到零负载，并在连续稳定采样后显示
   `stable`。

## 验收标准

* 实例负载不再按固定窗口清零。
* 实例负载按 `processingSpeed * elapsedSeconds` 正确下降。
* 聚合负载永远不会超过 `maxLoad`。
* 接纳量与丢弃量完整覆盖每个批次的接收量。
* 页面在无新请求时仍持续显示负载下降。
* 一个请求批次始终只选择一个 App 实例。
* 性能百分比只改变处理速度，不改变最大负载。
* 固定模式保持用户权重，自适应模式根据负载比例反馈调整权重。
* 均衡状态下目标权重回归处理速度比例。
* 细小负载和权重波动不会频繁触发 Nginx reload。
* 公开控制器状态保持简单且失败可自动恢复。
