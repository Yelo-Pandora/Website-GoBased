# 互联网后端架构学习平台底层脚手架设计

* 文档类型：Foundation Scaffold Design
* 基线日期：2026-07-13
* 上游需求：2026-07-11 SRS（R1-R50）
* 上游设计：2026-07-12 SDS
* 设计状态：已由用户确认
* 实施状态：尚未开始

## 1. 目标

本设计定义互联网后端架构学习平台首轮工程脚手架。
本轮会移除已有购买示例代码，保留 SRS、SDS、许可证和必要的仓库元数据。

本轮交付必须满足以下条件：

* 建立正式的前端、后端、数据库和 Docker 目录边界。
* 提供可构建的 Vue、平台 API、编排器和实验应用最小程序。
* 提供全部项目自有 Dockerfile、根目录 Compose 文件和环境文件。
* 提供平台数据库、实验数据库管理过程和种子数据初始化脚本。
* 默认执行 `docker compose up -d --wait` 后，所有常驻服务通过健康检查。
* 本轮只实现健康检查和最小运行身份，不实现 SRS 中的完整业务功能。

## 2. 仓库组织方案

项目采用单仓库、单 Go 模块、按服务分目录的组织方式。
服务私有代码放在各自的 `internal` 目录。
只有多个 Go 服务真正共享的基础代码才能放在仓库根部的 `internal` 目录。

```text
Website-GoBased/
├─ services/
│  ├─ platform-api/
│  │  ├─ cmd/platform-api/main.go
│  │  ├─ internal/
│  │  │  ├─ app/
│  │  │  ├─ httpapi/
│  │  │  ├─ auth/
│  │  │  ├─ course/
│  │  │  ├─ lab/
│  │  │  ├─ operation/
│  │  │  ├─ traffic/
│  │  │  ├─ balancer/
│  │  │  ├─ event/
│  │  │  ├─ scheduler/
│  │  │  └─ repository/mysql/
│  │  └─ Dockerfile
│  ├─ orchestrator/
│  │  ├─ cmd/orchestrator/main.go
│  │  ├─ internal/
│  │  │  ├─ app/
│  │  │  ├─ transport/uds/
│  │  │  ├─ command/
│  │  │  ├─ template/
│  │  │  ├─ dockerops/
│  │  │  ├─ nginxops/
│  │  │  ├─ dbops/
│  │  │  └─ reconcile/
│  │  └─ Dockerfile
│  └─ lab-app/
│     ├─ cmd/lab-app/main.go
│     ├─ internal/
│     │  ├─ app/
│     │  ├─ httpapi/
│     │  ├─ product/
│     │  ├─ ordersim/
│     │  ├─ cache/
│     │  ├─ capacity/
│     │  ├─ metrics/
│     │  └─ repository/mysql/
│     └─ Dockerfile
├─ internal/
│  ├─ config/
│  ├─ health/
│  ├─ logging/
│  ├─ protocol/
│  └─ version/
├─ web/
│  ├─ src/
│  │  ├─ api/
│  │  ├─ assets/
│  │  ├─ components/
│  │  ├─ composables/
│  │  ├─ features/auth/
│  │  ├─ features/courses/
│  │  ├─ features/labs/
│  │  ├─ router/
│  │  ├─ stores/
│  │  ├─ styles/
│  │  ├─ views/
│  │  ├─ App.vue
│  │  └─ main.js
│  ├─ tests/
│  ├─ index.html
│  ├─ package.json
│  ├─ package-lock.json
│  ├─ vite.config.js
│  └─ Dockerfile
├─ deploy/
│  ├─ nginx/
│  │  ├─ edge/
│  │  │  ├─ nginx.conf
│  │  │  └─ default.conf
│  │  └─ lab-gateway/
│  │     ├─ Dockerfile
│  │     ├─ nginx.conf
│  │     ├─ static/health.conf
│  │     └─ templates/lab-upstream.conf.tmpl
│  ├─ docker-socket-proxy/README.md
│  └─ volume-init/init-volumes.sh
├─ database/
│  └─ mysql/
│     ├─ Dockerfile
│     └─ init/
│        ├─ 001_platform_schema.sql
│        ├─ 002_lab_database_routines.sql
│        ├─ 003_seed_courses.sql
│        └─ 004_seed_users.sql
├─ configs/
│  ├─ scenarios/application_cluster_scenario_v1.json
│  └─ resources/
│     ├─ app_container_v1.json
│     ├─ cache_app_container_v1.json
│     ├─ session_redis_v1.json
│     ├─ session_network_v1.json
│     ├─ lab_nginx_fragment_v1.json
│     └─ lab_database_profile_v1.json
├─ contracts/
│  ├─ http/platform-api.openapi.yaml
│  └─ orchestrator/
│     ├─ command.schema.json
│     └─ response.schema.json
├─ scripts/
│  ├─ mysql-init.sh
│  ├─ compose-health.sh
│  └─ wait-for-service.sh
├─ tests/
│  ├─ integration/
│  └─ e2e/
├─ docs/superpowers/specs/
├─ .dockerignore
├─ .env
├─ .env.example
├─ .gitattributes
├─ .gitignore
├─ docker-compose.yml
├─ go.mod
├─ go.sum
├─ Makefile
├─ README.md
└─ LICENSE
```

