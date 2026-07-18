# 阶段九真实多级缓存与缓存故障设计

## 目标

阶段九在阶段八真实应用集群、固定等权路由、同步批次请求和实验生命周期能力之上，
增加真实的进程内 L1、会话 Redis L2 和实验 MySQL 回源链路。

每个公开 HTTP 批次只执行一次真实缓存查询。
`requestUnits` 继续表示教学等效查询数量，不拆分为多次真实缓存访问。

后端返回真实命中结果和模板定义的模拟耗时，但不通过休眠延长 HTTP 响应。
前端只根据后端结果播放动画，并允许后发的短路径动画超过先发的长路径动画。

## 已确认决策

* Cache Processor 与现有 Order Processor 保持独立。
* Lab App Router 只依赖稳定的 `BatchProcessor` 接口。
* 场景选择只发生在 Lab App 启动装配层。
* 缓存实验默认创建三台应用实例。
* 三台实例使用实验 Nginx 固定等权路由。
* 缓存实验不展示手动权重、自适应权重和 CPU 性能调整。
* 每台应用实例拥有独立 L1，最多缓存九个正向商品条目。
* Redis L2 不设置商品数量上限。
* Redis 的 48 MB 和 `allkeys-lru` 仅作为资源安全限制。
* 实验商品扩展为十二个 SKU，分属四个商品分类。
* L1 正向商品 TTL 默认十五秒。
* Redis L2 正向商品 TTL 默认三十秒，并使用正负 20% 抖动。
* 缓存命中不刷新 TTL。
* 前端使用打乱后的商品序列持续生成真实请求。
* 批次 Delta 立即更新缓存视图，实验快照负责最终校正。
* Redis 创建、重启和删除继续使用已有 Orchestrator COMMAND。
* 缓存策略、过期、预热和商品更新由 Lab App 运行时接口处理。

## 非目标

* 不创建独立缓存微服务。
* 不新增消息队列、SSE 或缓存事件历史流。
* 不把逐请求缓存记录持久化到平台 MySQL。
* 不允许前端直接访问 Redis 或 Lab App 容器地址。
* 不使用 Redis Lua 脚本。
* 不增加 Redis 商品数量管理或自定义 Redis 管理器。
* 不演示 Redis 容量耗尽或 Redis LRU 商品淘汰。
* 不通过大对象或降低 Redis 内存人为制造容量压力。
* 不修改阶段七和阶段八的订单处理语义。

## 当前基线

现有公开批次请求已经同步经过 Platform API、实验 Nginx 和某个真实 Lab App 实例。
Platform API 已负责认证、CSRF、实验归属、运行状态、限流和结果校验。

现有 `order.Processor` 每个 HTTP 批次只验证一次商品，然后按 `requestUnits`
结算教学等效负载。
它不会把一个批次拆成多次商品查询。

现有缓存场景模板已经引用 `cache_app_container_v1`、`session_redis_v1`、
实验数据库和实验 Nginx，但尚未实现缓存运行时。

现有 Redis COMMAND 已包含：

* `CREATE_SESSION_REDIS`
* `RESTART_SESSION_REDIS`
* `DELETE_SESSION_REDIS`

这些 COMMAND 已真实操作 Docker 容器，不是空模板。

## 总体架构

```text
Browser Traffic Generator
        |
        v
Platform API /traffic-batches
        |
        v
Lab Nginx fixed equal routing
        |
        v
Lab App HTTP Router
        |
        v
BatchProcessor
        |----------------------|
        v                      v
order.Processor          cache.Processor
        |                      |
        v                      v
order_stats          L1 -> Redis -> MySQL
```

Router 和 Handler 不判断场景类型。
`app/server.go` 根据 `SCENARIO_TYPE` 注入 `order.Processor` 或
`cache.Processor`。

`order` 包不得导入 `cache` 包。
`cache` 包不得依赖 `order_stats`、订单 Repository 或订单错误语义。

## 缓存实验拓扑

缓存实验默认拓扑为：

