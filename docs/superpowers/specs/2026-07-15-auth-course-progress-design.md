# 认证会话与课程进度闭环设计

* 文档类型：Implementation Design Amendment
* 基线日期：2026-07-15
* 设计状态：已由用户确认
* 实施范围：课程进度完善、阶段二认证能力、文档边界修订

## 1. 目的

本文档补充课程模块与认证模块之间缺失的用户上下文。
它解决课程接口始终按匿名用户返回、学习进度没有查询和写入、用户重新登录后无法恢复进度等问题。

本文档同时修订管理员能力的 MVP 范围。
MVP 不实现管理员 CLI，而是永久保留仅绑定宿主机回环地址的 MySQL 管理入口，供受信任的本机维护使用。

## 2. 输入来源

| 来源 ID | 类型 | 定位 | 用途 |
| --- | --- | --- | --- |
| SRC-DOC-001 | requirement_doc | `2026-07-11-backend-architecture-learning-platform-srs-design.md` | R2、R6、R8、R9、R50 和角色边界 |
| SRC-DOC-002 | design_doc | `2026-07-12-backend-architecture-learning-platform-design.md` | Session、CSRF、数据模型和安全约束 |
| SRC-DOC-003 | implementation_design | `2026-07-14-backend-mvp-implementation-design.md` | 阶段一、阶段二和提交顺序 |
| SRC-DOC-004 | api_guide | `frontend-api-integration-guide.md` | 课程、登录、退出和当前用户响应草案 |
| SRC-CODE-001 | implementation | 当前 `platform-api`、OpenAPI 和 MySQL 初始化文件 | 确认已有表结构与实现缺口 |
| SRC-CLI-001 | user_input | 2026-07-15 当前任务对话 | 取消管理员 CLI，永久保留本机数据库入口 |

## 3. 需求变更

### 3.1 R50 修订

原 R50 要求提供管理员命令行能力。
该要求调整为：MVP 使用初始化脚本提供预创建测试账号，并允许受信任维护人员通过仅监听
`127.0.0.1:3306` 的 MySQL 管理入口维护账号数据。
实验资源清理由实验生命周期、失败补偿和资源核对机制承担，不增加管理员 CLI 或管理员 Web 页面。

### 3.2 新增需求

| ID | 类型 | 需求 | 条件与约束 | 验收标准 | 来源 |
| --- | --- | --- | --- | --- | --- |
| R56 | functional | 课程接口应支持可选认证，并正确返回 `meta.authenticated`。 | 匿名用户仍可读取理论课程；不得接收客户端提供的用户 ID。 | 匿名请求返回 `false` 和空进度；有效 Session 请求返回 `true` 和当前用户进度。 | SRC-DOC-001, SRC-CLI-001 |
| R57 | functional | 系统应查询并持久化当前用户的课程浏览进度。 | 只有有效 Session 才写入进度；课程详情浏览记录 `viewed` 和 `last_viewed_at`。 | 登录用户读取详情后，列表和详情均返回持久化进度；重新登录后记录仍存在。 | SRC-DOC-001, SRC-CODE-001, SRC-CLI-001 |
| R58 | functional | 系统应提供登录、退出和当前用户接口。 | 不提供公开注册；匿名访问当前用户接口返回 `401 AUTH_REQUIRED`。 | 有效账号可登录并读取当前身份；退出后原 Session 失效。 | SRC-DOC-001, SRC-DOC-004, SRC-CLI-001 |
| R59 | non_functional | 登录、Session 和状态变更请求应受到凭据、Cookie、CSRF 和限流保护。 | Session Token 不进入 URL 或日志；Cookie 使用 `HttpOnly` 和 `SameSite=Strict`；当前 HTTP 部署不设置 `Secure`。 | 无效凭据、禁用账号、过期限流、缺失 Session 和错误 CSRF 均返回稳定错误码且不产生副作用。 | SRC-DOC-001, SRC-DOC-002 |
| R60 | constraint | MySQL 本机管理入口应在 MVP 发布后继续保留。 | 端口只能绑定 `127.0.0.1`，不得监听全部网卡或作为公网管理 API。 | 发布验收确认 Edge Nginx 是唯一公网入口，同时 MySQL 只可从宿主机本地访问。 | SRC-CLI-001 |

下一可用需求编号为 R61。

## 4. 方案选择

课程进度采用课程读取时的可选认证，不增加 `userId` 查询参数。
服务端从 Session Cookie 解析用户身份，避免横向读取其他用户进度。

详情浏览采用读取后原子更新 `course_progress` 的方式。
不增加单独的“标记已读”端点，因为 SRS 将浏览详情本身定义为学习进度事件，额外端点只会增加前端调用和失败状态。

Session 使用 MySQL 持久化，登录限流使用进程内有界状态。
Session 和进度必须跨 `platform-api` 重启保留，而短期登录失败计数允许在进程重启后清空，以控制 MVP 复杂度。

