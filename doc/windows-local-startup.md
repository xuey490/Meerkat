# Windows/Linux 监控平台安装与启动命令

本文档适用于当前工作区：

```text
C:\Users\Administrator\Desktop\monitor
```

## 1. 安装 protoc 和生成工具（首次执行）

打开 Protobuf 官方发布页：
https://github.com/protocolbuffers/protobuf/releases
下载：protoc-*-win64.zip，本机已经将 `protoc` 解压到：

```text
C:\Users\Administrator\Desktop\monitor\protoc64
```

目录必须包含：

```text
protoc64\bin\protoc.exe
protoc64\include\google\protobuf\
```

将 `protoc` 加入当前用户 PATH：

```powershell
$protocBin = "C:\Users\Administrator\Desktop\monitor\protoc64\bin"
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")

if ($userPath -notlike "*$protocBin*") {
  [Environment]::SetEnvironmentVariable(
    "Path",
    "$($userPath.TrimEnd(';'));$protocBin",
    "User"
  )
}

$env:Path = "$protocBin;$(go env GOPATH)\bin;$env:Path"
```

安装 Go 代码生成插件：

```powershell
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.10
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
```

验证工具：

```powershell
protoc --version
protoc-gen-go --version
protoc-gen-go-grpc --version
```

生成正式强类型协议代码：

```powershell
Set-Location "C:\Users\Administrator\Desktop\monitor"

protoc -I "server\api\proto" `
  --go_out="server\api\proto" `
  --go_opt=paths=source_relative `
  --go-grpc_out="server\api\proto" `
  --go-grpc_opt=paths=source_relative `
  "server\api\proto\collector\v1\collector.proto"

protoc -I "server\api\proto" `
  --go_out="client" `
  --go_opt=paths=source_relative `
  --go-grpc_out="client" `
  --go-grpc_opt=paths=source_relative `
  "server\api\proto\collector\v1\collector.proto"
```

重新编译客户端和服务端：

```powershell
Set-Location "C:\Users\Administrator\Desktop\monitor\client"
go test ./...
go build -o "..\client\monitor-agent.exe" .\cmd\monitor-agent

Set-Location "..\server"
go test ./...
go build -o "collector.exe" .\cmd\collector
```

协议源文件只有这一份：

```text
server\api\proto\collector\v1\collector.proto
```

修改协议后，必须重新生成代码并同时重启 Agent 和 Collector。该步骤只在
首次安装或修改 `.proto` 后执行，日常启动不需要重复执行。

## 1.1 编译 Monitor Agent 和 Server Collector

构建前确认已安装 Go，并在仓库根目录执行依赖整理：

```powershell
go version
Set-Location "C:\Users\Administrator\Desktop\monitor\client"
go mod download
Set-Location "..\server"
go mod download
```

### 编译 Windows amd64

Windows 构建产物为 `.exe`，输出到 `dist\windows-amd64`：

```powershell
$root = "C:\Users\Administrator\Desktop\monitor"
New-Item -ItemType Directory -Force "$root\dist\windows-amd64" | Out-Null

Set-Location "$root\client"
$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags "-s -w" `
  -o "$root\dist\windows-amd64\monitor-agent.exe" `
  .\cmd\monitor-agent

Set-Location "$root\server"
go build -trimpath -ldflags "-s -w" `
  -o "$root\dist\windows-amd64\collector.exe" `
  .\cmd\collector

Remove-Item Env:GOOS,Env:GOARCH,Env:CGO_ENABLED
```

复制到当前 Windows 测试目录：

```powershell
Copy-Item "$root\dist\windows-amd64\monitor-agent.exe" `
  "$root\client\monitor-agent.exe" -Force
Copy-Item "$root\dist\windows-amd64\collector.exe" `
  "$root\server\collector.exe" -Force
```

### 编译 Linux amd64

Linux 构建产物没有扩展名，输出到 `dist\linux-amd64`：

