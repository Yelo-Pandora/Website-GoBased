# 互联网后端架构学习平台软件设计说明书

* 文档类型：Software Design Specification（SDS）
* 文档版本：0.1
* 基线日期：2026-07-12
* 上游需求基线：2026-07-11 SRS（R1-R50）
* 设计状态：已在对话中确认
* 实施状态：未开始实现

## 1. 文档目的

本文档定义互联网后端架构学习平台首期 MVP 的实施级软件设计。
本文档只基于现有 SRS 与本轮已确认的设计决策，不考虑项目仓库内已有代码。
设计结果用于后续实施计划、模块拆分、接口定义、部署设计和测试设计。

## 2. 设计范围

本文档覆盖：

* 系统分解与部署边界
* 控制面与高权限编排层设计
* 内部接口契约和操作时序
* 精简数据模型与 E-R 设计
* 实验应用容器、缓存和负载模型
* Nginx 动态配置与实验数据库生命周期
* 故障恢复、可观测性、安全与测试设计

本文档不覆盖：

* 仓库中已有无关代码的分析或复用
* MVP 范围外的后续课程实现
* 具体代码、项目脚手架和实施计划

## 3. 设计驱动约束

* 部署在单台 4 核 8G Linux 服务器。
* 同时在线测试用户不超过 10 名。
* 前端使用 Vue，后端使用 Go，入口使用 Nginx。
* 平台数据库和实验数据库使用同一个 MySQL 容器。
* Redis 只在缓存实验中按会话创建。
* 所有运行组件均容器化。
* 一台模拟应用服务器对应一个真实 Docker 容器。
* 高负载教学效果不得依赖真实压满宿主机资源。
* 用户只能执行白名单内的引导式操作。
* 普通 Web 服务不得直接拥有 Docker 管理权限。

## 4. 总体架构

### 4.1 部署拓扑

~~~mermaid
flowchart TB
    Browser["Browser / Vue SPA"]
    Edge["edge-nginx"]
    API["platform-api"]
    Sock["Unix Domain Socket"]
    Orch["orchestrator"]
    Proxy["docker-socket-proxy"]
    Engine["Docker Engine"]
    LabNginx["lab-gateway-nginx"]
    MySQL["shared-mysql"]
    Apps["lab app containers"]
    Redis["lab redis container"]

    Browser --> Edge
    Edge --> API
    API --> MySQL
    API --> LabNginx
    API --> Sock
    Sock --> Orch
    Orch --> Proxy
    Proxy --> Engine
    Engine --> Apps
    Engine --> Redis
    Orch --> MySQL
    LabNginx --> Apps
    Apps --> MySQL
    Apps --> Redis
~~~

### 4.2 分层职责

访问层由 Vue SPA 和公网入口 Nginx 构成。
它负责页面、登录、理论内容、实验动作发起和 SSE 消费。

平台控制层由模块化单体 `platform-api` 构成。
它负责决定“应该发生什么”，但不直接操作 Docker、实验数据库 DDL 或 Nginx 文件。

高权限编排层由独立 `orchestrator` 构成。
它负责将结构化命令安全地转换为 Docker、Nginx 和 MySQL 生命周期操作。

实验运行层由共享实验 Nginx、实验应用容器、会话 Redis 和实验数据库构成。
商品查询、缓存命中、订单模拟和容量判断都在该层真实发生。

### 4.3 Docker 网络分区

基础网络分为：

* `edge-net`：连接公网入口 Nginx 与 `platform-api`
* `platform-data-net`：连接 `platform-api` 与共享 MySQL
* `lab-control-net`：连接流量生成模块与实验 Nginx
* `orchestration-net`：只连接 `orchestrator` 与 Docker Socket Proxy
* `db-admin-net`：只连接 `orchestrator` 与共享 MySQL 的管理入口

Edge Nginx 是唯一公网入口。
共享 MySQL 额外发布 `127.0.0.1:3306`，只供宿主机本地的受信任数据库管理工具使用。
该回环地址映射在 MVP 发布后继续保留，但不得改为监听全部网卡。

每个活动实验另建独立网络：

`lab-{labId}-net`

该网络只连接：

* 当前实验的应用容器
* 当前实验的 Redis
* 共享实验 Nginx
* 共享 MySQL

共享实验 Nginx 和共享 MySQL 可以动态加入多个实验网络。
实验应用容器不能加入其他实验网络。
`platform-api` 不加入实验网络，流量统一通过 `lab-control-net` 进入实验 Nginx。

## 5. platform-api 设计

### 5.1 模块分解

~~~mermaid
flowchart TB
    HTTP["HTTP Router / SSE Router"]
    Auth["Auth Service"]
    Course["Course Service"]
    Lab["Lab Session Service"]
    Action["Action Coordinator"]
    Queue["Operation Queue Worker"]
    Traffic["Traffic Generator"]
    Adaptive["Adaptive Balancer"]
    Event["Event Hub"]
    Scheduler["Lifecycle Scheduler"]
    Repo["Repositories"]

    HTTP --> Auth
    HTTP --> Course
    HTTP --> Lab
    HTTP --> Action
    HTTP --> Event
    Action --> Repo
    Action --> Queue
    Action --> Event
    Queue --> Lab
    Queue --> Traffic
    Queue --> Adaptive
    Queue --> Repo
    Scheduler --> Action
