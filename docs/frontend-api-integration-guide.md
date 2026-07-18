# 前端 API 对接指南

本文基于当前代码、OpenAPI、SRS 和 SDS，整理预期向前端开放的 URL
端点及 JSON 示例。

本文用于前端 Mock、页面状态设计和后端接口评审。
它不是已经冻结的最终 API 契约。

示例中的 ID、Token 和时间只用于说明字段形状。
除文中明确给出的范围和枚举外，示例值不构成后端约束。

前端快速使用本文的顺序如下：

1. 先阅读第 3 节，确认端点当前属于哪个实现阶段。
2. 使用第 5 至第 7 节的 JSON 建立 Course、Auth 和 Lab Mock。
3. 对实验异步变更统一模拟 `202 Accepted` 和 Operation 对象。
4. 使用第 7.3 节模拟同步批次响应，把小球动画和实例状态更新到最新结果；异步操作通过 Lab Snapshot 轮询。
5. 使用第 9 节的稳定错误码驱动页面错误分支。
6. 在后端正式开发前评审第 13 节的开放问题。

## 1. 状态标记

本文使用以下状态：

- `available`：当前代码已经实现并通过 Compose 验收。
- `reserved`：OpenAPI 和 Go Router 已注册，但业务能力尚未实现。
- `planned`：SDS 已定义 URL 和语义，但 OpenAPI 与代码尚未实现。
- `needs-review`：前端对接需要，但 SDS 尚未固定具体契约。

`reserved` 端点用于验证认证、CSRF 和占位错误响应，当前返回
`501 Not Implemented`。
前端若提前开发完整实验流程，仍需对尚未实现的成功响应使用 Mock。

## 2. 基础调用约定

### 2.1 Base URL

本地 Compose 环境使用同源访问：

```text
http://127.0.0.1:8080
```

平台 API 前缀为：

```text
/api/v1
```

前端代码应优先使用相对 URL，例如 `/api/v1/courses`。
Edge Nginx 会把 `/api/` 请求代理到 `platform-api`。

### 2.2 通用请求头

JSON 请求建议使用：

```http
Accept: application/json
Content-Type: application/json
```

认证使用服务端 Session Cookie。
浏览器请求必须允许携带 Cookie：

```javascript
fetch('/api/v1/auth/me', {
  credentials: 'include',
  headers: {
    'Accept': 'application/json',
  },
});
```

### 2.3 Session 与 CSRF

SDS 已确认以下安全规则：

- Session Token 由服务端随机生成。
- Session Cookie 使用 `HttpOnly` 和 `SameSite=Strict`。
- 当前 HTTP 部署不能设置 `Secure`，迁移 HTTPS 后必须启用。
- Session Token 和 CSRF Token 不得进入 URL 或日志。
- 需要认证的状态变更请求必须通过 CSRF 校验。

当前接口固定使用以下名称：

```http
Cookie: session=<http-only-token>
X-CSRF-Token: <csrf-token>
```

登录和 `auth/me` 响应在 JSON 中返回 `csrfToken`，由前端保存在内存中。
页面刷新后应先调用 `auth/me`，重新取得同一 Session 对应的 CSRF Token。

### 2.4 operationId

所有实验状态变更请求必须携带唯一 `operationId`。
该字段用于浏览器重试幂等和同一实验操作串行化。

本文示例使用 UUID v4：

```json
{
  "operationId": "4f6ed5ac-1d24-49df-8de3-efac91d09a65"
}
```

SDS 只要求唯一性，没有固定 UUID 格式。
最终格式需要在 OpenAPI 中确认。

### 2.5 时间和命名

建议遵守以下规则：

- JSON 字段使用 `camelCase`。
- 时间使用 UTC ISO 8601，例如 `2026-07-13T06:30:00Z`。
- 列表始终返回数组，即使只有一个元素。
- 不存在的可选对象使用 `null`，不使用空字符串。
- 金额使用 JSON number，币种使用三位大写代码。

## 3. 端点总览

