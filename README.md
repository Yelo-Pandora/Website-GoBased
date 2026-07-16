# 互联网后端架构学习平台

本仓库是互联网后端架构学习平台 MVP 工程。
当前已经实现课程、认证、学习进度、实验生命周期、安全资源编排、
应用集群、固定权重和浏览器生成的真实请求批次动画。

## 基础组件

* Vue 单页应用和公网入口 Nginx
* Go 平台 API
* Go 高权限编排器
* Go 动态实验应用镜像
* 共享 MySQL 8.4 LTS
* 一次性数据库迁移任务
* 内部实验 Nginx
* 受限 Docker Socket Proxy

## 启动

1. 根据 `.env.example` 准备 `.env`。
1. 构建并启动常驻服务。

```bash
docker compose build
docker compose up -d --wait
docker compose ps
```

浏览器访问 `http://127.0.0.1:8080`。
端口可以通过 `.env` 中的 `HTTP_PORT` 修改。

## 验证

```bash
make verify
npm --prefix web test
npm --prefix web run test:e2e
docker compose config --quiet
docker compose --profile images build lab-app
curl --fail http://127.0.0.1:8080/readyz
```

完整需求、架构和脚手架设计位于 `docs/superpowers/specs`。

基础脚手架的作用、组件边界和推荐阅读顺序见
[`docs/foundation-scaffold-guide.md`](docs/foundation-scaffold-guide.md)。

前端预期 API、JSON 示例和待确认契约见
[`docs/frontend-api-integration-guide.md`](docs/frontend-api-integration-guide.md)。

实验控制面和安全编排器边界分别见
[`docs/lab-control-foundation-guide.md`](docs/lab-control-foundation-guide.md) 与
[`docs/secure-orchestrator-guide.md`](docs/secure-orchestrator-guide.md)。

阶段七真实流量、扩缩容、性能和固定权重验收见
[`docs/stage-7-user-acceptance-guide.md`](docs/stage-7-user-acceptance-guide.md)。
