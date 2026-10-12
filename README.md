# Meerkat服务器集群实时监控平台


<div align="center">

  <img  src="https://www.phpframe.org/logo.png" width="600" alt="Logo">

  “meerkat”在中文中通常翻译为猫鼬或狐獴，属于獴科的一种小型食肉哺乳动物，原产于非洲南部,它们喜欢群居生活，跟小伙伴在一起的时候，总有一个专职“站岗放哨”的哨兵，发现危险后，通过不同频率组合的叫声传递超过20种信息，精准指示天敌类型(如鹰隼、狐狸)和方位，让其它小伙伴及时警觉危险。Meerkat服务器集群实时监控平台的寓意也是像狐獴一样，及早发现服务器的状态异常，及时预警。
</div>


背景痛点：如果你是公司的运维人员，要管理几十上百台裸金属服务器，虚拟机，Docker容器，K8s节点，你必须时刻紧盯上面的服务状态，负载性能等各项指标，时刻24小时stand By。 meerkat-admin就是为解决这个痛点而来的，现在有个狐獴帮你主动报警，及时介入。
Agent端一旦部署在客户机上，主动采集指标，向中心机汇聚、存储与展示，几分钟后就可以在web dashboard看到各项指标。
**指标不经 Telegraf 直写 InfluxDB**，经 **monitor-agent** 用 **mTLS gRPC** 上报到 **Collector**。

已完成的功能：
- [x]内置ssh控制台，可以用户名连接，密钥连接;
- [x]sftp模块，管理服务器文件上传，下载，删除，重命名;
- [x]高危命令过滤，防止出现服务器崩盘;
- [x]SSH命令会话审计，录像回放;
- [x]计划任务,计划任务日志;
- [x]告警参数设置,告警规则设置,告警日志查看;
- [x]告警日志留存;
- [x]完成RDP,VNC,SSH远程连接，操作;
- [x]还内置了一套完整的后台管理系统，基于Gin Gorm, Sqlite，（包括完整的用户、部门、角色、岗位、菜单、字典、系统设置、缓存、系统日志、附件等）可以单独独立出来;

下一步计划：
- [ ]告警通知，短信、飞书、邮件等
- [ ]接入大模型对告警进行分析处理，形成报告
- [ ]命令编排等


