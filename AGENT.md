# AGENT.md — Meerkat 监控平台开发规范

> 本文件是**本仓库的最高开发约束**，供 AI Agent 与人类开发者共同遵守。
> 与任何其它文档冲突时，**以本文件为准**。
> 阅读顺序：`README.md`（部署/运维）→ `project-idea-v3.md`（设计基线）→ **本文件**（开发规范）→ 各子目录 README。

---

## 0. Agent 开工前必做（强制）

1. **先读**：`README.md`、`project-idea-v3.md`、本文件，以及你将要修改目录的 README。
2. **先定位**：确认改动落在哪个 module（`client` / `server` / `webserver` / `web`），**不要跨 module 顺手改**。
3. **先读代码再写**：用语义工具查定义/引用，禁止凭猜测改 API 签名。
4. **先跑测试**：`go test ./...`（对应 module）或前端 `pnpm type-check && pnpm lint`。
5. **禁止**：未确认就改 proto、改端口、改数据库表结构、删除缓冲/队列文件。

---

## 1. 项目一句话定位

面向**内网服务器集群**的统一监控平台：被监控机由 **Telegraf** 采集指标 → **monitor-agent** 接收、本地缓存、经 **mTLS gRPC** 上报 → 中心 **Collector** 汇聚写入 **PostgreSQL（资产/心跳/告警）+ InfluxDB（时序）** → **webserver + Vue3** 提供登录、列表、详情、趋势与告警。

### 1.1 架构不变式（红线，任何改动不得破坏）

| # | 不变式 | 违反后果 |
| --- | --- | --- |
| R1 | 指标**绝不**由 Telegraf 直写 InfluxDB，必须经 Agent → Collector | 失去身份/认证/缓存/限流能力 |
| R2 | Agent → Collector 走 **mTLS 双向证书**，生产环境 `insecure_tls: false` | 内网被冒充上报 |
| R3 | Telegraf 输出 URL **恒为本机** `http://127.0.0.1:9510/v1/telegraf/metrics` | 绕过 Agent 控制面 |
| R4 | Agent 的 `9510` / `9511` **只监听 127.0.0.1** | 被监控机端口对外暴露 |
| R5 | **心跳与指标分离**：指标卡住不得导致 Agent 判离线 | 误判整机 OFFLINE |
| R6 | 中心不可用时 **Agent SQLite 积压 + Telegraf 磁盘 buffer**，恢复后限速补传 | 数据丢失 / 恢复流量冲垮中心 |
| R7 | 协议源文件**唯一**：`server/api/proto/collector/v1/collector.proto` | 两端协议漂移 |
| R8 | PostgreSQL 存元数据，InfluxDB 存时序，SQLite(web) 只存用户/令牌/审计 | 数据职责混乱 |
| R9 | 全链路 `CGO_ENABLED=0`，禁止引入 CGO 依赖 | 交叉编译与部署破裂 |
| R10 | 本仓库**不使用 Docker**（第三方组件用系统包或工作区二进制） | 与现有部署体系冲突 |

---

## 2. 技术栈基线

| 层 | 技术 | 版本基线 | 说明 |
| --- | --- | --- | --- |
| Agent | Go（module `monitor-client`） | **Go 1.27+** | grpc v1.79、protobuf v1.36.10、`modernc.org/sqlite`（纯 Go）、`yaml.v3`、标准 `log` |
| Collector | Go（module `monitor-server`） | **Go 1.27+** | grpc、`log/slog`、GORM + `gorm.io/driver/postgres`、`influxdb-client-go/v2`、`yaml.v3` |
| Web API | Go（module `github.com/company/monitor-webserver`） | **Go 1.27.1** | Gin、GORM（Postgres + `glebarez/sqlite`）、zap（`pkg/logger`）、viper、`influxdb-client-go/v2` |
| 采集器 | Telegraf | **1.40+**（工作区 1.40.1） | 官方插件，禁止自研采集器 |
| 时序库 | InfluxDB | **2.x** | org `hkc`、bucket `bucket` |
| 元数据 | PostgreSQL | **16+** | GORM 管理 |
| 控制台 | Vue 3 + Vite 8 + TypeScript | Vue 3.5 / Node `^20.19 \|\| >=22.12` | Element Plus、Pinia、ECharts、Axios、UnoCSS |
| 包管理（前端） | **pnpm** | >= 8 | `preinstall: only-allow pnpm`，禁止 npm/yarn |
| RPC | gRPC + Protobuf | protoc 31+ | 插件 `protoc-gen-go@v1.36.10`、`protoc-gen-go-grpc@v1.5.1` |
| 传输安全 | TLS 1.2+ / mTLS | — | `RequireAndVerifyClientCert` |

