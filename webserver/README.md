# Monitor Webserver

监控平台的浏览器后台。给 Vue 控制台提供登录、动态菜单、权限管理与监控 API：权限治理数据与 Agent 资产 / 心跳 / 告警都在 PostgreSQL，指标曲线在 InfluxDB。

日常只要关心这一条进程：

```text
cmd/webserver  →  webserver.exe（Windows）或 webserver（Linux）
配置           →  configs/webserver.yaml
默认监听       →  http://127.0.0.1:8090
```

两套数据库，各管各的：

| 库 | 位置 | 内容 |
| --- | --- | --- |
| PostgreSQL | `127.0.0.1:5432`（`postgres` 库） | 权限与治理数据：`sa_system_user/role/dept/menu/post/dict_*/config*/notice*/login_log/oper_log/user_*/role_*` 等；监控数据：`agents`、`alerts`、`service_probe_states`、`docker_container_states`。两类数据**同库**，靠 `sa_system_` 前缀区分 |
| InfluxDB 2 | `127.0.0.1:8086` | 指标曲线（CPU / 内存 / 磁盘 / 网络…） |

权限库原先用 SQLite 文件（`data/webserver.db`），受单写者模型限制，并发一高就报 `database is locked`（当时的规避手段是把连接数限制为 1，等于所有请求串行排队）。现已迁到 PostgreSQL：连接可并发、不再有文件锁。启动时自动建表并写入种子数据（幂等），并按需校正自增序列。

---

## 1. 依赖

运行前中心机上要已经有：

| 依赖 | 本机默认 | 用途 |
| --- | --- | --- |
| Go 1.27+ | 构建机需要 | 编译；纯运行只要现成 exe |
| PostgreSQL | `127.0.0.1:5432`，库 `postgres` | Agent 列表、心跳、告警 |
| InfluxDB 2 | `http://127.0.0.1:8086` | CPU / 内存等趋势 |
| Collector | `:9500` | 先把 Agent 数据写入上面两个库 |

前端在仓库根目录的 `web/`，开发时 `pnpm dev`（`:3000`），`.env.development` 把 `/dev-api` 代理到 `http://localhost:8090`。

---

## 2. 配置

编辑 [`configs/webserver.yaml`](configs/webserver.yaml)。工作目录必须是 `webserver/`，相对路径 `backup_dir`、`-config` 才找得到。

```yaml
http_address: ":8090"          # 监听地址，空 host 表示所有网卡
access_log: false              # true 时控制台滚动打印每次 API 和耗时
backup_dir: "data/backup"      # 计划任务的备份产物目录

postgres:
  host: "localhost"
  port: 5432
  user: "postgres"
  password: "123456"
  database: "postgres"
  ssl_mode: "disable"

influx:
  url: "http://localhost:8086"
  organization: "hkc"
  bucket: "bucket"
  token: "<InfluxDB write token>"

jwt:
  secret: "<至少一段随机串>"
  access_seconds: 7200
  refresh_days: 30

# 非空时每次启动都会把 admin 的密码重置为该值（便于找回入口）；置空后不再重置
bootstrap_admin_password: ""

# 登录验证码（enabled 缺省 true；置 false 后登录跳过验证码校验，仅建议联调时使用）
captcha:
  enabled: true
  length: 4            # 验证码字符数
  ttl_seconds: 180     # 有效期
  max_items: 1000      # 进程内同时存在的验证码上限
  width: 120           # 图片宽度（像素）
  height: 44           # 图片高度（像素）

# 附件上传（文件按 upload/年/月/日/ 存放，通过 /upload/... 静态访问）
upload:
  dir: "upload"        # 存储目录，相对进程工作目录
  url_prefix: "/upload"  # 静态访问前缀
  max_size_mb: 50      # 单文件上限；0 取默认 50，负数表示不限制

# 缓存管理（系统管理 → 缓存管理）：接入 Redis 后可浏览键值并清理缓存
redis:
  enabled: true        # false 时页面只展示进程内缓存
  addr: "127.0.0.1:6379"
  username: ""
  password: ""
  db: 0
  databases: [0]       # 需要展示的库，可写多个
  scan_limit: 5000     # 单库最多扫描的键数
  timeout_seconds: 5
  # groups:            # 自定义分组（可选）
  #   - name: "登录会话"
  #     pattern: "token:*"

# 业务缓存（字典、系统配置、IP 归属地）。Redis 不可用时自动退化为进程内 TTL 缓存
cache:
  enabled: true
  prefix: "monitor:"
  ttl_seconds: 1800

# IP 归属地（登录地点 / 在线用户 / 操作日志）
ip_location:
  enabled: true        # 关闭后统一显示“未解析(已关闭归属地查询)”
  provider: "baidu"    # baidu（默认，国内毫秒级）/ ipapi / pconline / custom
  endpoint: ""         # provider=custom 时的地址模板，含 {ip}
  timeout_seconds: 6
  sync_wait_ms: 1500   # 登录最多等待归属地结果的毫秒数，超时不阻塞登录
  cache_ttl_hours: 168

# 演示模式：开启后除白名单外的写操作（POST/PUT/PATCH/DELETE）一律 403 / B0206
demo_enabled: false
demo_whitelist: []     # 追加放行路径（默认已含登录、验证码、退出、刷新令牌）

# 安全中间件：cors 复用 pkg/middleware.CORS；xss 清洗输入；xsrf 校验 Origin/Referer
middleware:
  cors:
    allow_origins: ["*"]
    allow_methods: [GET, POST, PUT, PATCH, DELETE, OPTIONS]
    allow_headers: [Origin, Content-Type, Authorization, X-Request-ID]
    allow_credentials: false
  xss:
    enabled: true
  xsrf:
    enabled: true
    allow_origins: []   # 留空=只校验同源；"*" 不参与放行

# 日志：复用 pkg/logger（zap + lumberjack），同时写控制台与文件
log:
  service: webserver
  environment: local
  level: info
  encoding: json
  file:
    enabled: true
    filename: logs/app.log
    max_size_mb: 128
    max_backups: 14
    max_age_days: 7
    compress: true
```

### 环境变量（覆盖 YAML）

| 变量 | 覆盖项 |
| --- | --- |
| `WEBSERVER_JWT_SECRET` | `jwt.secret`（必填，YAML 和环境变量至少有一个） |
| `WEBSERVER_ADMIN_PASSWORD` | `bootstrap_admin_password`，重置/创建 `admin` 密码 |
| `MONITOR_POSTGRES_PASSWORD` | `postgres.password` |
| `MONITOR_INFLUX_TOKEN` | `influx.token` |
| `WEBSERVER_DEMO_ENABLED` | `demo_enabled`（`true` / `1` 开启，`false` / 其它值关闭） |

生产把口令和 token 放环境变量，不要把真实 token 提交进仓库。

### 访问日志

`access_log: true` 后，打开 Web 页时控制台会滚：

```text
[2026-09-29 21:02:13] GET http://192.168.1.16:8090/api/v1/monitor/overview 200 12.4 ms
```

`/healthz` 和 SSE 长连接不打。改配置后要重启进程。同一条访问记录也会写进 `logs/app.log`（zap，字段 `line`），便于事后排查。

---

## 3. 运行

启动顺序：PostgreSQL → InfluxDB → Collector →（可选 Agent/Telegraf）→ **webserver** → 前端。

### 3.1 本机一键（推荐）

在仓库根目录：

```powershell
powershell -File .\scripts\windows\server\start.ps1
```

脚本会在缺少 `webserver\webserver.exe` 时自动编译，用 `configs\webserver.yaml` 拉起进程。

停中心进程（含 webserver）：

```powershell
powershell -File .\scripts\windows\server\stop.ps1
```

Linux 中心机对应 `scripts/linux/server/start.sh`。

### 3.2 源码直接跑

```powershell
Set-Location C:\Users\Administrator\Desktop\monitor\webserver
$env:CGO_ENABLED = "0"
$env:WEBSERVER_ADMIN_PASSWORD = "Monitor123!"
# YAML 里已有 jwt.secret 时可省略下一行
# $env:WEBSERVER_JWT_SECRET = "change-this-development-secret"
go run .\cmd\webserver -config .\configs\webserver.yaml
```

