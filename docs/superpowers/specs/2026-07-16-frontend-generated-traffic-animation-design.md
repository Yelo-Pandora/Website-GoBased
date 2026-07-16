# 前端生成实验流量与结果驱动动画设计

* 文档类型：实施设计修订
* 日期：2026-07-16
* 状态：已由用户批准
* 影响范围：SRS、SDS、后端 MVP 实施顺序、前端 API、OpenAPI

## 1. 设计目标

实验页面使用小球表示一批教学等效请求。小球由当前浏览器页面按本地定时器生成，
后端不再维护 Traffic Generator。每个小球生成后立即发起一次同步批次请求，
实际路由、处理量、丢弃量和服务器负载状态全部以后端结果为准。

本设计不新增单机架构实验。应用集群实验以一台初始实例运行时，前端可呈现为
用户池、实验网关和单服务器之间的请求链路；扩容后继续使用同一协议显示实际目标实例。

## 2. 已批准决策

* 浏览器通过 `platform-api` 调用公开批次接口，不直接访问 Lab Gateway 或 Lab App。
* 小球生成和开始、停止状态只存在于当前页面，刷新后默认停止。
* 一个小球代表 `requestUnits` 个教学等效请求，请求量受场景模板范围和步长限制。
* 当前页面严格串行发送批次，同一时刻最多一个未完成 POST。
* 后端成功完成容量判定时统一返回 `200 OK`，包括全部丢弃。
* 自动批次不刷新实验空闲时间。
* 平台使用统一 HTTP 限流，不增加单实验最小请求间隔；默认按已认证用户限制为每秒 10 个批次、突发 10 个。
* MVP 删除 SSE、Event Hub、有界事件环、断线补发和后端 Traffic Generator。
* 实验创建、重置和结束继续使用持久操作队列；批次请求不进入 `lab_operations`。

## 3. 外部数据流

~~~text
Browser
  -> Edge Nginx
  -> POST /api/v1/labs/:id/traffic-batches
  -> platform-api
  -> Lab Gateway Nginx
  -> actual Lab App instance
  -> platform-api
  -> Browser
~~~

`platform-api` 负责认证、CSRF、归属、实验状态、模板参数和统一限流校验，
并根据已验证的 `labId` 构造固定内部目标。浏览器不能提交内部地址、Host、
容器名称或目标实例。

Lab Gateway 依据当前 upstream 把请求分配到实际实例。Lab App 返回自己的实例身份、
容量计算结果和处理后的负载状态。`platform-api` 校验响应身份后再返回浏览器。

## 4. 公开批次接口

~~~http
POST /api/v1/labs/{labId}/traffic-batches
Content-Type: application/json
X-CSRF-Token: <token>
~~~

请求：

~~~json
{
  "batchId": "batch-8f3c",
  "productId": 1,
  "requestUnits": 10
}
~~~

`batchId` 只用于关联前端小球和同步响应，不作为持久操作 ID。
前端不自动重试失败批次。后端使用接收时间计算容量窗口，不信任客户端时间。
响应中的 `path` 由后端根据实验快照拓扑和实际 `targetInstanceId` 生成，前端不得修改。

成功响应：

~~~json
{
  "data": {
    "result": {
      "batchId": "batch-8f3c",
      "labId": "lab-123",
      "status": "partially_processed",
      "occurredAt": "2026-07-16T10:00:00Z",
      "path": ["user-pool", "lab-gateway", "app-1"],
      "targetInstanceId": "app-1",
      "receivedUnits": 10,
      "processedUnits": 6,
      "droppedUnits": 4,
      "instanceState": {
        "effectiveCapacity": 100,
        "remainingCapacity": 0,
        "loadRatio": 1.04,
        "loadState": "overloaded",
        "capacityWindowStartedAt": "2026-07-16T10:00:00Z",
        "capacityWindowEndsAt": "2026-07-16T10:00:01Z"
      }
    }
  }
}
~~~

`status` 只允许 `processed`、`partially_processed` 和 `dropped`。
响应必须满足：

~~~text
receivedUnits = requestUnits
processedUnits + droppedUnits = receivedUnits
~~~

## 5. 动画状态机