| 状态 | 方法 | URL | 用途 |
| --- | --- | --- | --- |
| `available` | GET | `/healthz` | Edge Nginx 存活检查 |
| `available` | GET | `/api/v1/system/info` | 脚手架构建信息 |
| `available` | GET | `/api/v1/courses` | 课程和知识地图列表 |
| `available` | GET | `/api/v1/courses/:slug` | 课程详情和理论内容 |
| `available` | POST | `/api/v1/auth/login` | 测试账户登录 |
| `available` | POST | `/api/v1/auth/logout` | 注销当前 Session |
| `available` | GET | `/api/v1/auth/me` | 获取当前登录用户 |
| `available` | POST | `/api/v1/labs` | 创建实验 |
| `available` | GET | `/api/v1/labs/:id` | 获取完整实验快照 |
| `implemented` | POST | `/api/v1/labs/:id/actions` | 提交白名单实验动作 |
| `available` | POST | `/api/v1/labs/:id/reset` | 重置实验 |
| `available` | DELETE | `/api/v1/labs/:id` | 主动结束实验 |
| `implemented` | POST | `/api/v1/labs/:id/traffic-batches` | 提交一个前端生成的请求批次 |

## 4. 当前可用端点

### 4.1 `GET /healthz`

状态：`available`

该端点由 Edge Nginx 直接响应。
它只表示公网入口进程可用，不表示 MySQL 或平台 API 已就绪。

请求没有 JSON Body。

成功响应：`200 OK`

```json
{
  "service": "edge-nginx",
  "status": "ok"
}
```

### 4.2 `GET /api/v1/system/info`

状态：`available`

该端点用于当前脚手架页面显示平台身份和构建版本。

请求没有 JSON Body。

成功响应：`200 OK`

```json
{
  "service": "platform-api",
  "status": "scaffold",
  "version": "dev",
  "commit": "unknown",
  "labGatewayAddr": "http://lab-gateway-nginx:8080"
}
```

`labGatewayAddr` 是当前诊断字段，包含 Docker 内部服务地址。
前端业务代码不应依赖或直接请求该地址。

## 5. 课程端点

课程允许匿名读取。
有效 Session 用户的响应会附带已经存在的个人学习进度。

### 5.1 `GET /api/v1/courses`

状态：`implemented`

请求没有 JSON Body。

成功响应：`200 OK`

```json
{
  "data": {
    "courses": [
      {
        "id": 1,
        "slug": "standalone-architecture",
        "title": "单机架构",
        "category": "foundation",
        "status": "theory",
        "sortOrder": 10,
        "summary": "理解应用、数据和入口集中在单机时的职责与限制。",
        "labAvailable": false,
        "progress": null
      },
      {
        "id": 3,
        "slug": "application-cluster",
        "title": "应用集群与负载均衡",
        "category": "application",
        "status": "active",
        "sortOrder": 30,
        "summary": "通过应用容器和内部 Nginx 观察容量与流量分配。",
        "labAvailable": true,
        "progress": {
          "viewed": true,
          "lastViewedAt": "2026-07-13T06:30:00Z",
          "lastLabId": "lab_01J2M8Y5A4D7KQ2V9N6P3R1T0X"
        }
      }
    ]
  },
  "meta": {
    "authenticated": true,
    "total": 12
  }
}
```

明确来源字段：

- `id`
- `slug`
- `title`
- `category`
- `status`
- `sortOrder`
- `summary`
- `progress.viewed`
- `progress.lastViewedAt`
- `progress.lastLabId`

`labAvailable`、`data` Envelope 和 `meta` 已纳入正式 OpenAPI 契约。
`meta.authenticated` 由服务端 Session 解析结果产生，不由前端传入。

课程状态当前数据库允许：

```json
["theory", "active", "coming_soon"]
```

### 5.2 `GET /api/v1/courses/:slug`

状态：`implemented`

路径参数使用课程的稳定 `slug`，OpenAPI 已固定该语义。

请求没有 JSON Body。

成功响应：`200 OK`

```json
{
  "data": {
    "course": {
      "id": 3,
      "slug": "application-cluster",
      "title": "应用集群与负载均衡",
      "category": "application",
      "status": "active",
      "sortOrder": 30,
      "summary": "通过应用容器和内部 Nginx 观察容量与流量分配。",
      "content": "# 应用集群与负载均衡\n\n一台应用忙不过来时……",
      "contentFormat": "markdown",
      "implementation": {
        "requestPath": [
          "lab-gateway-nginx",
          "lab-app",
          "shared-mysql"
        ],
        "keyConcepts": [
          "fixed-weight-balancing",
          "adaptive-balancing",
          "effective-capacity"
        ]
      },
      "lab": {
        "available": true,
        "scenarioType": "application_cluster",
        "allowedInstanceRange": {
          "min": 1,
          "max": 4
        }
      },
      "progress": {
        "viewed": true,
        "lastViewedAt": "2026-07-13T06:30:00Z",
        "lastLabId": null
      }
    }
  }
}
```

