# Telegraf 与 monitor-agent 部署指南

## 架构

Telegraf 只采集指标，并将 Influx Line Protocol 发送到本机 `monitor-agent`：

```text
Telegraf -> http://127.0.0.1:9510/v1/telegraf/metrics
monitor-agent -> mTLS gRPC -> 中央 Collector
```

不要让 Telegraf 直接写 InfluxDB，也不要将 InfluxDB 写入令牌放到被监控主机。

## Linux 安装

1. 安装官方 Telegraf 包，并安装本项目构建出的 `monitor-agent` 到 `/usr/local/bin/monitor-agent`。
2. 创建目录：

```bash
install -d -m 0700 /etc/monitor-agent /var/lib/monitor-agent /var/log/monitor-agent
install -d -m 0750 /var/lib/telegraf/buffer
```

3. 将 `client/configs/agent.example.yaml` 保存为 `/etc/monitor-agent/agent.yaml`，设置稳定的 `agent_id`、标签、中心地址和证书文件路径。私钥权限必须为 `0600`。
4. 从 `client/telegraf/telegraf.conf.template` 生成 `/etc/telegraf/telegraf.conf`，替换 `AGENT_ID`、`ENVIRONMENT`、`SITE`、`ROLE`。先校验：

```bash
telegraf --config /etc/telegraf/telegraf.conf --test
```

5. 先启动 Agent，再启动 Telegraf：

```bash
systemctl enable --now monitor-agent
systemctl enable --now telegraf
```

6. 验证：

```bash
systemctl status monitor-agent telegraf
curl -i http://127.0.0.1:9510/v1/telegraf/metrics
journalctl -u monitor-agent -u telegraf -n 100
```

`curl` 的 GET 返回 `404` 是预期行为；Telegraf 的 POST 成功应返回 `204`。

## Windows 安装

1. 测试时直接使用工作区中的 `client\monitor-agent.exe` 和 `Telegraf\telegraf.exe`。
2. 测试配置使用 `client\configs\agent.yaml`；模板为 `client\configs\agent.windows.yaml`。修改 `agent_id`、标签、中央 gRPC 地址和证书路径。工作区目录已准备好，证书目录可按需创建：

```powershell
New-Item -ItemType Directory -Force `
  "C:\Users\Administrator\Desktop\monitor\client\configs", `
  "C:\Users\Administrator\Desktop\monitor\client\state", `
  "C:\Users\Administrator\Desktop\monitor\client\certs", `
  "C:\Users\Administrator\Desktop\monitor\Telegraf\buffer" | Out-Null
```

3. 直接使用 `Telegraf\telegraf.conf`。为 Telegraf 设置与 `client\configs\agent.yaml` 一致的标签环境变量：

```powershell
[Environment]::SetEnvironmentVariable("MONITOR_AGENT_ID", "replace-with-stable-ulid", "Machine")
[Environment]::SetEnvironmentVariable("MONITOR_ENVIRONMENT", "production", "Machine")
[Environment]::SetEnvironmentVariable("MONITOR_SITE", "replace-site", "Machine")
[Environment]::SetEnvironmentVariable("MONITOR_ROLE", "replace-role", "Machine")
```

重新打开 PowerShell 或重启 Telegraf 进程后环境变量才会生效。配置已将 `buffer_strategy` 设为 `disk`，目录为 `C:\Users\Administrator\Desktop\monitor\Telegraf\buffer`，用于 Agent 或中央端短暂不可用时持久化待发送指标。
4. 将 CA、客户端证书和私钥放入 `C:\Users\Administrator\Desktop\monitor\client\certs\`。测试环境也建议保持 `insecure_tls: false`。
5. 启动 Agent。下面的命令启动的是 `monitor-agent`，不是 Telegraf：

```powershell
& "C:\Users\Administrator\Desktop\monitor\client\monitor-agent.exe" `
  --config "C:\Users\Administrator\Desktop\monitor\client\configs\agent.yaml"
```

看到以下日志表示 Agent 已监听本机指标接收端口：

```text
accepting Telegraf metrics on 127.0.0.1:9510
```

6. 启动 Telegraf。推荐执行 `Telegraf\run.txt`，因为该脚本会设置
Agent ID、环境、站点、角色等标签，并在当前 PowerShell 会话中注入数据库连接
环境变量，然后再启动 Telegraf：

```powershell
Set-Location "C:\Users\Administrator\Desktop\monitor"
$env:MONITOR_POSTGRES_ADDRESS = "host=localhost port=5432 user=postgres password=<password> sslmode=disable dbname=postgres connect_timeout=5"
$env:MONITOR_MYSQL_DSN = "<user>:<password>@tcp(127.0.0.1:3306)/?tls=false&timeout=5s"
$env:MONITOR_REDIS_URL = "tcp://127.0.0.1:6379"
$script = Get-Content ".\Telegraf\run.txt" -Raw
Invoke-Expression $script
```

上面四行的执行顺序不能颠倒：

```text
Set-Location → 设置连接环境变量 → Get-Content → Invoke-Expression
```

`Invoke-Expression $script` 本身不是独立的 Telegraf 启动命令；它负责执行
前面通过 `Get-Content` 读取的 `run.txt` 内容。

也可以直接启动 Telegraf：

```powershell
& "C:\Users\Administrator\Desktop\monitor\Telegraf\telegraf.exe" `
  --config "C:\Users\Administrator\Desktop\monitor\Telegraf\telegraf.conf"