不采用无状态签名令牌，因为它无法自然满足服务端撤销和现有 `auth_sessions` 数据模型。
不采用管理员 CLI，因为用户已明确将其移出 MVP。

## 5. 模块设计

### 5.1 `internal/auth`

认证包负责：

* 根据用户名读取用户和密码哈希；
* 使用 bcrypt 校验密码；
* 生成高熵 Session Token；
* 只把 Session Token 和 CSRF Token 的 SHA-256 哈希写入 MySQL；
* 查询、更新最后访问时间和撤销 Session；
* 区分无效凭据、禁用账号、过期 Session 和内部存储错误；
* 维护有界的进程内登录失败计数。

Session Token 使用 32 字节安全随机数并以 Base64 URL 无填充形式传输。
CSRF Token 使用 Session Token 作为 HMAC-SHA-256 密钥，对固定域标签派生。
这样服务端可以在登录和 `/auth/me` 时重新计算相同 CSRF Token，而数据库仍只保存哈希。

Session 默认有效期为 8 小时。
每次有效认证更新 `last_seen_at`，但不自动延长 `expires_at`。

### 5.2 HTTP 认证中间件

HTTP 层提供可选认证、强制认证和 CSRF 校验三种组合能力。

可选认证用于课程列表和详情：

* 没有 Cookie 时按匿名请求处理；
* Cookie 无效、过期或已撤销时按匿名请求处理并清理失效 Cookie；
* 数据库查询失败时返回内部错误，不把故障伪装成匿名状态；
* 有效 Session 将用户和 Session 身份写入请求上下文。

强制认证用于 `/api/v1/auth/me`、退出和后续实验接口。
没有有效身份时返回 `401 AUTH_REQUIRED`。

CSRF 校验用于退出和后续实验状态变更接口。
请求必须同时具有有效 Session Cookie 和 `X-CSRF-Token`。

### 5.3 课程 Repository 和 Service

`Course.Progress` 改为明确的可空结构，不再使用 `any`。
结构包含：

* `viewed`；
* `lastViewedAt`；
* `lastLabId`。

课程列表在有用户身份时通过 `LEFT JOIN course_progress` 查询当前用户进度。
匿名查询不产生进度对象。

登录用户读取课程详情时，Repository 使用幂等 upsert：

```sql
INSERT INTO course_progress (...)
VALUES (...)
ON DUPLICATE KEY UPDATE
  viewed = TRUE,
  last_viewed_at = VALUES(last_viewed_at);
```

写入完成后返回更新后的课程详情和进度。
匿名读取课程详情不写入数据库。

### 5.4 本机数据库管理边界

Compose 继续发布 `127.0.0.1:3306:3306`。
该绑定只允许宿主机本地数据库客户端访问，不允许其他主机通过服务器网卡连接。

本地管理入口用于受信任维护，包括检查用户、调整账号状态和必要的数据修复。
MVP 只保证初始化脚本能够创建预置测试账号，不提供公开注册、管理员 HTTP API、管理员 Web 页面或专用管理员 CLI。

平台应用仍使用受限的 `MYSQL_PLATFORM_USER`，不得改用 root 账号。
本机管理凭据继续由未提交的 `.env` 保存。

## 6. API 契约

### 6.1 登录

`POST /api/v1/auth/login`

请求字段为 `username` 和 `password`。
成功时返回用户、CSRF Token 和 Session 过期时间，并设置：

```http
Set-Cookie: session=<token>; HttpOnly; SameSite=Strict; Path=/
```

当前 HTTP 环境不设置 `Secure`。
切换 HTTPS 后必须通过配置启用。

### 6.2 当前用户

`GET /api/v1/auth/me`

有效 Session 返回用户、CSRF Token 和 Session 过期时间。
匿名、过期或已撤销 Session 返回 `401 AUTH_REQUIRED`。

### 6.3 退出

`POST /api/v1/auth/logout`

请求必须携带 Session Cookie 和 `X-CSRF-Token`。
成功时撤销数据库 Session 并返回过期 Cookie。

### 6.4 课程

课程列表和详情允许匿名访问，并在 OpenAPI 中声明可选 Cookie 认证。
列表响应的 `meta.authenticated` 来源于认证上下文，不再硬编码。

匿名课程的 `progress` 为 `null`。
登录用户尚未浏览的课程也可以为 `null`；一旦浏览详情，则返回进度对象。

## 7. 登录限流

登录限流键由规范化用户名和客户端地址组成。
默认策略为 5 分钟窗口内最多 5 次失败，超过后阻止 10 分钟。
成功登录后清除对应失败记录。

限流器必须设置最大记录数并清理过期项，避免未绑定内存增长。
默认配置为：

