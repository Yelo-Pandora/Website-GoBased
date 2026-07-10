# Purchase API Demo

这是一个使用 Go + Gin + MySQL 构建的购买功能 RESTful API 示例项目。

项目目标不是做一个“大而全”的商城系统，而是提供一个**结构清晰、便于继续扩展**的后端骨架，帮助你理解：

- Go 项目如何分层
- Gin 在 Web 层如何使用
- `service` / `repository` / `domain` 如何配合
- Go 如何连接 MySQL
- 如何通过 `Makefile` 快速构建、运行和测试

---

## 1. 项目功能

当前实现了一个购买资源 `purchase` 的基础 CRUD 风格接口：

- `GET /healthz`
- `GET /api/v1/purchases`
- `POST /api/v1/purchases`
- `GET /api/v1/purchases/:id`
- `PATCH /api/v1/purchases/:id/status`
- `DELETE /api/v1/purchases/:id`

购买记录字段包括：

- `id`
- `user_id`
- `product_id`
- `quantity`
- `total_amount`
- `currency`
- `status`
- `created_at`
- `updated_at`

---

## 2. 技术栈

- Go `1.26.2`
- Gin
- MySQL
- `database/sql`
- `github.com/go-sql-driver/mysql`

---

## 3. 项目结构

```text
.
├─ cmd/
│  └─ api/
│     └─ main.go
├─ internal/
│  ├─ app/
│  │  └─ server.go
│  ├─ config/
│  │  └─ config.go
│  ├─ domain/
│  │  └─ purchase.go
│  ├─ httpapi/
│  │  ├─ handler_test.go
│  │  ├─ health_handler.go
│  │  ├─ middleware.go
│  │  ├─ purchase_handler.go
│  │  ├─ response.go
│  │  └─ router.go
│  ├─ repository/
│  │  ├─ purchase.go
│  │  └─ mysql/
│  │     └─ purchase_repository.go
│  └─ service/
│     └─ purchase_service.go
├─ bin/
│  └─ purchase-api
├─ go.mod
├─ go.sum
├─ Makefile
├─ schema.sql
└─ README.md
```

---

## 4. 每个文件的作用

下面按“从外到内”的方式解释每个文件的职责。

### 4.1 `cmd/api/main.go`

程序入口。

作用：

- 加载配置
- 调用应用装配层创建 HTTP 服务
- 启动服务

可以把它理解成：

> “告诉程序从哪里开始跑”

它本身尽量保持简单，不直接处理具体业务。

---

### 4.2 `internal/app/server.go`

应用装配层（bootstrap / wiring）。

作用：

- 打开 MySQL 连接
- `Ping` 数据库确认可用
- 创建 repository
- 创建 service
- 创建 Gin router
- 组装成 `http.Server`

可以把它理解成：

> “把项目里各个模块接起来”

如果以后要加 Redis、消息队列、日志器，这里通常也是重要入口之一。

---

### 4.3 `internal/config/config.go`

配置层。

作用：

- 从环境变量读取配置
- 提供默认值
- 拼出 MySQL DSN

当前支持的配置：

- `API_ADDR`
- `DB_USER`
- `DB_PASSWORD`
- `DB_HOST`
- `DB_PORT`
- `DB_NAME`

它解决的问题是：

> “程序运行时，地址、端口、数据库信息从哪里来？”

---

### 4.4 `internal/domain/purchase.go`

领域模型（domain model）。

作用：

- 定义购买记录 `Purchase` 的核心数据结构

这里放的是系统里的“业务对象长什么样”，而不是 HTTP 或数据库细节。

你可以把它理解成：

> “购买对象的标准样子”

---

### 4.5 `internal/repository/purchase.go`

仓储接口层。

作用：

- 定义购买数据访问需要具备哪些能力
- 定义 `PurchaseRepository` 接口
- 定义创建购买时的输入结构 `CreatePurchaseInput`
- 定义仓储层错误，例如 `ErrPurchaseNotFound`

这层的价值是：

- 业务层只依赖接口，不依赖 MySQL 实现细节
- 测试时可以用 fake/mock 替代真实数据库

---

### 4.6 `internal/repository/mysql/purchase_repository.go`

MySQL 仓储实现层。

作用：

- 实现 `PurchaseRepository` 接口
- 执行 SQL
- 把数据库记录读写成 `domain.Purchase`

包含的主要方法：

- `Create`
- `GetByID`
- `List`
- `UpdateStatus`
- `Delete`

这层解决的问题是：