**版本纪律**：升级 Go / gRPC / GORM / Vue 主版本属于**架构级变更**，需先更新本文件基线再动代码。

---

## 3. 仓库地图

```text
monitor/
├── AGENT.md              ← 本文件（开发规范，最高约束）
├── README.md             ← 部署/运维手册
├── project-idea-v3.md    ← 设计基线（V1 技术基线、参数表）
├── client/               monitor-agent（被监控机）
│   ├── cmd/monitor-agent/   主入口（含 /healthz、/readyz）
│   ├── cmd/certgen/         mTLS 测试证书生成
│   ├── internal/config/     YAML 加载 + 稳定 ULID 身份
│   ├── internal/receiver/   Telegraf HTTP 接收 :9510
│   ├── internal/spool/      SQLite 积压队列（优先级/老化/限额）
│   ├── internal/transport/  gRPC 客户端（双向流、退避重连、限速）
│   ├── internal/systeminfo/ 主机快照
│   ├── internal/telegraf/   Telegraf 进程看护与自动恢复
│   ├── configs/             agent.example.yaml / agent.windows.yaml
│   ├── packaging/           monitor-agent.service
│   └── collector/v1/        【生成物】客户端侧 pb 代码
├── server/               Collector（中心）
│   ├── cmd/collector/       主入口
│   ├── api/proto/collector/v1/collector.proto   ★ 唯一协议源
│   ├── internal/collector/  gRPC 服务端（Connect 双向流）
│   ├── internal/state/      GORM 持久化 + Outbox 事件表
│   ├── internal/metricwriter/ Outbox → Influx 写入 worker
│   ├── internal/evaluator/  在线状态机 + 离线告警评估
│   ├── internal/influx/     Influx 写入封装
│   ├── internal/api/        管理 HTTPS API（:8080）
│   ├── internal/config/     服务端配置
│   ├── internal/transport/  mTLS 配置
│   └── configs/
├── webserver/            监控后台 HTTP API（中心）
│   ├── cmd/webserver/       ★ 唯一入口
│   ├── api/                控制器 / 中间件 / 路由（全部 Gin 代码）
│   │   ├── v1/             控制器：绑定 → service → 统一响应
│   │   ├── middleware/     RequireAuth、RequirePerm、操作日志、访问日志
│   │   └── router/         路由装配
│   ├── internal/           分层实现
│   │   ├── model/ dto/ apperr/ util/ permission/
│   │   ├── logic/ service/                    领域操作与应用服务
│   │   ├── captcha/ monitor/ eventhub/         验证码、监控查询、SSE 广播
│   │   └── app/                                装配根（配置/两库/迁移种子/横幅）
│   │       └── sql/                            内嵌 DDL 与种子（源自 database/init.sql）
│   ├── pkg/                 公共能力（logger、database、middleware…）
│   └── configs/webserver.yaml
├── web/                  Vue3 控制台
│   └── src/{api,views,components,router,stores,utils,composables,layouts}
├── Monitor/              Windows Agent 运行目录（exe/config/certs/state）
├── Telegraf/             Telegraf 发行包与 telegraf.conf
├── influxdb2/ / protoc64/  工作区自带二进制（可选）
├── dist/                 交叉编译产物 linux-amd64 / linux-arm64
├── scripts/              setup、windows/*、linux/*、cert/*
├── database/init.sql     权限库结构事实来源（sa_system_*）
└── doc/                  补充备忘
```

> ℹ️ `webserver/` 原由 `bw-cli` 生成的脚手架（`cmd/gateway`、`cmd/user`、`cmd/role`、`cmd/menu`、`api/proto`、`tools/protogen`）**已删除**；`cmd/webserver` 是唯一入口，`internal/webapp` 为唯一业务包。权限相关数据结构以 `database/init.sql` 为准（译文见 `internal/webapp/sql/`）。

---

## 4. 端口与网络规范

