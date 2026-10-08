# 工具组件总览与调用流程

当前项目由 bw-cli 生成，已移除脚手架命令源码。这里列出项目内可直接调用的公共工具包。

当前 module：

```text
github.com/company/monitor-webserver
```

## 1. 工具列表

| 包 | 能力 |
| --- | --- |
| `pkg/config` | YAML 配置加载和默认值 |
| `pkg/logger` | Zap 结构化日志和文件轮转 |
| `pkg/errors` | 统一业务错误码，HTTP/gRPC 状态映射 |
| `pkg/httpx` | Gin HTTP 统一响应 |
| `pkg/middleware` | CORS、JWT、RequestID、请求日志 |
| `pkg/grpcx` | gRPC request_id 透传和日志拦截器 |
| `pkg/database` | SQLite/MySQL/PostgreSQL Gorm 统一入口 |
| `pkg/mysqlx` | MySQL Gorm 初始化 |
| `pkg/postgresx` | PostgreSQL Gorm 初始化 |
| `pkg/mongox` | MongoDB 官方 driver 初始化和公共 DocumentStore CRUD 操作 |
| `pkg/redisx` | Redis client 初始化 |
| `pkg/esx` | Elasticsearch v7 client、模糊搜索、高亮和聚合封装 |
| `pkg/kafkax` | Kafka producer/consumer 和原生 reader/writer 初始化 |
| `pkg/filex` | MinIO/OSS/Qiniu/COS 文件上传封装 |
| `pkg/alipayx` | 支付宝支付、回调验签和退款封装 |
| `pkg/timex` | 周岁计算、中文相对时间和日期时间格式化 |
| `pkg/validator` | 轻量参数校验 |

## 2. 推荐初始化顺序

```text
config.InitGlobal
  -> logger.New
  -> database.Open / mongox.NewClient / redisx.NewClient
  -> esx.NewClient / kafkax.NewProducer / alipayx.NewClient
  -> filex.NewUploader
  -> repo/service/handler
  -> Gin 或 gRPC server
```

## 3. 配置加载

```go
if err := config.InitGlobal("configs/config.yaml"); err != nil {
    panic(err)
}
cfg := config.MustGlobal()
```

## 4. 日志

```go
logCfg := logger.WithDailyFileName(cfg.Log, time.Now())
log, err := logger.New(logCfg)
if err != nil {
    panic(err)
}
defer log.Sync()
```

默认日志保留 7 天，文件名按服务名和日期生成。

## 5. 时间处理

```go
age := timex.Age(user.Birthday)
display := timex.RelativeTime(note.CreatedAt)
```

`RelativeTime` 会按时间差返回 `N秒前`、`N分钟前`、`N小时前`、`N天前`、`N月前`，超过一年返回 `2006-01-02 15:04:05` 格式的具体日期时间。需要固定当前时间时使用 `AgeAt` 或 `RelativeTimeAt`。

## 6. HTTP 中间件和 JWT

```go
r.Use(middleware.RequestID())
r.Use(middleware.RequestLogger(log))
r.Use(middleware.CORS(cfg.Middleware.CORS))

auth := r.Group("/api/v1")
auth.Use(middleware.JWTAuth(cfg.Middleware.JWT))
```

签发 token：

```go
token, err := middleware.GenerateToken(cfg.Middleware.JWT, middleware.JWTClaims{
    UserID: userID,
    Role:   "user",
})
if err != nil {
    return err
}
```

`middleware.jwt.secret` 必须在 `configs/config.yaml` 中设置。

## 6. Gorm 数据库

```go
db, err := database.Open(cfg.Database, cfg.MySQL, cfg.PostgreSQL, log)
if err != nil {
    log.Fatal("open database failed", zap.Error(err))
}
```

支持：

```text
sqlite
mysql
postgres
postgresql
pg
```

## 7. MongoDB

```go
type Document struct {
    ID string `bson:"_id"`
}

func (Document) MongoCollectionName() string {
    return "documents"
}

client, err := mongox.NewClient(cfg.MongoDB.MongoxConfig())
if err != nil {
    panic(err)
}
defer client.Disconnect(context.Background())

db := mongox.Database(client, cfg.MongoDB.Database)
documents := mongox.NewDocumentStore[Document](db)
_, err = documents.UpsertByID(context.Background(), "doc-1", &Document{ID: "doc-1"})
if err != nil {
    panic(err)
}
```