Linux / macOS：

```bash
cd webserver
CGO_ENABLED=0 WEBSERVER_ADMIN_PASSWORD='Monitor123!' \
  go run ./cmd/webserver -config ./configs/webserver.yaml
```

### 3.3 跑已经编好的二进制

```powershell
Set-Location C:\Users\Administrator\Desktop\monitor\webserver
.\webserver.exe -config .\configs\webserver.yaml
```

参数只有一个：`-config`，默认 `configs/webserver.yaml`。

### 3.4 启动成功长什么样

端口听起来之后，控制台类似：

```text
[2026-09-29 21:01:51] 中心服务器webserver端启动成功
[2026-09-29 21:01:51] 操作系统  Windows 10.0.26100 amd64
[2026-09-29 21:01:51] PostgreSQL 18.4
[2026-09-29 21:01:51] 正在监听地址:  http://127.0.0.1:8090
[2026-09-29 21:01:51] 正在监听地址:  http://192.168.1.14:8090
```

健康检查：

```powershell
curl http://127.0.0.1:8090/healthz
```

应返回 `"code":"00000"`。浏览器打开前端 `http://localhost:3000`，账号 `admin` / 初始密码（`bootstrap_admin_password`，本机默认 `Monitor123!`）。

---

## 4. 权限库与初始数据

### 4.1 结构与来源

* 表结构由项目初期的 MySQL 建表脚本（Navicat 导出，现已不在仓库内）逐字段翻译为 PostgreSQL：`bigint AUTO_INCREMENT → integer GENERATED BY DEFAULT AS IDENTITY`、`datetime → timestamp`、`tinyint → integer`；`tenant_id` 等租户列与 `created_by/updated_by/create_time/update_time/delete_time` 审计列全部保留（系统不实现租户功能，租户列只按 init.sql 默认值写入）。
* DDL 与种子 SQL 内嵌在 `internal/app/sql/`（`001_schema.sql`、`002_menu_seed.sql`、`003_system_seed.sql`），随二进制发布，启动时幂等执行（种子用 `ON CONFLICT DO NOTHING`）：
  * `001` 建表，并清理早期模板遗留表 `web_users/web_roles/web_menus/...`；
  * `002` 写入菜单与按钮权限（含原监控中心菜单）以及角色菜单授权；
  * `003` 写入租户、角色、部门、岗位、字典、配置等基础数据。
* `database/init.sql` 是新环境可直接导入的 PostgreSQL 范本（结构 + 权限与基础配置数据 + 索引 + 序列 + setval），用 `go run ./tools/pgdump -out ../database/init.sql -exclude-data <运行期表清单>` 生成：日志/告警/会话/事件类表与账号数据只保留结构，不落任何口令哈希，首次启动由启动迁移创建 admin。计划任务的 `backupPostgres` 与它同一实现（`internal/pgdump`），产物是带全量数据的同格式备份，同样可直接 `psql -f` 导入。
* 为对齐前端契约新增的少量扩展列（均在 SQL 中以 `[扩展]` 注释标出）：`sa_system_menu.is_always_show/redirect/params`、`sa_system_dict_data.tag_type`、`sa_system_notice.*`（发布状态等）、`sa_system_oper_log.action_type/operator_id/device/browser/os/status/error_msg`、`sa_system_notice_read`、`sa_system_meta`。
* 时间统一按本机本地时区写入；早期种子数据（UTC）会在首次启动时通过一次性订正脚本换算为本地时间，`sa_system_meta` 中的标记保证只执行一次。

### 4.2 初始账号与角色

| 账号 | 密码 | 说明 |
| --- | --- | --- |
| `admin` | `bootstrap_admin_password`（本机默认 `Monitor123!`） | 内置超级管理员，`is_super=1`，跳过权限校验、拥有全部菜单与权限标识 |

种子角色：`super_admin`（超级管理员，全部数据）、`ops`（运维人员，本部门及下属，可看监控与日志）、`viewer`（只读访客，仅本部门，可看监控）。
种子部门：监控平台 / 平台运维部 / 研发中心 / 后端组 / 前端组。种子岗位：系统管理员 / 运维工程师 / 开发工程师。

### 4.3 鉴权与审计

* 访问令牌为 HS256 JWT，载荷含 `sub`、`username`、`isSuper`；刷新令牌只存 SHA-256 指纹，支持退出与改密后批量吊销。
* 请求校验分两层：`requireAuth`（登录态）与 `requirePerm(<权限标识>)`（写操作按 `sa_system_menu.slug` 判定，越权返回 `A0301`）。
* 登录/退出写 `sa_system_login_log`（IP、归属地、操作系统、浏览器、状态、提示消息）。
* 非 GET 的 `/api/v1/*` 请求由中间件写 `sa_system_oper_log`（模块、操作类型、标题、请求体脱敏后最多 2KB、耗时、结果、错误信息）。
* 权限集合与数据权限范围（`data_scope`：1 全部 / 2 本部门及下属 / 3 本部门 / 4 本人 / 5 自定义）在内存缓存 60 秒，角色菜单变更时主动失效。

---

## 5. HTTP API

前缀 `/api/v1`。响应统一为 `{"code","msg","data"}`，成功码 `00000`；分页统一 `pageNum`/`pageSize`，返回 `{"list":[],"total":0}`。除 `/healthz`、`/api/v1/auth/captcha`、登录 / 刷新外都要 `Authorization: Bearer <accessToken>`。

### 5.1 认证与当前用户

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/healthz` | 探活 |
| GET | `/api/v1/auth/captcha` | 登录验证码（返回 `captchaId` + PNG Data URI） |
| POST | `/api/v1/auth/login` | 登录（启用验证码时需带 `captchaId` / `captchaCode`） |
| POST | `/api/v1/auth/refresh-token` | 刷新（`refreshToken` 支持 query 传参） |
| DELETE | `/api/v1/auth/logout` | 退出 |
| GET | `/api/v1/users/me` | 当前用户（角色、权限标识、是否超管） |
| GET | `/api/v1/menus/routes` | 动态菜单（按角色过滤，数据库驱动） |

### 5.2 监控模块（行为保持不变）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/monitor/overview` | 总览 |
| GET | `/api/v1/monitor/agents` | 服务器列表 |
| GET | `/api/v1/monitor/agents/:id` | 服务器详情 |
| GET | `/api/v1/monitor/agents/:id/snapshot` | **主机概况当前值**（CPU / 内存 / 交换分区 / 负载 / 进程 / 连接数 / 磁盘 / 磁盘 IO / 网卡） |
| GET | `/api/v1/monitor/agents/:id/metrics` | 时序（Influx），支持字段过滤与聚合降采样 |
| GET | `/api/v1/monitor/services/:id` | 服务探测状态 |
| GET | `/api/v1/monitor/containers/:id` | 容器状态 |
| GET | `/api/v1/monitor/alerts` | 告警（支持 `agentId`/`state`/`level`/`acknowledged`） |
| GET | `/api/v1/monitor/maintenance` | 维护窗口 |
| GET | `/api/v1/sse/connect` | SSE（`monitor-agent-status`、`dict` 等主题） |

#### 5.2.1 时序查询参数（`/agents/:id/metrics`）

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `measurement` | `cpu` | 指标类型，白名单：`cpu/mem/swap/disk/diskio/net/system/processes/procstat/netstat/mysql/redis/postgresql/rabbitmq/http_response/docker/win_cpu/win_memory/win_system` |
| `start` / `end` | `-1h` / 空 | 查询区间，相对时长（`-30m`、`-1h`、`-7d`）或 RFC3339；`end` 空表示到 `now()` |
| `field` | 空 | 字段白名单，逗号分隔（`usage_user,usage_system`），窗口计数器字段含空格也可（`% Processor Time`） |
| `aggregate` | 空 | `mean/median/max/min/sum/first/last/count`，或 `rate`（计数器 → 每秒速率：`aggregateWindow(last)` → `derivative(1s)` → `aggregateWindow(mean)`） |
| `window` | 有聚合时 `1m` | 聚合窗口（`30s`/`1m`/`5m`/`1h`…，上限 1 天） |
| `limit` | `5000` | 单序列点数上限，最大 20000 |

