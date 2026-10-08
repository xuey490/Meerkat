# Monitor Webserver

监控平台的浏览器后台。给 Vue 控制台提供登录、动态菜单、权限管理与监控 API：读 PostgreSQL 里的 Agent 资产 / 心跳 / 告警，读 InfluxDB 里的指标曲线，权限数据落在本地 SQLite。

日常只要关心这一条进程：

```text
cmd/webserver  →  webserver.exe（Windows）或 webserver（Linux）
配置           →  configs/webserver.yaml
默认监听       →  http://127.0.0.1:8090
```

两套数据库，各管各的：

| 库 | 位置 | 内容 |
| --- | --- | --- |
| SQLite | `data/webserver.db` | 权限与治理数据：`sa_system_user/role/dept/menu/post/dict_*/config*/notice*/login_log/oper_log/user_*/role_*` 等（结构以仓库根目录 `database/init.sql` 为准，租户列原样保留但系统不实现租户功能） |
| PostgreSQL | `127.0.0.1:5432` | 监控数据：`agents`、`alerts`、`service_probe_states`、`docker_container_states` |
| InfluxDB 2 | `127.0.0.1:8086` | 指标曲线（CPU / 内存 / 磁盘 / 网络…） |

SQLite 用纯 Go 驱动（`glebarez/sqlite`），Windows 不需要 CGO / GCC；启动时自动建表并写入种子数据（幂等）。

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

编辑 [`configs/webserver.yaml`](configs/webserver.yaml)。工作目录必须是 `webserver/`，相对路径 `sqlite_path`、`-config` 才找得到。

```yaml
http_address: ":8090"          # 监听地址，空 host 表示所有网卡
access_log: false              # true 时控制台滚动打印每次 API 和耗时
sqlite_path: "data/webserver.db"

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

* 表结构以仓库根目录 `database/init.sql` 为准逐字段翻译为 SQLite：`bigint AUTO_INCREMENT → INTEGER PRIMARY KEY AUTOINCREMENT`、`datetime → DATETIME`、`tinyint → INTEGER`；`tenant_id` 等租户列与 `created_by/updated_by/create_time/update_time/delete_time` 审计列全部保留（系统不实现租户功能，租户列只按 init.sql 默认值写入）。
* DDL 与种子 SQL 内嵌在 `internal/webapp/sql/`（`001_schema.sql`、`002_menu_seed.sql`、`003_system_seed.sql`），随二进制发布，启动时幂等执行：
  * `001` 建表，并清理早期模板遗留表 `web_users/web_roles/web_menus/...`；
  * `002` 写入菜单与按钮权限（含原监控中心菜单）以及角色菜单授权；
  * `003` 写入租户、角色、部门、岗位、字典、配置等基础数据。
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
| GET | `/api/v1/monitor/agents/:id/metrics` | 时序（Influx） |
| GET | `/api/v1/monitor/services/:id` | 服务探测状态 |
| GET | `/api/v1/monitor/containers/:id` | 容器状态 |
| GET | `/api/v1/monitor/alerts` | 告警 |
| GET | `/api/v1/monitor/maintenance` | 维护窗口 |
| GET | `/api/v1/sse/connect` | SSE（`monitor-agent-status`、`dict` 等主题） |

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
* **refresh token**：随机 32 字节，明文只下发一次，库里只存 `sha256` 指纹（`sa_system_refresh_token.token_hash`），权威数据**始终在 SQLite**，不写缓存——缓存一丢就全体掉登录，得不偿失。
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

# Apache Guacamole 嵌入（RDP）；enabled=false 时前端隐藏 RDP 入口
guacamole:
  enabled: false
  base_url: "http://127.0.0.1:8080/guacamole"
  username: "guacadmin"
  password: "guacadmin"
  data_source: "mysql"
```

#### API

| 方法 | 路径 | 权限标识 | 说明 |
| --- | --- | --- | --- |
| GET | `/api/v1/monitor/agents/:id/endpoint` | 登录 | 查询连接配置（不含明文） |
| PUT | `/api/v1/monitor/agents/:id/endpoint` | `monitor:access:manage` | 保存连接配置 |
| DELETE | `/api/v1/monitor/agents/:id/endpoint` | `monitor:access:manage` | 删除连接配置 |
| POST | `/api/v1/monitor/ssh/ticket` | `monitor:ssh:connect` | 换取 SSH 一次性票据 |
| GET | `/api/v1/monitor/ssh/ws?ticket=` | ticket | SSH 终端 WebSocket（浏览器无法带 Authorization，故用一次性 ticket） |
| POST | `/api/v1/monitor/rdp/token` | `monitor:rdp:connect` | 换取 RDP 一次性嵌入地址 |
| GET | `/api/v1/monitor/audit/sessions` | `monitor:audit:view` | 会话审计列表 |
| GET | `/api/v1/monitor/audit/sessions/:id/commands` | `monitor:audit:view` | 命令流 |
| GET | `/api/v1/monitor/audit/sessions/:id/recording` | `monitor:audit:view` | 下载 asciinema 录像 |

连接配置与审计数据存 PostgreSQL 监控库（`remote_endpoints` / `ssh_sessions` / `ssh_commands` / `rdp_sessions`，启动时 `AutoMigrate` 幂等建表）。

#### Guacamole 部署（RDP 前置依赖）

Guacamole 官方**没有 Windows 原生二进制**，建议用 Docker（Windows 上需 Docker Desktop / WSL2，或部署在 Linux 服务器）：

```yaml
# docker-compose.yml
services:
  guacd:
    image: guacamole/guacd:1.5.5
    restart: unless-stopped
  guacamole:
    image: guacamole/guacamole:1.5.5
    restart: unless-stopped
    ports: ["8080:8080"]
    environment:
      GUACD_HOSTNAME: guacd
      MYSQL_HOSTNAME: mysql
      MYSQL_DATABASE: guacamole_db
      MYSQL_USER: guacamole
      MYSQL_PASSWORD: guacamole
    depends_on: [guacd, mysql]
  mysql:
    image: mysql:8
    restart: unless-stopped
    environment:
      MYSQL_DATABASE: guacamole_db
      MYSQL_USER: guacamole
      MYSQL_PASSWORD: guacamole
      MYSQL_ROOT_PASSWORD: rootpass
    volumes: ["guac-db:/var/lib/mysql"]
volumes:
  guac-db:
```

首次需用官方镜像初始化数据库：`docker run --rm guacamole/guacamole:1.5.5 /opt/guacamole/bin/initdb.sh --mysql > initdb.sql` 后导入。启动后把 `guacamole.enabled` 置为 `true` 并填 `base_url` 与 `guacadmin` 账号即可。

#### 权限标识

`monitor:access:manage`（连接配置）、`monitor:ssh:connect`（SSH）、`monitor:rdp:connect`（RDP）、`monitor:audit:view`（会话审计）。已通过 `002_menu_seed.sql` 写入菜单树（监控中心 → 会话审计菜单 id=105，按钮 1021/1022/1023），非超管角色需在「角色管理 → 菜单设置」勾选授权。

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
data/webserver.db        首次启动后生成的 SQLite（可直接删除后重建）
upload/                  附件与头像落盘目录（upload/年/月/日/，可通过 upload.dir 修改）
webserver.exe            本机编译产物（不进 git 也没关系）
```

分层与依赖方向、契约映射表、监控模块红线见 [`docs/architecture.md`](docs/architecture.md)。

Makefile 提供 `make build` / `make test` / `make run` / `make fmt` / `make vet`。