`content` 是 UTF-8 Markdown 正文，`contentFormat` 当前固定为 `markdown`。
`implementation` 和 `lab` 提供调用链、关键概念与实验可用范围。
登录用户成功读取详情后，服务端会把 `viewed` 设为 `true` 并更新 `lastViewedAt`。
匿名用户读取详情不会创建 `course_progress` 记录。

错误：

- `404 COURSE_NOT_FOUND`

## 6. 认证端点

MVP 不提供公开注册。
只有初始化脚本或受信任本机维护预创建且未禁用的测试账户可以登录。
默认本地验收账号为 `learner`，密码为 `example-password`。

### 6.1 `POST /api/v1/auth/login`

状态：`available`

请求 JSON：

```json
{
  "username": "learner",
  "password": "example-password"
}
```

成功响应：`200 OK`

```json
{
  "data": {
    "user": {
      "id": 1,
      "username": "learner",
      "status": "active"
    },
    "csrfToken": "csrf_9a7c1f6b2d4e8a0c",
    "sessionExpiresAt": "2026-07-13T14:30:00Z"
  }
}
```

同时返回 Session Cookie：

```http
Set-Cookie: session=<opaque-token>; HttpOnly; SameSite=Strict; Path=/
```

错误：

- `400 VALIDATION_FAILED`
- `401 INVALID_CREDENTIALS`
- `403 ACCOUNT_DISABLED`
- `429 LOGIN_RATE_LIMITED`
- `500 INTERNAL_ERROR`

默认限流策略为 5 分钟内最多 5 次失败，超过后阻止 10 分钟。
登录成功后会清除该用户名和客户端地址对应的失败记录。

### 6.2 `POST /api/v1/auth/logout`

状态：`available`

请求需要 Session Cookie 和 CSRF Header。
请求没有 JSON Body。

成功响应：`200 OK`

```json
{
  "data": {
    "loggedOut": true
  }
}
```

服务端撤销 Session，并返回过期 Cookie。

错误：

- `401 AUTH_REQUIRED`
- `403 CSRF_INVALID`
- `500 INTERNAL_ERROR`

### 6.3 `GET /api/v1/auth/me`

状态：`available`

请求没有 JSON Body。

成功响应：`200 OK`

```json
{
  "data": {
    "user": {
      "id": 1,
      "username": "learner",
      "status": "active"
    },
    "csrfToken": "csrf_9a7c1f6b2d4e8a0c",
    "sessionExpiresAt": "2026-07-13T14:30:00Z"
  }
}
```

`activeLabId` 尚未返回，将在实验控制面实现时确定恢复活动实验入口的契约。

未登录响应：`401 Unauthorized`

```json
{
  "error": {
    "code": "AUTH_REQUIRED",
    "message": "authentication is required"
  },
  "requestId": "9d7d45bd25bf4b7e8d89998ad66578d6"
}
```

## 7. 实验端点

所有实验读取和操作都必须验证登录状态、实验归属和目标实例归属。
每个账户同时最多拥有一个活动实验。

活动状态包括：

```json
[
  "Preparing",
  "Running",
  "Expiring",
  "Terminating"
]
```

完整状态集合为：

```json
[
  "Preparing",
  "Running",
  "Expiring",
  "Failed",
  "Terminating",
  "Terminated"
]
```

### 7.1 `POST /api/v1/labs`

状态：`available`

阶段五已接通实验创建闭环。请求先在平台数据库中创建 `Preparing` 会话和
`CREATE_LAB` 操作，再由租约 Worker 调用受限 UDS 编排器；编排结果会持久化到
`lab_sessions`、`lab_instances` 和 `lab_resources`。资源创建失败时会把会话标记为
`Failed`，编排器负责清理已创建的半成品。

请求 JSON：

```json
{
  "operationId": "4f6ed5ac-1d24-49df-8de3-efac91d09a65",
  "courseId": 3
}
```

前端只提交课程身份。
`scenarioType`、模板 ID、镜像、网络和资源限制应由后端可信模板决定。

建议成功响应：`202 Accepted`