详细教程见 `docs/mongodb.md`。

## 8. Elasticsearch

本地或无认证集群只需要配置 `elasticsearch.addresses`。其它字段保留为云服务或认证集群使用。

```go
client, err := esx.NewClient(cfg.Elasticsearch)
if err != nil {
    return err
}
searcher := esx.NewSearcherFromClient(client)

result, err := searcher.FuzzySearch(ctx, esx.FuzzySearchRequest{
    Index:   "documents",
    Keyword: "golang",
    Fields:  []string{"title^2", "content"},
    Highlight: esx.HighlightConfig{
        Fields:   []string{"title", "content"},
        PreTags:  []string{"<mark>"},
        PostTags: []string{"</mark>"},
    },
})
if err != nil {
    return err
}
_ = result
```

聚合查询：

```go
result, err := searcher.Aggregate(ctx, esx.AggregationRequest{
    Index: "documents",
    Aggregations: map[string]esx.Aggregation{
        "by_author": esx.TermsAggregation("author_id", 10),
        "by_day":    esx.DateHistogramAggregation("created_at", "day"),
    },
})
if err != nil {
    return err
}
fmt.Println(string(result.Aggregations))
```

MySQL 同步 ES 的增量扫描、bulk 写入和删除同步示例见 `docs/elasticsearch.md`。

## 9. Kafka

进程入口初始化一次 producer，然后注入业务 service：

```go
producer, err := kafkax.NewProducer(cfg.Kafka)
if err != nil {
    return err
}
defer producer.Close()

noteSvc := noteservice.NewService(repo, producer, log)
```

业务 service 直接发布事件。`kafkax.NewProducer(cfg.Kafka)` 创建的 writer 已经绑定 `cfg.Kafka.Topic`，不要再给 `kafkax.Message.Topic` 赋值。

```go
func (s *Service) PublishNoteCreated(ctx context.Context, noteID string, authorID string) error {
    payload, err := json.Marshal(map[string]string{
        "event":     "note.created",
        "note_id":   noteID,
        "author_id": authorID,
    })
    if err != nil {
        return err
    }

    return s.kafka.Publish(ctx, kafkax.Message{
        Key:   noteID,
        Value: payload,
        Headers: map[string]string{
            "event": "note.created",
        },
    })
}
```

如果出现 `[3] Unknown Topic Or Partition`，说明 broker 上没有配置里的 topic。生产环境建议提前创建 topic：

```bash
kafka-topics.sh --bootstrap-server 127.0.0.1:9092 \
  --create \
  --topic xiaolanshu-events \
  --partitions 3 \
  --replication-factor 1
```

开发环境也可以临时打开：

```yaml
kafka:
  producer:
    allow_auto_topic_creation: true
```

消费者：

```go
consumer, err := kafkax.NewConsumer(cfg.Kafka)
if err != nil {
    return err
}
defer consumer.Close()

return consumer.Run(ctx, func(ctx context.Context, msg kafka.Message) error {
    return handleEvent(ctx, msg.Key, msg.Value)
})
```

如果业务需要直接使用 `segmentio/kafka-go` 的完整能力，也可以使用 `kafkax.NewWriter` 和 `kafkax.NewReader`。

## 10. 支付宝支付

```go
payClient, err := alipayx.NewClient(cfg.Alipay)
if err != nil {
    return err
}

payURL, err := payClient.PagePay(alipayx.PayRequest{
    OutTradeNo:  orderID,
    Subject:     "订单支付",
    TotalAmount: "9.90",
})
if err != nil {
    return err
}
_ = payURL.String()
```

同步回调使用 `VerifyReturn` 验签，异步通知使用 `DecodeNotification` 解析并验签，退款使用 `Refund`。Gin handler 和 service 接入示例见 `docs/alipay.md`。

## 11. 文件上传

```go
uploader, err := filex.NewUploader(cfg.FileStorage)
if err != nil {
    panic(err)
}

result, err := uploader.Upload(ctx, filex.UploadRequest{
    Reader:      file,
    Filename:    header.Filename,
    ContentType: header.Header.Get("Content-Type"),
    Size:        header.Size,
})
```

支持 provider：

```text
minio
oss
qiniu
cos
```