> “购买数据如何真正存进 MySQL、从 MySQL 查出来？”

---

### 4.7 `internal/service/purchase_service.go`

业务层。

作用：

- 校验输入是否合法
- 处理业务规则
- 调用 repository 完成持久化

当前做的业务逻辑包括：

- 校验 `user_id`、`product_id`、`quantity`、`total_amount`
- 标准化 `currency`
- 标准化 `status`
- 设置默认状态 `pending`
- 限制状态只能是：
  - `pending`
  - `paid`
  - `cancelled`
  - `refunded`

这层的意义是：

> “规则放这里，而不是直接写进 handler 或 SQL”

---

### 4.8 `internal/httpapi/router.go`

Gin 路由注册层。

作用：

- 创建 Gin 引擎
- 注册中间件
- 注册所有 HTTP 路由
- 把请求路径分配给对应 handler

可以理解成：

> “URL 该交给谁处理”

---

### 4.9 `internal/httpapi/middleware.go`

Gin 中间件层。

当前实现：

- `RequestID()`：为每个请求注入 `X-Request-ID`

作用：

- 给每个请求附加公共能力
- 不污染具体 handler

以后很适合在这里继续加：

- 认证鉴权
- CORS
- 统一日志
- 统一错误恢复

---

### 4.10 `internal/httpapi/health_handler.go`

健康检查 handler。

作用：

- 响应 `/healthz`
- 用来验证服务是否存活

这类 handler 一般都很简单，但在真实服务里非常常见。

---

### 4.11 `internal/httpapi/purchase_handler.go`

购买资源的 HTTP 处理层。

作用：

- 接收 HTTP 请求
- 解析路径参数和 JSON 请求体
- 调用 `service`
- 返回 JSON 和状态码

它是“Web 层”，所以会出现：

- `gin.Context`
- `ShouldBindJSON`
- `c.Param`
- `c.JSON`

但它不应该负责复杂业务逻辑，这些逻辑应该下放到 `service`。

---

### 4.12 `internal/httpapi/response.go`

统一响应辅助层。

当前主要提供：

- `writeError(...)`

作用：

- 统一错误 JSON 结构

以后如果要做统一响应格式，比如：

```json
{
  "code": 0,
  "message": "ok",
  "data": {}
}
```

这类逻辑也适合从这里继续扩展。

---

### 4.13 `internal/httpapi/handler_test.go`

HTTP 接口测试。

作用：

- 用 `httptest` 启动测试服务器
- 构造 fake repository
- 验证购买接口生命周期

当前覆盖了：

- 创建购买
- 查询购买
- 获取列表
- 更新状态

这个文件很重要，因为它展示了：

- Gin 路由如何测试
- 如何不依赖真实数据库测试 HTTP 层

---

### 4.14 `schema.sql`

数据库建表脚本。

作用：

- 创建 `purchases` 表

也就是：

> “数据库最基本要长成什么样”

如果以后增加字段、索引、外键，通常会继续在数据库迁移系统里维护，而不只靠这一份 SQL。

---

### 4.15 `Makefile`

便捷命令入口。

当前目标：

- `make build`
- `make run`
- `make test`
- `make tidy`
- `make fmt`

作用：

- 统一常用开发命令
- 避免每次手打一长串 Go 命令

---

### 4.16 `go.mod`

Go 模块定义文件。

作用：

- 定义模块名 `website-gobased`
- 记录直接依赖

你可以把它理解成：

> “这个项目依赖了哪些 Go 包”

---

### 4.17 `go.sum`

依赖校验文件。

作用：

- 记录依赖包的哈希
- 确保下载内容一致

一般由 Go 自动维护，不需要手改。

---

### 4.18 `.gitignore`

Git 忽略配置。

当前主要忽略：

- `bin/`
- `.env`

作用：

- 避免把编译产物和本地敏感配置提交进仓库

---

### 4.19 `bin/purchase-api`

编译产物。

作用：

- `go build` 后生成的可执行文件

这个文件不是源码，一般不需要阅读。

---

## 5. 项目分层关系

当前项目大致按下面的方向流动：

```text
HTTP Request
    ↓
Gin Router
    ↓
Handler
    ↓
Service
    ↓
Repository Interface
    ↓
MySQL Repository
    ↓
MySQL
```

也可以换一种说法：

- `handler` 负责“接请求、回响应”
- `service` 负责“处理业务规则”
- `repository` 负责“读写数据库”

---

## 6. 推荐阅读顺序

如果你是第一次看这个项目，建议按下面顺序阅读。