* `AUTH_LOGIN_WINDOW=5m`；
* `AUTH_LOGIN_MAX_FAILURES=5`；
* `AUTH_LOGIN_BLOCK_DURATION=10m`；
* `AUTH_LOGIN_LIMITER_MAX_ENTRIES=2048`。

Session 和 Cookie 配置为：

* `AUTH_SESSION_TTL=8h`；
* `AUTH_COOKIE_NAME=session`；
* `AUTH_COOKIE_SECURE=false`。

## 8. 错误语义

| HTTP 状态 | 错误码 | 场景 |
| --- | --- | --- |
| 400 | `VALIDATION_FAILED` | 登录请求字段缺失、格式错误或请求体无效 |
| 401 | `INVALID_CREDENTIALS` | 用户不存在或密码错误 |
| 401 | `AUTH_REQUIRED` | 强制认证端点没有有效 Session |
| 403 | `ACCOUNT_DISABLED` | 账号已禁用 |
| 403 | `CSRF_INVALID` | CSRF Token 缺失或不匹配 |
| 429 | `LOGIN_RATE_LIMITED` | 登录失败次数超过限制 |
| 500 | `INTERNAL_ERROR` | 数据库或安全随机数生成失败 |

错误响应不得泄露用户是否存在、密码哈希、Session Token、CSRF Token 或内部 SQL。

## 9. 数据流

### 9.1 登录

1. HTTP 层校验请求体。
1. 限流器检查用户名和客户端地址。
1. Auth Service 读取用户并执行固定成本的 bcrypt 校验。
1. 有效且未禁用的用户获得新的 Session。
1. Repository 写入 Session 和 CSRF 哈希。
1. HTTP 层设置 Cookie 并返回用户和 CSRF Token。

### 9.2 登录用户读取课程详情

1. 可选认证中间件读取 Session Cookie。
1. Auth Service 通过 Token 哈希恢复用户身份。
1. Course Service 对当前用户和课程执行进度 upsert。
1. Repository 返回课程元数据和更新后的个人进度。
1. Service 合并 Markdown 正文和实验说明。
1. HTTP 层返回课程详情。

### 9.3 重新进入平台

1. 浏览器自动携带仍有效的 Session Cookie。
1. 前端调用 `/api/v1/auth/me` 获取用户和 CSRF Token。
1. 前端请求课程列表。
1. Repository 从 `course_progress` 返回此前持久化记录。

## 10. 测试与验收

单元测试覆盖：

* Session 和 CSRF Token 生成与哈希；
* 密码正确、错误和禁用账号；
* 登录限流窗口、阻止和过期清理；
* 可选认证、强制认证和 CSRF 中间件；
* 课程进度 JSON 的空值和时间字段。

HTTP 路由测试覆盖：

* 登录、退出和当前用户端点；
* 匿名与登录课程列表的 `meta.authenticated`；
* 匿名详情不写进度；
* 登录详情写入并返回进度；
* 无效 Session、错误 CSRF 和登录限流错误码。

MySQL 集成和容器验收覆盖：

* 登录后 `auth_sessions` 写入哈希而非明文 Token；
* 浏览详情后 `course_progress` 写入；
* 重启 `platform-api` 后 Session 和进度仍可读取；
* 退出后 Session 被撤销；
* MySQL 只监听宿主机 `127.0.0.1:3306`；
* Swagger UI 可手动完成登录、当前用户、课程读取和退出检查。

## 11. 文档变更范围

实现时同步更新：

* SRS 的管理员角色和 R50；
* 结构化需求文件中的 R50；
* SDS 的宿主机端口边界；
* 基础设施设计和使用指南；
* 后端 MVP 实施设计中的阶段二、提交说明和发布验收；
* 前端 API 集成指南；
* OpenAPI 认证、进度和错误契约。

## 12. 提交边界

本阶段按以下独立提交交付：

1. `docs: 确认认证与课程进度闭环设计`
1. `docs: 调整认证阶段与数据库本地管理边界`
1. `feat: 实现认证会话与安全中间件`
1. `feat: 实现课程进度持久化闭环`
1. `docs: 补齐认证与课程进度API契约`

每个提交前执行与变更风险相称的格式化、单元测试、静态检查和容器验收。

## 13. 自检结论

* 需求编号从 R56 连续到 R60，下一编号为 R61。
* 设计明确了 `/auth/me` 的匿名响应、Cookie 名称、CSRF Header、Session 有效期和限流默认值。
* 设计不增加新表，也不依赖删除 MySQL 数据卷。
* 设计没有公开注册、管理员 CLI 或管理员 Web 页面。
* 课程进度由服务端 Session 身份决定，不接受客户端用户 ID。
* MySQL 本机端口永久保留，但仍只绑定回环地址。
* 文档中不存在 `TODO`、`TBD` 或未决产品问题。