> 参数非法（measurement / field / aggregate / window 不在白名单或格式不合法）统一返回 400 `A0001`；Influx 不可用返回 502 `B0201`。原始点语义（不带 `aggregate`）与迁移前一致。
>
> **Windows 计数器字段名**：`win_perf_counters` 会把计数器名规范化后落库（`% Processor Time` → `Percent_Processor_Time`、`System Calls/sec` → `System_Calls_persec`、空格 → `_`），按原名查会取不到值。`internal/monitor` 的 `winCounterField` / `latestWinCounter` 同时兼容两种写法；详情页的 CPU / 内存已统一改用原生 `cpu` / `mem` 插件，不再依赖该规范化规则。

#### 5.2.2 主机概况快照（`/agents/:id/snapshot`）

返回该机最近 3 分钟的点现算出的当前值（不落库、不做缓存）：

* `cpu`：`total/user/system/iowait/idle`，取 **Telegraf 原生 `cpu` 插件**（两端字段名一致，优先 `cpu-total` 汇总序列）；`source` 为空表示该机没采到 cpu 指标，前端显示「暂无数据」而不是 0%；
* `memory` / `swap`：字节数 + 使用率，同样取原生 `mem` / `swap`（凭据缺失时退回 `agents` 表的心跳值）；
* `load`：`load1/load5/load15`（Linux）与运行时长；Windows 原生 `system` 插件的 load 恒为 0，故改回填 `win_system` 的 `queueLength` 并用 `hasQueue` 标明是否真的采到；
* `processes`：`total/running/sleeping/blocked/zombies`；
* `netstat`：`tcp/udp/total`（未启用 Telegraf `inputs.netstat` 时三项为 0，前端回退显示心跳上报的监听端口数）；
* `disks`：按 `path` 分组的 `totalBytes/usedBytes/usedPercent`（磁盘大小由前端求和）；
* `diskIo`：按 `name` 分组的 `readBytesPerSec/writeBytesPerSec/readIops/writeIops`；
* `networks`：按 `interface` 分组的 `upBytesPerSec/downBytesPerSec`；
* `collectedAt`：快照生成时间。

> 单个 measurement 查询失败时该字段降级为零值并记 `slog.Warn`，只有全部失败才返回 502。
> 硬件身份类字段（SN、CPU 主频、网卡链路速率）当前采集链路未提供，前端显示 `-` 并提示缺失原因。

### 5.3 系统管理

| 模块 | 读接口 | 写接口（需权限标识） |
| --- | --- | --- |
| 用户 | `GET /users`、`/users/:id/form`、`/users/:id/menu-ids`、`/users/options`、`/users/template`、`/users/export` | `POST /users`、`PUT /users/:id`、`PUT /users/:id/menus`（`sys:user:assign-menu`）、`PUT /users/:id/password/reset`、`DELETE /users/:ids`、`POST /users/import` |
| 个人中心 | `GET /users/profile` | `PUT /users/profile`、`PUT /users/password` |
| 角色 | `GET /roles`、`/roles/:id/form`、`/roles/:id/menu-ids`、`/roles/:id/dept-ids`、`/roles/options`、`/roles/code-options` | `POST /roles`、`PUT /roles/:id`、`PUT /roles/:id/menus`、`DELETE /roles/:ids` |
| 部门 | `GET /depts`、`/depts/:id/form`、`/depts/options` | `POST /depts`、`PUT /depts/:id`、`DELETE /depts/:ids` |
| 菜单 | `GET /menus`、`/menus/:id/form`、`/menus/options` | `POST /menus`、`PUT /menus/:id`、`DELETE /menus/:id` |
| 岗位 | `GET /posts`、`/posts/:id/form`、`/posts/options` | `POST /posts`、`PUT /posts/:id`、`DELETE /posts/:ids` |
| 字典 | `GET /dicts`、`/dicts/options`、`/dicts/:dictCode/items`、`/dicts/:dictCode/items/options`、`/dicts/:dictCode/items/:id/form`、`/dicts/:dictCode/form` | `POST /dicts`、`PUT /dicts/:dictCode`、`DELETE /dicts/:dictCode`、`POST /dicts/:dictCode/items`、`PUT /dicts/:dictCode/items/:id`、`DELETE /dicts/:dictCode/items/:ids` |
| 配置 | `GET /configs`、`/configs/:id/form` | `POST /configs`、`PUT /configs/:id`、`PUT /configs/refresh`、`DELETE /configs/:id` |
| 公告 | `GET /notices`、`/notices/my`、`/notices/:id/form`、`/notices/:id/detail` | `POST /notices`、`PUT /notices/:id`、`PUT /notices/:id/publish`、`PUT /notices/:id/revoke`、`PUT /notices/read-all`、`DELETE /notices/:ids` |
| 日志 | `GET /logs`（操作）、`GET /logs/login`（登录）、`GET /logs/analytics/trend`、`GET /logs/analytics/overview` | `DELETE /logs/:ids`、`DELETE /logs/login/:ids` |
| 附件 | `GET /attachments`（分页，支持 `categoryId`/`keywords`）、`GET /attachment-categories`（分类树） | `POST /attachments`（multipart 上传）、`PUT /attachments/:id`（重命名/改分类）、`DELETE /attachments/:id`、`POST /attachment-categories`、`PUT /attachment-categories/:id`、`DELETE /attachment-categories/:id` |
| 通用文件 | — | `POST /files`（multipart，返回 `{name,url}`）、`DELETE /files?filePath=`（供前端模板上传组件与头像上传复用，仅需登录态） |
| 缓存 | `GET /cache/overview`、`GET /cache/groups`、`GET /cache/keys?group=&keywords=`、`GET /cache/value?group=&key=` | `DELETE /cache/keys?group=&keys=a,b`（清理指定键）、`DELETE /cache/groups?group=`（清理整组） |
| 在线用户 | `GET /online-users`（分页，支持 `username`/`ip`/`keywords`）、`GET /online-users/overview` | `DELETE /online-users/:id`（强制下线，`sys:online:kick`） |

### 5.4 附件存储

* 文件落在 `upload/年/月/日/` 下（目录由 `upload.dir` 配置，默认 `webserver/upload`），落盘名形如 `1784390696210_44fcb67fc6044be9.png`，**不改动原文件名**，重命名只更新 `sa_system_attachment.origin_name`。
* 静态访问走 `GET /upload/...`（由 `upload.url_prefix` 配置），该路径不做登录校验以便 `<img>` / `<a>` 直接引用，响应统一带 `X-Content-Type-Options: nosniff`。
* 单文件大小上限 `upload.max_size_mb`（默认 50，填负数表示不限制）。
* 通用文件接口 `POST /api/v1/files` 与前端模板自带上传组件（`SingleImageUpload` / `FileUpload` 等）契约一致，用户头像上传即复用该接口，上传结果写入 `sa_system_user.avatar`（归类为「未分类」，可在附件管理里调整）。

> 路径参数说明：Gin 要求同一路径位置的参数名一致，因此字典类型的表单/更新/删除接口内部参数名为 `:dictCode`（承载的仍是字典类型主键），对外路径与前端契约一致。
>
> 短信 / 邮件验证码相关接口（`/users/mobile/code`、`/users/email/code`、`/users/mobile`、`/users/email`）当前返回「未接入短信/邮件服务」的业务提示。

权限标识（写接口使用，超管直通）：`sys:user:*`、`sys:role:*`、`sys:dept:*`、`sys:menu:*`、`sys:post:*`、`sys:dict:*`、`sys:config:*`、`sys:notice:*`、`sys:log:delete`、`sys:login-log:delete`、`sys:attachment:list`、`sys:attachment:upload`、`sys:attachment:update`、`sys:attachment:delete`、`sys:attachment:category:create/update/delete`、`sys:cache:list`、`sys:cache:clear`、`sys:online:list`、`sys:online:kick`。

### 5.5 令牌、会话与在线用户