~~~

### 5.2 Auth Service

负责登录、退出、密码哈希校验、服务端会话、CSRF 和登录限流。
它只向下游提供已认证用户上下文。

### 5.3 Course Service

负责知识地图、理论课程、待完成占位页和学习进度。
课程正文与后端实现说明以静态内容管理，数据库只保存课程元数据。

### 5.4 Lab Session Service

负责实验状态机、拓扑快照、活动实验约束和超时判断。
状态集合为：

* `Preparing`
* `Running`
* `Expiring`
* `Failed`
* `Terminating`
* `Terminated`

### 5.5 Action Coordinator

所有实验状态变更必须经过 Action Coordinator。

它负责：

* 校验登录、实验归属和资源归属
* 校验场景动作白名单
* 校验参数范围和资源配额
* 处理 `operationId` 幂等
* 串行化同一实验的拓扑变更
* 持久化 `lab_operations`
* 返回 `202 Accepted`

### 5.6 Operation Queue Worker

耗时动作使用 MySQL 持久化操作队列。

~~~mermaid
stateDiagram-v2
    [*] --> pending
    pending --> claimed
    claimed --> running
    running --> succeeded
    running --> failed
    failed --> compensating
    claimed --> pending: lease expired
    running --> pending: lease expired
~~~

Worker 使用租约领取任务。
平台重启后，过期租约任务可以重新领取。
队列采用“至少一次执行”语义。
所有基础设施命令必须使用 `operationId` 和资源标签实现幂等，不能假定任务只执行一次。

### 5.7 Traffic Generator

Traffic Generator 是 `platform-api` 内部有界后台任务。
它以低真实速率向实验 Nginx 发送请求，并为每个请求附加教学等效订单批次。
它不独立部署，也不得无限创建 goroutine。

### 5.8 Adaptive Balancer

Adaptive Balancer 每两秒读取实例容量和负载快照。
它使用平滑窗口、持续阈值和冷却时间抑制权重抖动。
固定模式下不自动调整权重。
自适应模式下按有效容量生成目标 upstream 模型。

### 5.9 Event Hub

Event Hub 为每个实验维护有界内存事件环。
它负责递增事件序号、SSE 广播和短期补发。
高频事件不逐条写入 MySQL。

### 5.10 Lifecycle Scheduler

Lifecycle Scheduler 负责：

* 空闲 10 分钟超时
* 最长 30 分钟超时
* 即将过期提醒
* 自动终止
* 周期性资源核对触发

### 5.11 外部 API 与事件契约

外部 API 保持以下资源边界：

* `GET /api/v1/courses`
* `GET /api/v1/courses/:slug`
* `POST /api/v1/auth/login`
* `POST /api/v1/auth/logout`
* `GET /api/v1/auth/me`
* `POST /api/v1/labs`
* `GET /api/v1/labs/:id`
* `POST /api/v1/labs/:id/actions`
* `POST /api/v1/labs/:id/reset`
* `DELETE /api/v1/labs/:id`
* `GET /api/v1/labs/:id/events`

部署和诊断端点单独提供：

* `GET /healthz`
* `GET /readyz`
* `GET /api/v1/system/info`

`/healthz` 用于容器和入口存活检查，不写入 Swagger UI 的业务接口清单。
实验创建、拓扑调整和生命周期变更请求必须包含唯一 `operationId`。
具体实验动作还应包含动作类型、可选目标实例和受限参数对象。
耗时动作返回 `202 Accepted`，完成状态通过查询接口和 SSE 获取。

SSE 事件统一包含：

* `eventId`
* `labId`
* `eventType`
* `occurredAt`
* `payload`

浏览器断线重连时，必须先获取完整实验快照，再使用最后事件序号继续订阅。

## 6. orchestrator 设计

### 6.1 通信方式

`platform-api` 与 `orchestrator` 通过以下 Unix Domain Socket 通信：

`/run/platform/orchestrator.sock`

`orchestrator` 不监听 TCP 端口。
只有两个控制面容器挂载该 socket 目录。
socket 所在命名卷只授予两个服务的共享用户组访问权限。
文件模式使用 `0660`，编排器启动时必须安全清理陈旧 socket 文件后再监听。

### 6.2 模块分解

~~~mermaid
flowchart TB
    Socket["UDS Listener"]
    Validator["Command Validator"]
    Templates["Template Registry"]
    DockerOps["Docker Operator"]
    NginxOps["Nginx Fragment Manager"]
    DbOps["Lab Database Provisioner"]
    Reconcile["Resource Reconciler"]
    Proxy["Docker Socket Proxy"]
    MySQL["MySQL Stored Procedures"]

    Socket --> Validator
    Validator --> Templates
    Templates --> DockerOps
    Templates --> NginxOps
    Templates --> DbOps
    Reconcile --> DockerOps
    Reconcile --> NginxOps
    DockerOps --> Proxy
    DbOps --> MySQL