| 端口 | 组件 | 机器 | 暴露范围 |
| --- | --- | --- | --- |
| 5432 | PostgreSQL | 中心 | 仅本机/内网，**禁止客户端直连** |
| 8086 | InfluxDB | 中心 | 仅本机/内网，**禁止客户端直连** |
| 9500 | Collector gRPC（mTLS） | 中心 | 对 Agent 网段开放，**唯一必须对外** |
| 8080 | Collector 管理 HTTPS（自签 CA） | 中心 | 开发用，未接 RBAC，**禁止公网** |
| 8090 | webserver | 中心 | 生产由 Nginx 反代，禁止直接公网 |
| 3000 | Vite dev / 静态站 | 中心 | 开发 |
| 9510 | Agent 收 Telegraf | 被监控 | **仅 127.0.0.1** |
| 9511 | Agent healthz | 被监控 | **仅 127.0.0.1** |

**新增端口必须先更新本表 + `README.md` §11**，并在 `scripts/**/start*.ps1|sh` 健康检查中同步。

---

## 5. 模块开发规范

### 5.1 client（monitor-agent）

- 入口 `cmd/monitor-agent/main.go`：`-config`（默认 `configs/agent.yaml`）、`-u`（Windows 后台）。
- 新增能力放在 `internal/<包名>/`，一个包一件事；**禁止在 `cmd/` 写业务逻辑**。
- 状态/缓冲一律走 `internal/spool`（SQLite），**禁止**新增自制文件队列。
- 重连必须是**指数退避 + jitter**（基线 1/2/4/8/16/30s）；补传走令牌桶限速，禁止一次性倾泻。
- `agent_id` 必须是稳定 **ULID**，禁止用 IP/hostname 生成；占位值 `replace-with-stable-ulid` 由 `internal/config/identity.go` 首次启动回写。
- 采集不到的数据返回零值即可，**禁止 panic**；Agent 崩溃等于整机上报告警缺失。

### 5.2 server（Collector）

- 新增 gRPC 能力改 `internal/collector/`；新增持久化先改 `internal/state/`（GORM 模型 + Outbox）。
- 写 Influx **只能**经 `internal/metricwriter`（Outbox 消费），禁止在 gRPC 处理路径里同步写 Influx。
- 毒丸判定沿用 `isPermanentWriteError`（field type conflict / 422）→ 丢弃并记日志；其余 `ReleaseEvents` 重试。
- 在线状态机 `ONLINE → SUSPECT(10s) → OFFLINE(15s)` 只在 `internal/evaluator/` 内演进。
- 日志用 `log/slog`，带 `slog.String("agent_id", ...)` 等结构化字段。

### 5.3 webserver

- 入口只有 `cmd/webserver`；参数只有 `-config`（默认 `configs/webserver.yaml`）。**工作目录必须是 `webserver/`**，否则相对路径 `sqlite_path` 找不到。
- 当前 `internal/webapp/` 是**扁平单包**结构：`app.go`（配置/装配/GORM 模型）、`routes.go`（路由+鉴权）、`monitor.go`（handler + SSE）、`console*.go`（启动横幅）。
  - 新接口：handler 写进 `monitor.go`，路由注册进 `routes.go`，**保持现有风格**，不要擅自重构成 DDD 分层。
  - 若单包超过可维护规模需要拆分，属于架构级变更，先改本文件。
- 统一响应必须通过 `write(c, httpStatus, code, message, data)`：
  - `00000` 成功；`A0xxx` 客户端错误（A0001 参数、A0210 认证失败、A0230 令牌失效、A0301 无权限、A0404 不存在）；`B0xxx` 服务端错误（B0001 内部、B0200 查询监控失败、B0201 时序失败）。
  - **禁止**在 handler 里直接 `c.JSON(...)` 造响应。
- 日志用 `pkg/logger`（zap 封装），不用 `fmt.Println`。
- 鉴权：`*App` 的方法 `a.requireAuth` / `a.requireAdmin` 作中间件。`protected` 组挂 `a.requireAuth`，`admin` 组再挂 `a.requireAdmin`；除 `/healthz`、登录、刷新外一律不可匿名访问。

### 5.4 web（Vue3 控制台）

- 包管理器 **pnpm**；脚本：`dev` / `build` / `preview` / `type-check` / `lint:eslint` / `lint:prettier` / `lint:stylelint` / `commit`。
- 目录约定：
  - `src/api/<域>/index.ts` + `types.ts`（监控域 BASE_URL `/api/v1/monitor`）
  - `src/views/monitor/*`（overview / agent / agent-detail / alert）
  - `src/components/` 通用组件、`src/composables/` 复用逻辑、`src/utils/request.ts` axios 封装、`src/utils/sse.ts`