```json
{
  "data": {
    "lab": {
      "id": "lab_01J2M8Y5A4D7KQ2V9N6P3R1T0X",
      "courseId": 3,
      "scenarioType": "application_cluster",
      "status": "Preparing",
      "createdAt": "2026-07-13T06:30:00Z"
    },
    "operation": {
      "operationId": "4f6ed5ac-1d24-49df-8de3-efac91d09a65",
      "action": "CREATE_LAB",
      "status": "pending",
      "submittedAt": "2026-07-13T06:30:00Z"
    }
  }
}
```

建议错误：

- `401 AUTH_REQUIRED`
- `403 CSRF_INVALID`
- `404 COURSE_NOT_FOUND`
- `409 LAB_ALREADY_ACTIVE`
- `409 LAB_BUSY`
- `503 RESOURCE_CAPACITY_EXCEEDED`
- `503 DOCKER_UNAVAILABLE`

### 7.2 `GET /api/v1/labs/:id`

状态：`available`

该端点应返回页面恢复所需的完整实验快照。
页面加载、刷新和异步操作轮询都必须先调用该端点。页面刷新后默认停止生成流量。

请求没有 JSON Body。

建议成功响应：`200 OK`

```json
{
  "data": {
    "lab": {
      "id": "lab_01J2M8Y5A4D7KQ2V9N6P3R1T0X",
      "courseId": 3,
      "scenarioType": "application_cluster",
      "scenarioTemplateId": "application_cluster_scenario_v1",
      "status": "Running",
      "balancingMode": "fixed",
      "redisEnabled": false,
      "startedAt": "2026-07-13T06:30:12Z",
      "lastEffectiveActionAt": "2026-07-13T06:32:10Z",
      "idleExpiresAt": "2026-07-13T06:42:10Z",
      "maximumExpiresAt": "2026-07-13T07:00:12Z",
      "terminatedAt": null,
      "terminationReason": null,
      "createdAt": "2026-07-13T06:30:00Z",
      "updatedAt": "2026-07-13T06:32:10Z"
    },
    "topology": {
      "instances": [
        {
          "instanceId": "app-1",
          "instanceName": "app-1",
          "status": "running",
          "cpuLimitCores": 0.1,
          "memoryLimitMb": 128,
          "performancePercent": 100,
          "processingSpeed": 20,
          "maxLoad": 100,
          "currentWeight": 100,
          "currentLoad": 0,
          "loadRatio": 0,
          "loadState": "idle",
          "observedAt": "2026-07-13T06:32:10Z"
        }
      ],
      "redis": null,
      "gateway": {
        "status": "ready"
      }
    },
    "latestOperation": {
      "operationId": "4f6ed5ac-1d24-49df-8de3-efac91d09a65",
      "action": "CREATE_LAB",
      "status": "succeeded",
      "completedAt": "2026-07-13T06:30:12Z"
    }
  }
}
```

明确来源字段包括实验状态、模板 ID、平衡模式、Redis 状态、实例资源、
性能比例、处理速度、最大负载、权重和操作状态。

以下截止字段由阶段六根据持久化活动时间计算：

- `idleExpiresAt`
- `maximumExpiresAt`

以下运行时计算字段已由阶段七真实批次接入：

- `topology.instances[].currentLoad`
- `topology.instances[].loadRatio`
- `trafficPolicy`
- `cache`

异步操作执行中建议每秒轮询，稳定运行时每三至五秒轮询。同步批次响应直接携带处理后的实例状态。

建议错误：

- `401 AUTH_REQUIRED`
- `404 LAB_NOT_FOUND`

### 7.3 `POST /api/v1/labs/:id/actions`

状态：`implemented`

所有用户动作使用统一请求结构：

```json
{
  "operationId": "9f65d723-e8d0-4813-bf27-b6bc4485ebc6",
  "actionType": "SET_INSTANCE_PERFORMANCE",
  "targetInstanceId": "app-3",
  "parameters": {
    "performancePercent": 30
  }
}
```

SDS 明确要求以下字段：

- `operationId`
- 动作类型
- 可选目标实例
- 受限参数对象

本文使用 `actionType`、`targetInstanceId` 和 `parameters` 作为建议字段名。

建议成功响应：`202 Accepted`

