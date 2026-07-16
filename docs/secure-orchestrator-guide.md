# 安全编排器说明

## 1. 阶段边界

阶段四实现受限资源编排，但不开放浏览器侧实验创建。
公开端点 `POST /api/v1/labs` 继续完成 Session 和 CSRF 校验后返回
`501 LABS_NOT_IMPLEMENTED`。
阶段五完成平台资源快照持久化和应用与数据分离闭环后，才会返回
`202 Accepted`。

当前阶段可以通过内部 UDS 命令真实创建和清理实验数据库、Docker 网络、应用容器以及
Nginx 片段。
该入口只挂载在 `platform-api` 与 `orchestrator` 之间，不监听 TCP 端口。

## 2. 启动校验

`orchestrator` 在监听 UDS 前依次完成：

1. 严格加载资源模板和场景模板。
2. 检查模板继承、引用和安全属性。
3. 连接受限 MySQL 账号。
4. 探测 Docker Socket Proxy。
5. 加载 Nginx 结构化片段模板。
6. 组装命令执行、补偿和资源核对依赖。

任一步失败都会阻止服务进入健康状态。
平台 API 不会绕过编排器直接访问 Docker 控制面。

## 3. 模板注册表

模板注册表从 `/opt/platform/configs` 读取固定 JSON 文件。
解析时拒绝未知字段、重复 ID、循环继承、缺失引用和无效资源限制。

容器模板固定以下内容：

* 镜像环境变量名称；
* 非 root 用户；
* 只读根文件系统；
* 非特权模式；
* capability 全部移除；
* 内存、CPU 和 PID 限制；
* 健康检查；
* 固定启动命令和模块集合。

命令 payload 不能传入镜像、挂载、端口发布、用户、特权配置或原始 Docker 参数。
Redis 模板同样使用非 root、无持久化、无端口发布和有界内存配置。

## 4. 结构化命令

UDS 入口只接受合同中列出的 13 种命令。
每条命令先验证 `commandId`、`operationId`、`labId`、`requestedBy` 和严格 payload，
再调用固定适配器。

非法 JSON 返回 `400 INVALID_COMMAND`。
命令类型或参数不符合白名单时返回 `rejected` 响应。
执行失败返回 `failed` 响应和稳定错误码，内部错误正文只进入日志。

`PROVISION_LAB` 按以下顺序执行：

1. 验证场景模板和预拉取镜像。
2. 调用存储过程创建实验数据库和账号。
3. 创建带平台标签的内部网络。
4. 将固定的实验 Nginx 和共享 MySQL 接入该网络。
5. 创建受模板约束的应用实例和可选 Redis。
6. 生成、校验并重载 Nginx 片段。

资源名称、数据库密码和标签都由可信配置与 `labId` 确定性产生。
相同命令重试会识别现有资源，不重复创建数据库、网络或容器。

## 5. Docker 适配器

Docker 客户端只封装阶段四需要的 Engine API：

* 镜像存在性检查；
* 容器创建、启动、重启、删除和 CPU 配额更新；
* 网络创建、连接、断开和删除；
* 按平台标签查询资源；
* 在固定网关容器中执行 Nginx 校验和重载。

Socket Proxy 不公开宿主机端口。
Compose 只开启 `CONTAINERS`、`NETWORKS`、`IMAGES`、`EXEC`、`POST`、
`DELETE` 以及健康和版本查询所需类别。

动态资源必须包含 `platform.managed=true`、`platform.labId`、
`platform.operationId`、`platform.resourceType` 和 `platform.templateId`。
如果同名资源存在但标签或镜像不符合模板，编排器会拒绝复用。

## 6. 实验数据库

编排器数据库账号仍然只有固定存储过程的 `EXECUTE` 权限。
Go 适配器不接受原始 SQL，也不持有 DDL 权限。

迁移 `003_lab_database_reconciliation.sql` 增加
`orchestrator_lab_databases` 登记表以及受限枚举过程。
只有同时满足受管登记和真实 Schema 存在的数据库才会进入资源核对结果。
这避免仅凭 `lab_` 名称前缀误删维护人员创建的其他数据库。

每个实验数据库密码通过服务端密钥和 `labId` 确定性派生，不写入命令响应、平台表或日志。
重复创建会得到同一凭据，平台重启后仍可重新创建应用实例。

## 7. Nginx 配置

实验 upstream 使用结构化模板生成，用户不能提交原始 Nginx 文本。
每个 upstream 地址、端口和权重都经过范围校验。

更新流程为：

1. 在共享卷生成候选文件。
2. 原子替换目标片段。
3. 在固定 `lab-gateway-nginx` 容器内执行 `nginx -t`。
4. 校验成功后执行优雅重载。
5. 校验或重载失败时恢复旧文件并重新加载旧配置。

运行中的 Nginx 不会因为失败候选文件切换到不可用配置。

## 8. 补偿与资源核对

实验创建中途失败时，编排器按反向顺序删除当前实验的 Nginx 片段、容器、固定服务网络连接、
动态网络、数据库和数据库账号。
清理目标必须带匹配的实验标签或受管数据库登记。

`RECONCILE_RESOURCES` 从以下来源汇总状态：

* `platform.lab_sessions` 中仍应持有资源的实验；
* Docker 管理标签；
* Nginx 片段文件；
* 受管实验数据库登记表。

命令默认只报告孤儿资源。
只有明确设置清理参数时才执行反向清理。

## 9. 验证结果

阶段四验收覆盖：

* 模板严格解析和继承；
* Docker 安全 HostConfig；
* 数据库名称和账号校验；
* Nginx 失败回滚；
* UDS 命令执行和身份回显；
* 创建失败补偿；
* 真实 UDS 重复创建幂等；
* Docker、MySQL 和 Nginx 真实创建后完整清理；
* Compose 健康状态；
* 公开实验端点继续返回 501。

## 10. 下一阶段

阶段五将把平台实验创建服务接入现有 HTTP handler，并把编排结果写入
`lab_sessions`、`lab_instances` 和 `lab_resources`。
随后实现应用与数据分离课程所需的商品查询、实验快照、重置和主动结束接口。