本仓库是**源码 + 本机开发运行包**的合集（多个 Go module），**未使用 Docker**。第三方组件用系统安装包或工作区已解压的二进制；Windows / Linux 均提供一键脚本（见 [§8](#8-一键脚本总览)）。

### 演示网址：https://monitor.phpframe.org/   商业部署联系作者微信：xdbyvibm6 （注明来意）

---

## 目录

1. [项目概况](#1-项目概况)
2. [架构与数据流](#2-架构与数据流)
3. [技术栈（按端）](#3-技术栈按端)
4. [需要安装的软件（Agent 端 vs 中心端）](#4-需要安装的软件agent-端-vs-中心端)
5. [仓库目录](#5-仓库目录)
6. [编译（Windows / Linux，amd64 / arm64）](#6-编译windows--linuxamd64--arm64)
7. [部署与必改配置](#7-部署与必改配置)
8. [一键脚本总览](#8-一键脚本总览)
9. [Windows 操作手册](#9-windows-操作手册)
10. [Linux 操作手册（Ubuntu / Debian 系）](#10-linux-操作手册ubuntu--debian-系)
11. [健康检查：端口、进程、HTTP](#11-健康检查端口进程http)
12. [远程桌面（RDP / VNC）部署](#12-远程桌面rdp--vnc部署)
13. [证书（mTLS）](#13-证书mtls)
14. [配置文件索引](#14-配置文件索引)
15. [安全与更多文档](#15-安全与更多文档)

---

## 1. 项目概况

| 概念 | 说明 |
| --- | --- |
| **用途** | 多机 CPU / 内存 / 磁盘 / 网络 / 进程等指标采集；中心控制台查看列表、详情、趋势与告警状态 |
| **规划网段** | 被监控机可以内网机房、混合云、信创环境 `可以部署内网、可跨网段、跨云、例如192.168.1~99`，中心机 `192.168.1.100`（本机开发可全部 `localhost`） |
| **被监控机** | **Telegraf** 采集 → **monitor-agent** 接收、缓存、上报 |
| **中心机** | **Collector** 接入 Agent；**PostgreSQL** 存资产/心跳/告警；**InfluxDB** 存时序；**webserver + Vue** 提供登录与 API |

| 角色 | 源码目录 | 典型产物 / 运行目录 |
| --- | --- | --- |
| 采集器 | `Telegraf/` | `Telegraf/telegraf.exe` 或系统 `telegraf` |
| Agent | `client/` | `client/monitor-agent.exe` 或 `dist/linux-*/monitor-agent` |
| Collector | `server/` | `server/collector.exe` 或 `dist/linux-*/collector` |
| Web API | `webserver/` | `webserver/webserver.exe` 或 `dist/linux-*/webserver` |
| 控制台 | `web/` | 开发 `pnpm dev`（:3000）；生产 `web/dist` + Nginx |

协议源文件唯一：`server/api/proto/collector/v1/collector.proto`。修改后需 `protoc` 重新生成并**同时**重启 Agent 与 Collector。

---

## 2. 架构与数据流

### 2.1 部署视图

```text
                         中心机
┌────────────────────────────────────────────────────────────┐
│  浏览器 :3000 ──► Vue（/dev-api 代理到 webserver :8090）    │
│  webserver :8090 ──读──► PostgreSQL :5432、InfluxDB :8086   │
│  Collector :9500 (mTLS gRPC) ──写──► PG + InfluxDB         │
│  Collector 管理 HTTPS :8080（自签 CA）                        │
└────────────────────────────┬───────────────────────────────┘
                             │ gRPC + mTLS
        ┌────────────────────┼────────────────────┐
        ▼                    ▼                    ▼
   被监控机 A            被监控机 B            被监控机 N
   Telegraf → :9510      同左                  同左
   monitor-agent         monitor-agent         monitor-agent
   health :9511           health :9511          health :9511
```

### 2.2 关键访问关系

| 从 | 到 | 地址 / 说明 |
| --- | --- | --- |
| Telegraf | 本机 Agent | `http://127.0.0.1:9510/v1/telegraf/metrics`（仅回环，成功 `204`） |
| Agent | Collector | `server_address`，如 `192.168.1.100:9500`，mTLS |
| Collector / webserver | PostgreSQL | 默认 `localhost:5432` |
| Collector / webserver | InfluxDB | 默认 `http://localhost:8086`，org `hkc`，bucket `bucket` |
| Vue 开发 | webserver | `web/.env.development` → `VITE_APP_API_URL=http://localhost:8090` |

跨机部署时：Agent 的 `server_address` 改为中心 IP/域名；**服务端证书 SAN 须包含该地址**。Telegraf 输出 URL **始终本机 9510**，不要写成中心地址。

### 2.3 时序（简图）

```mermaid
sequenceDiagram
  participant T as Telegraf
  participant A as monitor-agent
  participant C as Collector
  participant P as PostgreSQL
  participant I as InfluxDB
  participant W as webserver

  T->>A: POST 指标 :9510
  A->>C: gRPC Metrics (mTLS)
  C->>P: 资产/心跳/Outbox
  C->>I: 时序写入
  W->>P: 读列表/详情
  W->>I: Flux 查趋势
```

---

## 3. 技术栈（按端）

### 3.1 自研程序（Go / 前端）

| 组件 | 技术 | 说明 |
| --- | --- | --- |
| monitor-agent | Go **1.27+**，`CGO_ENABLED=0` | gRPC 客户端、SQLite 积压、HTTP 收 Telegraf |
| collector | Go 1.27+ | gRPC 服务端、GORM、Influx 写入、Outbox |
| webserver | Go 1.27+，Gin、JWT | 登录/菜单/监控 API；会话 SQLite |
| 控制台 | Vue **3**、Vite、TypeScript、Element Plus、ECharts、**pnpm** | 仅中心机需要（开发或构建静态资源） |
| RPC | gRPC + Protobuf | `protoc` 31+ 推荐；插件 `protoc-gen-go` / `protoc-gen-go-grpc` |
| 传输安全 | mTLS | 测试证书：`client/cmd/certgen` 或 `scripts/cert/Generate-mTLSCerts.ps1` |

### 3.2 第三方（按机器角色）

见下一节「安装清单」。Agent 端**不需要** PostgreSQL / InfluxDB / Collector / webserver。

---

## 4. 需要安装的软件（Agent 端 vs 中心端）

### 4.1 被监控机（Agent 主机）

| 软件 | 版本（本仓库验证） | 是否必须 | 官方站点 |
| --- | --- | --- | --- |
| **monitor-agent** | 自编译 | 必须 | 本仓库 `client/` |
| **Telegraf** | **1.40+**（工作区自带 1.40.1） | 必须 | https://www.influxdata.com/time-series-platform/telegraf |
| **Go** | **1.27+** | 仅编译机需要 | https://go.dev/dl/ |
| MySQL / Redis / PostgreSQL 等 | 视 `telegraf.conf` 插件 | 可选采集目标 | 各上游官网 |

**不需要**：InfluxDB、PostgreSQL、Collector、webserver、Node.js。

### 4.2 中心机（Server 主机）

| 软件 | 版本（本仓库验证） | 是否必须 | 官方站点 |
| --- | --- | --- | --- |
| **PostgreSQL** | **16+** 推荐 | 必须 | https://www.postgresql.org/download/ |
| **InfluxDB** | **2.x** | 必须 | https://docs.influxdata.com/influxdb/v2/ |
| **collector** / **webserver** | 自编译 | 必须 | 本仓库 `server/`、`webserver/` |
| **Go** | 1.27+ | 仅编译机需要 | https://go.dev/dl/ |
| **Node.js LTS** + **pnpm** | LTS + 9.x 常见 | 开发控制台必须；生产可只部署 `web/dist` | https://nodejs.org/ https://pnpm.io/ |
| **protoc** | 31+（改 proto 时） | 开发 | https://github.com/protocolbuffers/protobuf/releases |

**不需要**：在被监控机上安装 Telegraf 以外的中心组件。

### 4.3 工作区自带（Windows 开发包，可选）

| 内容 | 路径 |
| --- | --- |
| protoc | `protoc64/bin/protoc.exe` |
| InfluxDB 2 Windows 二进制 | `influxdb2/influxdb2_windows_amd64/influxd.exe` |
| Telegraf Windows 二进制 | `Telegraf/telegraf.exe` |

> **InfluxDB 启动**：`setup-windows.ps1` / `setup-linux.sh` **均不**自动拉起 InfluxDB；请用 `scripts/windows/server/start.ps1` 或 `scripts/linux/server/start.sh`。Windows 上 `start.ps1` 使用 `%USERPROFILE%\.influxdbv2`（避免与工作区错误 bolt 路径冲突）。

---

## 5. 仓库目录

```text
monitor/
├── client/              Agent、certgen
├── server/              Collector
├── webserver/           监控后台 HTTP API
├── web/                 Vue 控制台
├── Monitor/             Windows Agent 运行目录（exe、config、certs、state）
├── Telegraf/            Telegraf 发行包与 telegraf.conf
├── influxdb2/           InfluxDB 2 Windows 发行包（可选）
├── protoc64/            protoc（可选）
├── scripts/             setup、windows/*、linux/*、cert/*
├── dist/                交叉编译输出 linux-amd64 / linux-arm64
└── doc/                 补充备忘
```

---

## 6. 编译（Windows / Linux，amd64 / arm64）

统一要求：`CGO_ENABLED=0`（Agent / Collector / webserver 均不依赖 CGO）。

### 6.1 Windows 本机（amd64）

在**包根目录**执行，或先 `cd` 到对应子目录：

```powershell
$root = "C:\Users\Administrator\Desktop\monitor"   # 改成你的路径
$env:CGO_ENABLED = "0"

Set-Location "$root\client"
go test ./...
go build -trimpath -ldflags "-s -w" -o "$root\client\monitor-agent.exe" .\cmd\monitor-agent

Set-Location "$root\server"
go build -trimpath -ldflags "-s -w" -o "$root\server\collector.exe" .\cmd\collector

Set-Location "$root\webserver"
go build -trimpath -ldflags "-s -w" -o "$root\webserver\webserver.exe" .\cmd\webserver
```

也可运行 `powershell -File .\scripts\setup-windows.ps1`（含 `web\` 下 `pnpm install`、证书、编译）。参数：`-SkipInstall`、`-BuildOnly`。

### 6.2 Linux 本机（amd64 / arm64）

```bash
ROOT=/opt/monitor   # 改成你的路径
export CGO_ENABLED=0

cd "$ROOT/client"  && go build -trimpath -ldflags "-s -w" -o "$ROOT/dist/linux-amd64/monitor-agent" ./cmd/monitor-agent
cd "$ROOT/server"  && go build -trimpath -ldflags "-s -w" -o "$ROOT/dist/linux-amd64/collector" ./cmd/collector
cd "$ROOT/webserver" && go build -trimpath -ldflags "-s -w" -o "$ROOT/dist/linux-amd64/webserver" ./cmd/webserver
```

本机为 **arm64** 时，把输出目录改为 `dist/linux-arm64`（或在 arm64 机器上直接编译到该目录）。

`./scripts/setup-linux.sh` 从 [go.dev](https://go.dev/dl/) 安装 **Go 1.27.1** 到 `/usr/local/go`（不用 apt 的旧版 `golang-go`），按本机架构编译到 `dist/linux-amd64/` 或 `dist/linux-arm64/`。环境变量写入 `scripts/linux/monitor.env` 与 `/etc/monitor/monitor.env`，执行前：

```bash
set -a && source /etc/monitor/monitor.env && set +a
# 或 source /path/to/monitor/scripts/linux/monitor.env
```

可选 `GO_VERSION=1.27.1`、`GO_INSTALL_DIR=/usr/local/go`。Go 的 `PATH` 会写入 `/etc/profile.d/monitor-go.sh`，并在 `/etc/bash.bashrc` 中 `source`（login 与面板终端均可用）。业务变量见 `/etc/monitor/monitor.env`；可选 `MONITOR_ENV_AUTO=1` 在登录时一并加载（`/etc/profile.d/monitor-env.sh`）。示例见 `scripts/linux/monitor.env.example`。

### 6.3 交叉编译（在 Windows 上出 Linux 包）

```powershell
$root = "C:\Users\Administrator\Desktop\monitor"
New-Item -ItemType Directory -Force "$root\dist\linux-amd64","$root\dist\linux-arm64" | Out-Null
$env:CGO_ENABLED = "0"

# amd64
$env:GOOS = "linux"; $env:GOARCH = "amd64"
go build -C "$root\client" -trimpath -ldflags "-s -w" -o "$root\dist\linux-amd64\monitor-agent" ./cmd/monitor-agent
go build -C "$root\server" -trimpath -ldflags "-s -w" -o "$root\dist\linux-amd64\collector" ./cmd/collector
go build -C "$root\webserver" -trimpath -ldflags "-s -w" -o "$root\dist\linux-amd64\webserver" ./cmd/webserver

# arm64
$env:GOARCH = "arm64"
go build -C "$root\client" -trimpath -ldflags "-s -w" -o "$root\dist\linux-arm64\monitor-agent" ./cmd/monitor-agent
go build -C "$root\server" -trimpath -ldflags "-s -w" -o "$root\dist\linux-arm64\collector" ./cmd/collector
go build -C "$root\webserver" -trimpath -ldflags "-s -w" -o "$root\dist\linux-arm64\webserver" ./cmd/webserver

Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
```

### 6.4 前端

```powershell
Set-Location C:\Users\Administrator\Desktop\monitor\web
pnpm install
pnpm build          # 生产静态资源 → web/dist
pnpm dev            # 开发 :3000
```

---

## 7. 部署与必改配置

### 7.1 启动顺序（逻辑依赖）

```text
中心：PostgreSQL → InfluxDB（初始化 org/bucket/token）→ Collector → webserver →（仅 Windows 开发机）`pnpm dev`；Linux 中心机用 `pnpm build` + Nginx 或本机手动 `pnpm dev`
被监控：monitor-agent → Telegraf
```

### 7.2 中心机必改/必核对

| 文件 | 改什么 |
| --- | --- |
| `server/configs/server.local.yaml` | `postgres.*`、`influx.url/token/org/bucket`、`tls` 证书路径、`grpc_address` |
| `webserver/configs/webserver.yaml` | 同上 PG/Influx；`jwt.secret`；`http_address`（默认 `:8090`） |
| 环境变量（推荐生产） | `MONITOR_INFLUX_TOKEN`、`WEBSERVER_JWT_SECRET`、`WEBSERVER_ADMIN_PASSWORD`、`MONITOR_POSTGRES_PASSWORD` |

Influx 首次初始化：浏览器打开 `http://<中心>:8086`，**org=`hkc`**，**bucket=`bucket`**，创建 **write** Token 写入 yaml 或环境变量。

### 7.3 被监控机必改/必核对

| 文件 | 改什么 |
| --- | --- |
| `client/configs/agent.yaml`（或 `/etc/monitor-agent/agent.yaml`） | **`agent_id` 全局唯一**；`server_address` 为中心 `IP:9500`；`ca_file` / `cert_file` / `key_file` |
| `Telegraf/telegraf.conf` | 输出 URL 保持 `http://127.0.0.1:9510/v1/telegraf/metrics`；标签环境变量与 Agent 一致 |
| 证书 | 各机复制同一套 **`ca.pem` + `client.pem` + `client-key.pem`**（测试环境共用一张 client 证即可） |

### 7.4 Linux 中心机文件落位示例

```bash
sudo install -Dm755 collector /usr/local/bin/monitor-collector
sudo install -Dm755 webserver /usr/local/bin/monitor-webserver
sudo install -Dm600 server.local.yaml /etc/monitor-server/server.yaml
sudo install -Dm600 webserver.yaml /etc/monitor-webserver/webserver.yaml
sudo install -Dm600 ca.pem server.pem server-key.pem /etc/monitor-server/certs/
```

webserver 工作目录需可写 `data/webserver.db`（SQLite）。

### 7.5 Linux 被监控机示例

```bash
sudo install -Dm755 monitor-agent /usr/local/bin/monitor-agent
sudo install -Dm600 agent.yaml /etc/monitor-agent/agent.yaml
sudo install -Dm600 ca.pem client.pem client-key.pem /etc/monitor-agent/certs/
# systemd: client/packaging/monitor-agent.service
```

---

## 8. 一键脚本总览

脚本从自身路径**上溯三层**定位包根目录，路径与绝对安装位置无关。

| 脚本 | 平台 | 作用 |
| --- | --- | --- |
| `scripts/setup-windows.ps1` | Windows | 检查/安装依赖、`web` 的 `pnpm install`、生成证书、编译三端；**不启动 InfluxDB** |
| `scripts/windows/server/start.ps1` | Windows | PG 检查、**重启 Influx**（`.influxdbv2`）、Collector、webserver、**pnpm dev + 打开浏览器** |
| `scripts/windows/server/stop.ps1` | Windows | 停前端 :3000、collector、webserver、Influx；`-Infra` 另停 PostgreSQL |
| `scripts/windows/agent/start.ps1` | Windows | monitor-agent + Telegraf（可见控制台） |
| `scripts/windows/agent/stop.ps1` | Windows | telegraf + monitor-agent |
| `scripts/cert/Generate-mTLSCerts.ps1` | Windows | 默认 Go certgen → `client/certs` |
| `scripts/setup-linux.sh` | Linux | 安装 **Go 1.27.1**、写 `monitor.env`、apt 装 PG/Influx/Telegraf（可跳过）、编译到 `dist/linux-*`；**不启动 InfluxDB**；安装 telegraf 后会停用其默认自启服务（避免默认配置抢占） |
| `scripts/linux/server/start.sh` | Linux | PG、Influx、Collector、webserver；**不启动** `pnpm dev` |
| `scripts/linux/server/stop.sh` | Linux | 停中心进程；`INFRA=1` 停 PostgreSQL |
| `scripts/linux/agent/start.sh` | Linux | agent + telegraf（用 `Telegraf/telegraf-linux.conf`；发现系统自带 telegraf 服务用默认配置时会自动停用并接管，`MONITOR_TELEGRAF_TAKEOVER=0` 可关闭接管） |
| `scripts/linux/agent/stop.sh` | Linux | agent + telegraf |

---

## 9. Windows 操作手册

### 9.1 首次准备

```powershell
cd C:\Users\Administrator\Desktop\monitor
powershell -ExecutionPolicy Bypass -File .\scripts\setup-windows.ps1
```

### 9.2 启动 / 停止

```powershell
# 中心机（本机可同时当中心）
powershell -File .\scripts\windows\server\start.ps1

# 被监控机（或本机第二套 Agent）
powershell -File .\scripts\windows\agent\start.ps1

# 停止
powershell -File .\scripts\windows\server\stop.ps1
powershell -File .\scripts\windows\agent\stop.ps1
# powershell -File .\scripts\windows\server\stop.ps1 -Infra   # 含 PostgreSQL
```

`start.ps1` 未设置 `WEBSERVER_ADMIN_PASSWORD` 时默认 `Monitor123!`；首次 SQLite 无用户时会创建 **admin**。

### 9.3 手动启动（排障用）

见 `doc/windows-local-startup.md`；核心端口见 [§11](#11-健康检查端口进程http)。

---

## 10. Linux 操作手册（Ubuntu / Debian 系）

### 10.1 首次准备

```bash
cd /path/to/monitor
chmod +x scripts/setup-linux.sh scripts/linux/**/*.sh
./scripts/setup-linux.sh
# 可选：SKIP_INSTALL=1 ./scripts/setup-linux.sh
```

`setup-linux.sh` 会尝试拉起 PostgreSQL（若已装但未监听），**不会**启动 InfluxDB；Influx 由下一节的 `server/start.sh` 负责。Go 的 PATH 安装后对新终端自动生效；当前 shell 可 `source /etc/profile.d/monitor-go.sh`。启服务前再 `source /etc/monitor/monitor.env` 以加载 `MONITOR_ROOT`、`MONITOR_INFLUX_TOKEN` 等。

### 10.2 启动 / 停止

```bash
./scripts/linux/server/start.sh
./scripts/linux/agent/start.sh

./scripts/linux/server/stop.sh
./scripts/linux/agent/stop.sh
# INFRA=1 ./scripts/linux/server/stop.sh
```

### 10.3 前端（Linux 中心机）

```bash
cd web && pnpm install && pnpm dev
# 或 pnpm build 后由 Nginx 托管 dist，反代 API 到 :8090
```

### 10.4 arm64 服务器

使用 `dist/linux-arm64/` 下二进制，或在该机器上按 [§6.2](#62-linux-本机amd64--arm64) 编译；配置与 amd64 相同。

---

## 11. 健康检查：端口、进程、HTTP

### 11.1 端口一览

| 端口 | 组件 | 所在机器 |
| --- | --- | --- |
| 5432 | PostgreSQL | 中心 |
| 8086 | InfluxDB | 中心 |
| 9500 | Collector gRPC | 中心 |
| 8080 | Collector 管理 HTTPS | 中心 |
| 8090 | webserver | 中心 |
| 3000 | Vite 开发 / 静态站 | 中心（开发） |
| 4822 | guacd（远程桌面网关，见第 12 节） | 网关机（开发机在 WSL2） |
| 5900 / 590N | 目标机 VNC Server | 被控机器（`vncserver :N` → `5900+N`） |
| 3389 | 目标机 RDP | 被控 Windows |
| 9510 | Agent 收 Telegraf | 被监控（仅 127.0.0.1） |
| 9511 | Agent healthz | 被监控（仅 127.0.0.1） |

### 11.2 Windows：批量查端口

```powershell
$ports = 5432,8086,9500,8080,8090,3000,9510,9511
foreach ($p in $ports) {
  $up = [bool](netstat -ano | Select-String ":$p\s+\S+\s+LISTENING")
  "{0,5} {1}" -f $p, $(if ($up) { "LISTEN" } else { "down" })
}
```

### 11.3 Windows：进程

```powershell
Get-Process postgres,influxd,collector,webserver,monitor-agent,telegraf,node -ErrorAction SilentlyContinue |
  Format-Table Id, ProcessName, StartTime -AutoSize
```

### 11.4 Linux：端口与进程

```bash
ss -lnt | grep -E ':(5432|8086|9500|8080|8090|3000|9510|9511|4822)\b'
pgrep -a influxd; pgrep -a collector; pgrep -a webserver; pgrep -a monitor-agent; pgrep -a telegraf; pgrep -a guacd
```

### 11.5 HTTP 探活

```powershell
# 中心
Invoke-WebRequest http://127.0.0.1:8086/health -UseBasicParsing
Invoke-WebRequest http://127.0.0.1:8090/healthz -UseBasicParsing   # code 00000
curl.exe --ssl-no-revoke --cacert .\client\certs\ca.pem https://localhost:8080/healthz

# Agent
Invoke-WebRequest http://127.0.0.1:9511/healthz -UseBasicParsing

# Telegraf → Agent 通路（期望 204）
Invoke-WebRequest -Method POST -Uri http://127.0.0.1:9510/v1/telegraf/metrics `
  -ContentType "text/plain" -Body "probe,source=manual value=1i" -UseBasicParsing
```

```bash
curl -sS http://127.0.0.1:8086/health
curl -sS http://127.0.0.1:8090/healthz
curl -sS http://127.0.0.1:9511/healthz
```

浏览器：开发环境 http://localhost:3000 ，登录 **admin** / 初始密码（见 `WEBSERVER_ADMIN_PASSWORD`）。

---

## 12. 远程桌面（RDP / VNC）部署

### 12.1 三个角色装在三台不同的机器上

远程桌面要三样东西，**它们通常不在同一台机器上**，这是配置出错的最大来源：

```
浏览器 ──HTTP/WS──> webserver(:8090) ──Guacamole 协议──> guacd(:4822) ──RDP/VNC──> 目标机
                    (中心机)                             (网关机，任意 Linux)     (被控机器本身)
```

| 角色 | 装在哪 | 说明 |
| --- | --- | --- |
| **guacd** | 任意 Linux（开发机可放 WSL2，也可独立虚拟机 / 容器） | 唯一的协议网关，只监听 4822 |
| **VNC / RDP Server** | **被控的目标机自己** | Windows 装 TightVNC；Linux 装 TigerVNC；Windows 也可直接用系统 RDP |
| **webserver** | 中心机 | `guac.address` 指向 guacd |

**关键认知**：guacd 只是「代理」，它去连的是**目标机自己的** VNC/RDP 服务。目标机上必须真的有一个在跑的 VNC Server，否则怎么配都连不上。

### 12.2 guacd 部署

只部署 guacd 一个 C 守护进程，**不装 guacamole-client**（省掉 Java / Tomcat / MySQL，也不再占用 8080）。官方没有 Windows 原生二进制，Windows 开发机装在 WSL2 内即可。

```powershell
# Windows 开发机（WSL2）
pwsh -File scripts\guacd\deploy-guacd.ps1
```

```bash
# Linux 生产机
sudo bash scripts/guacd/deploy-guacd.sh

# 独立验证 guacd 是否响应握手（排查「端口通但连不上」的第一现场）
python3 scripts/guacd/probe-guacd.py rdp vnc

# 更细的诊断（版本、监听、进程、日志）
sudo bash scripts/guacd/diag-guacd.sh
```

脚本做四件事：装 guacd → **绑定 `0.0.0.0`** → 用 systemd 常驻 → 准备录像目录并做连通性自检。

> **两个必须知道的坑**（脚本已处理，手装极易踩中）：
>
> 1. guacd 默认只监听 `127.0.0.1`，而 WSL2 的 localhost 转发**不转发回环监听**，必须改成 `0.0.0.0`。
> 2. guacd 1.3.0 自行 daemonize 后会留下「端口在监听、进程也在、但 accept 后不处理请求」的半死状态——表现为**握手永远无应答，且日志里连一条连接记录都没有**，很容易被误判成协议实现错误。必须加 `-f` 让它前台运行、交给 systemd 托管。

容器方式（最少只需 guacd 一个服务，不再需要 MySQL）：

```yaml
# docker-compose.yml
services:
  guacd:
    image: guacamole/guacd:1.5.5
    restart: unless-stopped
    ports: ["4822:4822"]
    volumes:
      - ./guac-recordings:/var/lib/guacd/recordings
```

webserver 侧对应配置（`webserver/configs/webserver.yaml`）：

```yaml
guac:
  enabled: true
  address: "192.168.1.18:4822"   # guacd 所在机；同机填 127.0.0.1:4822
```

> 字段的完整说明见 `webserver/README.md` 的「guacd 部署」小节。

### 12.3 Windows 目标机：装 TightVNC（**不要用 RealVNC**）

RealVNC 与 guacd **不兼容，且无法靠调配置绕过**（实测结论）：

| | RealVNC 7.x | TightVNC 2.8.84 |
| --- | --- | --- |
| 协议版本 | `RFB 005.000`（私有） | **`RFB 003.008`**（标准） |
| 认证类型 | 6 个**全是私有编号**（6 / 130 / 13 / 133 / 5 / 129），没有标准 VNC Auth | **含标准类型 2** |
| guacd（libvncclient） | 解析到 major=5 直接放弃 → 报 **`519`** | 原生兼容 |

> `519 = 0x0207 = UPSTREAM_NOT_FOUND`。「上游」指 guacd 要连的那个 VNC/RDP——**连不上、或不兼容**都报这个码，所以要结合目标机侧一起判断。

静默安装并预设密码：

```powershell
# 1) 下载
Invoke-WebRequest -Uri 'https://www.tightvnc.com/download/2.8.84/tightvnc-2.8.84-gpl-setup-64bit.msi' `
  -OutFile "$env:TEMP\tightvnc.msi"

# 2) 若装有 RealVNC，先静默卸载（GUID 取自注册表 UninstallString）
MsiExec.exe /X{粘贴_GUID} /qn /norestart

# 3) 静默安装 + 设置 VNC 密码
msiexec /i "$env:TEMP\tightvnc.msi" /qn ADDLOCAL=Server SILENT=1 `
  SET_USEVNCAUTHENTICATION=1 VALUE_OF_USEVNCAUTHENTICATION=1 `
  SET_PASSWORD=1 VALUE_OF_PASSWORD=你的密码 `
  SET_ALLOWLOOPBACK=1 VALUE_OF_ALLOWLOOPBACK=1 `
  SET_RFBPORT=1 VALUE_OF_RFBPORT=5900
```

要点：

- 服务名 **`tvnserver`**，默认监听 **`0.0.0.0:5900`**
- **VNC 密码最长 8 字符**（VNC Authentication 协议本身的限制）
- 验证：`Get-Service tvnserver` + `Get-NetTCPConnection -State Listen -LocalPort 5900`
- 防火墙需放行**入站 5900**（TightVNC 安装时会自带规则，profile 覆盖 Any）

确认目标机的协议能被 guacd 接受（读 banner → 回发标准版本 → 看安全类型）：

```powershell
$c = New-Object Net.Sockets.TcpClient; $c.Connect("目标机IP", 5900)
$s = $c.GetStream(); $s.ReadTimeout = 4000
$b = New-Object byte[] 12; $null = $s.Read($b, 0, 12)
"banner = " + [System.Text.Encoding]::ASCII.GetString($b, 0, 12).Trim()   # 期望 RFB 003.008
$send = [System.Text.Encoding]::ASCII.GetBytes("RFB 003.008`n")
$s.Write($send, 0, $send.Length); $s.Flush(); Start-Sleep -Milliseconds 600
$r = New-Object byte[] 32; $n = $s.Read($r, 0, 32)
($r[1..$r[0]] | ForEach-Object { "$_" }) -join ", "   # 期望含 2（标准 VNC Auth）
$c.Close()
```

### 12.4 Linux 目标机：TigerVNC + XFCE，并交给 systemd

```bash
# 0) 先确保 apt 可用：失效的 PPA 会让 apt update 失败，
#    而 `apt update && apt install` 会因短路而「静默跳过安装」
rm -f /etc/apt/sources.list.d/guacamole*.list

apt update
apt install -y tigervnc-standalone-server xfce4 xfce4-goodies dbus-x11

# 1) xstartup 必须 exec 一个「前台常驻」的桌面，否则脚本跑完就退，
#    Xtigervnc 判定会话已死（日志: cleanly exited too early）
cat > ~/.vnc/xstartup <<'EOF'
#!/bin/sh
unset SESSION_MANAGER
unset DBUS_SESSION_BUS_ADDRESS
exec startxfce4
EOF
chmod +x ~/.vnc/xstartup

# 2) 设置连接密码
vncpasswd
```

交给 systemd 常驻——**手动 `vncserver :2` 在 SSH 断开或机器重启后就没了**：

```bash
cat > /etc/systemd/system/vncserver-2.service <<'EOF'
[Unit]
Description=TigerVNC display :2 (port 5902)
After=network.target

[Service]
Type=forking
User=root
Environment=HOME=/root
Environment=USER=root
Environment=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
ExecStartPre=-/usr/bin/vncserver -kill :2
ExecStartPre=-/bin/rm -f /tmp/.X2-lock /tmp/.X11-unix/X2
ExecStart=/usr/bin/vncserver :2 -localhost no
ExecStop=/usr/bin/vncserver -kill :2
Restart=on-failure
RestartSec=3
StartLimitIntervalSec=60
StartLimitBurst=3

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now vncserver-2
ss -lntp | grep 5902
```

要点：

- **display 号就是端口**：`:2` → **5902**、`:1` → **5901**。连接信息里的端口必须跟着改，填 5900 一定连不上。
- **TigerVNC 默认 `-localhost`，只监听 `127.0.0.1`**：
  - guacd 与它**同机** → 连接信息的地址填 `127.0.0.1`（推荐，不必把端口对外开放）
  - 需要别的机器直连 → 启动参数加 `-localhost no`，或在 `/etc/tigervnc/vncserver-config-defaults` 写 `localhost=no`
- systemd 单元里 **`HOME` / `USER` / `PATH` 不能省**：服务环境不继承登录会话，缺 `HOME` 会让 `vncserver` 找不到 `~/.vnc/` 而启动失败——这是「手动能跑、systemd 一跑就挂」的典型原因。
- `Cannot establish any listening sockets` = 残留锁，清掉 `/tmp/.X{n}-lock` 与 `/tmp/.X11-unix/X{n}`；**换个 display 号能立刻判断**是该 display 专属残留，还是环境问题。

### 12.5 在 webserver 里填连接信息（视角最容易错）

服务器列表 → 该机器 → **连接信息**。三层地址各有各的视角：

| 环节 | 填在哪里 | 值 |
| --- | --- | --- |
| 浏览器 → webserver | 浏览器地址栏 | webserver 所在机 IP:8090 |
| webserver → guacd | `webserver.yaml` 的 `guac.address` | guacd 所在机 IP:4822 |
| **guacd → VNC/RDP** | **界面的「连接信息」** | **以 guacd 的视角**：同机填 `127.0.0.1`，不同机填目标机可达 IP |

| 协议 | 默认端口 | 用户名 |
| --- | --- | --- |
| SSH | 22 | 必填 |
| RDP | 3389 | 必填 |
| VNC | 5900（`vncserver :N` 则为 `5900+N`） | **可留空**（VNC Auth 只有密码） |

**同一台机器可以并存多条配置**（SSH / RDP / VNC 各一条，互不影响）；删除时也只删当前协议那一条。

### 12.6 排障对照

| 现象 | 含义 | 处理 |
| --- | --- | --- |
| `519` / `Aborted. See logs. 519` | UPSTREAM_NOT_FOUND：guacd 连不上目标 VNC/RDP（含协议不兼容） | 核对「连接信息」的地址视角与端口；确认目标机 VNC 在监听 |
| 目标端口 `nc` 通、guacd 却连不上 | 协议 / 认证不兼容（典型是 RealVNC） | 换 TightVNC / TigerVNC，见 12.3 |
| `WebSocket is closed before the connection is established` | WS 握手失败（票据无效或后端提前拒绝） | 查 `webserver.run.log` 里的远程桌面日志 |
| guacd 在监听、握手无应答、日志无连接记录 | guacd 自行 daemonize 的半死状态 | `-f` 前台 + systemd，见 12.2 |
| 录像能生成、回放 404 | `local_recording_dir` 填成了 guacd 视角的路径 | 见 12.7 |
| 画面里出现「自己」并无限嵌套 | VNC 目标就是本机，且本机桌面正显示这个页面 | 换一台机器访问；不要在画面里操作（键鼠会真实作用到目标机） |

### 12.7 分机部署时录像目录的两个视角

录像是**两个字段**，指同一份文件的两个视角，填错只会表现为「回放 404」：

```yaml
guac:
  # guacd 所在机器上写出的路径
  recording_dir_in_guacd: "/var/lib/monitor/guac-recordings/sessions"
  # webserver 本机读到的路径（分机时需能访问同一份文件）
  local_recording_dir: '\\192.168.1.18\guac-rec\sessions'
```

- guacd 与 webserver **同机**：两个值可以相同
- **分机**：必须让 webserver 读到 guacd 写出的文件（SMB / NFS 共享），否则录像正常生成但**回放 404**
- 该目录在 guacd 侧必须存在且 `guacd` 用户可写，否则**开启录像会让握手直接失败**

---

## 13. 证书（mTLS）

测试环境一键生成（**共用一张 client 证**，多机复制 `ca.pem` + `client.pem` + `client-key.pem`）：

```powershell
powershell -File .\scripts\cert\Generate-mTLSCerts.ps1
```

或：

```powershell
cd client
go run .\cmd\certgen -out ..\client\certs
```

| 文件 | 用途 |
| --- | --- |
| `ca.pem` | 双方信任根 |
| `server.pem` / `server-key.pem` | 仅 Collector 中心机 |
| `client.pem` / `client-key.pem` | 所有 Agent（可复制分发） |

轮换/吊销：重新生成整套证书，替换文件并重启 Collector 与全部 Agent。Windows 生成的 PEM 可直接用于 Linux。

---

## 14. 配置文件索引

| 文件 | 用途 |
| --- | --- |
| `client/configs/agent.yaml` | 本机 Windows Agent |
| `client/configs/agent.example.yaml` | Linux 模板 |
| `client/configs/agent.windows.yaml` | Windows 模板 |
| `server/configs/server.local.yaml` | Collector 本机配置 |
| `server/configs/server.local.example.yaml` | 示例 |
| `webserver/configs/webserver.yaml` | Web API |
| `web/.env.development` | 前端开发代理与 API 地址 |
| `Telegraf/telegraf.conf` | 本机 Telegraf |
| `Telegraf/telegraf-linux.conf` | Linux 一键脚本使用的 Telegraf 配置（配合 `Telegraf/run-linux.sh` 注入 `MONITOR_*` 环境变量） |
| `client/telegraf/telegraf.conf.template` | Linux Telegraf 模板（systemd 方式用） |

环境变量覆盖（webserver）：`WEBSERVER_JWT_SECRET`、`WEBSERVER_ADMIN_PASSWORD`、`MONITOR_POSTGRES_PASSWORD`、`MONITOR_INFLUX_TOKEN` — 详见 `webserver/README.md`。

---

## 15. 安全与更多文档

- 口令、Influx Token、JWT **不要提交 git**；生产用环境变量或权限受限的配置文件。
- Agent 监听 **9510 / 9511 仅 127.0.0.1**；Telegraf 不要携带中心写 Token。
- 中心故障时：Agent SQLite 积压、Telegraf 磁盘 buffer，恢复后追送。

| 文档 | 内容 |
| --- | --- |
| [doc/windows-local-startup.md](doc/windows-local-startup.md) | Windows 本机细节、systemd 摘录 |
| [doc/telegraf-monitor-agent-deployment.md](doc/telegraf-monitor-agent-deployment.md) | Agent + Telegraf 部署 |
| [server/README_ZH.md](server/README_ZH.md) | Collector |
| [webserver/README.md](webserver/README.md) | webserver 配置、编译、运行 |
| [project-idea-v3.md](project-idea-v3.md) | 原始方案与后续规划 |