```json
{
  "data": {
    "operation": {
      "operationId": "9f65d723-e8d0-4813-bf27-b6bc4485ebc6",
      "labId": "lab_01J2M8Y5A4D7KQ2V9N6P3R1T0X",
      "action": "SET_INSTANCE_PERFORMANCE",
      "targetInstanceId": "app-3",
      "status": "pending",
      "submittedAt": "2026-07-13T06:35:00Z"
    }
  }
}
```

建议错误：

- `400 VALIDATION_FAILED`
- `401 AUTH_REQUIRED`
- `403 CSRF_INVALID`
- `403 LAB_NOT_OWNED`
- `404 LAB_NOT_FOUND`
- `404 INSTANCE_NOT_FOUND`
- `409 LAB_BUSY`
- `409 MIN_INSTANCE_LIMIT`
- `409 MAX_INSTANCE_LIMIT`
- `422 ACTION_NOT_ALLOWED`
- `503 RESOURCE_CAPACITY_EXCEEDED`
- `503 DOCKER_UNAVAILABLE`
- `503 NGINX_CONFIG_INVALID`

#### 7.3.1 建议动作类型

外部动作名称尚未由 SDS 固定。
以下名称按用户意图设计，不直接暴露内部编排命令。

增加实例：

```json
{
  "operationId": "e368b7c2-550c-469a-90cd-733bf2af93d8",
  "actionType": "ADD_INSTANCE",
  "targetInstanceId": null,
  "parameters": {}
}
```

删除实例：

```json
{
  "operationId": "599dd065-c71c-4d7b-98e0-81f0c06fb7f2",
  "actionType": "REMOVE_INSTANCE",
  "targetInstanceId": "app-2",
  "parameters": {}
}
```

调整实例性能：

```json
{
  "operationId": "9f65d723-e8d0-4813-bf27-b6bc4485ebc6",
  "actionType": "SET_INSTANCE_PERFORMANCE",
  "targetInstanceId": "app-3",
  "parameters": {
    "performancePercent": 30
  }
}
```

`performancePercent` 的明确范围为 `20` 至 `100`。

调整固定权重：

```json
{
  "operationId": "dfd583ef-a73f-499c-8b9f-0b93291bf0b8",
  "actionType": "SET_INSTANCE_WEIGHTS",
  "targetInstanceId": null,
  "parameters": {
    "weights": [
      {
        "instanceId": "app-1",
        "weight": 70
      },
      {
        "instanceId": "app-2",
        "weight": 30
      }
    ]
  }
}
```

权重允许范围和是否要求总和为 `100` 尚未固定。

切换负载均衡模式：

```json
{
  "operationId": "0bf7cc30-4789-4cea-b7fb-d37dad806529",
  "actionType": "SET_BALANCING_MODE",
  "targetInstanceId": null,
  "parameters": {
    "mode": "adaptive"
  }
}
```

明确模式为：

```json
["fixed", "adaptive"]
```

重启会话 Redis：

```json
{
  "operationId": "d6fc1554-788a-4e98-bd41-af5d1d6cb142",
  "actionType": "RESTART_REDIS",
  "targetInstanceId": null,
  "parameters": {}
}
```

设置缓存保护策略：

```json
{
  "operationId": "218ca489-e036-46e0-bb5b-92df9c1cf289",
  "actionType": "SET_CACHE_PROTECTION",
  "targetInstanceId": null,
  "parameters": {
    "scenario": "penetration",
    "strategy": "null_cache",
    "enabled": true
  }
}
```

缓存场景、策略枚举和触发故障动作仍需单独固定。

### 7.4 `POST /api/v1/labs/:id/traffic-batches`

状态：`implemented`

该端点接收当前页面生成的一批教学等效请求。它是同步数据面接口，不创建
`lab_operations`，也不使用 `operationId`。当前页面必须保证同一时间只有一个未完成请求。

请求：

```json
{
  "batchId": "batch-8f3c",
  "productId": 1,
  "requestUnits": 10
}
```

成功响应：`200 OK`。全部丢弃也是正常业务结果，不返回 `429`。

```json
{
  "data": {
    "result": {
      "batchId": "batch-8f3c",
      "labId": "lab_01J2M8Y5A4D7KQ2V9N6P3R1T0X",
      "status": "partially_accepted",
      "occurredAt": "2026-07-16T10:00:00Z",
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
        "observedAt": "2026-07-16T10:00:00Z"
      }
    }
  }
}
```