1. 前端生成原球，球上显示 `requestUnits`，同时发起 POST。
2. 原球沿用户池到实验网关的线路移动。
3. 如果响应先到，前端缓存结果并等待球到达网关；如果球先到，则停在网关等待。
4. 全部成功时，原球继续进入 `targetInstanceId` 对应服务器并融入。
5. 部分成功时，原球在网关替换成两个球：成功球显示 `processedUnits` 并进入服务器，
   丢弃球显示 `droppedUnits` 并偏离线路后消失。
6. 全部丢弃时，原球显示 `droppedUnits`，偏离线路后消失。
7. `4xx`、`5xx` 或网络失败时，原球变为错误球并退回或淡出，
   不计入 `processedUnits` 或 `droppedUnits`。
8. 服务器颜色在响应到达时立即使用 `instanceState` 更新，前端不得自行累计推算负载。
9. 当前 POST 返回后才启动下一生成周期，但已经取得结果的动画可以继续完成。

## 6. 快照与轮询

`GET /api/v1/labs/:id` 是页面恢复的事实来源。快照增加语义拓扑和请求量策略：

~~~json
{
  "topology": {
    "nodes": [
      {"id": "user-pool", "type": "user_pool"},
      {"id": "lab-gateway", "type": "gateway"},
      {"id": "app-1", "type": "application"}
    ],
    "edges": [
      {"from": "user-pool", "to": "lab-gateway"},
      {"from": "lab-gateway", "to": "app-1"}
    ]
  },
  "trafficPolicy": {
    "requestUnits": {
      "minimum": 1,
      "maximum": 100,
      "step": 1,
      "default": 60
    }
  }
}
~~~

快照不再包含 `traffic.running`、后端生成周期或 `lastEventId`。
异步控制操作执行中建议每秒轮询，稳定运行时每三至五秒轮询。

## 7. 错误契约

外部错误码精简为：

| HTTP | 错误码 | 含义 |
| ---: | --- | --- |
| 400 | `TRAFFIC_REQUEST_INVALID` | 批次字段、商品或请求量无效 |
| 401 | `AUTH_REQUIRED` | Session 无效 |
| 403 | `CSRF_INVALID` | CSRF 校验失败 |
| 404 | `LAB_NOT_FOUND` | 实验不存在或不属于当前用户 |
| 409 | `LAB_NOT_RUNNING` | 实验不处于可接收流量的状态 |
| 429 | `RATE_LIMITED` | 触发平台统一 HTTP 限流 |
| 503 | `LAB_UNAVAILABLE` | 实验网关、实例、内部超时或响应校验失败 |

容量不足不是 HTTP 错误。全部丢弃仍返回 `200 OK` 和 `status: "dropped"`。

## 8. 持久化与一致性

批次不写入 `lab_operations`，也不保存逐批次明细。Lab App 继续按
`(product_id, instance_name, time_bucket)` 聚合 `order_stats`。

Lab App 可能已经处理批次，但响应在返回浏览器前断开。MVP 不自动重试，
前端显示错误球；后续快照和下一批响应恢复服务器状态和聚合统计。

## 9. 生命周期边界

自动批次属于数据面流量，不是有效控制操作，不更新 `last_effective_action_at`。
空闲十分钟、最长三十分钟、自动终止和周期性资源核对继续保留。

MVP 删除：

* `GET /api/v1/labs/:id/events`
* SSE、Event Hub、有界事件环和断线补发
* `START_TRAFFIC`、`STOP_TRAFFIC`
* 后端 Traffic Generator
* `TRAFFIC_SAMPLE`、`ORDERS_DROPPED` 事件

## 10. 验收范围

* 验证全部成功、部分成功和全部丢弃的数量守恒。
* 验证实际 Nginx 路由实例与 `targetInstanceId` 一致。
* 验证响应包含处理后的容量和负载状态。
* 验证批次不刷新空闲时间。
* 验证前端同一页面最多一个未完成 POST。
* 验证响应先到和球先到两种动画时序。
* 验证部分成功生成两个分别标注处理量和丢弃量的球。
* 验证七个稳定错误码和错误球行为。
* 验证浏览器不能控制内部目标地址或实例身份。
* 验证页面刷新后生成器默认停止并通过快照恢复。