- 组件必须 `<script setup lang="ts">`；样式用 UnoCSS / BEM，**禁止**在组件里写大段裸 CSS。
- 新增接口类型必须同时更新 `types.ts`，**禁止 `any`**（确需时用 `unknown` + 类型守卫）。
- 提交前 `pnpm type-check` 必须零错误（`build` 内含 `vue-tsc --noEmit`）。

---

## 6. Protobuf 协议变更规范（重点）

协议源唯一：`server/api/proto/collector/v1/collector.proto`（服务 `CollectorService.Connect` 双向流，`AgentMessage` / `ServerMessage` 用 `oneof`）。

**改 proto 的完整流程**（缺一步即视为未完成）：

```powershell
# 1) 安装插件（一次性）
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.10
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1

# 2) 生成服务端
cd C:\Users\Administrator\Desktop\monitor\server
protoc -I "api\proto" `
  --go_out="api\proto" --go_opt=paths=source_relative `
  --go-grpc_out="api\proto" --go-grpc_opt=paths=source_relative `
  "api\proto\collector\v1\collector.proto"

# 3) 生成客户端（在 client 目录执行，路径相对）
cd C:\Users\Administrator\Desktop\monitor\client
protoc -I "..\server\api\proto" `
  --go_out="..\client" --go_opt=paths=source_relative `
  --go-grpc_out="..\client" --go-grpc_opt=paths=source_relative `
  "..\server\api\proto\collector\v1\collector.proto"

# 4) 两端 go test ./... 通过后，重启 Collector 与全部 Agent
```

规则：

- **两端必须同时重新生成**，且**提交生成物**（`server/api/proto/collector/v1/*.pb.go`、`client/collector/v1/*.pb.go`）。
- **只增字段不复用/不改号**：已使用的 `field number` 永久保留，废弃用 `reserved` 标记，禁止删除或改语义。
- 新增字段必须**向后兼容**：Collector 要能容忍旧 Agent 未填该字段（零值处理）。
- `ServerMessage` 新增指令时，旧 Agent 忽略未知指令不得报错。
- 改 proto 必须同步更新 `README.md`（协议说明处）与相关文档。

---

## 7. 配置规范

| 文件 | 用途 |
| --- | --- |
| `client/configs/agent.yaml` | 本机 Windows Agent |
| `client/configs/agent.example.yaml` | Linux 模板 |
| `client/configs/agent.windows.yaml` | Windows 模板 |
| `server/configs/server.local.yaml` | Collector 本机配置（由 `.example.yaml` 复制） |
| `webserver/configs/webserver.yaml` | Web API 配置 |
| `web/.env.development` | 前端 `/dev-api` 代理 → `http://localhost:8090` |
| `Telegraf/telegraf.conf` | 本机 Telegraf |
| `client/telegraf/telegraf.conf.template` | Linux Telegraf 模板 |

**优先级**：环境变量 > YAML > 代码默认值。

| 环境变量 | 覆盖项 |
| --- | --- |
| `WEBSERVER_JWT_SECRET` | `jwt.secret` |
| `WEBSERVER_ADMIN_PASSWORD` | `bootstrap_admin_password`（仅 SQLite 无用户时生效，创建 `admin`） |
| `MONITOR_POSTGRES_PASSWORD` | `postgres.password` |
| `MONITOR_INFLUX_TOKEN` | `influx.token` / Collector Influx token |

规则：

- 新增配置项必须**同时**提供：代码默认值 + YAML 示例 + 本文档/子 README 说明。
- 时间字段用 Go `time.Duration` 字符串（`5s`、`24h`），**禁止**裸数字秒。
- 路径配置一律支持绝对/相对，Linux 与 Windows 模板都要同步更新。
- Telegraf 标签（`MONITOR_AGENT_ID` / `MONITOR_ENVIRONMENT` / `MONITOR_SITE` / `MONITOR_ROLE`）必须与 `agent.yaml` 的 `labels` 保持一致。

---

## 8. 安全规范（硬性）