前端动画规则：接纳球进入 `targetInstanceId`。
部分接纳时在网关生成两个分别标注 `acceptedUnits` 和 `droppedUnits` 的球。
全部丢弃时原球偏离并消失，错误响应生成错误球。

建议错误：

- `400 TRAFFIC_REQUEST_INVALID`
- `401 AUTH_REQUIRED`
- `403 CSRF_INVALID`
- `404 LAB_NOT_FOUND`
- `409 LAB_NOT_RUNNING`
- `429 RATE_LIMITED`
- `503 LAB_UNAVAILABLE`

### 7.5 `POST /api/v1/labs/:id/reset`

状态：`available`

请求 JSON：

```json
{
  "operationId": "158bfe94-e5f6-49a1-96ae-51df0a54184b"
}
```

建议成功响应：`202 Accepted`

```json
{
  "data": {
    "operation": {
      "operationId": "158bfe94-e5f6-49a1-96ae-51df0a54184b",
      "labId": "lab_01J2M8Y5A4D7KQ2V9N6P3R1T0X",
      "action": "RESET_LAB",
      "status": "pending",
      "submittedAt": "2026-07-13T06:40:00Z"
    }
  }
}
```

重置成功后应恢复场景初始拓扑、性能、权重、数据库和缓存状态。

建议错误：

- `401 AUTH_REQUIRED`
- `403 CSRF_INVALID`
- `404 LAB_NOT_FOUND`
- `409 LAB_BUSY`
- `503 DOCKER_UNAVAILABLE`

### 7.6 `DELETE /api/v1/labs/:id`

状态：`available`

SRS 要求实验生命周期变更携带 `operationId`。
本文建议 DELETE 请求使用 JSON Body。

请求 JSON：

```json
{
  "operationId": "fba6127c-a17e-44fa-a14a-0635fcb2b80a"
}
```

建议成功响应：`202 Accepted`

```json
{
  "data": {
    "operation": {
      "operationId": "fba6127c-a17e-44fa-a14a-0635fcb2b80a",
      "labId": "lab_01J2M8Y5A4D7KQ2V9N6P3R1T0X",
      "action": "DESTROY_LAB",
      "status": "pending",
      "submittedAt": "2026-07-13T06:45:00Z"
    }
  }
}
```

DELETE 请求使用 JSON Body 携带 `operationId`，与重置操作保持一致。

## 8. 操作状态

SDS 定义的操作状态为：

```json
[
  "pending",
  "claimed",
  "running",
  "succeeded",
  "failed",
  "compensating"
]
```

耗时动作返回 `202 Accepted`。
完成结果通过实验快照轮询获取。

建议操作对象：

```json
{
  "operationId": "9f65d723-e8d0-4813-bf27-b6bc4485ebc6",
  "labId": "lab_01J2M8Y5A4D7KQ2V9N6P3R1T0X",
  "action": "SET_INSTANCE_PERFORMANCE",
  "targetInstanceId": "app-3",
  "status": "succeeded",
  "attemptCount": 1,
  "submittedAt": "2026-07-13T06:35:00Z",
  "completedAt": "2026-07-13T06:35:02Z",
  "result": {
    "performancePercent": 30,
    "cpuLimitCores": 0.03,
    "processingSpeed": 6,
    "maxLoad": 100
  },
  "error": null
}
```

实验控制操作通过实验快照返回 `latestOperation`，不增加独立的操作查询 URL。

## 9. 统一错误响应

当前 Gin 入口统一返回以下错误结构：

```json
{
  "error": {
    "code": "AUTH_REQUIRED",
    "message": "authentication is required"
  },
  "requestId": "9d7d45bd25bf4b7e8d89998ad66578d6"
}
```

`code` 是稳定机器字段。
`message` 可用于展示或日志，但前端业务分支不应依赖其文本。
`requestId` 位于响应顶层，用于关联服务端日志。
当前响应不包含 `details`。

建议 HTTP 状态映射：

