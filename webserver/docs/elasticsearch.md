# Elasticsearch 调用示例

本文档说明如何使用 `pkg/esx` 创建 Elasticsearch v7 client，并调用模糊搜索、高亮和聚合查询。本地或无认证集群只需要配置 `addresses`，其它认证参数保留为可选项。

## 配置

```yaml
elasticsearch:
  addresses:
    - http://127.0.0.1:9200
  username: ""
  password: ""
  cloud_id: ""
  api_key: ""
```

最小配置只需要：

```yaml
elasticsearch:
  addresses:
    - http://127.0.0.1:9200
```

## 初始化

```go
if err := config.InitGlobal("configs/config.yaml"); err != nil {
    panic(err)
}
cfg := config.MustGlobal()

client, err := esx.NewClient(cfg.Elasticsearch)
if err != nil {
    panic(err)
}
searcher := esx.NewSearcherFromClient(client)
```

## 模糊搜索和高亮

```go
result, err := searcher.FuzzySearch(ctx, esx.FuzzySearchRequest{
    Index:   "documents",
    Keyword: "golang",
    Fields:  []string{"title^2", "content"},
    Filters: []esx.Filter{
        esx.TermFilter("status", "published"),
    },
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

## 聚合查询

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

## MySQL 同步到 ES

常见同步方式有两种：

1. 事件同步：MySQL 写入成功后发布 Kafka 事件，由消费者写 ES。
2. 增量轮询：定时按 `updated_at` 和 `id` 游标扫描 MySQL，批量写 ES。

Bulk 写入示例：

```go
func SyncDocumentsToES(ctx context.Context, client *elasticsearch.Client, docs []Document) error {
    var buf bytes.Buffer
    encoder := json.NewEncoder(&buf)

    for _, doc := range docs {
        if err := encoder.Encode(map[string]any{
            "index": map[string]any{"_index": "documents", "_id": doc.ID},
        }); err != nil {
            return err
        }
        if err := encoder.Encode(doc); err != nil {
            return err
        }
    }

    res, err := client.Bulk(bytes.NewReader(buf.Bytes()), client.Bulk.WithContext(ctx))
    if err != nil {
        return err
    }
    defer res.Body.Close()
    if res.IsError() {
        data, _ := io.ReadAll(res.Body)
        return fmt.Errorf("bulk index documents failed: %s", data)
    }
    return nil
}
```