~~~

### 6.3 结构化命令集合

允许的命令包括：

* `PROVISION_LAB`
* `RESET_LAB`
* `DESTROY_LAB`
* `CREATE_APP_INSTANCE`
* `DELETE_APP_INSTANCE`
* `UPDATE_APP_CAPACITY`
* `CREATE_SESSION_REDIS`
* `RESTART_SESSION_REDIS`
* `DELETE_SESSION_REDIS`
* `APPLY_LAB_UPSTREAM`
* `REMOVE_LAB_UPSTREAM`
* `RESET_LAB_DATABASE`
* `RECONCILE_RESOURCES`

编排器拒绝：

* 任意 shell 命令
* 原始 Docker CLI 参数
* 原始 SQL
* 原始 Nginx 文本
* 用户提供的镜像、挂载和特权配置

### 6.4 UDS 命令契约

请求消息统一包含：

* `commandType`
* `commandId`
* `operationId`
* `labId`
* `requestedBy`
* `payload`

响应消息统一包含：

* `commandId`
* `operationId`
* `status`
* `result`
* `error`

`commandId` 标识一次内部调用，`operationId` 标识用户侧幂等操作。
编排器必须根据 `operationId`、Docker 标签和目标资源状态识别重复命令。
传输协议采用 HTTP/JSON over Unix Domain Socket。
命令入口固定为 `POST /v1/commands`，并限制请求体大小、连接超时和命令执行超时。

### 6.5 模板注册表

固定模板包括：

* `app_container_v1`
* `cache_app_container_v1`
* `session_redis_v1`
* `session_network_v1`
* `lab_nginx_fragment_v1`
* `lab_database_profile_v1`
* `application_cluster_scenario_v1`

模板固定镜像摘要、资源限制、健康检查、环境变量白名单、挂载规则、标签和网络策略。

模板同时是实验初始值的唯一配置来源。
模板名称包含版本号，例如 `app_container_v1`；已经启动的实验始终使用创建时选定的版本，后续新增 `v2` 不得静默改变运行中的实验。
MVP 不增加 `scenario_settings` 表，也不允许用户长期保存任意模板变体。

场景模板至少定义：

* 基础 Docker CPU 配额和内存上限
* 基础教学容量和容量时间窗
* 初始性能百分比及允许调整范围
* 初始 Nginx 权重
* 订单批次大小和生成间隔
* 超载策略、并发控制方式和队列开关
* 商品种子数据

场景模板引用资源模板，并集中给出该场景的教学初始值。
`application_cluster_scenario_v1` 的示例如下。
该示例是 SDS 规定的 MVP 默认值；它引用的资源模板负责固定实际镜像摘要和容器安全配置。

~~~json
{
  "templateId": "application_cluster_scenario_v1",
  "templateVersion": 1,
  "scenarioType": "application_cluster",
  "resourceTemplates": {
    "application": "app_container_v1",
    "database": "lab_database_profile_v1",
    "network": "session_network_v1",
    "nginxFragment": "lab_nginx_fragment_v1"
  },
  "resources": {
    "baseCpuLimitCores": 0.1,
    "memoryLimitMb": 128,
    "pidsLimit": 64
  },
  "capacity": {
    "baseCapacity": 100,
    "capacityWindowMs": 1000,
    "initialPerformancePercent": 100,
    "minPerformancePercent": 20,
    "maxPerformancePercent": 100
  },
  "loadBalancing": {
    "initialWeight": 100
  },
  "orderSimulation": {
    "defaultBatchSize": 60,
    "defaultGenerationIntervalMs": 1000,
    "processingDelayMs": 0,
    "overloadPolicy": "drop_excess",
    "queueMode": "disabled",
    "concurrencyControl": "mutex",
    "preserveArrivalOrder": false
  },
  "productSeeds": [
    {
      "id": 1,
      "name": "Architecture Practice Laptop",
      "category": "electronics",
      "price": 6999.00,
      "currency": "CNY",
      "stockLabel": "in_stock",
      "description": "Product used by the application cluster lab.",
      "version": 1
    },
    {
      "id": 2,
      "name": "Distributed Systems Handbook",
      "category": "books",
      "price": 129.00,
      "currency": "CNY",
      "stockLabel": "in_stock",
      "description": "Product used by cache and database labs.",
      "version": 1
    }
  ]
}
~~~

## 7. 数据设计

### 7.1 数据域

系统数据分为三类：

* 平台控制数据：`shared-mysql.platform`
* 运行时瞬态数据：平台和实验应用内存
* 实验业务数据：每实验独立数据库 `lab_xxxx`

### 7.2 精简后的 platform E-R 图