```text
                 +-> App-1 L1 (9)
Browser -> Nginx +-> App-2 L1 (9) -> Redis L2 -> Lab MySQL
                 +-> App-3 L1 (9)
```

三台应用实例使用相同 CPU、内存和固定 Nginx 权重。
Nginx 继续真实决定目标实例，前端不得指定 App ID。

多实例拓扑用于展示：

* 每个实例拥有独立 L1。
* Redis L2 被三个实例共享。
* 新实例从空 L1 开始预热。
* 商品更新通过 Pub/Sub 清理三个实例的 L1。
* `singleflight` 只合并单实例内的并发查询。
* Redis 互斥锁协调多个实例的缓存重建。

缓存实验可以保留一至三台实例的扩缩容操作。
前端默认隐藏权重编辑、自适应控制器和性能调整控件。

## 场景模板

缓存场景模板增加可信缓存配置。

```json
{
  "topology": {
    "initialInstances": 3,
    "minimumInstances": 1,
    "maximumInstances": 3
  },
  "cache": {
    "l1MaxProductEntries": 9,
    "l1TtlMs": 15000,
    "l2TtlMs": 30000,
    "l2TtlJitterPercent": 20,
    "refreshTtlOnHit": false,
    "simulatedLatencyMs": {
      "l1": 120,
      "redis": 320,
      "mysql": 800,
      "negativeCache": 160,
      "bloomFilter": 80,
      "degraded": 500
    }
  }
}
```

模板是缓存容量、TTL 和模拟耗时初始值的唯一来源。
用户不能提交任意 TTL、Redis key 或 Redis 命令。

十二个 SKU 分属以下四类：

* `electronics`
* `books`
* `home`
* `sports`

每类包含三个商品。
商品继续使用实验数据库 `products` 表，并保留 `version` 字段。

负向缓存是缓存保护元数据，不属于前端展示的正向商品库存。
负向缓存只接受场景模板提供的有限无效商品 ID，并使用短 TTL。

## 运行时配置接线

缓存场景创建时，Orchestrator 先创建会话 Redis，再创建三台缓存应用实例。
普通集群场景保持现有创建顺序和配置。

缓存应用实例增加以下可信环境配置：

* `REDIS_ADDR=lab-redis:6379`
* `MODULES`
* `CACHE_L1_MAX_PRODUCT_ENTRIES`
* `CACHE_L1_TTL_MS`
* `CACHE_L2_TTL_MS`
* `CACHE_L2_TTL_JITTER_PERCENT`
* 各命中来源的模拟耗时

所有值由已验证的场景模板产生，不接受浏览器透传。
Lab App 从实验 MySQL 读取十二个商品 ID，用于有界 Redis 状态查询和快照商品目录。
前端只使用快照返回的商品目录生成打乱请求序列。

## Lab App 缓存组件

建议新增以下内部组件：

```text
services/lab-app/internal/cache/
  processor.go
  service.go
  l1.go
  redis.go
  policy.go
  metrics.go
  inventory.go
  types.go
```

### Cache Processor

`cache.Processor` 实现与订单处理器相同的 `BatchProcessor` 接口。
它负责批次校验、一次真实缓存查询、缓存结果构造和缓存指标更新。

缓存查询不存在商品时返回正常 `not_found` 结果。
它不得复用订单处理器的 `product.ErrNotFound` HTTP 错误语义。

Cache Processor 独立维护与现有协议兼容的聚合负载状态。
`acceptedUnits` 和 `droppedUnits` 表示实例对教学等效查询量的接纳结果，
不表示商品是否存在。

每个合法批次先执行一次真实缓存查询，再结算教学等效负载。
即使批次因容量被部分或全部丢弃，`actualLookupCount` 仍然是 1。

缓存处理器不得写入 `order_stats`。
缓存命中、回源、重建、淘汰和降级只写入缓存运行时聚合指标。

### L1 Store

每个 Lab App 进程创建一个独立 L1 Store。
L1 使用进程内 map、双向链表和互斥锁实现有界 LRU。

