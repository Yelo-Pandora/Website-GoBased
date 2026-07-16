# 前端生成流量与结果驱动动画实施计划

## 1. 目标

实现已批准的前端生成流量方案：浏览器按本地定时器生成请求批次，
通过 `platform-api` 同步提交，实验应用返回实际路由、处理量、丢弃量和处理后负载。
前端根据结果驱动小球成功、分裂、丢弃或错误动画。

本计划不新增单机实验，不实现 SSE、Event Hub、有界事件环、断线补发或后端 Traffic Generator。

## 2. 固定契约

公开端点：

```text
POST /api/v1/labs/:id/traffic-batches
GET  /api/v1/labs/:id
```

批次请求：

```json
{
  "batchId": "batch-8f3c",
  "productId": 1,
  "requestUnits": 10
}
```

正常业务结果统一为 `200 OK`，包括全部丢弃。结果必须包含：

```text
batchId
labId
status: processed | partially_processed | dropped
path
targetInstanceId
receivedUnits
processedUnits
droppedUnits
instanceState.effectiveCapacity
instanceState.remainingCapacity
instanceState.loadRatio
instanceState.loadState
```

前端页面同一时间最多一个未完成批次请求。自动批次不更新
`last_effective_action_at`，不写 `lab_operations`，不自动重试。

用户可见错误码固定为：

```text
TRAFFIC_REQUEST_INVALID
AUTH_REQUIRED
CSRF_INVALID
LAB_NOT_FOUND
LAB_NOT_RUNNING
RATE_LIMITED
LAB_UNAVAILABLE
```

统一限流默认按已认证用户每秒 10 个批次、突发 10 个，不按实验设置最小请求间隔。

## 3. 实施顺序

### Step 1：运行时配置和数据契约

目标：让各服务共享同一批次、容量和响应结构。

文件范围：

- `internal/protocol/`：新增或扩展批次请求、结果和实例状态 DTO。
- `services/platform-api/internal/app/config.go`：增加批次代理超时、统一限流速率和突发值配置。
- `services/platform-api/internal/app/config_test.go`：覆盖默认值、非法值和边界值。
- `services/platform-api/internal/traffic/types.go`：定义平台内部批次请求、结果、错误和路径类型。
- `contracts/http/platform-api.openapi.yaml`：保持已批准的公开请求和响应 Schema。
- `configs/scenarios/*_scenario_v1.json`：确认请求量范围和容量窗口来源仍由场景模板提供；不新增未知字段，避免破坏严格模板解析。应用集群、应用与数据分离、多级缓存和缓存故障各自使用固定版本模板。

实现要点：

1. 服务端永远使用接收时间计算容量窗口。
2. `receivedUnits = requestUnits` 且 `processedUnits + droppedUnits = receivedUnits`。
3. `path` 由后端根据快照拓扑和实际目标实例生成。
4. 内部错误保留详细日志，外部只返回七个稳定错误码。

验收：JSON/YAML 解析通过，配置测试通过，OpenAPI 契约测试覆盖 `200`、全部丢弃和七类错误。

### Step 2：Lab App 商品读取与容量批次处理

目标：让真实实验应用能够处理一个同步批次，并返回实例身份和处理后负载。

文件范围：

- `services/lab-app/internal/app/config.go`：读取实验数据库连接、实验身份、容量基线、时间窗口和性能比例。
- `services/lab-app/internal/app/server.go`：创建数据库连接、商品仓储、容量状态和 HTTP 服务依赖。
- `services/lab-app/internal/product/`：新增商品读取仓储和商品 ID 校验。
- `services/lab-app/internal/order/`：新增互斥容量窗口、部分处理/全部丢弃计算和 `order_stats` 聚合。
- `services/lab-app/internal/runtime/`：保存实例身份、容量状态、负载状态和受控状态快照。
- `services/lab-app/internal/httpapi/handler.go`：新增 `POST /internal/order-batch`，保留健康检查和运行状态接口。
- `services/lab-app/internal/httpapi/router.go`：注册内部批次路由，不向浏览器公开。
- `services/orchestrator/internal/control/service.go`：为应用容器注入数据库、实例和容量配置，并校验返回资源身份。
- `services/orchestrator/internal/control/service_test.go`：覆盖应用环境变量和实例标签映射。