### 第一遍：先看整体入口

1. `cmd/api/main.go`
2. `internal/app/server.go`
3. `internal/httpapi/router.go`

这三步能帮助你先回答：

- 程序从哪里启动？
- 服务是怎么组装出来的？
- URL 是怎么接到 handler 上的？

---

### 第二遍：看 HTTP 层

4. `internal/httpapi/purchase_handler.go`
5. `internal/httpapi/health_handler.go`
6. `internal/httpapi/middleware.go`
7. `internal/httpapi/response.go`

这一步重点理解：

- Gin 怎么处理请求
- 路径参数怎么取
- JSON 怎么绑定
- 错误怎么返回

---

### 第三遍：看业务层

8. `internal/service/purchase_service.go`
9. `internal/repository/purchase.go`
10. `internal/domain/purchase.go`

这一步重点理解：

- 为什么校验逻辑在 `service`
- 为什么 `repository` 先定义接口
- `domain` 为什么尽量简单

---

### 第四遍：看数据库实现

11. `internal/repository/mysql/purchase_repository.go`
12. `schema.sql`
13. `internal/config/config.go`

这一步重点理解：

- SQL 是怎么写的
- `domain.Purchase` 如何映射到数据库表
- MySQL 连接字符串怎么拼

---

### 第五遍：看测试和开发工具

14. `internal/httpapi/handler_test.go`
15. `Makefile`
16. `go.mod`

这一步重点理解：

- HTTP 接口如何测试
- 开发命令如何统一
- 依赖如何管理

---

## 7. 如何运行

### 7.1 先准备数据库

创建一个 MySQL 数据库，例如：

```sql
CREATE DATABASE gobank;
```

然后执行：

```sql
SOURCE schema.sql;
```

或者把 `schema.sql` 里的内容直接执行到数据库里。

---

### 7.2 设置环境变量

PowerShell 示例：

```powershell
$env:API_ADDR=":8080"
$env:DB_USER="root"
$env:DB_PASSWORD="change-me"
$env:DB_HOST="127.0.0.1"
$env:DB_PORT="3306"
$env:DB_NAME="gobank"
```

---

### 7.3 运行项目

```bash
make run
```

或者：

```bash
go run ./cmd/api
```

---

### 7.4 测试项目

```bash
make test
```

---

### 7.5 构建项目

```bash
make build
```

构建结果位于：

```text
bin/purchase-api
```

---

## 8. 一个请求示例

### 创建购买

请求：

```http
POST /api/v1/purchases
Content-Type: application/json

{
  "user_id": 1,
  "product_id": 1001,
  "quantity": 2,
  "total_amount": 99.9,
  "currency": "cny"
}
```

响应示意：

```json
{
  "id": 1,
  "user_id": 1,
  "product_id": 1001,
  "quantity": 2,
  "total_amount": 99.9,
  "currency": "CNY",
  "status": "pending",
  "created_at": "2026-07-10T12:00:00Z",
  "updated_at": "2026-07-10T12:00:00Z"
}
```

---

## 9. 这个项目最值得你重点理解的点

如果你现在主要是为了学习 Go 后端，我建议重点抓下面几点：

### 9.1 为什么 `main` 很薄

因为入口越薄，项目越容易扩展和测试。

---

### 9.2 为什么 `service` 不依赖 Gin

因为业务逻辑最好不和具体 Web 框架耦合。

这样将来：

- 换 Echo / Fiber
- 写命令行工具
- 接消息队列消费者

都还能复用业务层。

---

### 9.3 为什么 `repository` 要先定义接口

因为这样：

- 业务层依赖抽象
- 测试时可用 fake repository
- 以后能换 MySQL / PostgreSQL / mock 实现

---

### 9.4 为什么 Gin 相关逻辑尽量只留在 `httpapi`

这是分层的关键：

- HTTP 是“接入方式”
- 不是“业务本身”

---

## 10. 后续可以怎么扩展

这个项目现在只是一个干净骨架，后面可以继续加：

- 用户和商品表
- 购买前库存校验
- 事务处理
- JWT 鉴权
- 请求参数校验标签
- 统一日志
- Swagger/OpenAPI 文档
- 数据库迁移工具
- Dockerfile / docker-compose
- `.env.example`
- 更细的目录拆分（如 `handlers/requests/responses`）

---

## 11. 一句话理解整个项目

如果要用一句话概括当前项目：

> 这是一个采用 **Gin 接入层 + Service 业务层 + Repository 数据层** 的 Go RESTful API 示例项目。