正向商品条目上限为九个。
写入第十个不同商品时，真实淘汰最久未访问的正向条目。

L1 条目保存：

* 商品 ID
* 名称
* 分类
* 版本
* 到期时间
* 最近访问时间

### Redis Adapter

Redis Adapter 使用会话 Redis 的真实 `GET`、`SET`、`DEL`、`MGET` 和 Pub/Sub。
商品 key 继续使用实验 ID 命名空间。

Redis 不维护逻辑商品数量上限。
Redis 商品内容展示根据场景已知的十二个商品 ID 执行一次有界 `MGET`。

该方式不使用全量 `SCAN`、Lua 脚本或额外 LRU 索引。

### Product Repository

现有 Product Repository 扩展为返回完整商品数据和版本。
缓存商品更新继续写入实验 MySQL。

## 真实分层查询

每个 HTTP 批次执行一次以下流程：

1. 校验批次、商品 ID 和实验场景。
1. 查询目标实例的进程内 L1。
1. L1 命中时返回真实 L1 结果。
1. L1 未命中时查询会话 Redis。
1. Redis 命中时回填目标实例 L1。
1. Redis 未命中时查询实验 MySQL。
1. MySQL 查询成功后回填 Redis 和目标实例 L1。
1. 返回真实查询轨迹、缓存变化和模拟耗时。

典型持续流量顺序为：

```text
Batch 1 -> App-1 -> L1 miss -> Redis miss -> MySQL
Batch 2 -> App-2 -> L1 miss -> Redis hit
Batch 3 -> App-1 -> L1 hit
Batch 4 -> App-3 -> L1 miss -> Redis hit
```

`requestUnits` 不影响真实缓存查询次数。
批次结果始终声明 `actualLookupCount = 1`。

## TTL 与周期回源

十二个商品远小于 Redis 的 48 MB 内存限制。
正常实验不能依赖 Redis 内存耗尽触发回源。

缓存周转由真实 TTL、失效、商品更新和 Redis 重启产生。

L1 正向商品 TTL 默认十五秒。
Redis 正向商品 TTL 默认三十秒。

Redis 写入时按以下规则生成实际 TTL：

```text
actualTTL = baseTTL * (1 + random[-0.20, +0.20])
```

三十秒基础 TTL 的实际范围为二十四至三十六秒。
抖动只在写入时计算，命中不会刷新 TTL。

普通多级缓存场景启用抖动，使 MySQL 回源分散发生。
缓存雪崩场景关闭抖动，使一组商品使用相同 TTL。

## L1 容量演示

前端不能继续固定请求商品 ID 1。
缓存实验使用十二个商品 ID 的打乱循环序列。

Nginx 继续真实分配请求。
持续请求运行后，各实例逐步接触不同商品并形成不同 L1 内容。

当某实例收到第十个不同商品时，L1 返回真实淘汰 Delta。

```json
{
  "scope": "instance",
  "instanceId": "app-2",
  "operation": "remove",
  "productId": 4,
  "reason": "capacity_lru"
}
```

再次查询被淘汰商品时，预期产生 L1 miss 和 Redis hit。
这用于展示 Redis 作为共享 L2 对有限 L1 的补充作用。

## 批次响应协议

公共 `TrafficBatchRequest` 保持不变。
`TrafficBatchResult` 增加可选 `cache` 对象。

```json
{
  "cache": {
    "actualLookupCount": 1,
    "outcome": "found",
    "resolvedBy": "redis",
    "trace": [
      {"layer": "l1", "result": "miss"},
      {"layer": "redis", "result": "hit"}
    ],
    "product": {
      "id": 1,
      "name": "Architecture Practice Laptop",
      "category": "electronics",
      "version": 1
    },
    "simulatedLatencyMs": 300,
    "observedAt": "2026-07-18T10:00:00Z",
    "deltas": []
  }
}
```

`resolvedBy` 允许以下值：