实现要点：

1. 同一实例内的容量切换、扣减和聚合计数在同一个互斥临界区完成。
2. 数据库 upsert 在释放容量锁后执行。
3. 不创建请求队列，不引入非零处理延迟，不按等效订单写明细。
4. 响应包含 `targetInstanceId`、`receivedUnits`、`processedUnits`、`droppedUnits`、
   `remainingCapacity`、`loadRatio` 和 `loadState`。
5. Lab App 不接受客户端提交的实例身份、容量或 Docker 参数。

测试：

- `product`：存在商品、不存在商品、实验数据库隔离。
- `order`：全部成功、部分成功、全部丢弃、时间窗切换、并发互斥和计数不覆盖。
- `httpapi`：请求体大小、字段校验、身份回显和统一错误结构。
- 真实 MySQL 集成测试：复合唯一键 upsert 和重置后的聚合清空。

### Step 3：Platform API 同步批次代理

目标：让浏览器只能通过公开平台接口访问实验流量面。

文件范围：

- `services/platform-api/internal/traffic/client.go`：固定目标 URL、Host、超时、响应体大小和 JSON 校验。
- `services/platform-api/internal/traffic/service.go`：认证后实验归属、`Running` 状态、模板范围、错误映射和结果校验。
- `services/platform-api/internal/traffic/limiter.go`：按已认证用户维护有界 HTTP 限流器，默认 10/s、burst 10。
- `services/platform-api/internal/httpapi/handler.go`：新增 `submitTrafficBatch`，解析请求并映射七个错误码。
- `services/platform-api/internal/httpapi/router.go`：注册认证、CSRF 保护的 `/labs/:id/traffic-batches`。
- `services/platform-api/internal/app/server.go`：构造批次代理、限流器和实验服务并注入 Router。
- `services/platform-api/internal/httpapi/*_test.go`：路由、CSRF、归属、状态、错误和响应映射测试。
- `services/platform-api/internal/traffic/*_test.go`：代理超时、错误内容、身份不匹配、部分结果和全丢弃测试。

内部调用约束：

1. `platform-api` 根据 `labId` 固定构造 `labId.lab.internal` 的 Host。
2. 浏览器不能提交内部 URL、Host、容器名或目标实例。
3. Lab App 返回的 `labId`、`batchId`、`targetInstanceId` 必须与平台状态匹配。
4. 合法但全部丢弃返回 `200`；网关、实例、超时和响应校验错误统一返回 `LAB_UNAVAILABLE`。
5. 批次请求不使用 `operationId`，不进入持久操作 Worker。

### Step 4：实验快照与生命周期接入

目标：在没有 SSE 的情况下，页面仍能恢复拓扑、实例状态和异步操作。

文件范围：

- `services/platform-api/internal/lab/session.go`：增加快照、拓扑和流量策略 DTO。
- `services/platform-api/internal/lab/repository.go`：查询实例、资源、最新操作、容量和过期字段。
- `services/platform-api/internal/httpapi/handler.go`：新增 `GET /labs/:id`。
- `services/platform-api/internal/httpapi/router.go`：注册归属保护的快照路由。
- `services/platform-api/internal/lifecycle/`：实现空闲十分钟、最长三十分钟、终止提交和周期性资源核对。
- `services/platform-api/internal/operation/`：保留创建、重置、结束等异步操作；不得接收高频批次。
- `docs/frontend-api-integration-guide.md` 对应快照示例：加入拓扑节点/边和 `trafficPolicy`。

实现要点：

1. 页面刷新后默认停止本地流量生成。
2. 异步操作执行中每秒轮询快照，稳定运行时每三至五秒轮询。
3. 自动批次不更新 `last_effective_action_at`。
4. 快照是恢复事实来源，批次响应只负责当前小球和即时服务器颜色。
5. 不增加 `GET /operations/:id`，操作结果从 `latestOperation` 获取。

测试：

