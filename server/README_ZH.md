# Monitor Server 中文说明

这是监控平台服务端的本地开发版本，当前包含：

- mTLS gRPC Collector，默认监听 `9500`
- HTTPS 管理 API，默认监听 `8080`
- PostgreSQL Agent、心跳、指标收据和状态模型
- PostgreSQL Outbox 持久事件队列
- 独立 Metric Writer，负责写入 InfluxDB
- Agent 在线状态和基础离线告警评估器
- Agent 注册、心跳回退、配置、资产、告警规则、维护窗口和审计 API
- offline Agent 默认保留；离线告警每 1 分钟写入一次
- 强类型 Protobuf 协议，源文件为 `api/proto/collector/v1/collector.proto`

## 本地配置

复制配置模板：

```powershell
Copy-Item .\configs\server.local.example.yaml .\configs\server.local.yaml
```

当前本机 PostgreSQL 配置：

```text
地址：localhost
端口：5432
用户：postgres
数据库：postgres
```

InfluxDB 配置：

```text
地址：http://localhost:8086
组织：hkc
Bucket：bucket
```

InfluxDB 写入必须使用 API Token。不要将 Token 写入仓库文件，启动时注入：

```powershell
$env:MONITOR_INFLUX_TOKEN = "<hkc-bucket-write-token>"
```

## Windows 启动

先确认 PostgreSQL、Redis、MySQL 和 InfluxDB 已启动，然后在服务端目录执行：

```powershell
Set-Location "C:\Users\Administrator\Desktop\monitor\server"
$env:MONITOR_INFLUX_TOKEN = "<hkc-bucket-write-token>"
.\collector.exe -config .\configs\server.local.yaml
```

也可以使用源码运行：

```powershell
go run .\cmd\collector -config .\configs\server.local.yaml
```

启动日志应包含：

```text
gRPC Collector listening address=:9500
HTTPS management API listening address=:8080
```

Collector 启动时还会用彩色文本输出服务端自身的操作系统版本、UTC 时间、
内存、进程数和监听端口数。收到 Agent 消息后会持续输出分类日志：

```text
绿色 REGISTER  : Agent 注册、主机名、系统架构和版本
青色 HEARTBEAT : 客户端系统版本、时间、内存、进程数、端口数和 Telegraf 状态
黄色 METRICS   : 指标批次序号、采集时间和载荷大小
紫色 EVENT     : Agent 事件
```

PowerShell/Windows Terminal 支持 ANSI 颜色时会显示彩色字符；日志本身仍保持
普通文本内容，不影响重定向到文件。

## Linux 启动

交叉编译：

```powershell
Set-Location "C:\Users\Administrator\Desktop\monitor\server"
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -o "..\dist\collector" .\cmd\collector
Remove-Item Env:GOOS,Env:GOARCH
```

复制到 Linux：

```bash
sudo install -Dm755 collector /usr/local/bin/monitor-collector
sudo install -Dm600 server.yaml /etc/monitor-server/server.yaml
sudo install -Dm600 ca.pem server.pem server-key.pem /etc/monitor-server/certs/
```

启动：

```bash
export MONITOR_INFLUX_TOKEN='<hkc-bucket-write-token>'
sudo -E /usr/local/bin/monitor-collector \
  -config /etc/monitor-server/server.yaml
```

生产环境应使用 systemd 和系统密钥服务管理 Token，不建议手工保持终端运行。

## 管理 API

```text
GET  https://localhost:8080/healthz
GET  https://localhost:8080/api/v1/agents
POST https://localhost:8080/api/v1/agents/enroll
POST https://localhost:8080/api/v1/agents/heartbeat
GET  https://localhost:8080/api/v1/agents/{id}
DELETE https://localhost:8080/api/v1/agents/{id}
GET  https://localhost:8080/api/v1/agents/{id}/config
GET  https://localhost:8080/api/v1/agents/{id}/services
GET  https://localhost:8080/api/v1/agents/{id}/containers
GET  https://localhost:8080/api/v1/alerts
GET  https://localhost:8080/api/v1/rules
POST https://localhost:8080/api/v1/rules
GET  https://localhost:8080/api/v1/maintenance
POST https://localhost:8080/api/v1/maintenance
POST https://localhost:8080/api/v1/audit
```

当前 HTTPS API 是开发版本，尚未接入 OIDC/RBAC；禁止直接暴露到公网。

`DELETE /api/v1/agents/{id}` 会删除 Agent 资产及其关联的当前告警、服务探针和
Docker 状态。删除后，该 Agent 不会再被离线告警评估器扫描。

## Protobuf 生成

本机 `protoc` 路径：

```text
C:\Users\Administrator\Desktop\monitor\protoc64\bin\protoc.exe
```

安装 Go 插件：

```powershell
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.10
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
```

生成服务端代码：

```powershell
protoc -I "api\proto" `
  --go_out="api\proto" --go_opt=paths=source_relative `
  --go-grpc_out="api\proto" --go-grpc_opt=paths=source_relative `
  "api\proto\collector\v1\collector.proto"
```

生成客户端代码：

```powershell
protoc -I "..\server\api\proto" `
  --go_out="..\client" --go_opt=paths=source_relative `
  --go-grpc_out="..\client" --go-grpc_opt=paths=source_relative `
  "..\server\api\proto\collector\v1\collector.proto"
```

修改协议后，必须同时重新生成客户端和服务端代码，并重启两个进程。

## 测试

```powershell
go test ./...
```

客户端和服务端都应通过测试后，再启动 Collector、Agent 和 Telegraf。