* `l1`
* `redis`
* `mysql`
* `negative_cache`
* `bloom_filter`
* `degraded`

使用 `resolvedBy` 而不是 `hitLayer`，因为 MySQL 回源不属于缓存命中。

## 模拟耗时

模拟耗时由场景模板按真实 `resolvedBy` 映射。
Cache Processor 只把 `simulatedLatencyMs` 写入响应，不执行 `sleep`。

前端不得根据命中层自行计算耗时。
前端允许多个动画同时存在，并允许短路径动画超过长路径动画。

动画顺序不用于推导缓存状态、命中率、版本或回源次数。
所有业务状态必须来自后端响应和快照。

页面明确同时显示：

* 实际 HTTP 请求次数
* 实际缓存查询次数
* 教学等效查询数量
* 模拟耗时

## 缓存可观测状态

前端需要展示三台应用实例 L1 和 Redis L2 当前包含的商品。

### L1 Inventory

每个 L1 Store 提供只读 `Snapshot()`。
快照在读锁下返回有界商品摘要，不暴露内部指针或原始序列化数据。

### Inventory Reporter

每个 Lab App 将本实例 L1 摘要发布到会话 Redis。
摘要带实例 ID、观察时间和短过期时间。

Reporter 对频繁变化执行短暂合并，避免每个请求都发布完整摘要。
Redis 恢复后 Reporter 重新发布当前 L1。

Reporter 默认使用五百毫秒合并窗口。
实例摘要在 Redis 中使用五秒过期时间。

### Redis Inventory

Cache State Service 根据十二个已知商品 ID 执行有界 `MGET`。
它返回当前存在于 Redis 的正向商品条目和 TTL。

### Cache State Service

任意一台 Lab App 可以通过 Redis 汇总三台实例最近摘要和 L2 内容。

内部接口为：

```text
GET /internal/cache-state
```

Platform API 通过现有实验网关读取该状态。
浏览器不得直接访问内部接口。

## 批次 Delta 与快照校正

批次响应返回本次目标实例和 Redis 的即时变化。
前端立即应用这些 Delta。

现有实验快照增加可选 `cache` 对象。

```json
{
  "cache": {
    "catalog": [],
    "instances": [
      {
        "instanceId": "app-1",
        "status": "live",
        "observedAt": "...",
        "entries": []
      }
    ],
    "redis": {
      "status": "running",
      "observedAt": "...",
      "entries": []
    }
  }
}
```

`catalog` 返回十二个可查询商品的 ID、名称、分类和当前版本。
前端不得假设商品 ID 连续，也不得自行构造实验商品。

前端按作用域比较 `observedAt`。
较旧快照不得覆盖较新的批次 Delta。

Redis 不可用时，实验快照本身仍成功。
当前响应实例的 L1 可以是 `live`，其他实例摘要可以是 `stale`，Redis 为
`unavailable`。

无法确认的条目不得伪造成空缓存。

## 缓存控制动作

公开控制入口继续使用：

```text
POST /api/v1/labs/:id/actions
```

新增白名单动作：

* `UPDATE_SAMPLE_PRODUCT`
* `EXPIRE_CACHE_KEY`
* `SET_CACHE_POLICY`
* `PREWARM_CACHE`
* `RESTART_SESSION_REDIS`

前四个动作通过 Platform Worker 调用 Lab App 内部接口：

```text
POST /internal/cache-actions
```

内部接口只接受商品 ID、策略枚举和布尔开关。
它拒绝任意 Redis key、Redis 命令、容器参数和配置正文。

`RESTART_SESSION_REDIS` 映射到已有 Orchestrator COMMAND。
不新增缓存业务 COMMAND。

## 缓存失效

商品更新执行以下流程：

1. 更新实验 MySQL 商品和 `version`。
1. 删除对应 Redis L2 key。
1. 发布商品失效消息。
1. 当前实例删除自己的 L1。
1. 其他实例收到 Pub/Sub 后删除对应 L1。
1. 后续请求重新回源并读取新版本。

