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
* `GET /api/v1/courses/:id`
* `POST /api/v1/auth/login`
* `POST /api/v1/auth/logout`
* `GET /api/v1/auth/me`
* `POST /api/v1/labs`
* `GET /api/v1/labs/:id`
* `POST /api/v1/labs/:id/actions`
* `POST /api/v1/labs/:id/reset`
* `DELETE /api/v1/labs/:id`
* `GET /api/v1/labs/:id/events`

实验动作请求必须包含唯一 `operationId`、动作类型、可选目标实例和受限参数对象。
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

模板固定镜像摘要、资源限制、健康检查、环境变量白名单、挂载规则、标签和网络策略。

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

### 7.4 被延后的数据能力

MVP 延后以下表：

* `lab_operation_events`
* `lab_runtime_snapshots`
* `product_cache_versions`
* `scenario_settings`
* `lab_audit`

对应的历史审计、运行回放、版本历史和长期场景参数保存不属于首版核心目标。

### 7.5 lab_resources

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
* `GET /internal/runtime-state`

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

每个订单批次按当前时间桶剩余容量处理：

* `received = batchSize`
* `processable = min(batchSize, remainingCapacity)`
* `dropped = received - processable`
* `remainingCapacity = max(0, remainingCapacity - processable)`

### 11.3 状态判定

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
4. 平台更新有效容量。
5. 固定模式不改权重。
6. 自适应模式等待下一次控制循环重新计算。

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
* Redis 重启和回退

### 17.3 端到端测试

覆盖：

* 匿名浏览
* 登录与创建实验
* 扩容、缩容与性能调整
* 固定和自适应负载均衡
* 订单丢失
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
* 共享 MySQL 中每实验独立数据库和临时账号。
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