空目录只有在近期实现需要时才创建。
Git 无法保存纯空目录，因此不为远期模块批量添加无意义的 `.gitkeep` 文件。

## 3. 镜像版本基线

版本在 2026-07-13 通过官方发布源和官方镜像仓库核对。
存在官方 LTS 的组件使用 LTS。
不存在官方 LTS 的组件使用最新稳定且具有完整版本号的发行版。

| 用途 | 固定版本 | 选择依据 |
|---|---|---|
| Go 构建 | `golang:1.26.5-alpine3.23` | Go 无 LTS，使用最新稳定补丁版 |
| Vue 构建 | `node:24.18.0-alpine3.23` | Node 24 LTS |
| Nginx | `nginx:1.30.3-alpine3.23` | Nginx 无 LTS，使用最新 stable 而非 mainline |
| MySQL | `mysql:8.4.10` | MySQL 8.4 LTS |
| 动态 Redis | `redis:8.8.0-alpine3.23` | Redis 无 LTS，使用最新确定版本 |
| Socket Proxy | `tecnativa/docker-socket-proxy:v0.4.2` | 最新确定版本 |
| 通用 Alpine | `alpine:3.23.5` | 最新稳定补丁版 |
| Vue | `3.5.39` | 最新稳定版本 |
| Vite | `8.1.4` | 最新稳定版本，兼容 Node 24 LTS |

镜像不得使用 `latest`、仅主版本或仅次版本标签。
后续生产加固可以在完整版本标签之外继续固定镜像摘要。

## 4. Compose 服务拓扑

默认 Compose 启动以下常驻服务：

| 服务 | 职责 |
|---|---|
| `edge-nginx` | 托管 Vue SPA，并反向代理平台 API |
| `platform-api` | 平台控制面 API 和后续内部流量生成模块 |
| `orchestrator` | 通过 Unix Domain Socket 接收受限编排命令 |
| `docker-socket-proxy` | 受限暴露 Docker API |
| `shared-mysql` | 保存平台数据，并承载每实验独立数据库 |
| `lab-gateway-nginx` | 内部实验流量入口和动态 upstream |

`volume-init` 是短生命周期初始化任务。
它只负责共享卷的属主和权限，成功后退出。

`lab-app` 使用 Compose profile 定义为只构建镜像的动态服务模板。
默认 `docker compose up` 不启动 `lab-app`。
Redis 也不作为常驻服务，由未来编排器按实验动态创建。

## 5. Docker 网络隔离

Compose 使用具名 bridge 网络实现基础隔离。
除公网入口网络外，内部网络均设置 `internal: true`。

| 网络 | 成员 | 用途 |
|---|---|---|
| `platform-edge-net` | `edge-nginx`、`platform-api` | 公网入口到平台 API |
| `platform-data-net` | `platform-api`、`shared-mysql` | 平台数据访问 |
| `lab-control-net` | `platform-api`、`lab-gateway-nginx` | 内部教学流量入口 |
| `orchestration-net` | `orchestrator`、`docker-socket-proxy` | 受限 Docker API |
| `db-admin-net` | `orchestrator`、`shared-mysql` | 实验数据库生命周期管理 |

