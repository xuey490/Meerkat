# 使用说明

当前项目是通过 `bw-cli new` 生成的干净框架，不包含业务 demo。

## 1. 初始化

```bash
cd <project>
make tidy
make proto
make test
```

如果当前还没有 proto 文件，`make proto` 会输出 `No proto files found` 并正常结束。
Makefile 只调用 Go 命令，不依赖 Unix shell 语法；`make proto` 会通过 `tools/protogen` 自动适配 Windows、macOS、Linux。
Windows 仍需要安装 GNU Make；如果没有 `make`，可以直接执行等价的 Go 命令，例如 `go run ./tools/protogen`、`go test ./...`、`go run ./cmd/gateway`。

## 2. 启动 gateway

```bash
make run-gateway
```

端口监听成功后，控制台会输出服务名、环境、监听地址、HTTP 地址、健康检查地址和 API 前缀。
如果端口被占用，控制台会输出 `[Gateway Start Failed]`，并显示失败的监听地址和系统错误。

默认监听：

```text
http://localhost:8080
```

健康检查：

```bash
curl http://localhost:8080/healthz
```

## 3. 配置

主配置文件：

```text
configs/config.yaml
```

需要调整端口、日志级别或数据库类型时，修改 `configs/config.yaml`。

## 4. 当前 module

```text
github.com/company/monitor-webserver
```

## 5. 查看公共工具

公共工具的详细调用流程见：

```text
docs/toolkit.md
```

MongoDB 教学文档见：

```text
docs/mongodb.md
```

Alipay、Elasticsearch、Kafka 和 JWT 的接入示例见：

```text
docs/alipay.md
docs/elasticsearch.md
docs/toolkit.md
```

## 6. 新增业务服务

进入项目根目录执行：

```bash
bw-cli service comment --tidy
```

不传 `--port` 时会根据 `configs/config.yaml` 中已有服务端口自动递增。需要指定端口时使用：

```bash
bw-cli service comment --port 9103 --tidy
```

如果要绑定已有数据库表，可以使用 `--table`：

```bash
bw-cli service comment --table comments --tidy
```

当前 `--table` 会连接 `configs/config.yaml` 中配置的数据库并读取指定表的真实字段，再按这些字段生成 entity、model、DTO、proto、repo 映射和 gateway 入参。它不再要求表中包含默认示例字段，例如 `description`；生成后的服务会跳过 `AutoMigrate`，避免修改既有表结构。MySQL 或 PostgreSQL 需要指定 schema 时，可以传 `--schema`。

如果要通过界面配置单表或多表关联，启动本地可视化设计器：

```bash
bw-cli designer --dir . --addr 127.0.0.1:6060
```

设计器只监听本机地址，默认驱动是 `mysql`，DSN 默认内容是：

```text
账号:密码@tcp(服务器IP:3306)/数据库?charset=utf8mb4&parseTime=True&loc=Local
```

在界面中读取表结构、选择主表和参与生成的表、配置表关系后，计划会保存到：

```text
scaffold-plans/<service>.json
```

也可以用命令行基于计划生成：

```bash
bw-cli service product --plan scaffold-plans/product.json --tidy
```

单表计划只选择一张表即可。多表计划会额外生成 `internal/<service>/repo/relationships.go`，其中包含 `SelectedTables()`、`TableRelations()`、`NewRelationQuery()` 和按关系生成的 Join 查询入口。

生成后的服务会自动追加 `configs/config.yaml`：

- 服务名、gRPC 端口和 gateway target 写在 `services.comment`。
- 数据库继续读取当前项目已有的 `database`、`mysql`、`postgresql` 配置。
- 默认使用 MySQL，启动前需要把 `mysql.dsn` 中的账号、密码、服务器 IP 和数据库名替换为真实值。
- proto、handler、entity、model、service、repo、gateway HTTP 入口和 service 单测都会同时生成。
- 如果启用了 Nacos，命令行会提示把本地新增配置同步到 Nacos。

生成结构：

```text
api/proto/comment/v1/comment.proto
api/gen/comment/v1
cmd/comment/main.go
internal/comment/entity     # 业务实体、业务错误、Repository 接口
internal/comment/model      # 数据库表结构和文档结构
internal/comment/dto/command.go      # 业务用例入参命令
internal/comment/dto/comment.go      # 业务用例出参 DTO 和转换
internal/comment/service/service.go  # 业务流程编排
internal/comment/repo       # 数据库访问，默认 Gorm，同时生成 MongoDB 实现
internal/comment/handler    # gRPC 协议转换
internal/gateway/request/comment_request.go
internal/gateway/handler/comment_handler.go
internal/gateway/router/comment_routes.go
docs/services/comment.md    # 单服务详细开发说明
```

删除脚手架生成的服务：

```bash
bw-cli delete-service comment --tidy
```

删除命令会清理服务目录、proto/gen、gateway request/handler/router、`docs/services/comment.md`、`Makefile` 目标和 `configs/config.yaml` 中的服务配置。如果启用了 Nacos，命令行会提示把本地配置删除结果同步到 Nacos。

生成后的基础 CRUD 调用链：

```text
gRPC client -> proto -> handler -> service -> entity.Repository -> repo(Gorm) -> database
```

默认启动使用 `repo/gorm_repository.go`。如果业务更适合 MongoDB，生成的 `repo/mongo_repository.go` 已经包含 `MongoCollectionName()`、`mongox.NewDocumentStore[T]` 和基础 CRUD 方法，只需要在服务 main 中改为注入 `repo.NewMongoRepository`。

HTTP 入口也已挂载：

```text
POST   /api/v1/comments
GET    /api/v1/comments
GET    /api/v1/comments/:id
PUT    /api/v1/comments/:id
DELETE /api/v1/comments/:id
```

默认提供：

```text
CreateComment
GetComment
ListComments
UpdateComment
DeleteComment
```

开发原则：

- `entity` 写业务核心，不依赖 Gin、gRPC、Gorm，也不写数据库 tag。
- `model` 只写数据库表结构、文档结构、`TableName()` 和 `MongoCollectionName()`，不写查询逻辑。
- `dto/command.go` 写业务入参，`dto/<service>.go` 写业务出参，`service/service.go` 写业务流程。
- `repo` 是数据库操作唯一入口，Gorm/MongoDB/Redis 查询都放这里。
- `handler` 只做 gRPC request/response 转换和错误映射。
- HTTP 入参放 `internal/gateway/request`，路由按 `/api/v1/<business>` 拆分。

数据库操作示例见每次生成的 `docs/services/<service>.md`。