* **access token**：HS256 JWT（有效期 `jwt.access_seconds`，默认 2h），**无状态**，只验签 + 按 `sub` 查用户；载荷为 `sub`（用户 ID）、`username`、`isSuper`、**`sid`（会话编号）**、`exp`、`iat`。因为它无状态，把它写进缓存没有收益，所以不缓存。
* **refresh token**：随机 32 字节，明文只下发一次，库里只存 `sha256` 指纹（`sa_system_refresh_token.token_hash`），权威数据**始终在权限库（PostgreSQL）**，不写缓存——缓存一丢就全体掉登录，得不偿失。
* **会话 = 一条未吊销且未过期的刷新令牌**：`id`（UUID）就是会话编号，也是 JWT 的 `sid`。登录时把 IP、归属地、设备、系统、浏览器、最近活跃时间一并写入该行，在线用户列表直接以它为数据源。
* **强制下线（强退）**：吊销该会话（`revoked_at`）**并**登记 `sid` 失效标记（`monitor:session:revoked:{sid}`，TTL 取 access token 有效期）。`RequireAuth` 每请求校验该标记，因此被强退的 access token **立即失效**（不必等 2h 过期）；退出登录同理。
* 缓存里只放**派生/可重建**的数据（字典、配置、归属地、失效标记），不放权威会话数据。

### 5.6 缓存管理

* 分组规则：Redis 分组名取键名前缀（首个 `:` 或 `.` 及其之前的部分），例如 `sys_config:site_name` 属于分组 `sys_config:`、`token:user_sessions:1` 属于分组 `token:`；也可用 `redis.groups` 按业务语义自定义分组（`name` + `pattern`）。
* 单库最多扫描 `redis.scan_limit` 个键（SCAN 分页拉取），超大库只展示前若干键，避免拖垮页面。
* 除 Redis 外，内置两个**进程内缓存**分组，便于运维直接清理：
  * `permission`：权限快照缓存（TTL 60s，清理后下一次请求重新加载）；
  * `captcha`：登录验证码缓存（清理后待使用的验证码立即失效）。
* 键详情按类型读取：string 原文（JSON 自动缩进）、hash/list/set/zset 转为 JSON，单次最多读取 1000 个元素、返回内容上限 64KB（超出标记截断）。
* 清理操作会写入操作日志（`DELETE /api/v1/cache/*`）。
* 写入缓存的业务键（`cache.prefix` 统一前缀，默认 `monitor:`）：

| 键 | 内容 | 失效时机 |
| --- | --- | --- |
| `monitor:dict:items:{code}` | 字典项下拉数据（`DictSelect`/`DictTag` 高频读取） | 该字典项增删改、字典类型变更（并同时发 SSE 通知前端） |
| `monitor:dict:types` | 字典类型下拉 | 字典类型增删改 |
| `monitor:config:all` | 全量系统配置键值 | 配置增删改；启动时预热；「刷新缓存」按钮强制重载 |
| `monitor:iploc:{ip}` | IP 归属地结果 | 到期（`cache_ttl_hours`）或手工清理 |
| `monitor:session:revoked:{sid}` | 被强退会话的失效标记 | 到期（取 access token 有效期） |

### 5.7 IP 归属地

* 默认数据源 **`baidu`**（`https://opendata.baidu.com/api.php?...&resource_id=6006`）：国内直连、免 key、返回中文、实测 ~300ms；`ipapi`（ip-api.com）为海外备选（国内 3~8s），`pconline` 常被 403，`custom` 支持自建接口模板。
* 内网/回环地址直接返回「内网地址」，不发起请求；查询失败返回「未解析」，**失败结果不写缓存**，避免固化瞬时故障。
* 登录路径最多等待 `ip_location.sync_wait_ms`（默认 1.5s）：结果及时返回就同步落库（在线用户、登录日志一并带上归属地），超时则先落兜底标签、查询转后台补写缓存，**不会把上游延迟转嫁给登录请求**。
* 操作日志走的也是同一个解析器，但用**非阻塞**取法（命中缓存直接返回，否则异步预取），不给写请求增加延迟。
* 同一 IP 的并发查询会合并为一次上游请求（单飞），避免缓存击穿。

### 5.8 演示模式与安全中间件

全局中间件顺序（`api/router/router.go`）：

```text
gin.Recovery → SecurityHeaders → RequestID → CORS → XSS → XSRF → 演示模式 → 访问日志 → 操作日志
```

| 能力 | 实现 | 说明 |
| --- | --- | --- |
| 演示模式 | `api/middleware/demo.go`（本项目新增） | `demo_enabled: true` 后，POST/PUT/PATCH/DELETE 一律 403 / `B0206`；白名单固定含登录、验证码、退出、刷新令牌，可用 `demo_whitelist` 追加 |
| CORS | **复用** `pkg/middleware.CORS` | 原版组件，已含预检（OPTIONS → 204）与 `Vary: Origin` 处理 |
| 请求 ID | **复用** `pkg/middleware.RequestID` | 生成 `X-Request-ID` 并写入上下文，贯穿访问日志与响应头 |
| XSS | `api/middleware/xss.go`（本项目新增） | 清洗 query、urlencoded/multipart 表单文本字段、JSON 体中的字符串：成对脚本标签连内容一起移除、残留脚本标签移除、`javascript:`/`vbscript:` 伪协议移除、`on*` 事件属性清空。**不做全量 HTML 转义**，避免破坏公告富文本；超过 1MB 的请求体不参与清洗（多为文件上传） |
| XSRF | `api/middleware/csrf.go`（本项目新增） | 写方法校验 Origin（缺失回落 Referer）：无 Origin/Referer 放行（curl、服务间调用）、`null` 拒绝、命中 `xsrf.allow_origins` / 与请求 Host（含 `X-Forwarded-Host`）同源 / **双方都是本机回环地址** 放行、其余 403 / `B0205`（同时打一条 warn，含 origin 与 host 便于排查）。凭证是 Authorization 头而非 Cookie，因此无需双提交令牌 |

`pkg/middleware/jwt.go`（网关风格 `user_id`/`role` claims）**未启用**：本服务的鉴权是会话式的（`RequireAuth` + `sid` 强退 + 权限标识），两者载荷与吊销语义不同，直接替换会丢掉强制下线与权限判定能力。

**XSRF 与本地开发**：前端 dev server（`http://127.0.0.1:3000`）通过 Vite 代理访问后端时 `changeOrigin: true` 会把 Host 改写成 `localhost:8090`，导致 Origin 与 Host “不同源”。已按两种方式处理：Origin 与请求 Host 均为本机回环时放行；`xsrf.allow_origins` 里默认列了 `localhost:3000` 与 `127.0.0.1:3000`。若用局域网 IP（如 `http://192.168.1.16:3000`）或生产站点与后端不同源，把该来源加进 `allow_origins` 即可；`allow_origins: ["*"]` 表示完全不校验。

### 5.9 运行日志

* **复用** `pkg/logger`（`go.uber.org/zap` + `lumberjack`）：默认写控制台 + `logs/app.log`，按 `max_size_mb` 轮转、保留 `max_backups` 份、`max_age_days` 天后清理、`compress` 压缩。
* `internal/logx` 做了两件事：按配置装配 zap；实现 `slog.Handler` 把标准库 `slog` 桥接到 zap。因此既有代码里的 `slog.*`（业务告警、GORM 慢查询/错误）无需改造即可落盘，`caller` 通过 `AddCallerSkip` 指回真实调用点。
* `log.encoding: console` 可切文本编码；`log.file.enabled: false` 只写控制台。

### 5.10 菜单授权（用户个人菜单 + 角色菜单）

前端在「用户管理」「角色管理」列表的操作列各有一个**菜单设置**按钮，弹窗是同一套 `MenuPermissionDialog`（搜索、展开/收缩、父子联动、el-tree 多选）。