只有 `edge-nginx` 发布宿主机端口。
`expose` 不作为安全边界，网络成员关系和不发布端口才是连通性边界。

`platform-api` 与 `orchestrator` 不共享 TCP 网络。
两者通过命名卷中的 `/run/platform/orchestrator.sock` 通信。

未来创建实验时，编排器动态创建 `lab-{labId}-net`。
该网络只连接当前实验应用、当前实验 Redis、共享实验 Nginx 和共享 MySQL。
实验结束后，共享服务先断开网络，再删除实验网络。

Docker 网络只解决连通性隔离。
实验 MySQL 账号仍必须仅能访问自己的 `lab_xxxx` 数据库。

## 6. Dockerfile 边界

三个 Go 服务分别由对应 `services/*/Dockerfile` 构建。
Go Dockerfile 使用多阶段构建，生成静态二进制，并以固定非 root UID/GID 运行。

`web/Dockerfile` 使用 Node LTS 构建 Vue，再将静态文件复制进固定 Nginx 镜像。
公网入口 Nginx 配置来自 `deploy/nginx/edge`。

`deploy/nginx/lab-gateway/Dockerfile` 构建共享实验 Nginx。
`database/mysql/Dockerfile` 将初始化脚本按顺序固化进 MySQL 镜像。

Redis、Socket Proxy 和 `volume-init` 直接使用固定版本镜像。
这些组件不增加只用于转发基础镜像的空壳 Dockerfile。

## 7. 最小服务行为

`platform-api` 监听容器内 `8080`。
它提供 `/healthz` 和 `/readyz`。
`/readyz` 必须检查平台 MySQL 连接。

`orchestrator` 只监听 `/run/platform/orchestrator.sock`。
它在 UDS 上提供 `/healthz`，并通过自身健康检查子命令验证 socket 可连接。

`lab-app` 提供 `/healthz` 和最小运行身份响应。
它不在本轮实现商品、订单或缓存业务。

`edge-nginx` 提供 SPA、API 代理和 `/healthz`。
`lab-gateway-nginx` 提供内部 `/healthz`，并加载动态实验配置目录。

Socket Proxy 提供 Docker API `_ping` 健康检查。
它不发布宿主机端口，只开启编排所需的 Docker API 类别。

## 8. 数据库初始化设计

MySQL 入口先运行环境驱动的账号初始化脚本，再按文件名执行 SQL。

### 8.1 `001_platform_schema.sql`

该文件创建 `platform` 数据库的控制面结构。
它创建以下核心表及必要主键、唯一约束、外键和查询索引：

* `users`
* `auth_sessions`
* `courses`
* `course_progress`
* `lab_sessions`
* `lab_instances`
* `lab_resources`
* `lab_operations`

该文件只定义结构，不写课程和账号种子数据。

### 8.2 `002_lab_database_routines.sql`

该文件创建以下受限存储过程：

* `provision_lab_database`
* `reset_lab_database`
* `destroy_lab_database`

存储过程负责严格校验实验标识符，创建或清理 `lab_xxxx` 数据库和临时账号。
实验数据库包含 `products` 和 `order_stats`。
`order_stats` 对 `(product_id, instance_name, time_bucket)` 建立复合唯一约束。

编排器只获得执行这些固定过程的权限。
它不能提交任意 SQL 或直接获得不受限的 DDL 权限。

### 8.3 `003_seed_courses.sql`

该文件写入完整知识地图、MVP 课程和待完成占位课程。
课程使用稳定 `slug`、状态和排序字段。
种子写入使用唯一键和 upsert，重复执行不会产生重复课程。

### 8.4 `004_seed_users.sql`

该文件写入本地开发和验收使用的预创建测试账号。
数据库只保存密码哈希，不保存明文密码。
用户名具有唯一约束，种子写入必须幂等。

测试账号初始化可以通过环境开关关闭。
正式账号后续由受信任的命令行管理工具创建，不提供公开注册或管理员 Web 后台。

## 9. 数据库权限

初始化过程创建两个最小权限账号。