```

直接命令只适用于已经在当前 PowerShell 会话设置好所有环境变量的情况，否则
MySQL、Redis、PostgreSQL 插件可能无法读取连接配置。它是 Telegraf 的正式运行
命令，不是 Agent 启动命令。

只验证 Telegraf 配置而不持续运行，使用 `--test`：

```powershell
& "C:\Users\Administrator\Desktop\monitor\Telegraf\telegraf.exe" `
  --config "C:\Users\Administrator\Desktop\monitor\Telegraf\telegraf.conf" --test
```

7. 如需注册 Windows Service，Agent 服务命令必须指向：

```text
C:\Users\Administrator\Desktop\monitor\client\monitor-agent.exe --config C:\Users\Administrator\Desktop\monitor\client\configs\agent.yaml
```

Telegraf 服务命令必须指向：

```text
C:\Users\Administrator\Desktop\monitor\Telegraf\telegraf.exe --config C:\Users\Administrator\Desktop\monitor\Telegraf\telegraf.conf
```

服务账户必须仅能读取自身证书和配置目录，并能写入各自的 `state`/`buffer` 目录。先启动 Agent，再启动 Telegraf。

## 生成本机测试证书

仓库内提供 Go 标准库证书生成工具，不依赖 OpenSSL。它会生成本地测试 CA、服务端证书和 Agent 客户端证书：

```powershell
cd "C:\Users\Administrator\Desktop\monitor\client"
go run ".\cmd\certgen" `
  -out "C:\Users\Administrator\Desktop\monitor\client\certs"
```

生成文件：

```text
client\certs\ca.pem
client\certs\ca-key.pem
client\certs\server.pem
client\certs\server-key.pem
client\certs\client.pem
client\certs\client-key.pem
```

`ca-key.pem` 是 CA 根私钥，只能由证书签发环境保管，不能复制到 Agent 或提交到代码仓库。当前 `client\configs\agent.yaml` 使用 `ca.pem`、`client.pem` 和 `client-key.pem`。服务端启动 gRPC TLS 服务时使用 `server.pem`、`server-key.pem`，并使用 `ca.pem` 校验客户端证书。

这套证书只适合本机开发测试，有效期分别为 CA 5 年、叶子证书 1 年。生产环境必须使用正式 CA、证书轮换和密钥管理服务。

## Windows 指标覆盖范围

当前 `Telegraf\telegraf.conf` 已启用：

| 指标 | Telegraf measurement | 说明 |
| --- | --- | --- |
| CPU 使用率 | `cpu`、`win_cpu` | `cpu` 来自原生插件，`win_cpu` 来自 Windows 性能计数器 |
| 内存使用率和总量 | `mem`、`win_memory` | `mem.total` 就是物理内存总量 |
| 磁盘和磁盘 I/O | `disk`、`diskio` | Windows 盘符会作为 `device`/`name` 标签 |
| 核数和线程数 | `system` | `n_cpus` 是逻辑 CPU 数，`n_physical_cpus` 是物理 CPU 数 |
| 系统调用和处理器队列 | `win_system` | 依赖 Windows 性能计数器 |

CPU 型号不是 Telegraf 基础采集插件稳定提供的指标；后续应由 Go Agent 使用 Windows WMI/API 采集静态主机元数据。主机温度同样取决于 BIOS、驱动和硬件是否暴露温度传感器，不能保证所有 Windows 主机都有数据；可在确认硬件支持后增加专用 WMI/LibreHardwareMonitor 采集，不建议默认执行任意 PowerShell 脚本。

## 启用 MySQL 和 Redis

MySQL、Redis 当前保持关闭状态，因为需要真实的只读账号。配置时将以下片段加入 `Telegraf\telegraf.conf` 的自定义配置区，并替换地址和凭据：

```toml
[[inputs.mysql]]
  servers = ["monitor_user:REPLACE_PASSWORD@tcp(127.0.0.1:3306)/?tls=false"]
  metric_version = 2
  gather_process_list = true
  gather_table_schema = false

[[inputs.redis]]
  servers = ["tcp://127.0.0.1:6379"]
  password = "REPLACE_PASSWORD"
  response_timeout = "5s"
```

不要把真实密码提交到仓库。测试阶段可以先使用本机环境变量渲染配置，生产阶段使用 Telegraf secret store 或 Windows 受限凭据存储。启用后验证：

```powershell
& "C:\Users\Administrator\Desktop\monitor\Telegraf\telegraf.exe" `
  --config "C:\Users\Administrator\Desktop\monitor\Telegraf\telegraf.conf" --test
```

若 MySQL 或 Redis 未运行、账号无权限或密码错误，Telegraf 会在测试/运行日志中报告对应插件错误；这不代表 CPU、内存等其他插件停止工作。

## Telegraf 配置原则

- 首期启用系统插件、`http_response` 和受批准的中间件插件。
- Docker 插件仅在 Docker socket 只读且已审批的主机启用。
- Redis、MySQL、RabbitMQ 等凭据使用 Telegraf secret store 或操作系统密钥存储；账户应为插件专属只读账户。
- 不允许 `exec`、任意脚本或来自前端的原始 TOML。
- `buffer_strategy = "disk"` 是持久缓冲，但目录没有自动容量上限。使用独立分区或 OS 配额，并在 70%/90% 使用率时告警。

## 故障处理

- `monitor-agent` 不可用：Telegraf 将指标保留在磁盘缓冲中；Agent 恢复后会继续投递。
- 中央端不可用：Agent 将本机已接收的指标保存到 SQLite 队列并以指数退避重连。
- 不要删除 Telegraf buffer 或 Agent SQLite 文件来“恢复”故障，这会丢失待补传指标。先检查磁盘容量、证书、中心网络与服务日志。
- 断网恢复后，Agent 会受服务端配额限制补传历史数据，防止恢复流量冲垮中央端。