- 快照归属和不存在实验统一返回 `LAB_NOT_FOUND`。
- Running、Terminating、Terminated 对批次请求的状态映射。
- 自动批次不刷新空闲计时。
- 异步操作轮询看到 `pending -> running -> succeeded/failed`。
- 生命周期回收不会删除其他实验资源。

### Step 5：Vue 请求和动画状态机

目标：实现严格串行请求、结果驱动分裂和后端负载颜色。

文件范围：

- `web/src/api/platform.js`：封装带 Cookie、CSRF 和统一错误解析的请求客户端。
- `web/src/features/lab/labApi.js`：封装快照和批次接口。
- `web/src/features/lab/trafficState.js`：实现纯状态机，不依赖具体 Canvas/SVG/DOM 绘制技术。
- `web/src/features/lab/trafficState.test.js`：使用 Node 测试或现有前端测试方案覆盖状态转换。
- `web/src/components/LabExperiment.vue`：页面加载、快照轮询、生成器本地启停和错误提示。
- `web/src/components/TrafficStage.vue`：把 `BallModel` 渲染为用户池、网关、服务器、线路和小球。
- `web/src/components/InstanceStatus.vue`：按 `loadState` 和 `loadRatio` 显示服务器状态。
- `web/src/App.vue`：从 Swagger 单页入口切换为实验工作台，并保留独立 OpenAPI 查验入口。
- `web/src/styles/base.css`：实验画布、状态颜色、数字标签和错误状态样式。
- `web/package.json`：增加前端测试命令；不引入消息推送依赖。

状态机：

```text
created -> moving_to_gateway -> waiting_response
waiting_response -> processed | partially_processed | dropped | error
partially_processed -> accepted_ball + dropped_ball
processed -> absorbed
dropped -> diverted -> removed
error -> returned_or_faded
```

约束：

1. 一个页面最多一个未完成批次 POST。
2. 请求一生成就移动到网关，同时发起 POST。
3. 响应先到则缓存结果；小球先到则停在网关等待。
4. 部分成功在网关把原球替换成两个分别标注处理量和丢弃量的球。
5. 服务器颜色和容量数字完全来自响应 `instanceState`。
6. `4xx/5xx` 不计入 processed 或 dropped，不自动重试。
7. 请求响应结束后才启动下一生成周期，已有结果动画可以继续完成。

测试：

- 全部成功、部分成功、全部丢弃和七种错误球。
- 响应先到、球先到、响应延迟和网络失败。
- 严格串行：第二个批次不能在第一个响应前发出。
- 服务器颜色使用后端状态，不使用前端累计推算。
- 刷新后生成器停止，快照可以恢复服务器状态和拓扑。

### Step 6：端到端与发布验收

目标：证明真实链路、动画契约和安全边界一致。

验收场景：

1. 登录用户创建或恢复一个 Running 实验。
2. 浏览器提交批次，真实经过 Edge、Platform API、Lab Gateway 和 Lab App。
3. 单实例全部成功时小球进入实际实例。
4. 容量不足时返回部分成功，两个新球的数字之和等于原球数量。
5. 全部丢弃返回 `200`，不触发 HTTP 错误球。
6. 网关不可用、超时或响应身份错误返回 `503 LAB_UNAVAILABLE`。
7. 跨用户、缺少 CSRF、非法请求量和过快请求分别命中稳定错误码。
8. 扩容后成功球进入响应中的实际 `targetInstanceId`。
9. 自动流量不延长空闲实验；到期后批次返回 `LAB_NOT_RUNNING`。
10. 浏览器无法直接请求 Lab Gateway、Lab App、Docker、MySQL 或 Redis。

命令：

```text
gofmt -w <changed-go-files>
go test ./...
go vet ./...
docker compose config --quiet
npm --prefix web run build
npm --prefix web test
```

## 4. 建议提交拆分

1. `feat: 增加实验应用同步批次处理`
2. `feat: 增加平台批次代理与统一限流`
3. `feat: 增加实验快照和生命周期轮询`
4. `feat: 增加前端结果驱动流量动画`
5. `test: 完成前端生成流量端到端验收`

每个提交都应通过与影响范围相称的测试；不要把前端动画、Lab App 容量计算和平台代理压成一个无法独立回滚的提交。
