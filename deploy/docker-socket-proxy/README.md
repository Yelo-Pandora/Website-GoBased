# Docker Socket Proxy 权限

Socket Proxy 是唯一挂载宿主机 Docker Socket 的常驻容器。
它只加入 `orchestration-net`，不发布宿主机端口。

Compose 仅开启以下 API 类别：

* `CONTAINERS`
* `EXEC`
* `IMAGES`
* `INFO`
* `NETWORKS`
* `PING`
* `VERSION`
* 受控的容器启动、停止和重启操作

写操作需要 `POST=1`。
该开关仍然具有较高权限，因此编排器必须继续执行模板白名单、参数校验和资源标签校验。