* **数据表**：角色菜单落 `sa_system_role_menu`（唯一键 `(role_id, menu_id)`）；用户个人菜单落 `sa_system_user_menu`（唯一键 `(user_id, menu_id, tenant_id)`，表里虽有 `delete_time`）。两表都按**物理删除**处理关联行——唯一索引不含 `delete_time`，软删除会让旧行继续占位，重新授权同一菜单必然撞唯一约束。
* **接口**：`GET /users/:id/menu-ids`（回显）、`PUT /users/:id/menus`（覆盖式保存，`sys:user:assign-menu`）；角色侧沿用 `GET /roles/:id/menu-ids`、`PUT /roles/:id/menus`（`sys:role:assign-menu`）。请求体都是菜单 ID 的数字数组。
* **父级链路自动补齐**：保存时按 `sa_system_menu.parent_id` 向上补齐所有祖先（`logic.CompleteParentMenus`，用户与角色共用），避免只勾了按钮却因父级目录缺失导致整棵菜单不显示；前端提交时也会带上半选父节点。
* **只读授权入口（列表按钮）**：每个系统模块菜单下都有一个 `列表` 按钮（slug 形如 `sys:user:list`，种子 id 规则为 `<模块菜单id>末尾补0>`，如 `2010`）。**父子联动开启时只勾它**即可得到「能进页面、没有任何操作按钮」的只读授权（`me.perms` 只有 `sys:<模块>:list`，写接口返回 403/A0301）；否则勾模块菜单会把该模块下所有按钮一并勾上。关闭父子联动后只勾模块菜单节点，效果相同。
  > 该按钮的 slug 与模块菜单节点（type=2）自身携带的 slug **重复**：库内没有 slug 唯一索引、`MenuLogic` 也没有唯一性校验，权限集合按 map 去重，因此功能上无副作用。但**不要**顺手给列表接口加上 `sys:<模块>:list` 的鉴权——从未勾过该按钮的既有角色会突然 403（列表接口目前只要求登录态）。
  > 监控中心的 4 个菜单（`101`–`104`）下面没有按钮节点，勾选它们本身就是只读，无需额外入口。
* **菜单可见性与接口鉴权是两套口径，务必保持一致**：侧边栏按**菜单 ID**（`sa_system_role_menu` / `sa_system_user_menu`）渲染，接口守卫按 **slug**（`guard("sys:online:list")`）判定。若某菜单行在库里 `slug` 为空，就会出现「菜单看得见、点进去提示权限不足」——附件管理/缓存管理/在线用户曾经就是这个状态（早期插入的行没有 slug，而种子用 `INSERT OR IGNORE`，后来在种子里补的 slug 更新不到已存在的行）。`002_menu_seed.sql` 末尾因此加了一段**幂等的 slug 回填**（只填空值、不覆盖人工修改）：新增内置菜单并带 slug 时，请在那里补一行。
  > 另外注意 `internal/app/sql/*.sql` 是通过 `//go:embed` 编进二进制的——**改完种子必须 `go build` 重新编译**再重启，否则不生效。
* **菜单 = 角色菜单 ∪ 个人菜单**：非超管登录后取 `GET /api/v1/menus/routes`，后端合并「其角色关联的 `sa_system_role_menu`」与「其个人 `sa_system_user_menu`」再过滤菜单表；个人菜单的 `slug` 同时并入权限集合（`permission.Load`），否则侧边栏有菜单但页面内按钮会被 `v-hasPerm` 隐藏。无角色、仅被单独授权菜单的用户也能正常看到菜单。
* **超级管理员**：`is_super = 1` 或 `id = 1` 直接返回全部菜单，且**不允许单独设置**——`PUT /users/:id/menus` 返回 400「超级管理员默认拥有全部菜单，无需单独设置」，前端该按钮为禁用态。
* **缓存**：用户菜单保存后 `permission.Invalidate(userID)`，下次请求即生效；角色菜单保存后 `InvalidateAll()`。
* **演示模式**：这两项保存都是写操作，`demo_enabled: true` 时会被演示模式中间件拦下（403 / B0206）。要配置菜单请先关闭演示模式。

### 5.11 远程运维（SSH 终端 / RDP 远程桌面 / 操作审计）

在「监控中心 → 服务器列表」操作栏为每台服务器增加「连接信息 / SSH / RDP」三个入口：

* **SSH 终端**（Linux）：浏览器内 Web 终端连接目标机，支持「用户名 + 密码 + 端口」与「SSH 私钥」两种认证。技术栈 `golang.org/x/crypto/ssh`（官方 SSH 客户端）+ `gorilla/websocket` + 前端 `xterm.js`。
* **SSH 操作审计**：pty 输出流旁路录制为 **asciinema v2** 格式（可在会话审计页回放），同时按回车聚合**命令流**落库；会话日志记录操作人 / 时间 / 目标机 / 来源 IP / 时长 / 断开原因。
* **RDP 远程桌面**（Windows 3389）：经 **Apache Guacamole** 嵌入（iframe），后端调 Guacamole REST API 生成一次性 token，浏览器内直接操作。Guacamole 未启用时 RDP 入口隐藏、SSH 不受影响。
* **凭据安全**：SSH/RDP 的密码与私钥以 **AES-256-GCM** 加密入库（密钥来自 `remote.secret_key` / `WEBSERVER_SECRET_KEY`，留空回退 `jwt.secret`）；明文永不下发前端——编辑回显「已设置」，留空表示不修改。

#### 配置（webserver.yaml）

```yaml
# 远程运维：SSH 终端 + RDP + 操作审计
remote:
  secret_key: ""                 # 凭据加密密钥；留空回退 jwt.secret，建议 WEBSERVER_SECRET_KEY 覆盖
  recording_dir: "data/ssh-recordings"  # asciinema 会话录像目录（按日期分子目录）

# 远程桌面（Apache Guacamole 的 guacd 网关）：RDP / VNC。
# 只部署 guacd 一个 C 守护进程，不需要 guacamole-client（Java/Tomcat/MySQL）。
guac:
  enabled: true
  address: "127.0.0.1:4822"
  recording_enabled: true
  # 录像目录的两个视角：guacd 侧路径（开发机 guacd 跑在 WSL2 内，故为 /mnt/c/...）
  # 与 webserver 本地读取路径。两者指向同一份文件，迁移 Linux 时按实际挂载点改这两个值即可。
  recording_dir_in_guacd: "/mnt/c/monitor/guac-recordings/sessions"
  local_recording_dir: 'C:\monitor\guac-recordings\sessions'
  connect_timeout_seconds: 5
  handshake_timeout_seconds: 15
  idle_timeout_seconds: 600
  retention_days: 30
```

#### API

| 方法 | 路径 | 权限标识 | 说明 |
| --- | --- | --- | --- |
| GET | `/api/v1/monitor/agents/:id/endpoint` | 登录 | 查询连接配置（不含明文） |
| PUT | `/api/v1/monitor/agents/:id/endpoint` | `monitor:access:manage` | 保存连接配置 |
| DELETE | `/api/v1/monitor/agents/:id/endpoint` | `monitor:access:manage` | 删除连接配置 |
| POST | `/api/v1/monitor/ssh/ticket` | `monitor:ssh:connect` | 换取 SSH 一次性票据 |
| GET | `/api/v1/monitor/ssh/ws?ticket=` | ticket | SSH 终端 WebSocket（浏览器无法带 Authorization，故用一次性 ticket） |
| POST | `/api/v1/monitor/rdp/token` | `monitor:rdp:connect` | 换取远程桌面（RDP / VNC）一次性票据，返回票据、协议与 WS 路径（**不含任何连接凭据**） |
| GET | `/api/v1/monitor/rdp/ws?ticket=` | ticket | 远程桌面 WebSocket（代理到 guacd；握手期注入凭据与录像参数） |
| GET | `/api/v1/monitor/rdp/sessions` | `monitor:rdp:view` | 远程桌面会话审计列表（按协议 / 操作人 / 主机 / 时间筛选） |
| GET | `/api/v1/monitor/rdp/sessions/:id/recording` | `monitor:rdp:view` | 下载 `.guac` 会话录像（支持 Range，供回放拖动进度） |
| GET | `/api/v1/monitor/agents/:id/processes` | `monitor:ssh:connect` | 查询主机进程列表（后端走独立 SSH 连接执行 ps / tasklist；结果缓存约 8 秒，带 `refresh=1` 强制重采） |
| GET | `/api/v1/monitor/agents/:id/services` | `monitor:ssh:connect` | 查询主机 systemd 服务清单（仅 Linux；结果缓存约 15 秒，带 `refresh=1` 强制重采） |
| POST | `/api/v1/monitor/agents/:id/processes/kill` | `monitor:ssh:process:kill` | 结束远程进程（高危：PID 1/2 被拒绝，请求会进操作日志） |
| POST | `/api/v1/monitor/agents/:id/processes/restart` | `monitor:ssh:process:restart` | 重启进程（仅 systemd 托管的服务），返回 `{unit, mainPID, previousPID, changed}` |
| GET | `/api/v1/monitor/audit/sessions` | `monitor:audit:view` | 会话审计列表 |
| GET | `/api/v1/monitor/audit/sessions/:id/commands` | `monitor:audit:view` | 命令流 |
| GET | `/api/v1/monitor/audit/sessions/:id/recording` | `monitor:audit:view` | 下载 asciinema 录像 |

