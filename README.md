# 互联网后端架构学习平台

本仓库是互联网后端架构学习平台的基础工程。
当前阶段提供可构建、可启动、可健康检查的容器化脚手架，尚未实现完整课程和实验业务。

## 基础组件

* Vue 单页应用和公网入口 Nginx
* Go 平台 API
* Go 高权限编排器
* Go 动态实验应用镜像
* 共享 MySQL 8.4 LTS
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
docker compose config --quiet
docker compose --profile images build lab-app
```

完整需求、架构和脚手架设计位于 `docs/superpowers/specs`。