* 平台 API 账号只能读写 `platform` 数据库。
* 编排器账号只能连接管理入口并执行固定实验数据库存储过程。

平台 API 不具有建库和删库权限。
实验应用临时账号只能访问自己的实验数据库。
平台库与实验库之间不创建跨库外键。

## 10. 环境变量

根目录 `.env` 保存本地实际配置，并由 Git 忽略。
`.env.example` 保存字段说明、非敏感示例和镜像版本基线。

环境变量至少包括以下字段。
注释说明字段的消费者、用途和推荐的本地开发值。

```dotenv
# Compose 项目名。
# 它用于生成默认容器、网络和卷名称，避免与同一宿主机上的其他项目冲突。
COMPOSE_PROJECT_NAME=backend-learning-platform

# 公网入口 Nginx 映射到宿主机的 HTTP 端口。
# 这是本轮唯一允许发布到宿主机的应用端口。
HTTP_PORT=8080

# 所有支持该变量的容器使用的时区。
# 日志和数据库时间仍应保存为 UTC，展示时再转换为本地时区。
TZ=Asia/Shanghai

# MySQL root 账号密码。
# 仅供 MySQL 首次初始化和受信任的维护操作使用，应用服务不得使用该账号。
# 该值属于敏感信息，实际 .env 必须使用随机强密码。
MYSQL_ROOT_PASSWORD=change-me-root

# 平台控制数据库名称。
# platform-api 只访问该数据库，不直接访问各实验数据库。
MYSQL_PLATFORM_DATABASE=platform

# platform-api 使用的最小权限 MySQL 用户名。
# 该账号只获得平台数据库所需的查询和写入权限。
MYSQL_PLATFORM_USER=platform_api

# platform-api 数据库账号密码。
# 该值属于敏感信息，实际 .env 必须使用随机强密码。
MYSQL_PLATFORM_PASSWORD=change-me-platform

# orchestrator 使用的 MySQL 管理账号名称。
# 该账号只允许执行固定实验数据库生命周期存储过程。
MYSQL_ORCHESTRATOR_USER=orchestrator

# orchestrator 数据库账号密码。
# 该值属于敏感信息，实际 .env 必须使用随机强密码。
MYSQL_ORCHESTRATOR_PASSWORD=change-me-orchestrator

# platform-api 在容器内部监听的地址。
# 使用 :8080 表示监听容器的全部网络接口，不代表向宿主机发布端口。
PLATFORM_API_ADDR=:8080

# platform-api 与 orchestrator 共享的 Unix Domain Socket 路径。
# 两个容器必须在同一路径挂载同一个命名卷。
ORCHESTRATOR_SOCKET_PATH=/run/platform/orchestrator.sock

# platform-api 内部流量生成模块访问实验 Nginx 的基础地址。
# 该名称只在 lab-control-net 内通过 Docker DNS 解析。
LAB_GATEWAY_ADDR=http://lab-gateway-nginx:8080

# 构建三个 Go 服务时使用的固定 Go 构建镜像。
GO_IMAGE=golang:1.26.5-alpine3.23

# 构建 Vue SPA 时使用的固定 Node LTS 镜像。
NODE_IMAGE=node:24.18.0-alpine3.23

# 运行公网入口和实验网关时使用的固定 Nginx stable 镜像。
NGINX_IMAGE=nginx:1.30.3-alpine3.23

# shared-mysql 使用的固定 MySQL LTS 镜像。
MYSQL_IMAGE=mysql:8.4.10

# orchestrator 未来创建会话 Redis 时使用的固定镜像。
# 默认 Compose 启动不会创建 Redis 容器。
REDIS_IMAGE=redis:8.8.0-alpine3.23

# 受限 Docker API 代理使用的固定镜像。
SOCKET_PROXY_IMAGE=tecnativa/docker-socket-proxy:v0.4.2

# volume-init 等短生命周期基础任务使用的固定 Alpine 镜像。
ALPINE_IMAGE=alpine:3.23.5

# lab-app 构建结果的完整镜像名称和标签。
# orchestrator 未来只能从受信任模板选择该镜像，不能接受用户提交的镜像名。
LAB_APP_IMAGE=website-gobased/lab-app:dev

# 动态实验网络名称前缀。
# 实际网络名称使用 lab-{labId}-net，labId 必须先经过严格格式校验。
LAB_NETWORK_PREFIX=lab

# 整个平台允许同时处于活动状态的实验数量上限。
# 初始值 10 对应 SRS 中最多 10 名同时在线测试用户和单用户单活动实验规则。
LAB_MAX_ACTIVE=10

# 单个实验允许同时存在的 lab-app 实例数量上限。
# 该值来自应用集群实验的一至四实例约束。
LAB_MAX_INSTANCES_PER_SESSION=4

# 单个动态 lab-app 容器的默认内存上限，单位为 MB。
# 场景模板可以在不超过平台安全上限的前提下引用该值。
LAB_DEFAULT_MEMORY_MB=128

# 单个动态实验容器允许创建的最大进程数。
# 它用于限制进程或线程异常增长对宿主机的影响。
LAB_DEFAULT_PIDS_LIMIT=64

# 是否在首次初始化时写入本地开发和验收用的预创建测试账号。
# 正式部署应设置为 false，并通过受信任的命令行管理工具创建账号。
SEED_TEST_USERS=true
```

