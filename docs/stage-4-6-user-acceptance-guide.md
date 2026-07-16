# 阶段四至六真实用户验收指南

## 1. 验收入口

启动完整环境：

```powershell
docker compose up -d --build
docker compose ps
```

所有长期服务应为 `healthy`。浏览器打开：

```text
http://localhost:8080
```

本地测试账号：

```text
账号：learner
密码：example-password
```

首页是普通用户实验工作台，顶部的 `API` 标签保留 Swagger UI。

## 2. 标准用户流程

1. 登录并确认出现课程列表。
2. 选择“应用集群与负载均衡”。
3. 点击“创建实验”。
4. 观察状态从“准备中”进入“运行中”。
5. 确认拓扑出现实验网关、`app-1` 和实验数据库。
6. 确认空闲时限和最长时限开始倒计时。
7. 刷新浏览器，确认登录态、实验 ID、拓扑和最新操作能够恢复。
8. 点击“重置”，确认最新操作变为“重置实验 / succeeded”，实验重新回到“运行中”。
9. 切换到 `API` 标签，确认 Swagger UI 可以正常加载，再切回实验视图。
10. 点击“结束”并确认，等待状态变为“已结束”，结束原因为“用户主动结束”。

## 3. 阶段四验收

阶段四没有浏览器直接调用编排器的端点，通过真实实验创建和结束间接验收。

实验运行时执行：

```powershell
docker ps --filter "label=platform.managed=true" `
  --format "{{.ID}} {{.Names}} {{.Labels}}"
docker network ls --filter "label=platform.managed=true"
```

预期至少出现带当前 `platform.labId` 的应用容器和实验网络资源。
实验页面中的网关和应用实例必须来自真实编排结果，而不是前端伪造。

结束实验后再次执行相同命令，应不再存在该实验的受管资源。
MySQL 仍应只绑定本机回环地址：

```powershell
docker compose port shared-mysql 3306
```

预期地址以 `127.0.0.1:` 开头。浏览器不能直接访问 Docker Socket、UDS、MySQL、
Lab App 或 Lab Gateway 内部端点。

## 4. 阶段五验收

阶段五的必须通过项：

| 操作 | 页面预期 | 后端预期 |
|---|---|---|
| 创建 | `Preparing -> Running` | 创建数据库、网络、应用实例和 Nginx 片段 |
| 刷新 | 恢复同一实验和最新操作 | 快照按用户归属读取 |
| 重置 | 操作最终 `succeeded` | 原资源清理后按模板重建，活动时间刷新 |
| 主动结束 | `Terminating -> Terminated` | 所有实验资源清理，原因 `user_requested` |
| 重复点击 | 按钮禁用或返回稳定冲突 | 不创建重复操作或重复资源 |

页面错误区域会显示稳定错误码和 `requestId`。写请求失败后不要在浏览器开发者工具中
手工重复提交相同请求，应等待快照恢复事实状态。

## 5. 阶段六短时验收

正常配置需要等待 10 分钟和 30 分钟。验收时可以在当前 PowerShell 会话临时缩短：

```powershell
$env:LAB_IDLE_TIMEOUT = "90s"
$env:LAB_MAX_DURATION = "3m"
$env:LAB_EXPIRING_LEAD = "30s"
$env:LAB_LIFECYCLE_POLL_INTERVAL = "2s"
docker compose up -d --force-recreate platform-api
```

重新登录并创建实验，不执行重置或结束：

1. `Running` 时空闲倒计时从约 `01:30` 开始。
2. 浏览器刷新和快照轮询不会让倒计时回升。
3. 剩余约 30 秒时状态进入 `Expiring`，页面显示“即将过期”。
4. 空闲时限到达后状态进入 `Terminating`。
5. Worker 完成清理后状态为 `Terminated`，原因显示“空闲超时”。
6. 刷新页面后终态和最新 `DESTROY_LAB` 操作仍可恢复。

再创建一次实验，在倒计时结束前点击“重置”。重置成功后空闲倒计时应重新开始，
最长时限仍以实验首次启动时间为准。

恢复默认环境：

```powershell
Remove-Item Env:LAB_IDLE_TIMEOUT
Remove-Item Env:LAB_MAX_DURATION
Remove-Item Env:LAB_EXPIRING_LEAD
Remove-Item Env:LAB_LIFECYCLE_POLL_INTERVAL
docker compose up -d --force-recreate platform-api
```

## 6. 自动化验收

前端 E2E 使用真实 Edge、Platform API、编排器、MySQL 和 Docker 资源：

```powershell
npm --prefix web run build
npm --prefix web run test:e2e
go test ./...
go vet ./...
docker compose config --quiet
```

E2E 覆盖登录、课程选择、创建至 `Running`、拓扑、倒计时、重置、Swagger 切换、
主动结束以及 390px 移动视口横向溢出检查。设置 `RUN_SHORT_LIFECYCLE=1` 并配合短时
生命周期配置时，还会验证 `Expiring` 和自动空闲回收。

## 7. 清理确认

验收结束后：

```powershell
docker ps --filter "label=platform.managed=true" --format "{{.ID}} {{.Names}}"
docker compose ps
```

第一条命令应无输出，长期服务应继续保持 `healthy`。