~~~mermaid
erDiagram
    USERS ||--o{ AUTH_SESSIONS : has
    USERS ||--o{ COURSE_PROGRESS : tracks
    USERS ||--o{ LAB_OPERATIONS : requests
    COURSES ||--o{ COURSE_PROGRESS : contains
    USERS ||--o{ LAB_SESSIONS : owns
    COURSES ||--o{ LAB_SESSIONS : starts
    LAB_SESSIONS ||--o{ LAB_INSTANCES : contains
    LAB_SESSIONS ||--o{ LAB_RESOURCES : contains
    LAB_SESSIONS ||--o{ LAB_OPERATIONS : queues

    USERS {
        bigint id PK
        varchar username UK
        varchar password_hash
        varchar status
        datetime created_at
        datetime updated_at
    }

    AUTH_SESSIONS {
        bigint id PK
        bigint user_id FK
        varchar session_token_hash
        varchar csrf_token_hash
        datetime expires_at
        datetime last_seen_at
        datetime created_at
        datetime revoked_at
    }

    COURSES {
        bigint id PK
        varchar slug UK
        varchar title
        varchar category
        varchar status
        int sort_order
        text summary
        datetime created_at
        datetime updated_at
    }

    COURSE_PROGRESS {
        bigint id PK
        bigint user_id FK
        bigint course_id FK
        boolean viewed
        datetime last_viewed_at
        varchar last_lab_id
        datetime updated_at
    }

    LAB_SESSIONS {
        varchar id PK
        bigint user_id FK
        bigint course_id FK
        varchar scenario_type
        varchar scenario_template_id
        varchar status
        varchar balancing_mode
        varchar mysql_db_name
        varchar mysql_db_user
        boolean redis_enabled
        datetime started_at
        datetime last_effective_action_at
        datetime terminated_at
        varchar termination_reason
        datetime created_at
        datetime updated_at
    }

    LAB_INSTANCES {
        bigint id PK
        varchar lab_id FK
        varchar instance_name
        varchar container_id
        varchar status
        decimal cpu_limit_cores
        int memory_limit_mb
        int performance_percent
        int effective_capacity
        int current_weight
        datetime created_at
        datetime updated_at
    }

    LAB_RESOURCES {
        bigint id PK
        varchar lab_id FK
        varchar resource_type
        varchar resource_name
        varchar external_id
        varchar status
        json metadata_json
        datetime created_at
        datetime updated_at
    }

    LAB_OPERATIONS {
        bigint id PK
        varchar operation_id UK
        varchar lab_id FK
        bigint requested_by FK
        varchar action
        varchar target_instance_id
        varchar status
        json payload_json
        json result_json
        varchar error_code
        text error_message
        varchar lease_owner
        datetime lease_expires_at
        int attempt_count
        datetime created_at
        datetime updated_at
        datetime completed_at
    }
~~~

### 7.3 精简后的实验数据库 E-R 图

~~~mermaid
erDiagram
    PRODUCTS ||--o{ ORDER_STATS : referenced_by

    PRODUCTS {
        bigint id PK
        varchar name
        varchar category
        decimal price
        char currency
        varchar stock_label
        text description
        int version
        datetime updated_at
    }

    ORDER_STATS {
        bigint id PK
        bigint product_id FK
        varchar instance_name
        datetime time_bucket
        int received_orders
        int processed_orders
        int dropped_orders
        datetime updated_at
    }
~~~

`order_stats` 必须对 `(product_id, instance_name, time_bucket)` 建立复合唯一约束，保证并发聚合使用原子 upsert 而不是插入重复时间桶。

### 7.4 初始值来源规则

字段初始值分为以下五类：

* 数据库生成：由自增主键、默认值或时间戳规则产生。
* 模板产生：由创建实验时选定的不可变版本模板产生。
* 系统生成：由平台、编排器或实验应用根据运行身份产生。
* 运行时计算：根据模板值、用户动作或请求内容计算产生。
* 请求产生：由流量生成器或用户请求携带。

实验数据库不得为了记录来源而增加 `value_source` 一类字段。
来源规则属于 SDS 和模板契约，由初始化、运行时逻辑和测试保证。

#### 7.4.1 实验业务表字段来源

| 表.字段 | 首次产生方式 | 初始值或规则 | 后续变化 |
| --- | --- | --- | --- |
| `products.id` | 模板产生 | 使用 `productSeeds[].id`，保证演示中商品 ID 稳定 | MVP 不变 |
| `products.name` | 模板产生 | 使用商品种子名称 | 商品演示操作可更新 |
| `products.category` | 模板产生 | 使用商品种子分类 | 商品演示操作可更新 |
| `products.price` | 模板产生 | 使用商品种子价格 | 商品演示操作可更新 |
| `products.currency` | 模板产生 | 默认模板值为 `CNY` | MVP 不变 |
| `products.stock_label` | 模板产生 | 使用商品种子库存标签 | 商品演示操作可更新 |
| `products.description` | 模板产生 | 使用商品种子说明 | 商品演示操作可更新 |
| `products.version` | 模板产生 | 初始值为 `1` | 每次商品更新后递增 |
| `products.updated_at` | 数据库生成 | 插入种子行时使用数据库当前时间 | 更新商品时自动刷新 |
| `order_stats.id` | 数据库生成 | 自增主键 | 不变 |
| `order_stats.product_id` | 请求产生 | 使用模拟订单批次中的商品 ID | 不变 |
| `order_stats.instance_name` | 系统生成 | 使用实际接收请求的容器实例名 | 不变 |
| `order_stats.time_bucket` | 运行时计算 | 按模板的 `capacityWindowMs` 对接收时间向下取整 | 不变 |
| `order_stats.received_orders` | 运行时计算 | 首次聚合时为该批次订单数 | 同时间桶内累加 |
| `order_stats.processed_orders` | 运行时计算 | 首次聚合时为容量内订单数 | 同时间桶内累加 |
| `order_stats.dropped_orders` | 运行时计算 | 首次聚合时为超出容量的订单数 | 同时间桶内累加 |
| `order_stats.updated_at` | 数据库生成 | 首次写入时使用数据库当前时间 | 每次聚合写入时自动刷新 |

创建实验数据库时先建表并写入 `products` 模板种子数据。
`order_stats` 初始为空，只在模拟订单实际到达后产生聚合记录。

#### 7.4.2 实例容量字段来源

`baseCapacity` 不属于实验数据库字段，也不在 `lab_instances` 中重复保存。
它来自创建实验时选定的版本化模板，并表示一台实例在
`performancePercent = 100` 时，每个 `capacityWindowMs` 能处理的教学等效订单数。

| 值或字段 | 首次产生方式 | 初始值或规则 | 后续变化 |
| --- | --- | --- | --- |
| `baseCapacity` | 模板产生 | `application_cluster_scenario_v1` 为每秒 `100` 个教学等效订单 | 运行中的实验不变 |
| `capacityWindowMs` | 模板产生 | `application_cluster_scenario_v1` 为 `1000` 毫秒 | 运行中的实验不变 |
| `lab_sessions.scenario_template_id` | 系统生成 | 创建实验时记录所选模板 ID，MVP 默认 `application_cluster_scenario_v1` | 不变 |
| `lab_instances.performance_percent` | 模板产生 | `initialPerformancePercent`，MVP 为 `100` | 用户可在 `20` 至 `100` 间调整 |
| `lab_instances.cpu_limit_cores` | 运行时计算 | `baseCpuLimitCores * performancePercent / 100` | 性能调整成功后更新 |
| `lab_instances.memory_limit_mb` | 模板产生 | `memoryLimitMb`，MVP 为 `128` | MVP 不随性能调整变化 |
| `lab_instances.effective_capacity` | 运行时计算 | `baseCapacity * performancePercent / 100` | 性能调整成功后更新 |
| `lab_instances.current_weight` | 模板或控制器产生 | 固定模式初始为 `initialWeight`；自适应模式由控制器计算 | 自适应控制循环可更新 |
| `lab_instances.instance_name` | 系统生成 | 在实验内按创建顺序生成稳定名称 | 不变 |
| `lab_instances.container_id` | 系统生成 | Docker 创建容器后返回 | 容器重建时更新 |
| `lab_instances.status` | 系统生成 | 创建时为 `provisioning`，健康检查通过后为 `running` | 随生命周期变化 |

实例恢复默认性能时恢复到模板的 `initialPerformancePercent`，而不是恢复到用户最近一次设置的值。
模板升级必须使用新的模板版本，不能改变现有实验读取到的 `baseCapacity`、容量时间窗或资源基线。
平台通过 `lab_sessions.scenario_template_id` 重新定位运行实验使用的不可变模板。

### 7.5 被延后的数据能力

MVP 延后以下表：

* `lab_operation_events`
* `lab_runtime_snapshots`
* `product_cache_versions`
* `scenario_settings`
* `lab_audit`

对应的历史审计、运行回放、版本历史和长期场景参数保存不属于首版核心目标。

### 7.6 lab_resources

`lab_resources` 记录非模拟服务器资源：

* Redis 容器
* Docker 网络
* Nginx 配置片段
* 实验数据库
* 实验数据库账号
* 流量任务

`lab_instances` 始终只表示应用服务器实例。

## 8. 实验数据库生命周期

### 8.1 独立数据库与临时账号

MySQL 使用“共享容器 + 每实验独立数据库与账号”方案。

示例：

* `platform`
* `lab_a81f` + `lab_a81f_user`
* `lab_b39c` + `lab_b39c_user`

### 8.2 受限存储过程

`orchestrator` 只调用：

* `provision_lab_database`
* `reset_lab_database`
* `destroy_lab_database`

存储过程负责命名校验、建库、建账号、授权、初始化表和清理资源。
初始化表后，存储过程使用本次实验选定模板的 `productSeeds` 写入 `products`。
模板种子写入必须具备幂等性；相同实验的重试不得产生重复商品。
`order_stats` 不写入模板行，建库和重置完成后该表为空。

### 8.3 权限原则

* `platform-api` 不拥有建库删库权限。
* `orchestrator` 不拼接任意 DDL。
* 实验应用账号只访问自己的 `lab_xxxx` 数据库。
* 平台库与实验库之间不建立跨库外键。

## 9. 实验应用容器设计

### 9.1 单一实验应用镜像

所有模拟应用服务器共用同一个 Go 镜像。
通过模块配置启用：

* `product-read`
* `order-sim`
* `cache-l1`
* `cache-l2`
* `cache-protection`
* `metrics-export`
* `health`

### 9.2 启动配置

采用“最小环境变量 + 只读配置文件”。

环境变量包含：

* `LAB_ID`
* `INSTANCE_ID`
* `SCENARIO_TYPE`
* `DB_HOST`
* `DB_PORT`
* `DB_NAME`
* `DB_USER`
* `DB_PASSWORD`
* `REDIS_ADDR`
* `MODULES`

场景配置文件路径：

`/config/scenario.json`

### 9.3 内部模块

~~~mermaid
flowchart TB
    HTTP["HTTP Server"]
    Health["Health Module"]
    Product["Product Read Module"]
    Order["Order Simulation Module"]
    L1["L1 Cache Module"]
    L2["Redis L2 Adapter"]
    Protect["Cache Protection Module"]
    Metrics["Runtime Metrics Module"]
    Egress["Result Egress Module"]
    Store["MySQL Access Layer"]

    HTTP --> Health
    HTTP --> Product
    HTTP --> Order
    Product --> L1
    Product --> L2
    Product --> Store
    Product --> Protect
    Order --> Metrics
    Order --> Egress
    L2 --> Protect
    L2 --> Store
~~~

### 9.4 内部接口

* `GET /healthz`
* `GET /internal/products/:id`
* `POST /internal/order-batch`
* `PUT /internal/runtime-capacity`
* `GET /internal/runtime-state`

### 9.5 MVP 订单模拟执行模型

每个应用服务器容器在进程内维护自己的当前容量时间窗、剩余教学容量和聚合计数。
该状态不在不同容器之间共享；请求实际被 Nginx 分配到哪个容器，就消耗哪个容器的容量。

`POST /internal/order-batch` 采用同步、无队列处理：

1. 校验商品 ID 和批次订单数。
2. 获取该容器订单容量状态的进程内互斥锁。
3. 根据接收时间切换或复用当前容量时间窗。
4. 在锁内计算可处理数和丢失数，并一次性扣减剩余容量。
5. 更新内存聚合计数后释放互斥锁。
6. 释放锁后，对 `order_stats` 执行基于复合唯一键的原子 upsert，再将当前批次结果返回给调用方。

互斥锁只保证同一容器内并发请求不会重复扣减容量或覆盖计数。
互斥锁不保证公平性，也不保证并发请求严格按照到达时间取得处理资格。
MVP 不创建进程内请求队列，不引入 Kafka、RabbitMQ 或 Redis Stream，不重试被丢弃订单，也不使用消息队列维持订单先后顺序。
这里延后的只是模拟订单请求队列，不影响平台控制面继续使用第 5.5 节定义的 MySQL 持久化操作队列。

`processingDelayMs` 在 MVP 固定为 `0`。
“已处理”表示订单获得教学容量并完成记账模拟，不表示容器为每个订单执行真实耗时计算或休眠。
这种设计保证演示负载可见，但不会为了制造高负载而真实压满 4 核 8 GB 宿主机。

### 9.6 未来实现：请求排队模型

以下能力明确延后，不属于 MVP：

* 每容器有界请求队列
* 工作协程池和非零订单服务时间
* 排队等待超时和队列满拒绝策略
* 严格到达顺序或显式顺序号
* 失败重试、幂等消费和容器删除前排空

未来实现前必须另行确定队列容量、工作协程数、服务时间分布、等待超时和顺序语义。
MVP 不预留空表或空容器，只保留模板字段和接口兼容空间。

## 10. 缓存设计

### 10.1 缓存层次

查询顺序为：

1. 应用进程内 L1
2. 会话 Redis L2
3. 实验 MySQL

### 10.2 Redis Key

* `lab:{labId}:product:{productId}`
* `lab:{labId}:null-product:{productId}`
* `lab:{labId}:mutex:{productId}`
* `lab:{labId}:bloom:products`

### 10.3 缓存失效

商品更新后：

1. 更新 `products.version`
2. 删除 Redis key
3. 发布失效消息
4. 所有应用实例删除本地 L1
5. 后续请求重新回源

### 10.4 保护策略

* 参数校验
* 空值缓存
* 布隆过滤器
* `singleflight`
* Redis 互斥锁
* TTL 抖动
* 预热
* 回源限流
* 降级返回

## 11. 负载与容量模型

### 11.1 真实资源层

由 Docker cgroup 提供：

* CPU 配额
* 内存上限
* PID 限制
* 健康状态

### 11.2 教学容量层

使用：

`effectiveCapacity = baseCapacity * performancePercent / 100`

其中：

* `baseCapacity` 来自创建实验时选定的版本化模板。
* `performancePercent` 初始来自模板的 `initialPerformancePercent`，MVP 为 `100`。
* `effectiveCapacity` 的单位是每个 `capacityWindowMs` 可处理的教学等效订单数。
* `application_cluster_scenario_v1` 默认时间窗为 `1000` 毫秒，因此初始有效容量为每秒 `100` 个教学等效订单。

每个订单批次按当前时间桶剩余容量处理：

* `received = batchSize`
* `processable = min(batchSize, remainingCapacity)`
* `dropped = received - processable`
* `remainingCapacity = max(0, remainingCapacity - processable)`

若一个批次只有部分订单落在剩余容量内，容量内部分记为 `processed`，超出部分全部记为 `dropped`。
若剩余容量为零，则该批次的所有订单立即丢弃。
丢弃是最终结果，不进入等待队列，也不在后续时间窗补处理。

每个新时间窗开始时：

`remainingCapacity = effectiveCapacity`

时间窗切换、容量扣减和计数更新必须在同一进程内互斥临界区中完成。
互斥临界区只执行常数时间的状态计算，不得包含数据库写入、网络调用或人为延迟。

### 11.3 真实资源与教学容量同步

模板定义 `baseCpuLimitCores`、`baseCapacity` 和初始性能比例。
实例创建时同时计算并应用：

* `cpuLimitCores = baseCpuLimitCores * performancePercent / 100`
* `effectiveCapacity = baseCapacity * performancePercent / 100`

性能调整操作只有在 Docker CPU quota 修改成功后，才提交新的
`performance_percent`、`cpu_limit_cores` 和 `effective_capacity` 平台快照。
编排器随后通过实验应用的受控内部配置入口更新容器内存中的 `performancePercent` 和 `effectiveCapacity`。
如果容器内配置更新失败，操作不得报告成功，并由 Worker 重试或执行补偿，使 Docker 配额、平台快照和容器教学容量最终一致。

运行中的容器不得自行猜测 `baseCapacity`，也不得从实时 CPU 使用率反推教学容量。
实际 CPU 指标只用于展示和诊断，教学容量始终由模板基线与性能比例确定。

### 11.4 状态判定

`loadRatio = assignedEquivalentLoad / effectiveCapacity`

* `0 <= loadRatio <= 0.3`：`idle`
* `0.3 < loadRatio <= 0.7`：`normal`
* `0.7 < loadRatio <= 1.0`：`high`
* `loadRatio > 1.0`：`overloaded`
* 健康检查失败：`unavailable`

实际 CPU 指标和教学等效负载必须分别展示。

## 12. Nginx 设计

### 12.1 共享实验 Nginx

使用一个共享 `lab-gateway-nginx`。
每个实验生成独立配置片段：

`/conf.d/labs/{labId}.conf`

### 12.2 配置应用

~~~mermaid
flowchart LR
    Build["生成片段"] --> Temp["写临时文件"]
    Temp --> Lock["获取全局配置锁"]
    Lock --> Test["nginx -t"]
    Test --> Swap["原子替换"]
    Swap --> Reload["优雅重载"]
    Reload --> Unlock["释放配置锁"]
~~~

配置校验失败时继续使用旧配置。
删除实例时先移除 upstream，再排空并删除容器。
配置目录通过只允许 `orchestrator` 写入的共享卷提供给实验 Nginx。
`nginx -t` 和优雅重载通过 Docker API 在固定的 `lab-gateway-nginx` 容器内执行。
用户输入不能选择目标容器或覆盖执行命令。

## 13. 关键流程

### 13.1 创建实验

~~~mermaid
sequenceDiagram
    participant U as User
    participant API as platform-api
    participant DB as platform DB
    participant W as Queue Worker
    participant OR as orchestrator
    participant MY as shared MySQL
    participant DK as Docker
    participant NG as lab-gateway-nginx

    U->>API: POST /labs
    API->>DB: 创建会话和操作
    API-->>U: 202 Accepted
    W->>DB: claim operation
    W->>OR: PROVISION_LAB
    OR->>MY: provision_lab_database
    OR->>DK: 创建网络和初始容器
    OR->>NG: 应用实验片段
    OR-->>W: success
    W->>DB: 状态改为 Running
~~~

### 13.2 扩容

1. API 校验归属、配额和幂等。
2. 操作写入 MySQL 队列。
3. Worker 调用 `CREATE_APP_INSTANCE`。
4. 编排器创建容器并等待健康检查。
5. 编排器更新 Nginx 片段。
6. 平台保存实例快照并推送 SSE。

### 13.3 调整性能

1. 用户选择实例和 20%-100% 的性能比例。
2. Worker 调用 `UPDATE_APP_CAPACITY`。
3. 编排器真实修改 Docker CPU quota。
4. 编排器更新目标容器的教学容量配置。
5. 平台提交性能比例、CPU 配额和有效容量快照。
6. 固定模式不改权重。
7. 自适应模式等待下一次控制循环重新计算。

### 13.4 删除实例

1. 校验删除后至少保留一台。
2. 从 upstream 移除目标实例。
3. 校验并 reload Nginx。
4. 等待短暂排空。
5. 删除容器。
6. 更新持久状态。

## 14. 故障处理与恢复

### 14.1 原则

* 先保守拒绝，再尝试恢复。
* 通知失败不等于资源失败。
* 高权限动作必须可补偿或可核对。
* 恢复当前真实状态优先于回放完整历史。

### 14.2 平台 API 故障

已运行的实验容器和 Nginx 配置继续存在。
恢复后从 MySQL、Docker 标签和 Nginx 片段重建当前状态。
过期租约操作可以重新领取。

### 14.3 orchestrator 故障

新高权限操作失败并返回稳定错误码。
理论内容、登录和当前实验只读观察仍可用。
`platform-api` 不得绕过编排器直接操作 Docker 或 MySQL。

### 14.4 Redis 故障

影响仅限当前实验。
应用先使用 L1。
L1 未命中时按当前策略回源或返回降级结果。

### 14.5 状态核对

核对来源：

* `platform` 数据库
* Docker 标签资源
* Nginx 配置片段

孤儿容器、网络、Redis、实验数据库和 Nginx 片段必须可被识别并清理。

## 15. 安全设计

### 15.1 权限边界

* 普通用户只能操作自己的实验。
* `platform-api` 无 Docker 权限。
* `platform-api` 无实验数据库管理权限。
* `orchestrator` 无公网入口。
* 实验容器无平台数据库权限。
* 实验容器无 Docker 权限。
* MySQL 本机管理入口只绑定 `127.0.0.1`，不得作为远程管理接口。

### 15.2 容器基线

所有实验容器必须：

* 非 root
* 非特权
* 只读根文件系统
* 无宿主机目录挂载
* 无 Docker Socket
* 无公网端口
* 具备 CPU、内存和 PID 上限
* 仅加入实验网络

### 15.3 输入验证

必须严格校验：

* `labId`
* `instanceId`
* `operationId`
* `performancePercent`
* 动作白名单
* 商品 ID
* 流量档位和缓存策略参数

## 16. 可观测性

至少提供：

* 结构化日志
* 实验状态快照查询
* 活动实验和队列积压指标
* 容器创建、Nginx 更新和 Redis 重启计数
* 订单处理与丢失聚合
* 缓存命中与回源聚合

日志不得包含密码、session token、CSRF token 或实验数据库密码。

## 17. 测试设计

### 17.1 单元测试

覆盖：

* 认证、CSRF 和归属校验
* 实验状态机
* 操作幂等和租约
* 容量与订单丢失计算
* 容量时间窗切换和并发互斥一致性
* 模板初始值映射和容量派生计算
* 自适应权重算法
* 缓存策略
* Nginx 片段渲染
* 编排命令校验

### 17.2 集成测试

覆盖：

* MySQL 持久化队列
* UDS 通信
* Docker 资源生命周期
* MySQL 存储过程
* Nginx 配置校验与 reload
* 商品查询与缓存失效
* 超载订单立即丢弃且不跨时间窗补处理
* Docker CPU 配额、平台快照和容器教学容量同步
* Redis 重启和回退

### 17.3 端到端测试

覆盖：

* 匿名浏览
* 登录与创建实验
* 扩容、缩容与性能调整
* 固定和自适应负载均衡
* 订单丢失
* 并发订单批次下无重复扣减和无计数覆盖
* 缓存穿透、击穿和雪崩
* Redis 重启
* 实验重置、主动结束和超时回收

### 17.4 安全测试

覆盖：

* 跨用户实验越权
* CSRF 失败
* 非法标识符和越界参数
* 任意 Docker 参数、SQL 和 Nginx 文本注入
* 实验容器访问平台库、Docker Socket 或其他实验资源

## 18. 设计取舍总结

* 控制面采用模块化单体，降低常驻资源开销。
* 高权限能力集中在独立编排器，缩小攻击面。
* 使用真实 cgroup 限制与教学等效负载，不真实打满宿主机。
* MVP 订单模拟使用每容器互斥容量计数，超载订单立即丢弃且不进入队列。
* 请求队列、严格顺序、非零处理耗时和重试保留为未来实现。
* 共享 MySQL 中每实验独立数据库和临时账号。
* Edge Nginx 是唯一公网入口，MySQL 仅保留回环地址管理端口。
* 使用精简数据模型，优先保证运行、回收和恢复。
* 共享实验 Nginx 使用每实验独立片段和全局串行 reload。
* 测试优先覆盖控制链路、安全边界和恢复能力。

## 19. 自检结论

* 文档不依赖项目仓库中的现有代码。
* 文档与现有 SRS 的 R1-R50 范围保持一致。
* 文档不存在未解决的占位项。
* 控制面、编排层和实验运行层职责边界清晰。
* 数据模型、缓存模型、负载模型和资源生命周期相互一致。
* 安全设计与测试设计覆盖主要破坏面。