```powershell
$root = "C:\Users\Administrator\Desktop\monitor"
New-Item -ItemType Directory -Force "$root\dist\linux-amd64" | Out-Null

Set-Location "$root\client"
$env:GOOS = "linux"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags "-s -w" `
  -o "$root\dist\linux-amd64\monitor-agent" `
  .\cmd\monitor-agent

Set-Location "$root\server"
go build -trimpath -ldflags "-s -w" `
  -o "$root\dist\linux-amd64\collector" `
  .\cmd\collector

Remove-Item Env:GOOS,Env:GOARCH,Env:CGO_ENABLED
```

### 编译 Linux arm64

如果目标 Linux 主机是 ARM64，只需将架构改为 `arm64`：

```powershell
$root = "C:\Users\Administrator\Desktop\monitor"
New-Item -ItemType Directory -Force "$root\dist\linux-arm64" | Out-Null

Set-Location "$root\client"
$env:GOOS = "linux"
$env:GOARCH = "arm64"
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags "-s -w" `
  -o "$root\dist\linux-arm64\monitor-agent" `
  .\cmd\monitor-agent

Set-Location "$root\server"
go build -trimpath -ldflags "-s -w" `
  -o "$root\dist\linux-arm64\collector" `
  .\cmd\collector

Remove-Item Env:GOOS,Env:GOARCH,Env:CGO_ENABLED
```

### 构建验证

```powershell
Get-ChildItem "$root\dist" -Recurse -File |
  Select-Object FullName,Length

& "$root\dist\windows-amd64\monitor-agent.exe" --help
& "$root\dist\windows-amd64\collector.exe" --help
```

Linux 主机上验证：

```bash
chmod +x ./monitor-agent ./collector
file ./monitor-agent ./collector
```

`modernc.org/sqlite` 是纯 Go SQLite 驱动，上述构建可以使用
`CGO_ENABLED=0`。构建完成后，先停止旧进程，再替换二进制并启动；不要在旧
Collector 或 Agent 仍运行时直接覆盖正在使用的文件。

## 2. 组件和端口

| 组件 | 端口/地址 | 启动状态检查 |
| --- | --- | --- |
| PostgreSQL | `localhost:5432` | `Test-NetConnection localhost -Port 5432` |
| Redis | `127.0.0.1:6379` | `Test-NetConnection 127.0.0.1 -Port 6379` |
| MySQL | `localhost:3306` | `Test-NetConnection localhost -Port 3306` |
| InfluxDB | `http://localhost:8086` | `Invoke-WebRequest http://localhost:8086/health` |
| Server Collector | `localhost:9500` | `Test-NetConnection localhost -Port 9500` |
| HTTPS 管理 API | `https://localhost:8080` | `curl.exe --ssl-no-revoke --cacert .\client\certs\ca.pem https://localhost:8080/healthz` |
| Monitor Agent | `127.0.0.1:9510` | `Test-NetConnection 127.0.0.1 -Port 9510` |

Agent 使用 mTLS gRPC 连接 Collector。Telegraf 只向本机 Agent 的
`http://127.0.0.1:9510/v1/telegraf/metrics` 发送指标。

本地 HTTPS 管理 API 使用 `client\certs\ca.pem` 签发的开发自签名证书。未将该
CA 导入 Windows 信任库时，浏览器或 PowerShell 会报告
`tls: unknown certificate`，这是客户端不信任 CA，不是 Agent gRPC 连接失败。
临时检查使用：

```powershell
curl.exe --ssl-no-revoke --cacert ".\client\certs\ca.pem" https://localhost:8080/healthz
```

`--ssl-no-revoke` 仅用于本机开发证书，因为该测试 CA 没有可访问的吊销检查
服务；生产环境不应关闭证书吊销检查。

需要浏览器正常信任时，以管理员身份将 `client\certs\ca.pem` 导入“受信任的
根证书颁发机构”；生产环境必须替换为正式 CA，不要关闭证书校验。

