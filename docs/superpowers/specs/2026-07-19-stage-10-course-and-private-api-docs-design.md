# 阶段十课程页面与私有 API 文档入口设计

## 状态

本文记录已经确认的阶段十收尾设计。实施前基线提交为 `25eafa1`。
本文与实现、测试和运维文档在工作完成后统一形成最终提交。

## 输入来源

* 用户提出的 Swagger 访问边界、课程页面、Markdown 渲染、理论与实验切换及正文衔接要求。
* `web/src/App.vue`：当前实验/API 顶层切换与实验工作台状态。
* `services/platform-api/internal/course`：课程详情 API、Markdown 正文和实验元数据。
* `web/Dockerfile`、`deploy/nginx/edge/default.conf` 与 `docker-compose.yml`：当前公网打包和端口边界。
* `web/e2e/workspace.spec.js`：现有真实用户流程验收。

## 结构化要求

| ID | 类型 | 要求 | 条件与约束 | 可观察结果 |
| --- | --- | --- | --- | --- |
| R1 | 安全约束 | 公网 Web 不再提供 Swagger UI 入口 | 删除 API 页签和前端 Swagger 组件引用 | 登录后页面没有 API 或 Swagger 入口 |
| R2 | 安全约束 | Edge Nginx 不再公开 OpenAPI YAML | `/platform-api.openapi.yaml` 不进入公网镜像 | 公网端口请求该路径返回 404 |
| R3 | 功能 | Swagger UI 通过独立端口提供 | 仅绑定 `127.0.0.1:${SWAGGER_PORT:-8081}` | 本机可打开文档，局域网和公网不可达 |
| R4 | 功能 | 私有 Swagger 保留 Try it out | Swagger 服务加入内部 API 网络并代理 `/api` | 文档页面可向平台 API 发起请求 |
| R5 | 功能 | 所有课程在页面显示理论正文 | 选择课程后读取现有课程详情 API | 右侧显示由 Markdown 渲染的课程内容 |
| R6 | 安全约束 | Markdown 禁止原始 HTML | 支持标题、列表、表格、代码和链接 | 正文排版完整且不会执行正文内 HTML |
| R7 | 功能 | 纯理论课程直接显示正文 | 课程没有可用实验 | 页面不显示课程/实验切换按钮 |
| R8 | 功能 | 混合课程提供课程/实验切换 | 两个按钮并排位于内容区顶部 | 用户可在理论正文和实验工作台间切换 |
| R9 | 业务规则 | 混合课程使用上下文默认页签 | 普通选择默认课程；恢复运行实验默认实验 | 会话内保持用户当前选择，返回实验时刷新快照 |
| R10 | 内容 | 实验课程正文增加实验衔接 | 只修改三个真实实验课程的每个二级章节 | 每节说明操作、配置、后端资源同步和观察效果 |

下一可用要求编号为 `R11`。所有要求均来自用户指令或当前实现事实，状态为已确认。

## 方案选择

课程正文继续由 Platform API 内嵌 Markdown 作为唯一内容源。前端调用
`GET /api/v1/courses/:slug`，使用禁用原始 HTML 的 Markdown 渲染器生成页面。
不在数据库或前端复制正文，也不让 API 返回预渲染 HTML。

Swagger 从学习平台 Web 中完全拆出。独立 Swagger 镜像复用固定版本的
`swagger-ui-dist` 静态资源，包含 OpenAPI YAML，并通过自己的 Nginx 将 `/api` 代理到
`platform-api`。该服务只发布宿主机回环端口。

## 前端组件与状态

`App.vue` 删除顶层 `view=lab/api` 状态和异步 `ApiConsole`。主页面始终显示课程目录和内容区。

新增理论课程组件，职责为：

* 显示课程标题、摘要和加载状态；
* 渲染课程详情中的 Markdown；
* 对外部链接使用安全属性；
* 不负责实验生命周期或轮询。

`App.vue` 增加课程详情、详情加载错误和当前课程模式状态。选择新课程时读取详情并默认进入课程模式。
恢复活动实验时选择对应课程并进入实验模式。用户在当前课程切换后保持该模式；切回实验模式时立即读取
最新快照并恢复现有轮询。

混合课程的“课程 / 实验”按钮位于右侧内容顶部。纯理论和待完善课程直接显示 Markdown 正文，
不渲染无效实验入口。课程目录标题从“实验课程”调整为“课程目录”。

## Markdown 内容

Markdown 渲染启用常用标题、段落、列表、表格、引用、代码块和链接，关闭原始 HTML。
正文样式限定在课程内容容器内，不影响实验工作台和 Swagger。

以下三个正文文件的每个 `##` 小节增加一段引用格式的“实验衔接”：

* `application-data-separation.md`
* `application-cluster.md`
* `multi-level-cache.md`

衔接句必须对应平台真实能力。例如调整实例性能时说明 Platform API 入队、Orchestrator 重建目标 Lab App、
Nginx 更新 upstream、页面快照同步处理速度和权重结果。不能描述平台没有实现的自由配置、任意命令或
缓存保护策略。纯理论课程和缓存故障课程不增加不存在的实验承诺。

## Swagger 部署边界

公网 Edge 镜像不复制 OpenAPI 文件，也不包含 Swagger 依赖生成的应用代码。
`/platform-api.openapi.yaml` 在 Edge 上没有专用 location，SPA fallback 需要显式排除该路径并返回 404。

新增 `swagger-ui` Compose 服务：

* 宿主机绑定 `127.0.0.1:${SWAGGER_PORT:-8081}:8080`；
* 静态提供 Swagger UI 和 `platform-api.openapi.yaml`；
* 在内部 `platform-edge-net` 上访问 `platform-api:8080`；
* `/api/` 与 `/readyz` 使用与 Edge 相同的必要代理头；
* 使用只读根文件系统、临时缓存目录、能力移除和 `no-new-privileges`；
* 提供独立健康检查。

`.env.example`、README 和 Compose 验证说明新增 `SWAGGER_PORT` 与本机访问地址。

## 错误处理

课程详情读取失败时保留课程目录，内容区显示稳定错误和重试按钮，不清空运行中的实验状态。
切换到实验时若已有快照则先显示快照并刷新；刷新失败沿用现有实验错误展示。

Swagger 服务故障不得影响 Edge、课程 API 或实验。Edge 上的文档路径始终返回 404，不能回退到 SPA。

## 测试与验收

* Go 课程与 HTTP 测试继续验证详情返回 `contentFormat=markdown` 和学习进度。
* 前端单元测试验证 Markdown 标题、表格、代码、链接和原始 HTML 禁用。
* E2E 验证纯理论课程直接显示正文。
* E2E 验证混合课程默认课程、课程/实验切换和运行实验恢复。
* 现有实验生命周期、流量、扩缩容、自适应和缓存回归继续通过。
* `curl http://127.0.0.1:${HTTP_PORT}/platform-api.openapi.yaml` 返回 404。
* `curl http://127.0.0.1:${SWAGGER_PORT}` 和私有 YAML 返回 200。
* `docker compose config --quiet`、相关镜像构建和服务健康检查通过。

## 非目标

* 不增加课程编辑器、后台发布系统或数据库正文表。
* 不向公网重新开放 Swagger、OpenAPI YAML 或新的文档域名。
* 不为纯理论课程创建实验资源。
* 不改变现有实验动作、资源配额和课程进度语义。
