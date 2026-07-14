# 互联网后端架构学习平台基础脚手架指南

本文介绍当前基础脚手架的作用、各组件之间的边界，以及检查代码时的
推荐阅读顺序。

本文面向准备继续开发、审查安全边界或学习项目结构的维护者。
需求和设计依据仍以 `docs/superpowers/specs` 中的文档为准。

## 1. 脚手架解决什么问题

当前脚手架不是完整的课程平台，也不是已经实现业务流程的实验系统。
它提供的是一条可以构建、启动、检查和继续扩展的工程基线。

这条基线主要解决以下问题：

- 明确浏览器入口、平台控制面和动态实验运行面的职责边界。
- 为 Vue、Go、Nginx、MySQL 和 Docker 提供可复现的构建方式。
- 通过 Compose 固定服务依赖、网络成员关系、命名卷和健康检查。
- 限制 Docker Socket、数据库管理权限和宿主机端口的暴露范围。
- 为后续登录、课程、实验编排和流量实验保留稳定扩展位置。
- 提供可以重复执行的本地验收流程，避免只验证单个进程能否启动。

当前已经实现健康检查、最小运行身份、数据库结构和容器边界。
登录、课程 API、动态容器创建、实验状态机和流量生成尚未实现。

## 2. 整体架构

默认启动后的主要调用关系如下：

