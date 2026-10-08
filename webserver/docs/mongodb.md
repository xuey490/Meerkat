# MongoDB 从 0 到 1 使用教程

当前项目 module：

```text
github.com/company/monitor-webserver
```

## 1. MongoDB 是什么

MongoDB 是文档数据库，保存的是 BSON 文档。关系型数据库常见结构是 database/table/row/column，MongoDB 对应 database/collection/document/field。

适合使用 MongoDB 的场景：

- 内容草稿、富文本 JSON、扩展字段。
- 用户偏好、第三方回调原始数据。
- 操作日志、行为事件、审计记录。
- 结构变化快、字段差异大的业务文档。

不建议用 MongoDB 承担强事务账务、复杂 join 报表或必须依赖外键约束的核心关系模型。

## 2. 准备连接信息

脚手架不在文档中假设固定的 MongoDB 启动方式。你可以使用公司测试环境、本机已安装的 MongoDB 或云数据库，只需要把真实连接地址、认证信息和 database 名称写入 `configs/config.yaml`。

本地无认证示例：

```text
uri: mongodb://127.0.0.1:27017
username: ""
password: ""
database: app
```

## 3. mongosh 入门

如果本机已经安装 `mongosh`，可以用配置文件中的连接信息进入数据库验证连通性：

```bash
mongosh 'mongodb://127.0.0.1:27017/app'
```

基础命令：

```javascript
db.runCommand({ ping: 1 })
db.getName()
show collections
db.documents.insertOne({ title: "hello", created_at: new Date() })
db.documents.find()
```

如果你的数据库没有启用账号密码，则连接串可以不带用户名和密码：

```text
mongodb://127.0.0.1:27017/app
```

## 4. 项目配置

```yaml
mongodb:
  uri: mongodb://127.0.0.1:27017
  username: ""
  password: ""
  database: app
  app_name: app-service
  min_pool_size: 0
  max_pool_size: 100
  connect_timeout_seconds: 10
  server_selection_timeout_seconds: 5
```

服务启动时只读取 `configs/config.yaml` 中的 `mongodb.*` 配置。需要账号密码时，填写 `username`、`password`；需要连接副本集或指定认证库时，把完整连接串写入 `uri`。

## 5. Go 初始化

```go
type Document struct {
    ID string `bson:"_id"`
}

func (Document) MongoCollectionName() string {
    return "documents"
}

client, err := mongox.NewClient(cfg.MongoDB.MongoxConfig())
if err != nil {
    return err
}
defer client.Disconnect(context.Background())

if err := mongox.Ping(context.Background(), client); err != nil {
    return err
}

db := mongox.Database(client, cfg.MongoDB.Database)
documents := mongox.NewDocumentStore[Document](db)
```

## 6. Repo 层建议

建议把 MongoDB 查询代码放在 `internal/<service>/repo`，不要在 handler 或 service 里直接操作集合。MongoDB 文档结构放在 `internal/<service>/model`，写 `bson` tag 和 `MongoCollectionName()`；repo 层通过 `mongox.NewDocumentStore[T]` 复用公共 CRUD 操作，并负责 entity/model 转换：

```text
handler -> service -> entity.Repository
repo -> model -> MongoDB collection
```

```go
_, err := documents.UpsertByID(ctx, "doc-1", &Document{ID: "doc-1"})
if err != nil {
    return err
}

document, err := documents.FindByID(ctx, "doc-1")
```

示例结构：

```text
internal/content/entity/content.go
internal/content/entity/repository.go
internal/content/model/content.go
internal/content/repo/mongo_repository.go
```

## 7. 索引

在 repo 初始化时创建索引：

```go
_, err := collection.Indexes().CreateOne(ctx, mongo.IndexModel{
    Keys: bson.D{
        {Key: "owner_id", Value: 1},
        {Key: "created_at", Value: -1},
    },
})
```

常见索引策略：

- 等值查询字段放前面，例如 `owner_id`。
- 排序字段放后面，例如 `created_at`。
- 唯一业务键使用 unique index。
- 大集合分页优先使用游标条件，不要深页 skip。

## 8. 测试建议

轻量单元测试可以测 repo 的参数转换和错误处理；集成测试再连接真实 MongoDB。CI 中建议使用独立测试库或测试环境 MongoDB，连接信息统一写入测试配置文件。