商品更新已提交但失效广播失败时，不能回滚 MySQL 更新。
操作返回成功并携带 `CACHE_INVALIDATION_DEGRADED` 警告。
其他实例依赖 L1 TTL 最终删除旧版本。

## 穿透、击穿和雪崩

### 缓存穿透

不存在商品是正常查询结果。
场景可以比较参数校验、布隆过滤器和空值缓存。

空值缓存和布隆过滤结果必须来自真实 Cache Processor。
前端不得自行减少 MySQL 回源计数。

### 缓存击穿

串行自动请求不能真实制造并发重建。
击穿实验先执行 `EXPIRE_CACHE_KEY`，再由前端并行提交最多八个现有批次请求。

每个并发 HTTP 请求仍只执行一次真实缓存查询。
Cache Processor 使用进程内 `singleflight` 和 Redis 互斥锁协调重建。

Redis 互斥锁使用带所有者令牌和过期时间的 `SET NX PX` 获取锁。
释放锁使用 `WATCH`、令牌比较和 `MULTI/EXEC`，或者等待锁 TTL 自动过期。
锁实现不得使用 Lua 脚本，也不得删除不属于当前请求的锁。

### 缓存雪崩

雪崩实验关闭 TTL 抖动并让有限样例商品使用相同 TTL。
前端使用受限并发批次观察集中回源。

普通场景重新启用 TTL 抖动、预热、回源限流和降级策略。

## Redis 故障与恢复

用户可以通过已有 COMMAND 真实重启会话 Redis。

Redis 重启期间：

* 已存在的 L1 继续服务。
* L1 miss 按当前策略回源 MySQL 或返回降级数据。
* Redis L2 状态为 `unavailable`。
* 其他实例缓存摘要可以为 `stale`。

Redis 恢复后：

* Redis 客户端自动重连。
* Pub/Sub 自动重新订阅。
* Reporter 重新发布三个实例的 L1 摘要。
* Redis 运行时策略键不存在时恢复模板默认值。
* 后续请求按真实访问重新预热 L2。

## 错误处理

| 情况 | 行为 |
|---|---|
| L1 条目损坏 | 删除条目并继续访问 Redis |
| Redis 商品值损坏 | 删除 key、记录异常并回源 MySQL |
| Redis 不可用 | 记录 `redis_error` 并回源或降级 |
| Pub/Sub 中断 | 自动重订阅并将远端摘要标记为 `stale` |
| MySQL 不可用且缓存命中 | 正常返回缓存结果 |
| MySQL 不可用且缓存未命中 | 返回降级结果或稳定错误码 |
| 不存在商品 | 返回正常 `not_found` |
| 缓存状态采集失败 | 实验快照成功，缓存部分为 `unavailable` |
| 单个持续批次失败 | 展示错误动画，持续生成不停止 |
| 并发突发部分限流 | 被限流请求失败，其余请求继续 |

稳定错误码包括：

* `CACHE_ACTION_NOT_ALLOWED`
* `CACHE_POLICY_INVALID`
* `CACHE_STATE_UNAVAILABLE`
* `REDIS_UNAVAILABLE`
* `CACHE_INVALIDATION_DEGRADED`
* `LAB_DATABASE_UNAVAILABLE`

## 安全边界

* Cache Processor 只访问当前实验数据库和会话 Redis。
* 所有 Redis key 必须由后端根据实验 ID 和商品 ID 生成。
* 浏览器不得提交容器地址、内部 Host、Redis key 或 Redis 命令。
* 缓存状态只返回样例商品元数据、版本、TTL 和命中状态。
* 缓存状态不得返回数据库密码、Redis 地址、容器 ID 或原始错误输出。
* 缓存动作继续校验登录、CSRF、实验归属和 `operationId`。
* 同一实验的缓存控制动作通过现有操作队列串行执行。

## 测试设计

### 单元测试