Agent 还提供本机健康与系统摘要接口：

```text
http://127.0.0.1:9511/healthz
```

该接口包含 Windows 版本、UTC 系统时间、总/已用内存、进程数、监听端口数及
进程/端口明细。Agent 启动日志也会用彩色文本打印同一份系统摘要。

## 3. 启动前检查

在 PowerShell 中执行：

```powershell
$root = "C:\Users\Administrator\Desktop\monitor"
Set-Location $root

$ports = 5432,6379,3306,8086,9500,9510
foreach ($port in $ports) {
  $result = Test-NetConnection localhost -Port $port -WarningAction SilentlyContinue
  "{0}: {1}" -f $port, $result.TcpTestSucceeded
}
```

当前数据库账号：

```text
PostgreSQL: postgres / 123456
MySQL:      root / root
Redis:      无密码
```

InfluxDB 使用 API Token，不在本文档中保存 Token。启动 Collector 前，在当前
PowerShell 会话设置：

```powershell
$env:MONITOR_INFLUX_TOKEN = "<hkc-bucket-write-token>"
```

## 4. 启动 Server Collector

打开第一个 PowerShell 窗口：

```powershell
Set-Location "C:\Users\Administrator\Desktop\monitor\server"
$env:MONITOR_INFLUX_TOKEN = "<hkc-bucket-write-token>"
.\collector.exe -config .\configs\server.local.yaml
```

如果尚未编译，也可以使用 `go run`：

```powershell
go run .\cmd\collector -config .\configs\server.local.yaml
```

看到以下日志表示 Collector 已启动：

```text
gRPC Collector listening address=:9500
```

保持此窗口运行。生产环境应将该命令注册为 Windows Service 或由进程管理器
托管。

## 5. 启动 Monitor Agent

打开第二个 PowerShell 窗口：

```powershell
Set-Location "C:\Users\Administrator\Desktop\monitor"
& ".\client\monitor-agent.exe" `
  --config ".\client\configs\agent.yaml"
```

隐藏窗口
```powershell 
Set-Location "C:\Users\Administrator\Desktop\monitor"
& ".\client\monitor-agent.exe" `
  -u `
  --config ".\client\configs\agent.yaml"

```

Agent 配置文件为：

```text
client\configs\agent.yaml
```

当前 Agent 使用：

```text
agent_id: local-windows-002
server:   localhost:9500
listen:   127.0.0.1:9510
heartbeat: 5s
```

结束命令
```
 Stop-Process -Name monitor-agent
 Get-Process monitor-agent -ErrorAction SilentlyContinue | Stop-Process -Force
```

## 6. 启动 Telegraf

打开第三个 PowerShell 窗口。数据库连接串通过当前会话注入，不写入
`telegraf.conf` 或启动脚本：

```powershell
Set-Location "C:\Users\Administrator\Desktop\monitor"
$env:MONITOR_POSTGRES_ADDRESS = "host=localhost port=5432 user=postgres password=<password> sslmode=disable dbname=postgres connect_timeout=5"
$env:MONITOR_MYSQL_DSN = "<user>:<password>@tcp(127.0.0.1:3306)/?tls=false&timeout=5s"
$env:MONITOR_REDIS_URL = "tcp://127.0.0.1:6379"
$script = Get-Content ".\Telegraf\run.txt" -Raw
Invoke-Expression $script
```
```
& "C:\Users\Administrator\Desktop\monitor\Telegraf\telegraf.exe" `
  --config "C:\Users\Administrator\Desktop\monitor\Telegraf\telegraf.conf" `
  --non-strict-env-handling

Invoke-Expression (Get-Content -Raw "C:\Users\Administrator\Desktop\monitor\Telegraf\run.txt")