连接配置与审计数据存 PostgreSQL 监控库（`remote_endpoints` / `ssh_sessions` / `ssh_commands` / `rdp_sessions`，启动时 `AutoMigrate` 幂等建表）。

#### guacd 部署（远程桌面前置依赖）

远程桌面只需要 **guacd** 一个 C 守护进程，**不部署 guacamole-client**（即不需要 Java / Tomcat / MySQL）。这样既省掉三个重型组件，也不再占用 8080 端口（该端口已被 collector 使用）。

Apache Guacamole 官方**没有 Windows 原生二进制**，开发机上直接装在 WSL2 里即可，生产用系统包或容器。项目内置了幂等的部署脚本：

```bash
# Windows 开发机（WSL2）
pwsh -File scripts/guacd/deploy-guacd.ps1

# Linux 生产
sudo bash scripts/guacd/deploy-guacd.sh

# 独立验证 guacd 是否响应握手（排查「端口通但连不上」的第一现场）
python3 scripts/guacd/probe-guacd.py rdp vnc
```

脚本做四件事：装 guacd、**绑定 `0.0.0.0`**、用 systemd 常驻、准备录像目录并做连通性自检。

> **两个必须知道的坑**（脚本已处理，手装时极易踩中）：
> 1. guacd 默认只监听 `127.0.0.1`，而 WSL2 的 localhost 转发**不转发回环监听**，必须改成 `0.0.0.0`。
> 2. guacd 1.3.0 自行 daemonize 后会留下「端口在监听、进程也在、但 accept 后不处理请求」的半死状态，表现为**握手永远无应答，且日志里连一条连接记录都没有**——很容易被误判成协议实现错误。必须加 `-f` 让它前台运行、由 systemd 托管。

如果仍想用容器，最少只需要 guacd 一个服务（不再需要 MySQL）：

```yaml
# docker-compose.yml
services:
  guacd:
    image: guacamole/guacd:1.5.5
    restart: unless-stopped
    ports: ["4822:4822"]
    volumes:
      # 录像落盘目录：容器内路径与宿主机路径指向同一份文件，
      # 分别对应配置里的 recording_dir_in_guacd 与 local_recording_dir。
      - ./guac-recordings:/var/lib/guacd/recordings
  guac-db:
```

启动后把 `guac.enabled` 置为 `true`、`address` 指向 guacd 地址（默认 `127.0.0.1:4822`）即可，**无需任何数据库初始化**——连接参数由 webserver 在握手期直接注入 guacd，不再经过 guacamole-client。

#### 权限标识

`monitor:access:manage`（连接配置）、`monitor:ssh:connect`（SSH，含进程列表查看）、`monitor:ssh:process:kill`（结束远程进程）、`monitor:ssh:process:restart`（重启 systemd 托管的进程）——**后两个都是高危操作，建议只授予运维负责人**、`monitor:rdp:connect`（发起 RDP / VNC 远程桌面）、`monitor:rdp:view`（远程会话审计与录像回放）、`monitor:audit:view`（SSH 会话审计）。已通过 `002_menu_seed.sql` 写入菜单树（监控中心 → 会话审计 id=105、远程桌面会话 id=110，服务器列表按钮 1021-1026），非超管角色需在「角色管理 → 菜单设置」勾选授权。

#### SSH 终端的「系统进程」面板

终端页右上角有「系统进程」按钮，点开从右侧滑出抽屉：列出目标主机的进程（PID / 程序名 / USER / CPU / 内存 / 完整命令），支持按名称、命令、属主过滤，并可按需**重启**或**结束**进程。

有一处取舍需要知道：**进程查询与结束进程都是独立新建 SSH 连接**，不复用终端那条 WebSocket 连接——终端连接是请求私有的（会话结束即关闭），进程内也没有连接池。换来的是实现简单、没有跨请求的连接状态（SFTP 也是同样的模式）。

为了不让「关掉再打开」每次都重付一次 SSH 握手（服务列表还要跑两条 `systemctl`），进程列表与服务列表各有一层**进程内短 TTL 缓存**（进程 8 秒 / 服务 15 秒，见 `internal/remote/listcache.go`）。三条规则必须知道：

* **写操作会主动失效**：启停/重启服务、结束进程、重启进程（`systemctl restart`）都会立刻清掉该主机的两份缓存。失效在成功与失败时**都**执行——失败也可能是"做了一半"（restart 停成功、起失败），此时目标机状态确实变了。服务和进程缓存**成对**失效：杀掉服务的主进程会让 systemd 把该服务标成 failed。
* **`refresh=1` 强制重采**：界面的「刷新」按钮带这个参数，跳过缓存直接去目标机查。实测服务列表冷请求 1.4s、命中缓存 2ms、强制请求 1.5s。
* **终端里手输的命令拦不住**：终端是原始 pty 转发，后端不解析命令内容。你在终端里 `systemctl stop xxx` 之后打开抽屉，最多 15 秒内可能看到旧状态，点「刷新」即可。

界面对应的行为是"**立即出画 + 后台刷新**"：打开抽屉先渲染上一次的结果，顶部显示「数据更新于 N 秒前」，同时在后台重新请求；刷新失败时保留旧数据并说明，而不是把列表清空。

查询命令为 `ps -eo pid=,user=,pcpu=,pmem=,rss=,args=`（Linux）或 `tasklist /FO CSV /NH`（Windows 版无 USER 与 CPU 字段），结果按内存降序、最多返回 500 条。结束进程用 `kill -9`（Linux）或 `taskkill /F /PID`（Windows）；因为进程号是整数，命令拼接不存在注入面，且 PID 1/2 在本地校验阶段就被拒绝。

**「重启」为什么只支持 systemd 托管的服务**：Linux 没有「重启任意进程」的通用命令，可行做法只有两种——其一，先 kill 再按原命令行重新执行，会丢失环境变量、工作目录、文件描述符与日志重定向，多数服务重启后行为异常，风险高于收益；其二，交给服务管理器 restart，由 unit 定义原样拉起，这才是可预期的重启。因此实现走第二种：读 `/proc/<pid>/cgroup` 反查进程所属 unit（cgroup v1/v2 均支持），再执行 `systemctl restart <unit>`；不受 systemd 托管时返回明确原因，而不是"假装重启"。

unit 名是安全边界（它会被拼进远端 shell 命令）：只接受 `字母/数字/._@-` 组成且以 `.service` 结尾的名字，路径穿越与命令注入都在解析阶段被拦下；`user@*.service`、`systemd-*.service`、`dbus.service` 这类重启会掀掉整机或整个用户会话的 unit 禁止操作。Windows 主机不支持按进程号重启，会提示到服务管理器操作。

重启完成后接口会回传 `{unit, mainPID, previousPID, changed}`（新进程号取自 `systemctl show -p MainPID --value`），界面直接提示「已重启 xxx.service，新进程 PID yyy」。这个新进程号本身就是有用的自证：正常 restart 后主进程号必然变化，若 `mainPID` 仍等于原 PID（`changed=false`），界面会改为告警「可能未真正重启」并在日志里记一条 warn，而不是笼统报一句成功——避免「提示成功、进程号却还是旧的」这种看不出原因的怪现象。