```text
Browser
   |
   v
edge-nginx
   |-- Vue SPA
   `-- /api/* -----------------------> platform-api
                                          |-- shared-mysql
                                          |-- lab-gateway-nginx
                                          `-- Unix Domain Socket
                                                   |
                                                   v
                                              orchestrator
                                                   |-- shared-mysql
                                                   `-- docker-socket-proxy
                                                            |
                                                            v
                                                       Docker Engine
```

只有 `edge-nginx` 向宿主机发布 HTTP 端口。
平台 API 和编排器不共享 TCP 网络，而是通过 Unix Domain Socket 通信。
Docker Socket 只挂载到受限代理，业务容器不能直接访问宿主机 Docker API。

Compose 已准备好共享 UDS 卷，Orchestrator 也已经监听该 Socket。
当前最小 Platform API 尚未发送编排命令，命令调用属于后续实现范围。

## 3. 主要组件及职责

| 组件 | 主要路径 | 当前职责 |
| --- | --- | --- |
| Vue SPA | `web/` | 展示脚手架运行状态并调用平台 API |
| Edge Nginx | `deploy/nginx/edge/` | 托管 SPA、代理 API、提供公网健康检查 |
| Platform API | `services/platform-api/` | 提供控制面 HTTP 边界和数据库就绪检查 |
| Orchestrator | `services/orchestrator/` | 通过 UDS 接收受限编排命令 |
| Socket Proxy | `deploy/docker-socket-proxy/` | 限制可以访问的 Docker API 类别 |
| Shared MySQL | `database/mysql/` | 保存平台数据并承载实验数据库管理过程 |
| Database Migrate | `scripts/mysql_migrate.sh` | 在服务启动前按版本应用增量数据库迁移 |
| Lab Gateway | `deploy/nginx/lab-gateway/` | 提供内部实验入口和动态 upstream 加载目录 |
| Lab App | `services/lab-app/` | 提供未来动态实验容器使用的最小镜像模板 |
| Volume Init | `deploy/volume-init/` | 初始化共享卷的所有者和目录权限 |

`lab-app` 位于 `images` Compose profile 中。
默认启动不会创建实验应用容器或 Redis 容器。

## 4. Compose 提供的运行边界

根目录 `docker-compose.yml` 是理解运行架构的第一入口。
它负责把独立组件组合成可验证的本地平台。

Compose 当前定义以下边界：

- `platform-edge-net` 连接公网入口和平台 API。
- `platform-data-net` 允许平台 API 和短生命周期迁移任务访问共享 MySQL。
- `lab-control-net` 连接平台 API 和内部实验网关。
- `orchestration-net` 连接编排器和 Docker Socket Proxy。
- `db-admin-net` 允许编排器执行受限数据库管理过程。
- 除入口网络外，内部网络均设置为 `internal: true`。
- Go 服务和 Nginx 使用只读根文件系统及最小 Linux capabilities。
- `volume-init` 成功退出后，依赖共享卷的服务才会启动。
- `database-migrate` 成功退出后，平台 API 和编排器才会启动。
- Edge Nginx 等待平台 API 健康后才对外提供完整入口。

阅读 Compose 时不要把 `expose` 或镜像声明的内部端口理解为宿主机端口。
只有 `ports` 字段会把端口发布到宿主机。
开发期间 MySQL 额外映射到 `127.0.0.1:3306`，仅用于本机数据库管理工具；
最终发布验收会移除此映射和对应的宿主机访问网络。

## 5. 数据库初始化的作用

MySQL 首次使用空数据卷启动时，按文件名顺序执行初始化文件。

执行顺序如下：

1. `scripts/mysql_init.sh` 调整平台账户权限并创建编排器账户。
2. `001_platform_schema.sql` 创建平台数据库核心表和索引。
3. `002_lab_database_routines.sql` 创建三个实验数据库管理过程。
4. `003_seed_courses.sql` 写入课程知识地图和课程占位数据。
5. `004_seed_users.sql` 按环境开关写入本地测试用户。

空数据卷初始化完成后，`database-migrate` 会按文件名读取
`database/mysql/migrations/`，并使用 `platform.schema_migrations` 记录已执行版本。
后续结构变更必须新增顺序迁移文件，不能依赖重新创建数据卷。

平台 API 账户只能对 `platform` 数据库执行常规读写操作。
它不能创建或删除数据库。

编排器账户只能执行固定存储过程。
它不具备提交任意 DDL 或访问所有平台表的权限。

三个存储过程分别是：

- `provision_lab_database`
- `reset_lab_database`
- `destroy_lab_database`

这些过程为未来的实验数据库生命周期管理提供固定入口。
本轮脚手架只创建并验证这些过程，不执行完整实验创建流程。

## 6. 容器安全基线

当前安全设计的目标不是替代生产环境加固，而是避免脚手架从一开始就
形成明显的高权限耦合。

主要限制包括：

- 正式发布时只有 Edge Nginx 发布宿主机端口；开发期暂时保留 MySQL 回环地址映射。
- Platform API 不挂载 Docker Socket。
- Orchestrator 通过 Socket Proxy 访问受限 Docker API。
- 适用服务设置 `read_only: true`。
- 适用服务删除全部 Linux capabilities。
- 适用服务启用 `no-new-privileges:true`。
- Go 服务以固定的非 root UID 和 GID 运行。
- 可写目录通过命名卷或 `tmpfs` 单独提供。
- MySQL 平台账户和编排器账户采用不同的最小权限集合。
- 内部网络禁止直接访问外部网络。

`shared-mysql`、`volume-init` 和 Socket Proxy 存在合理例外。
检查时应结合它们的职责判断，而不是要求所有容器使用完全相同的参数。

## 7. 推荐阅读顺序

下面的顺序从运行全貌逐步进入实现细节。
每一步都列出建议重点检查的问题。

### 第一步：确认项目范围

先阅读：

1. `README.md`
2. `docs/foundation-scaffold-guide.md`
3. `docs/superpowers/specs/2026-07-13-foundation-scaffold-design.md`

重点检查：

- 当前阶段实现了什么。
- 哪些功能明确不在本轮范围内。
- 实际目录、镜像版本和服务名称是否与设计一致。

### 第二步：从 Compose 建立全局视图

依次阅读：

1. `.env.example`
2. `docker-compose.yml`
3. `.dockerignore`
4. `.gitignore`

重点检查：

- 环境变量是否有明确消费者。
- 是否只有 Edge Nginx 使用 `ports`。
- 服务是否只加入必要网络。
- 健康检查与 `depends_on` 是否表达正确启动顺序。
- 敏感值是否只存在于被 Git 忽略的 `.env`。

### 第三步：检查浏览器入口

依次阅读：

1. `web/package.json`
2. `web/src/main.js`
3. `web/src/App.vue`
4. `web/src/styles/base.css`
5. `web/Dockerfile`
6. `deploy/nginx/edge/nginx.conf`
7. `deploy/nginx/edge/default.conf`

重点检查：

- Vue 是否只承担展示和调用平台 API 的职责。
- `/api/` 是否由 Edge Nginx 代理到 `platform-api`。
- SPA 路由回退是否指向 `index.html`。
- Nginx 是否使用非 root 用户和可写临时目录。

### 第四步：阅读共享 Go 基础代码

依次阅读：

1. `internal/config/env.go`
2. `internal/health/http.go`
3. `internal/logging/logger.go`
4. `internal/version/version.go`
5. `internal/protocol/command.go`

重点检查：

- 共享目录是否只包含多个服务真正复用的能力。
- 配置缺失时是否能返回明确错误。
- 健康响应、日志字段和命令协议是否保持稳定。

### 第五步：阅读 Platform API

依次阅读：

1. `services/platform-api/cmd/platform-api/main.go`
2. `services/platform-api/internal/app/config.go`
3. `services/platform-api/internal/app/server.go`
4. `services/platform-api/internal/httpapi/router.go`
5. `services/platform-api/Dockerfile`

重点检查：

- `main` 是否只负责启动和信号处理。
- 数据库 DSN 是否来自最小权限账户。
- `/healthz` 和 `/readyz` 的语义是否不同。
- 就绪检查是否真实访问 MySQL。
- 平台 API 是否完全不接触 Docker Socket。

### 第六步：阅读 Orchestrator

依次阅读：

1. `services/orchestrator/cmd/orchestrator/main.go`
2. `services/orchestrator/internal/app/config.go`
3. `services/orchestrator/internal/transport/uds/server.go`
4. `contracts/orchestrator/command.schema.json`
5. `contracts/orchestrator/response.schema.json`
6. `services/orchestrator/Dockerfile`

重点检查：

- 服务是否只监听 Unix Domain Socket。
- 旧 Socket 文件是否经过类型检查后才会删除。
- 请求体是否有大小限制。
- 未实现命令是否返回稳定且明确的错误。
- 健康检查是否通过 UDS 访问服务自身。

### 第七步：检查 MySQL 初始化

依次阅读：

1. `database/mysql/Dockerfile`
2. `scripts/mysql_init.sh`
3. `database/mysql/init/001_platform_schema.sql`
4. `database/mysql/init/002_lab_database_routines.sql`
5. `database/mysql/init/003_seed_courses.sql`
6. `database/mysql/init/004_seed_users.sql`

重点检查：

- 初始化文件顺序是否稳定。
- 核心表是否具备主键、唯一约束、外键和查询索引。
- 动态 SQL 参数是否先通过严格格式校验。
- 实验账户是否只能访问自己的数据库。
- 种子数据是否可以重复执行而不产生重复记录。

### 第八步：检查实验入口和实验镜像

依次阅读：

1. `deploy/nginx/lab-gateway/nginx.conf`
2. `deploy/nginx/lab-gateway/static/health.conf`
3. `deploy/nginx/lab-gateway/templates/lab-upstream.conf.tmpl`
4. `services/lab-app/cmd/lab-app/main.go`
5. `services/lab-app/internal/app/config.go`
6. `services/lab-app/internal/app/server.go`
7. `services/lab-app/internal/httpapi/router.go`
8. `services/lab-app/Dockerfile`

重点检查：

- 默认实验网关是否只提供健康检查和 404 响应。
- 动态模板是否只包含受控 upstream 和实验标识。
- Lab App 是否只提供最小运行身份，没有提前实现业务逻辑。
- Lab App 镜像是否以非 root 用户运行。

### 第九步：检查模板、契约和验收入口

最后阅读：

1. `configs/scenarios/`
2. `configs/resources/`
3. `contracts/http/platform-api.openapi.yaml`
4. `deploy/volume-init/init-volumes.sh`
5. `scripts/compose-health.sh`
6. `scripts/wait-for-service.sh`
7. `Makefile`

重点检查：

- JSON 模板是否只描述受信任资源类型和安全上限。
- 契约是否与当前最小接口一致。
- 初始化脚本是否只修改预期命名卷。
- Makefile 和脚本是否复用根 Compose 配置。

## 8. 快速检查路径

如果只做一次快速审查，可以按以下顺序阅读：

1. `docker-compose.yml`
2. `.env.example`
3. `deploy/nginx/edge/default.conf`
4. `services/platform-api/internal/httpapi/router.go`
5. `services/orchestrator/internal/transport/uds/server.go`
6. `scripts/mysql_init.sh`
7. `database/mysql/init/001_platform_schema.sql`
8. `database/mysql/init/002_lab_database_routines.sql`
9. 三个 Go 服务和两个 Nginx 的 Dockerfile

这条路径可以快速回答以下问题：

- 请求从哪里进入。
- 服务如何互相访问。
- 哪个组件拥有高权限。
- 数据如何初始化。
- 哪些端口和网络可以被访问。
- 后续业务代码应添加到哪里。

## 9. 运行验收顺序

完成阅读后，按以下顺序验证实现：

```bash
docker compose config --quiet
docker compose build
docker compose --profile images build lab-app
docker compose up -d --wait
docker compose ps -a
```

然后检查公网入口：

```bash
curl --fail http://127.0.0.1:8080/healthz
curl --fail http://127.0.0.1:8080/readyz
curl --fail http://127.0.0.1:8080/api/v1/system/info
```

验收时至少确认：

- 所有常驻服务均为 `healthy`。
- `volume-init` 以状态码 `0` 退出。
- `database-migrate` 以状态码 `0` 退出，迁移版本已写入数据库。
- Vue 页面可以正常打开并显示平台状态。
- Platform API readiness 显示数据库可用。
- MySQL 存在八张核心表和三个存储过程。
- 平台账户和编排器账户符合最小权限要求。
- 除开发期的 MySQL 回环地址映射外，宿主机只能访问 Edge Nginx 发布的端口。
- 默认没有运行 Lab App 或 Redis 容器。

## 10. 后续扩展原则

继续开发时应保持当前边界，而不是绕过边界快速实现功能。

- 登录、课程和实验会话功能应进入 Platform API。
- Docker、动态网络和容器生命周期操作应进入 Orchestrator。
- 实验流量入口变更应通过 Lab Gateway 的受控配置完成。
- 实验数据库只能通过固定管理过程创建、重置和销毁。
- 动态 Lab App 不应直接获得宿主机目录或 Docker Socket。
- 新增共享 Go 包前，应确认至少有两个服务真正需要它。

这些原则可以让后续课程和实验功能继续沿用当前安全模型和运行模型。