```
Telegraf 当前采集频率：

```text
CPU、内存、网络、系统：10s
MySQL、Redis、PostgreSQL：30s
磁盘、磁盘 I/O、进程：30s
输出刷新：10s，带 2s 抖动
```

启动日志应包含：

```text
Loaded inputs: ... mysql postgresql redis ...
Loaded outputs: http
```

## 7. 一键检查所有进程

```powershell
Get-Process postgres,redis-server,mysqld,influxd,collector,monitor-agent,telegraf `
  -ErrorAction SilentlyContinue |
  Select-Object Id,ProcessName,StartTime,Path
```

检查端口：

```powershell
$ports = 5432,6379,3306,8086,9500,9510
foreach ($port in $ports) {
  Get-NetTCPConnection -State Listen -LocalPort $port `
    -ErrorAction SilentlyContinue |
    Select-Object LocalAddress,LocalPort,OwningProcess
}
```

检查 Agent 是否收到 Telegraf 指标：

```powershell
Invoke-WebRequest `
  -UseBasicParsing `
  -Uri "http://127.0.0.1:9510/v1/telegraf/metrics" `
  -Method POST `
  -ContentType "text/plain" `
  -Body "startup_check,source=manual value=1i"
```

预期返回：

```text
204 No Content
```

## 8. 停止和重启

停止：

```powershell
Get-Process telegraf,monitor-agent,collector `
  -ErrorAction SilentlyContinue |
  Stop-Process
```

如果 Collector 是通过 `go run` 启动的，Windows 会同时保留 `go.exe` 父进程
和临时目录中的 `collector.exe` 子进程。此时仅按 `Ctrl+C` 可能只结束当前
PowerShell 前台任务，9500 仍会被子进程占用。使用进程树停止：

```powershell
$children = Get-CimInstance Win32_Process `
  -Filter "Name='collector.exe' OR Name='go.exe'" |
  Where-Object { $_.CommandLine -match 'cmd[\\/]collector|collector.exe' }

$children |
  Sort-Object ProcessId -Descending |
  ForEach-Object {
    Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
  }
```

确认 9500 已释放：

```powershell
netstat -ano | Select-String ":9500"
```

建议使用已编译的 `server\collector.exe` 启动，避免 `go run` 父子进程残留。

重启顺序必须是：

```text
PostgreSQL / Redis / MySQL / InfluxDB
→ Server Collector
→ Monitor Agent
→ Telegraf
```

不要在运行中的 Telegraf 上执行 `telegraf --test`，因为当前配置使用磁盘缓冲；
并行测试会与正式实例竞争同一个 WAL 缓冲目录。

## 9. 启动检查说明

启动完成后，PostgreSQL、Redis、MySQL、InfluxDB、Server Collector、Monitor
Agent 和 Telegraf 都应处于运行状态。若 Collector 未启动，Agent 会自动重连，
Telegraf 指标会保留在本地磁盘缓冲和 Agent SQLite 队列中；请按第 4 节启动
Collector 后再检查 `9500` 端口。

## 10. InfluxDB 启动

InfluxDB 必须先于 Server Collector 启动。组织和 bucket 使用：

```text
组织：hkc
Bucket：bucket
地址：http://localhost:8086
```

InfluxDB 写入使用 API Token。不要把 Token 写入仓库、`server.local.yaml`
或启动脚本；在启动 Collector 的 PowerShell/终端会话中临时设置。

### Windows

如果 InfluxDB 已作为进程运行，先检查：

```powershell
Get-NetTCPConnection -State Listen -LocalPort 8086 `
  -ErrorAction SilentlyContinue
```

本地二进制路径：

```text
C:\Users\Administrator\Desktop\monitor\influxdb2\influxdb2_windows_amd64\influxd.exe
```

手动启动示例：

```powershell
$influxDir = "C:\Users\Administrator\Desktop\monitor\influxdb2\influxdb2_windows_amd64"
New-Item -ItemType Directory -Force "$influxDir\data","$influxDir\engine" | Out-Null

& "$influxDir\influxd.exe" run `
  --bolt-path "$influxDir\influxd.bolt" `
  --engine-path "$influxDir\engine" `
  --http-bind-address ":8086"
```