| HTTP | 错误码示例 | 含义 |
| --- | --- | --- |
| 400 | `VALIDATION_FAILED` | 通用 JSON 或字段格式不合法 |
| 400 | `TRAFFIC_REQUEST_INVALID` | 批次字段、商品或请求量无效 |
| 401 | `AUTH_REQUIRED` | 未登录或 Session 无效 |
| 401 | `INVALID_CREDENTIALS` | 登录凭据错误 |
| 403 | `ACCOUNT_DISABLED` | 登录账号已禁用 |
| 403 | `CSRF_INVALID` | CSRF Token 缺失或错误 |
| 404 | `COURSE_NOT_FOUND` | 课程不存在 |
| 404 | `LAB_NOT_FOUND` | 实验不存在 |
| 409 | `LAB_ALREADY_ACTIVE` | 当前账户已有活动实验 |
| 409 | `LAB_BUSY` | 同一实验存在串行拓扑操作 |
| 409 | `MIN_INSTANCE_LIMIT` | 删除后会低于最少实例数 |
| 409 | `MAX_INSTANCE_LIMIT` | 增加后会超过最多实例数 |
| 422 | `ACTION_NOT_ALLOWED` | 动作不在场景白名单中 |
| 429 | `RATE_LIMITED` | 触发平台统一 HTTP 限流 |
| 503 | `LAB_UNAVAILABLE` | 实验网关、实例、内部超时或响应校验失败 |

认证、课程和当前实验占位端点的状态映射已经写入 OpenAPI。
后续实验错误码仍将在对应阶段继续补充。

## 10. 前端 Mock 建议

实验控制面实现前，Mock 层建议具备以下特性：

- 仅对仍标记为 `planned` 的实验端点使用 Mock。
- 把所有枚举集中定义，不在组件中散落字符串。
- 所有实验异步变更先返回 `202` 和 `pending` Operation。
- 通过轮询实验快照把 Operation 更新为 `running` 和 `succeeded`。
- 模拟 `AUTH_REQUIRED`、`LAB_NOT_RUNNING`、`RATE_LIMITED` 和 `LAB_UNAVAILABLE` 等稳定错误码。
- 页面刷新时先加载 `auth/me`，再加载活动实验快照。
- 同步批次响应只更新当前小球和实例颜色，完整快照仍作为最终恢复依据。

前端类型建议按资源拆分：

```text
SystemInfo
CourseSummary
CourseDetail
AuthenticatedUser
LabSnapshot
LabInstance
LabOperation
TrafficBatchResult
ApiError
```

## 11. 不对前端开放的端点

以下端点不属于浏览器公开 API：

| URL | 原因 |
| --- | --- |
| `/readyz` | Platform API 内部依赖就绪检查，Edge 当前不代理 |
| `/v1/commands` | Orchestrator 的 UDS 内部命令入口 |
| `/internal/products/:id` | Lab App 内部实验接口 |
| `/internal/order-batch` | Lab App 内部流量生成接口 |
| `/internal/runtime-state` | Lab App 内部运行身份接口 |
| Docker API `:2375` | 只允许 Orchestrator 通过内部网络访问 |

前端不得直接请求 Lab Gateway、Lab App、MySQL、Redis 或 Docker Socket
Proxy。

## 12. 来源清单

| Source ID | 来源 | 用途 | 置信度 |
| --- | --- | --- | --- |
| `SRC-DOC-001` | `2026-07-12` SDS | URL、状态机、动作、批次响应和数据模型 | 高 |
| `SRC-DOC-002` | `2026-07-11` SRS | 权限、错误码、业务限制和验收语义 | 高 |
| `SRC-API-001` | `platform-api.openapi.yaml` | 当前保留路径和实现阶段 | 高 |
| `SRC-CODE-001` | Platform API Router | 当前真实可用响应 | 高 |
| `SRC-CFG-001` | `configs/scenarios` 和 `configs/resources` | 模板默认值和资源范围 | 高 |

内部 UDS Command 和 Lab App 内部 API 只用于确认边界。
它们没有被转换成浏览器公开端点。

## 13. 后续实验阶段待确认的问题

认证、课程、Session Cookie、CSRF Header、匿名 `auth/me` 响应和课程 `slug` 已写入正式 OpenAPI。
后续实验阶段仍需确认：

1. Lab API 对外使用的稳定 `instanceId` 格式。
2. 外部 `actionType` 枚举及每个动作的参数 Schema。
3. 固定权重的允许范围和总和规则。
4. 缓存实验场景、保护策略和故障触发动作枚举。
5. DELETE 请求的 `operationId` 放在 Body 还是 Header。
6. 是否增加独立 Operation 查询端点。
7. `auth/me` 是否在实验阶段增加 `activeLabId`。

每个实验阶段完成后都应同步更新 OpenAPI，并继续以 OpenAPI 作为前后端正式契约。