* 任意 `requestUnits` 每批只执行一次真实缓存查询。
* 冷缓存依次访问 L1、Redis 和 MySQL。
* Redis 命中回填目标实例 L1。
* L1 命中不访问 Redis 和 MySQL。
* 三个实例的 L1 相互独立。
* 第十个不同商品触发真实 L1 LRU 淘汰。
* TTL 到期、删除、版本更新和容量淘汰正确。
* `resolvedBy`、`trace` 和实际调用记录一致。
* 模拟耗时由真实 `resolvedBy` 和模板产生。
* 模拟耗时不得通过休眠延长 HTTP 响应。
* Redis 错误、损坏值和 MySQL 错误按策略降级。
* 较旧快照不能覆盖较新 Delta。

### 集成测试

使用真实 Redis 和实验 MySQL 验证：

1. App-1 冷查询从 MySQL 回源并回填。
1. App-2 查询同一商品真实命中 Redis。
1. App-1 再次查询真实命中 L1。
1. Redis `MGET` 状态只覆盖十二个已知商品。
1. 商品更新后 Redis key 和三台实例 L1 最终失效。
1. 新查询读取递增后的商品版本。
1. Redis 重启时已有 L1 仍可命中。
1. Redis 恢复后重新建立订阅和状态摘要。
1. 八个并发批次在保护开启时只产生受控重建。
1. 关闭保护后产生更多真实 MySQL 回源。

### Platform API 测试

* 缓存实验默认创建三台固定等权实例。
* 普通集群实验不要求返回 `cache`。
* 缓存实验必须返回合法缓存结果。
* 缓存动作继续校验认证、CSRF、归属和幂等。
* 缓存状态读取失败不导致整个实验快照失败。
* `RESTART_SESSION_REDIS` 映射到已有 COMMAND。

### 前端测试

* 三台实例分别展示自己的商品分类和缓存条目。
* Redis 独立展示共享商品内容和 TTL。
* 商品请求使用十二个 ID 的打乱循环。
* 动画路径完全使用后端 `trace`。
* 动画时间完全使用后端 `simulatedLatencyMs`。
* 后发 L1 动画可以超过先发 MySQL 动画。
* 超越不改变后端统计和缓存状态。
* 页面显示实际查询一次和教学等效数量。
* 批次 Delta 立即更新目标实例。
* 快照最终校正三个实例和 Redis。
* Redis 故障正确展示 `stale` 和 `unavailable`。
* 并发突发不创建新的持续调度链。

### 端到端验收

1. 创建缓存实验，确认三台应用实例和一个 Redis。
1. 冷查询显示 MySQL 回源。
1. 请求路由到另一实例并显示 Redis 命中。
1. 再次路由到已预热实例并显示 L1 命中。
1. 持续请求十二个商品并观察真实 L1 淘汰。
1. 查询被 L1 淘汰的商品并观察 Redis 命中。
1. 等待 Redis TTL 到期并观察周期性 MySQL 回源。
1. 修改商品并观察三个 L1 删除旧版本。
1. 触发热点过期和八个并发请求。
1. 比较保护开关前后的真实回源次数。
1. 重启 Redis 并观察 L1、回源、降级和恢复。
1. 运行阶段七和阶段八回归验收。

## 验收标准

* 缓存实验默认创建三台固定等权应用实例。
* 每个 HTTP 批次只执行一次真实缓存查询。
* `requestUnits` 明确表示教学等效数量。
* L1、Redis 和 MySQL 命中结果来自真实后端访问。
* 每台 L1 最多显示九个正向商品。
* Redis 可以显示十二个共享商品，但不设置逻辑数量上限。
* Redis TTL 到期后真实产生 MySQL 回源。
* L1 淘汰后再次查询可以真实命中 Redis。
* 三台实例和 Redis 的商品分类、版本和 TTL 可观察。
* 模拟耗时由后端返回，且不延长 HTTP 响应。
* 前端允许动画超越，但不从动画推导业务状态。
* Redis 故障不影响其他实验或平台理论页面。
* 阶段七和阶段八行为与测试保持不变。