#### 反向代理必须开启 WebSocket 升级（nginx / Apache）

SSH 终端走 WebSocket，**这是上线后最容易漏的一步**：反向代理默认按 HTTP 转发，不透传 `Upgrade`/`Connection` 头，握手就会失败。

**nginx：**

```nginx
location /prod-api/ {
    proxy_pass http://127.0.0.1:8090/;
    # WebSocket 升级必需的三行
    proxy_http_version 1.1;
    proxy_set_header Upgrade    $http_upgrade;
    proxy_set_header Connection "upgrade";
    # 常规透传（X-Forwarded-Proto 关系到 XSRF 同源判定与 wss 识别）
    proxy_set_header Host              $host;
    proxy_set_header X-Real-IP         $remote_addr;
    proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    # SSH 是长连接，超时时间要放宽，否则空闲一会儿就被断开
    proxy_read_timeout 3600s;
    proxy_send_timeout 3600s;
}
```

前端按 `location.protocol` 自动选择 `ws://` / `wss://`，HTTPS 站点无需额外配置；后端 `http_port`（默认 8090）只负责业务端口，SSH 终端不额外监听端口，**不存在端口冲突问题**——所有连接都复用同一个 HTTP 端口。

**Apache httpd（2.4.x）：**

Apache 没有 `proxy_set_header`，要用 `mod_proxy_http` 转发普通 API、`mod_proxy_wstunnel` 转发 WebSocket。先启用模块（`httpd.conf`，Windows 上路径同为 `modules/mod_proxy_wstunnel.so`）：

```apache
LoadModule proxy_module           modules/mod_proxy.so
LoadModule proxy_http_module      modules/mod_proxy_http.so
LoadModule proxy_wstunnel_module  modules/mod_proxy_wstunnel.so
# 下面两个用于透传 X-Forwarded-Proto（XSRF 同源判定要用）
LoadModule headers_module         modules/mod_headers.so
LoadModule rewrite_module         modules/mod_rewrite.so
```

然后在 443 站点里加规则：

```apache
<VirtualHost *:443>
    ServerName monitor.phpframe.org
    SSLEngine on
    SSLCertificateFile    /etc/httpd/certs/fullchain.pem
    SSLCertificateKeyFile /etc/httpd/certs/privkey.pem

    # 前端静态文件（Vue 打包产物）；history 路由回退到 index.html
    DocumentRoot "/var/www/monitor-web"
    <Directory "/var/www/monitor-web">
        Require all granted
        FallbackResource /index.html
    </Directory>

    # ① SSH 终端 WebSocket ——必须放在通用 /prod-api/ 规则【之前】
    #    ProxyPass 按出现顺序匹配，具体路径必须排在前面，否则会被下面的 http 规则先吃掉。
    #    浏览器 wss:// → Apache 解密 → 后端为明文 http/ws，所以目标是 ws://
    ProxyPass        /prod-api/api/v1/monitor/ssh/ws  ws://127.0.0.1:8090/api/v1/monitor/ssh/ws
    ProxyPassReverse /prod-api/api/v1/monitor/ssh/ws  ws://127.0.0.1:8090/api/v1/monitor/ssh/ws

    # ② 其余 API 走普通 HTTP 代理（等价于 nginx 的 proxy_pass 带结尾斜杠，会剥掉 /prod-api 前缀）
    ProxyPass        /prod-api/  http://127.0.0.1:8090/
    ProxyPassReverse /prod-api/  http://127.0.0.1:8090/

    # ③ SSH 是长连接：Apache 默认 Timeout 60s，空闲就会被切断
    ProxyTimeout 3600

    # ④ 保留原始 Host 并透传协议，供 XSRF 同源判定与访问日志使用
    ProxyPreserveHost On
    RequestHeader set X-Forwarded-Proto "https"
</VirtualHost>
```

**Apache 2.4.47 及以上**可以省掉 `mod_proxy_wstunnel`，直接用 `upgrade=` 参数（一条规则同时管 HTTP 与 WebSocket）：

```apache
ProxyPass        /prod-api/  http://127.0.0.1:8090/  upgrade=websocket
ProxyPassReverse /prod-api/  http://127.0.0.1:8090/
ProxyTimeout 3600
```

**没有 vhost 权限（`.htaccess` / 共享主机）**：`ProxyPass` 在 `.htaccess` 里会报 `not allowed here`，改用 `mod_rewrite` 的 `[P]` 标记（需 `AllowOverride FileInfo`）：

```apache
RewriteEngine On
RewriteCond %{HTTP:Upgrade} =websocket [NC]
RewriteRule ^/prod-api/api/v1/monitor/ssh/ws$ ws://127.0.0.1:8090/api/v1/monitor/ssh/ws [P,L]

RewriteCond %{HTTP:Upgrade} !=websocket [NC]
RewriteRule ^/prod-api/(.*)$ http://127.0.0.1:8090/$1 [P,L]
```

改完先 `apachectl configtest`（Windows：`httpd -t`）再 reload。

**验证代理是否真的通了**（不依赖前端，直接看状态码）：

```bash
curl -i -N -H "Connection: Upgrade" -H "Upgrade: websocket" \
     -H "Sec-WebSocket-Version: 13" -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
     https://monitor.phpframe.org/prod-api/api/v1/monitor/ssh/ws
```

| 返回 | 含义 |
| --- | --- |
| `101 Switching Protocols` | 代理与后端都正常（带上有效 ticket 时的结果） |
| `403` + `{"code":"A0301",...}` | **代理已经正常**，只是没带票据或票据过期——说明 WebSocket 转发是通的 |
| `400` / `200` / `502` | 代理没配好（未加载 `mod_proxy_wstunnel`、规则顺序不对、或 2.4.47 以下没写 ws 规则） |

#### 故障排查

页面打开 F12，看 `wss://<域名>/prod-api/api/v1/monitor/ssh/ws?ticket=...` 这条请求的结果：

| 现象 | 原因 | 处理 |
| --- | --- | --- |
| 握手 **400** / **200**，且后端日志有「WebSocket 升级失败」 | 反向代理未透传 `Upgrade`/`Connection` | nginx 补 `proxy_set_header Upgrade/Connection`；Apache 加载 `mod_proxy_wstunnel` 并写 ws 规则（两者都要放在通用 API 规则之前） |
| 握手 **403** + `A0301` | 票据无效/已过期/已被使用 | 票据 30 秒有效且一次即焚，回列表页重新点 SSH 即可 |
| 握手 **404** | 反向代理前缀写错 | 前端 `VITE_APP_BASE_API` 要与 nginx `location` 一致（默认 `/prod-api/`） |
| 握手 **500** / `B0001` | 后端在「读取凭据 / 建会话记录」阶段失败 | 看 `logs/app.log` 里对应的 error 日志（含 agent_id / operator / err） |
| 终端里显示红色「[连接失败] ...」 | **握手已成功，是 SSH 层失败**（地址端口不通、认证失败、私钥格式不对、录像目录不可写） | 终端提示里已给出原因，`logs/app.log` 搜「ssh 终端会话建立失败」有完整错误与 host/port/username |

日志示例（zap JSON，落 `logs/app.log`）：

```json
{"level":"error","msg":"ssh 终端会话建立失败","session_id":3,"agent_id":"loca-linux","operator":"admin","host":"192.168.1.10","port":22,"username":"root","auth_type":"password","err":"ssh dial 192.168.1.10:22: dial tcp 192.168.1.10:22: i/o timeout"}
```

> 排查顺序建议：**先看 F12 的握手状态码**（区分网关问题与 SSH 问题），再去 `logs/app.log` 搜关键字拿具体原因。握手阶段浏览器拿不到响应体，所以 SSH 层的错误是由后端通过 WebSocket 文本推到终端里的——终端里那条红字就是真实原因。

录像文件落在 `remote.recording_dir`（默认 `data/ssh-recordings/<日期>/<会话号>.cast`），**运行用户必须对该目录有写权限**，否则会话建立会在创建录像文件时失败。