1. **绝不**把口令、Influx Token、JWT secret、私钥提交进仓库。生产用环境变量或权限受限文件（Linux `0600`）。
2. `ca-key.pem` 只能留在签发环境，**禁止**复制到 Agent、禁止入库。
3. 被监控机**不持有** Influx 写 Token；Telegraf 配置里禁止出现中心写凭据。
4. Telegraf **禁止**启用 `exec` 插件、任意脚本，禁止接收前端上传的原始 TOML；配置由服务端结构化下发、Agent 渲染。
5. 数据库/Redis/Influx 端口**禁止客户端直连**，只有 Collector 与 webserver 可访问。
6. Collector 管理 HTTPS `:8080` 尚未接入 OIDC/RBAC，**禁止暴露公网**。
7. 中间件凭据走 Telegraf secret store 或 OS 密钥存储，使用**插件专属只读账户**。
8. `buffer_strategy = "disk"` 目录无自动容量上限，需独立分区/配额，并在 70%/90% 告警。
9. 故障处理：**禁止**删除 Telegraf buffer 或 Agent SQLite 文件来"恢复"，先查磁盘、证书、网络与服务日志。

---

## 9. Go 编码规范

- **格式**：一律 `gofmt`（Tab 缩进）；提交前 `gofmt -l .` 应无输出。
- **命名**：包名小写单词、不带下划线/复数；文件名 `snake_case.go`；导出标识符加注释（以标识符名开头），函数注释细则见 [§9.1](#91-函数注释规范强制)。
- **错误处理**：
  - 错误必须**向上传递或显式处理**，禁止 `_ =` 吞掉（除明确可忽略且有注释）。
  - 用 `fmt.Errorf("xxx: %w", err)` 包装保留链路；**禁止**丢弃 `%w`。
  - 业务哨兵错误放包内 `var ErrXxx = errors.New(...)`，用 `errors.Is` 判定。
- **并发**：
  - 长跑 goroutine 必须接收 `context.Context`，在 `ctx.Done()` 时退出；**禁止**无取消路径的 goroutine。
  - 共享状态用 `sync.Mutex`/`sync.Map` 或 channel，禁止数据竞争（提交前 `go test -race`）。
  - 主进程要有优雅退出（Agent 基线 10s）。
- **日志**：按模块统一（client 用 `log`，server 用 `log/slog`，webserver 用 `pkg/logger`）。**禁止**在一个 module 内混用两套日志；日志中禁止打印凭据、Token、证书私钥。
- **依赖**：新增第三方依赖属**架构级变更**，需说明理由；优先标准库；**禁止**引入 CGO 依赖（R9）。
- **internal 边界**：可复用能力放 `pkg/`（仅 webserver 有），业务放 `internal/`；禁止从 `internal/` 反向依赖 `cmd/`。
- **平台差异**：用 `xxx_windows.go` / `xxx_other.go` 构建标签分离（参考 `console_windows.go`），**禁止**在业务代码里散落 `runtime.GOOS` 判断。

### 9.1 函数注释规范（强制）

**每个函数、方法、构造函数都必须有文档注释**，采用 Go 官方 doc comment 风格：注释紧贴声明上方、**无空行**、**以被注释对象名开头**（`// FuncName ...`）。注释用**中文为主、术语保留英文**。

必写要素（按实际存在情况取舍，不要写空话）：

| 要素 | 写法 | 何时必写 |
| --- | --- | --- |
| **功能 Function** | 一句话说明做什么、以及**为什么** | 全部 |
| **入参 Parameters** | `//   - name (type): 含义、取值约定、是否可为 nil/零值` | 参数 ≥1 个且含义不自明 |
| **返回值 Returns** | `//   - (type): 含义`；多返回值逐一说明 | 返回值 ≥1 个且含义不自明 |
| **错误 Errors** | 说明**什么条件下返回什么错误**，是否可用 `errors.Is` 判定 | 返回 `error` 时**必写** |
| **行为约束** | 阻塞/超时、是否幂等、副作用（写库/写文件/启 goroutine）、并发安全 | 涉及 IO、并发、外部副作用时**必写** |
| **Panic** | 明确的 panic 条件 | 会 panic 时必写 |
| **弃用** | `// Deprecated: 原因，改用 Xxx` | 废弃函数必写 |

**导出函数**（`internal/` 内跨包使用也算）必须写完整要素；**非导出函数**至少写「功能 + 关键入参/返回」，单行也行。
**禁止**写与代码重复的废话（如 `// GetName 获取 name`），也**禁止**在注释里粘贴大段代码。

#### 模板与示例

```go
// WriteLineProtocol 将一批 Influx Line Protocol 文本写入 InfluxDB。
//
// 写入是同步阻塞的；调用方应先经 Outbox 取事件再调用本函数，不要在 gRPC
// 处理路径中直接调用。本函数并发安全，可被多个 worker goroutine 同时调用。
//
// 参数 Parameters:
//   - ctx (context.Context): 写入超时与取消控制；不可为 nil，超时由调用方设定。
//   - org (string): InfluxDB 组织名，取自配置 influx.organization；空串将返回错误。
//   - bucket (string): 目标 bucket 名，取自配置 influx.bucket；空串将返回错误。
//   - lines ([]string): Line Protocol 文本行，每项一条；nil 或空切片直接返回 nil，不发起请求。
//
// 返回 Returns:
//   - written (int): 成功写入的行数；部分成功时小于 len(lines)。
//   - err (error): Influx 服务端或网络错误（可用 errors.Is(err, context.DeadlineExceeded) 判定超时）；
//     field type conflict / HTTP 422 等永久性错误由 isPermanentWriteError 判定，调用方应丢弃而非重试。
func WriteLineProtocol(ctx context.Context, org, bucket string, lines []string) (written int, err error) {
	// ...
}
```

```go
// ClaimEvents 原子地认领一批待处理事件，供 Outbox 消费 worker 使用。
//
// 认领后事件进入 inflight 状态；调用方必须在成功后 AckEvents，失败时 ReleaseEvents，
// 否则事件会滞留到租约过期。同一 batchSize 下可被多个 worker 并发调用。
//
// 参数 Parameters:
//   - kind (string): 事件类型，如 "metrics"；未知类型返回空结果而非错误。
//   - batchSize (int): 单次最大认领条数，必须 > 0；<= 0 时按默认值 100 处理。
//
// 返回 Returns:
//   - events ([]Event): 已认领事件；无可用事件时返回空切片（非 nil）。
//   - err (error): PostgreSQL 不可用或事务失败时返回；不区分"无事件"与"出错"。
func (s *Store) ClaimEvents(kind string, batchSize int) (events []Event, err error) {
	// ...
}
```

```go
// nextBackoff 按指数退避计算下次重连等待时长，并叠加 ±20% 随机 jitter，
// 避免大规模恢复时多个 Agent 同时重连冲垮中心。
//
// 参数 Parameters:
//   - attempt (uint32): 连续失败次数，从 1 开始；0 按 1 处理。
//   - base, max (time.Duration): 基准与上限时长，来自配置 retry_base / retry_max；
//     max < base 时直接返回 base。
//
// 返回 Returns:
//   - d (time.Duration): 本次应等待的时长，恒满足 base <= d <= max。
func nextBackoff(attempt uint32, base, max time.Duration) (d time.Duration) {
	// ...
}
```

#### 注释检查清单

- [ ] 注释以函数名开头，紧贴声明，中间无空行
- [ ] 说清「功能」；返回 `error` 时说清**错误条件**
- [ ] 参数/返回值超过一个或含义不自明时，逐项列出**名称 + 类型 + 含义**
- [ ] 涉及 IO、并发、副作用、阻塞/超时、幂等性的已显式标注
- [ ] 中英文混排：中文叙述，类型名/字段名/配置项/错误名保留英文原文
- [ ] 无 `// 获取 Xxx` 这类同义反复；`Deprecated:` 已用于废弃函数
- [ ] 提交前 `gofmt -l .` 无输出；导出函数可 `go doc <pkg> <Func>` 正常显示

> 包注释同理：每个 `package` 目录至少有一个文件顶部写 `// Package xxx ...` 说明包职责（同一包只写一处）。

---

## 10. 前端编码规范

- 通过 ESLint（V9）+ Prettier + Stylelint + EditorConfig（`web/` 已配置齐全）。提交前 `pnpm lint`。
- 组件名 `PascalCase`，文件名 `kebab-case`；`views/` 下按功能目录 + `index.vue`。
- 只用 Element Plus 组件与 UnoCSS，**禁止**新增 UI 框架。
- 请求统一走 `src/utils/request.ts`，**禁止**组件里裸用 axios。
- 实时数据走 SSE（`src/utils/sse.ts`）；**禁止**高频轮询刷新大盘。

---

## 11. 测试规范

现状：共 23 个 `*_test.go`（client 5、server 1、webserver 17），均为标准库 `testing`、单函数顺序断言；前端无测试。

**目标与要求**：

- 新增 Go 逻辑（配置解析、spool 限额、退避算法、状态机、告警去重、响应码映射）**必须带测试**，优先表驱动：

```go
func TestEvaluateState(t *testing.T) {
    tests := []struct {
        name string
        lastSeen time.Duration
        want string
    }{
        {"online", 1 * time.Second, "ONLINE"},
        {"suspect", 12 * time.Second, "SUSPECT"},
        {"offline", 20 * time.Second, "OFFLINE"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // ...
        })
    }
}
```

- 断言用 `t.Fatalf` / `t.Errorf`，**不引入 testify**（`webserver/go.mod` 虽声明但实际未使用，不要启用它）。
- 涉及时序/并发的改动，测试需 `-race` 通过。
- 改 proto / 配置结构后，**两端 `go test ./...` 都要过**。
- 前端暂无测试框架，**不要擅自引入 vitest**；质量保障靠 `pnpm type-check` + `pnpm lint`。

---

## 12. 构建与发布规范

统一：`CGO_ENABLED=0`，`-trimpath -ldflags "-s -w"`。

```powershell
# Windows 本机
$env:CGO_ENABLED = "0"
cd client    && go build -trimpath -ldflags "-s -w" -o ..\client\monitor-agent.exe .\cmd\monitor-agent
cd ..\server && go build -trimpath -ldflags "-s -w" -o .\collector.exe .\cmd\collector
cd ..\webserver && go build -trimpath -ldflags "-s -w" -o .\webserver.exe .\cmd\webserver

# 交叉编译 Linux（GOOS=linux，GOARCH=amd64|arm64 → dist/linux-<arch>/）
# 结束后务必 Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED
```

规则：

- 产物目录固定：`Monitor\`（Windows Agent）、`server\` / `webserver\`（Windows 中心）、`dist/linux-amd64|linux-arm64`（Linux）。
- Windows 覆盖 exe 前**先停进程**，否则写不进去。
- 前端：`pnpm install` → `pnpm build` → `web/dist`；生产由 Nginx 托管并反代 API 到 `:8090`。
- 交付清单：二进制 + 目标机 YAML + 证书（`0600`）；**禁止**拷贝本机 `data/webserver.db` 与带明文 token 的 YAML。
- Linux 落位：`/usr/local/bin/monitor-{collector,webserver}`、`/usr/local/bin/monitor-agent`、`/etc/monitor-server/`、`/etc/monitor-webserver/`、`/etc/monitor-agent/`。
- **不要提交**：`*.exe`、`dist/`、`data/*.db`、`*.pem`、日志、`.env`。

---

## 13. 脚本规范

脚本位于 `scripts/`，从自身路径**上溯三层**定位根目录，**禁止**硬编码绝对路径。

| 脚本 | 作用 |
| --- | --- |
| `setup-windows.ps1` / `setup-linux.sh` | 装依赖、证书、编译（**均不启动 InfluxDB**） |
| `windows/server/start.ps1` / `stop.ps1` | 中心启停 |
| `windows/agent/start.ps1` / `stop.ps1` | Agent 启停 |
| `linux/server/start.sh` / `stop.sh`、`linux/agent/start.sh` / `stop.sh` | Linux 对应（ Linux `start.sh` **不启 `pnpm dev`**） |
| `cert/Generate-mTLSCerts.ps1` | mTLS 证书 |

规则：

- **优先改脚本，不要让用户手敲命令**；新增组件必须同时提供 Windows + Linux 的 start/stop。
- 覆盖开关统一用环境变量（`COLLECTOR_BIN`、`WEBSERVER_BIN`、`AGENT_BIN`、`*_CONFIG`、`SKIP_INSTALL`、`MONITOR_ENV_AUTO=1`）。
- Linux 脚本需 `chmod +x`，并在脚本内 `source /etc/monitor/monitor.env`。
- 改脚本后必须实际执行验证，并在 `README.md` §8 同步表格。

---

## 14. 文档维护规范

改动若影响以下任一事实，**必须同步更新对应文档**，否则视为未完成：

| 变更 | 需更新 |
| --- | --- |
| 端口 / 进程 / 依赖版本 | `README.md` §3 §11、本文件 §2 §4 |
| 配置文件 / 环境变量 | `README.md` §13、子 README、本文件 §7 |
| 启动顺序 / 脚本 | `README.md` §7 §8、`doc/one-by-one-run.md` |
| proto / API | `server/README_ZH.md`、`webserver/README.md` §5、本文件 §6 |
| Agent/Telegraf 部署 | `doc/telegraf-monitor-agent-deployment.md` |
| Windows 本机细节 | `doc/windows-local-startup.md` |
| 架构不变式 | 本文件 §1.1 |

文档风格：中文、表格优先、命令可直接复制；路径用仓库相对路径。

---

## 15. Git 提交规范

- 未设置 `user.name/email` 时用 `git -c user.name=... -c user.email=... commit`，**不要改全局 config**。
- 提交信息用 Conventional Commits：`feat(agent): ...` / `fix(collector): ...` / `docs: ...` / `chore(build): ...`；scope 取 `agent|collector|webserver|web|scripts|docs|proto`。
- 描述变更原因与验证方式；前端可用 `pnpm commit` 交互生成。
- **禁止**提交密钥、证书私钥、`*.exe`、`dist/`、`data/`、`node_modules/`。

---

## 16. 完成定义（DoD）

改动可交付前，逐项自检：

- [ ] 落在正确的 module，未跨模块顺手改
- [ ] `gofmt -l .` 无输出；前端 `pnpm type-check` + `pnpm lint` 通过
- [ ] 新增/修改的 Go 函数已按 [§9.1](#91-函数注释规范强制) 补全中英文注释（功能、入参、返回、错误条件）
- [ ] 对应 module `go test ./...` 通过（涉并发加 `-race`）
- [ ] 改了 proto → **两端**重新生成并提交，重启两端验证
- [ ] 新增配置项有默认值 + 示例 + 文档
- [ ] 未引入 CGO 依赖、未新增端口（若新增已更新 §4）
- [ ] 未写入凭据/Token/私钥；日志不泄漏敏感信息
- [ ] Windows 与 Linux 脚本均已同步并可实际运行
- [ ] 相关 README / doc 已同步
- [ ] 端到端冒烟：Telegraf POST `:9510` 返回 `204` → Agent `:9511/healthz` 正常 → Collector 日志出现 `HEARTBEAT`/`METRICS` → `webserver :8090/healthz` 返回 `"code":"00000"` → 前端 :3000 可见数据

---

## 17. 常见陷阱（踩坑清单）

| 陷阱 | 正确做法 |
| --- | --- |
| 让 Telegraf 直连 InfluxDB | 违反 R1，必须经 Agent `:9510` |
| 把 Telegraf 输出写成中心地址 | 恒为 `http://127.0.0.1:9510/v1/telegraf/metrics`（R3） |
| 只生成一端 pb 代码 | 两端同时生成（§6） |
| `curl GET` `:9510` 看到 404 以为故障 | 预期行为，POST 成功返回 `204` |
| 在 `webserver/` 外运行 webserver | 工作目录必须是 `webserver/`，否则 SQLite 相对路径失效 |
| 以为 `bootstrap_admin_password` 只在首次生效 | 该值非空时**每次启动都会重置** admin 密码，找回入口后应置空 |
| 同一路径位置用两个不同的 `:参数名` | Gin 路由树会 panic（如 `/dicts/:id/form` 与 `/dicts/:dictCode/items` 冲突） |
| 为了权限需求去改 `monitor.go` / `views/monitor/*` | 监控模块是红线，改造不得影响其行为（详见 `webserver/docs/architecture.md` §6） |
| 删 buffer / SQLite 文件"修故障" | 会丢待补传指标（§8.9） |
| 用 npm/yarn 装 `web/` 依赖 | 只能 pnpm，`preinstall` 会拦截 |
| 启动顺序颠倒 | 中心：PG → InfluxDB → Collector → webserver → 前端；被监控：Agent → Telegraf |
| 覆盖 Windows exe 未停进程 | 先 `Stop-Process` 再 build |
| 前端用 `any` 糊类型 | 必须补 `types.ts` |
| 在 gRPC 处理路径同步写 Influx | 走 Outbox + `metricwriter` |
| 忘记清理 `GOOS/GOARCH/CGO_ENABLED` | 交叉编译后 `Remove-Item Env:...`，否则后续本机编译产物错误 |