本地 `.env` 必须包含可直接启动的值。
密码不得写入镜像、SQL、日志或被 Git 跟踪的文件。
`.env.example` 中的 `change-me-*` 只能作为字段占位，不能用于实际部署。

## 11. 容器安全基线

Go 服务使用固定 UID/GID 的非 root 用户。
`platform-api` 不挂载 Docker Socket。
`orchestrator` 只访问 UDS、管理数据库网络和 Socket Proxy。
只有 Socket Proxy 挂载 `/var/run/docker.sock`。

适用的服务启用以下限制：

* `read_only: true`
* `cap_drop: [ALL]`
* `security_opt: [no-new-privileges:true]`
* 仅为必要写目录设置 `tmpfs` 或具名卷

动态实验容器默认采用非 root、只读根文件系统、无特权、无宿主机目录挂载、
无公开端口、CPU/内存/PID 上限和独立实验网络。

## 12. 健康检查与启动顺序

共享卷初始化完成后，MySQL、编排器和内部 Nginx 可以并行启动。
平台 API 必须等待 MySQL 和编排器健康。
公网入口必须等待平台 API 健康。

```text
volume-init
    ├─> orchestrator ─> platform-api ─> edge-nginx
    ├─> lab-gateway-nginx
    └─> shared-mysql ─> platform-api
```

Compose 使用 `depends_on` 的健康条件表达依赖。
应用自身仍必须正确处理依赖暂时不可用，不能只依赖启动顺序保证运行。

## 13. 验证设计

实施完成后至少执行：

```bash
docker compose config
docker compose build
docker compose up -d --wait
docker compose ps
```

验收条件如下：

* 公网入口健康检查正常。
* Vue 最小页面可访问。
* 平台 API 的 `/healthz` 和 `/readyz` 正常。
* 编排器 UDS 健康检查正常。
* MySQL 核心表、索引和三个存储过程存在。
* 共享实验 Nginx 健康检查正常。
* Socket Proxy `_ping` 正常，且无法从宿主机或公网直接访问。
* 默认启动不创建实验应用或 Redis 容器。
* `docker compose --profile images build lab-app` 能构建实验应用镜像。
* 除公网入口外没有容器端口发布到宿主机。

## 14. 明确不在本轮实现的内容

本轮不实现完整登录、课程 API、实验状态机、操作队列、SSE、动态 Docker 编排、
Nginx upstream 更新、实验数据库实际创建流程、订单模拟和缓存实验。

本轮会建立这些能力的目录、契约和运行边界。
后续功能必须在当前边界内逐步实现，不应绕过编排器或网络隔离设计。

## 15. 自检结论

* 文档不存在 `TBD`、`TODO` 或未确定版本。
* 目录边界、Docker 构建边界和 Compose 运行边界一致。
* 常驻服务与按会话创建的动态资源已经明确区分。
* 网络成员关系与 SDS 的权限边界一致。
* 数据库结构、存储过程和种子数据职责没有重叠。
* 本轮范围限定为可运行脚手架，没有提前实现完整业务功能。