---

### 5.12 资源告警（CPU / 内存 / 负载 / 磁盘持续超阈值）

阈值在「监控中心 → 告警规则」按全局/角色两级配置，「服务器列表 → 更多 → 告警设置」配置单机覆盖。
评估放在 **webserver** 而不是 collector：判定「持续超标」必须做时序窗口查询，而 collector 只写不查
InfluxDB，也没有文件日志设施。

**判定口径**（口径错了比阈值错了更容易造成误报）

| 指标 | 口径 | 默认警告 / 严重 | 判定期 |
| --- | --- | --- | --- |
| CPU 利用率 | `100 - usage_idle`（整机 `cpu-total`） | 85% / 95% | 5 分钟 |
| 内存利用率 | `100 - available_percent`（不含可回收 cache） | 85% / 95% | 5 分钟 |
| 系统负载 | `load1 / 逻辑核数` | 1.2 / 2.0 倍 | 10 分钟 |
| 磁盘利用率 | `used_percent`（按挂载点，已排除 tmpfs/overlay 等伪文件系统） | 85% / 92% | 30 分钟 |

* **「持续超标」判据**：窗口内最后一个采样点超标（说明当前仍在超）**且**超标点占比不低于规则设定值
  （默认 90%）。不用「连续 N 次」是因为 Telegraf 偶发丢点会导致漏报。
* **窗口 = 判定期 + 1 倍余量**：余量用于算出「已经持续多久」，也让占比判据更稳。
* **三层作用域**：单机覆盖 > 角色规则 > 全局默认；角色取 Agent 上报的 `role` 标签。

**防刷机制**（回应用户「怕频繁写日志/频繁报警」）

| 机制 | 默认值 | 说明 |
| --- | --- | --- |
| 仅状态跃迁输出 | — | 正常→警告、警告→严重、严重→警告、恢复；同级持续不写 |
| 重申间隔（警告级） | 30 分钟 | 持续未恢复时补一条简报 |
| 重申间隔（严重级） | 10 分钟 | **严重级单独设频率**（默认比警告更快） |
| 人工确认后停止重申 | — | 点「确认」后不再按频率提醒；升级/降级/恢复仍照常通知 |
| 恢复冷却 | 10 分钟 | 抑制临界值抖动导致的反复触发 |
| 维护窗口静默 | — | 复用 `maintenance_windows`，窗口内不写日志不推送 |

重申间隔设 `0` 表示该级别不重申（要完全关闭重申，把两个级别都设为 0）。稳态下每台机每天 0~5 条。

**告警记录放在哪**（回答「未恢复前是一直一条，还是按频率多条」）：

| 位置 | 行为 |
| --- | --- |
| `alerts` 表（告警中心列表） | 一个 `dedup_key` **始终只有一行**（当前状态快照），靠「更新时间」体现它还活着；列表的「重申」列显示本轮已重复提醒次数与最近一次时间 |
| `alert_events` 表 | **每次跃迁、每次重申各一行**，在「详情 → 状态流转」按时间线展示 |
| 告警日志文件 | 与事件流同步落一行（`<主机>-<日期>-alerts.txt/.log`） |

所以「未恢复前的周期性记录」在**事件流与日志文件**里。列表刻意不按频率新增行——否则一条持续一整天的磁盘告警会刷出上百行，反而没法看。

**告警输出**

* 日志：**按「主机 + 日期」分文件**（`alert.log.name_pattern`，默认 `{agent}-{date}-alerts`），
  一份文件就是「某台机某天发生过的全部告警」，便于整份发给相关人。同时产出两份：
  `logs/<主机>-<日期>-alerts.txt`（人读）与 `.log`（JSON Lines），例如
  `logs/loca-windows-20261009-alerts.txt`。每个文件仍按 `alert.log` 段轮转（默认 32MB / 14 份 /
  30 天 / 压缩）；主机标识里的 `/`、`:`、空格等非法字符会被替换为下划线（防止写坏路径）。
  内容包含：超标幅度、持续时长、窗口超标占比、**进程 TopN 归因**、CPU 明细（user/system/iowait）、
  系统上下文（load1 与核数、内存、swap）、同机关联指标。
* 数据库：写入 `alerts` 表（沿用 `dedup_key` 去重，同一告警持续期间只有一行）与 `alert_events`
  （状态流转时间线），「告警中心」可查看详情与流转记录，并通过 SSE `monitor-alert` 实时推送。
* **重启行为**：启动时按去重键从 `alerts` 表恢复运行态，避免把已在告警的主机重新报一次「首次触发」；
  时序库查询失败时保持上一次状态，**不会**误报恢复。

**进程归因**

* 优先使用 Telegraf `[[inputs.procstat]]`（任意进程都能归因），需在被监控机启用：
  `Telegraf/telegraf-linux.conf` 已加好，只保留 `cpu_usage` 与 `memory_rss` 两个字段、间隔 30 秒，
  且**不把 `pid` 作为 tag**——否则进程每次重启都会产生新 series，几周内就会拖垮时序库基数。
* 未启用时自动退化为心跳里的进程内存快照（只有内存维度，CPU 占比为空），告警不会因此变成一句话。

**接口**

| 方法 | 路径 | 权限标识 |
| --- | --- | --- |
| GET | `/monitor/alert-rules/metrics` | `monitor:alert:rule:list` |
| GET | `/monitor/alert-rules` | `monitor:alert:rule:list` |
| PUT | `/monitor/alert-rules` | `monitor:alert:rule:update` |
| DELETE | `/monitor/alert-rules/:id` | `monitor:alert:rule:update` |
| GET | `/monitor/agents/:id/alert-rules` | `monitor:alert:rule:list` |
| PUT | `/monitor/agents/:id/alert-rules` | `monitor:alert:rule:update` |
| GET | `/monitor/alerts/:id` | `monitor:alert:list` |
| POST | `/monitor/alerts/:id/ack` | `monitor:alert:list` |

> 与监控查询路由（`_ = guard`，不校验权限）不同，这些写接口**真正走 guard**：阈值直接决定何时告警，
> 不能只靠前端隐藏按钮。非超管需在「角色管理 → 菜单设置」勾选「告警规则」菜单与「告警设置」按钮权限。
>
> 保存是**覆盖式**的：某层级提交空数组即清空该层级（该指标回落到上一级）。未启用的规则不会被提交，
> 因此「关闭某条规则」在单机弹窗里的语义是「不覆盖上级」；要彻底关掉请到对应层级关闭。

---

## 6. 目录

```text
cmd/webserver/           监控后台唯一入口
api/                     全部 Gin 相关代码（控制器 / 中间件 / 路由）
  v1/                    控制器：参数绑定 → service → 统一响应
  middleware/            RequireAuth、RequirePerm、操作日志、访问日志
  router/                路由装配（Deps 注入，无全局单例）
internal/
  model/                 init.sql 对应的 GORM 实体（含租户列与审计列）
  dto/                   请求参数、视图对象与 DB↔前端契约转换
  apperr/                业务错误类别
  util/                  ID/时间/分页/User-Agent 等纯工具
  permission/            权限快照与数据权限范围（60s 缓存）
  logic/                 领域操作：GORM 读写、树构建、状态机
  service/               应用服务：校验、事务编排、VO 组装
  captcha/               验证码仓储与位图渲染
  monitor/               监控只读查询（PostgreSQL + InfluxDB）
  eventhub/              SSE 广播中心
  app/                   装配根：配置、两库、迁移种子、控制台横幅
    sql/                 内嵌 DDL 与种子数据（001/002/003）
configs/webserver.yaml   本机运行配置
data/backup/             计划任务产出的 PostgreSQL 备份（*.sql，可直接 psql -f 导入）
upload/                  附件与头像落盘目录（upload/年/月/日/，可通过 upload.dir 修改）
webserver.exe            本机编译产物（不进 git 也没关系）
```

分层与依赖方向、契约映射表、监控模块红线见 [`docs/architecture.md`](docs/architecture.md)。

Makefile 提供 `make build` / `make test` / `make run` / `make fmt` / `make vet`。
