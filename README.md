# 互联网后端架构学习平台

本仓库是互联网后端架构学习平台 MVP 工程。
当前已经实现课程、认证、学习进度、实验生命周期、安全资源编排、
应用集群、固定与自适应权重，以及浏览器生成的真实请求批次动画。

## 基础组件

* Vue 单页应用和公网入口 Nginx
* 回环地址独立 Swagger UI
* Go 平台 API
* Go 高权限编排器
* Go 动态实验应用镜像
* 共享 MySQL 8.4 LTS
* 一次性数据库迁移任务
* 内部实验 Nginx
* 受限 Docker Socket Proxy

## 启动

1. 根据 `.env.example` 准备 `.env`。
1. 确认 Docker daemon 可用，并验证 Compose 配置。

```bash
export ENV_FILE=.env
test -f "${ENV_FILE}"
docker info
docker compose --env-file "${ENV_FILE}" config --quiet
```

如果服务器使用其他环境文件，例如 `.env.server`，请改为
`export ENV_FILE=.env.server`。
后续 Compose 和镜像读取命令将始终使用同一个环境文件。

### 准备镜像

项目使用三类镜像，它们的准备方式不同：

* 常驻 Compose 服务镜像由常规 Compose 构建或启动命令准备。
* `lab-app` 是动态实验应用镜像，必须通过 `images` profile 显式构建。
* Redis 只在缓存实验运行时动态创建，不是常驻 Compose 服务，必须显式拉取。

`.env` 中的 `REDIS_IMAGE` 只是传给编排器的镜像名称。
`docker compose build` 和 `docker compose pull` 都不会自动准备该镜像。

1. 读取并拉取 `.env` 配置的 Redis 镜像。

```bash
REDIS_IMAGE="$(sed -n 's/^REDIS_IMAGE=//p' "${ENV_FILE}")"
test -n "${REDIS_IMAGE}"
docker pull "${REDIS_IMAGE}"
docker image inspect "${REDIS_IMAGE}" >/dev/null
```

1. 构建并检查动态实验应用镜像。

```bash
docker compose --env-file "${ENV_FILE}" --profile images build lab-app
LAB_APP_IMAGE="$(sed -n 's/^LAB_APP_IMAGE=//p' "${ENV_FILE}")"
test -n "${LAB_APP_IMAGE}"
docker image inspect "${LAB_APP_IMAGE}" >/dev/null
```

1. 构建常驻服务镜像。

```bash
docker compose --env-file "${ENV_FILE}" build
```

首次构建会拉取 Dockerfile 使用的 Go、Node、Nginx、MySQL 和 Alpine
基础镜像，并下载 Go 与 npm 依赖。

1. 启动服务并等待健康检查完成。

```bash
docker compose --env-file "${ENV_FILE}" up -d --wait
docker compose --env-file "${ENV_FILE}" ps -a
```

浏览器访问 `http://127.0.0.1:8080`。
端口可以通过 `.env` 中的 `HTTP_PORT` 修改。

普通用户页面不提供 Swagger 或 OpenAPI 文件。
运维人员可在服务器本机访问 `http://127.0.0.1:8081`，端口可以通过
`.env` 中的 `SWAGGER_PORT` 修改。
该端口只绑定回环地址，并通过容器内部网络代理 Platform API 的 Try it out 请求。

## 镜像问题排查

如果出现以下错误：

```text
failed to connect to the docker API at unix:///var/run/docker.sock
```

表示 Docker daemon 不可用。

```bash
systemctl status docker --no-pager -l
systemctl enable --now docker
docker info
```

如果创建缓存实验时出现：

```text
DOCKER_UNAVAILABLE
redis image is unavailable
```

表示 `REDIS_IMAGE` 指定的镜像不存在于服务器本地。

```bash
ENV_FILE="${ENV_FILE:-.env}"
REDIS_IMAGE="$(sed -n 's/^REDIS_IMAGE=//p' "${ENV_FILE}")"
docker image inspect "${REDIS_IMAGE}"
docker pull "${REDIS_IMAGE}"
```

常见拉取错误含义：

* `i/o timeout` 或 `TLS handshake timeout` 表示网络、DNS 或仓库访问异常。
* `manifest unknown` 表示镜像标签不存在。
* `no matching manifest` 表示镜像不支持当前服务器 CPU 架构。
* `429 Too Many Requests` 表示镜像仓库限制了拉取频率。

检查服务器架构和当前镜像：

```bash
uname -m
docker image ls
docker image ls redis
docker image ls 'website-gobased/*'
```

## 验证

```bash
ENV_FILE="${ENV_FILE:-.env}"
make verify
npm --prefix web test
npm --prefix web run test:e2e
docker compose --env-file "${ENV_FILE}" config --quiet
docker image inspect "$(sed -n 's/^REDIS_IMAGE=//p' "${ENV_FILE}")" >/dev/null
docker image inspect "$(sed -n 's/^LAB_APP_IMAGE=//p' "${ENV_FILE}")" >/dev/null
curl --fail http://127.0.0.1:8080/readyz
curl --fail http://127.0.0.1:8081/healthz
curl --fail http://127.0.0.1:8081/platform-api.openapi.yaml
curl --fail-with-body http://127.0.0.1:8080/platform-api.openapi.yaml
```

最后一条公网 OpenAPI 检查预期返回 `404`，因此命令本身应以非零状态退出。