### Linux

使用发行版服务安装时：

```bash
sudo systemctl enable --now influxdb
curl http://127.0.0.1:8086/health
```

手动二进制启动时：

```bash
sudo install -d -o influxdb -g influxdb /var/lib/influxdb3
sudo -u influxdb influxd run \
  --bolt-path /var/lib/influxdb/influxd.bolt \
  --engine-path /var/lib/influxdb/engine \
  --http-bind-address 0.0.0.0:8086
```

## 11. Linux 平台启动

Linux 与 Windows 的启动顺序相同：

```text
InfluxDB / PostgreSQL / Redis / MySQL
→ Server Collector
→ monitor-agent
→ Telegraf
```

### 编译 Linux 二进制

在开发机执行：

```powershell
Set-Location "C:\Users\Administrator\Desktop\monitor\client"
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -o "..\dist\monitor-agent" .\cmd\monitor-agent

Set-Location "..\server"
go build -o "..\dist\collector" .\cmd\collector
Remove-Item Env:GOOS,Env:GOARCH
```

将构件、配置和证书复制到 Linux 主机：

```bash
sudo install -Dm755 monitor-agent /usr/local/bin/monitor-agent
sudo install -Dm755 collector /usr/local/bin/monitor-collector
sudo install -Dm600 agent.yaml /etc/monitor-agent/agent.yaml
sudo install -Dm600 ca.pem client.pem client-key.pem /etc/monitor-agent/certs/
sudo install -d -m700 /var/lib/monitor-agent /var/log/monitor-agent
```

### Linux monitor-agent

已有 systemd 模板：

```text
client/packaging/monitor-agent.service
```

安装并启动：

```bash
sudo install -Dm644 monitor-agent.service /etc/systemd/system/monitor-agent.service
sudo systemctl daemon-reload
sudo systemctl enable --now monitor-agent
sudo systemctl status monitor-agent
```

### Linux Telegraf

将 `client/telegraf/telegraf.conf.template` 渲染为目标机配置，确认输出地址
仍为本机 Agent：

```toml
url = "http://127.0.0.1:9510/v1/telegraf/metrics"
```

启动：

```bash
sudo install -Dm644 telegraf.conf /etc/telegraf/telegraf.conf
sudo install -d -m750 /var/lib/telegraf/buffer
sudo systemctl enable --now telegraf
sudo systemctl status telegraf
```

数据库凭据通过 Linux Secret Store、systemd 环境文件或受限权限的运行环境注入，
不要直接写入 Telegraf TOML。

### Linux Server Collector

```bash
sudo install -Dm755 collector /usr/local/bin/monitor-collector
sudo install -Dm600 server.local.yaml /etc/monitor-server/server.yaml
sudo install -Dm600 ca.pem server.pem server-key.pem /etc/monitor-server/certs/
export MONITOR_INFLUX_TOKEN='<hkc-bucket-write-token>'
sudo -E /usr/local/bin/monitor-collector \
  -config /etc/monitor-server/server.yaml
```

生产环境应将 Collector 配置为 systemd 服务，并将
`MONITOR_INFLUX_TOKEN` 放入 root-only 的 EnvironmentFile 或系统密钥服务。

## 12. 平台差异注意事项

| 项目 | Windows | Linux |
| --- | --- | --- |
| Agent 服务管理 | PowerShell/Windows Service | systemd |
| Telegraf 配置路径 | `Telegraf\telegraf.conf` | `/etc/telegraf/telegraf.conf` |
| Agent 数据目录 | `client\state` | `/var/lib/monitor-agent` |
| Telegraf 缓冲目录 | `Telegraf\buffer` | `/var/lib/telegraf/buffer` |
| 证书路径 | `client\certs` | `/etc/monitor-agent/certs` |
| Agent 本地接收 | `127.0.0.1:9510` | `127.0.0.1:9510` |
| Collector gRPC | `:9500` | `:9500` |
