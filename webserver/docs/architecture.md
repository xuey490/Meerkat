# 架构说明

本文描述监控平台中心端 `webserver` 的实际架构：它是 Vue 控制台的唯一后端入口，同时承担**监控查询**与**权限治理**两类职责。历史上由脚手架模板生成的 `cmd/gateway`、`cmd/user`、`cmd/role`、`cmd/menu` 与 `internal/<service>` 分层示例已删除，仅保留 `cmd/webserver`。

## 1. 进程与依赖

```text
cmd/webserver/main.go
  ├─ webapp.LoadConfig(configs/webserver.yaml)
  ├─ webapp.New(cfg)
  │     ├─ SQLite 打开（glebarez/sqlite，WAL + busy_timeout，MaxOpenConns=1）
  │     ├─ migrate()：执行内嵌 SQL（建表 + 种子 + 一次性订正）
  │     ├─ PostgreSQL 打开（监控库，只读查询为主）
  │     ├─ InfluxDB 客户端
  │     ├─ eventHub（SSE）
  │     └─ routes()：装配路由与中间件
  ├─ go app.RunEventPublisher(ctx)   // 5s 周期推送 monitor-agent-status
  └─ http.Server{Handler: app.Router()}
```

| 数据源 | 用途 | 是否由本服务写入 |
| --- | --- | --- |
| SQLite `data/webserver.db` | 权限、菜单、字典、配置、公告、日志 | 是（唯一的写入库） |
| PostgreSQL | `agents`/`alerts`/`service_probe_states`/`docker_container_states`/`maintenance_windows` | 否（Collector 写入，本服务只查） |
| InfluxDB 2 | 指标曲线 | 否（Collector/Telegraf 写入，本服务只查） |

## 2. 内部分层（internal/webapp）

单包多文件，按职责切分，避免过度抽象：

```text
api/                        # 全部 Gin 相关代码集中在此，internal 内不出现 *gin.Context
├── v1/                     # 控制器：绑定参数 → 调用 service → 统一响应
│   ├── response.go         # write/OK/Created/Fail/PageParams/CurrentUser/Guard 约定
│   ├── auth.go             # 验证码、登录、刷新、退出、/users/me
│   ├── user.go             # 用户 CRUD、导入导出、个人中心、改密
│   ├── role.go dept.go menu.go post.go dict.go config.go notice.go log.go
│   └── monitor.go          # 监控 8 个接口 + /sse/connect（响应码与迁移前逐字一致）
├── middleware/
│   ├── auth.go             # RequireAuth（JWT → 上下文用户）、RequirePerm（权限标识）
│   ├── operation.go        # 操作日志：非 GET、跳过 auth/sse/logs、请求体脱敏 + 2KB 截断
│   └── accesslog.go        # 访问日志（access_log=true 时打印完整地址与耗时）
└── router/router.go        # 装配控制器与中间件（Deps 注入，无全局单例）

internal/
├── model/                  # GORM 实体：init.sql 逐字段（含 tenant_id 与审计列）
├── dto/                    # 请求参数、视图对象与「DB ↔ 前端契约」唯一翻译点（无 gorm 依赖）
├── apperr/                 # 业务错误类别（Invalid/NotFound/Conflict/Forbidden/Unauthorized/Unavailable/Internal）
├── util/                   # 纯工具：ID/时间/分页/关键字/User-Agent/上下文键
├── permission/             # 权限快照（角色、权限标识、data_scope）与 60s 缓存
├── logic/                  # 领域操作：GORM 读写、树构建、状态机（不认识 gin）
├── service/                # 应用服务：校验、事务编排、VO 组装、权限缓存失效
├── captcha/                # 验证码仓储（一次性校验 + 过期清理）与位图渲染
├── monitor/                # 监控只读查询：PostgreSQL（agents/alerts/...）+ InfluxDB 指标
├── eventhub/               # SSE 广播中心（monitor-agent-status / dict 主题）
└── app/                    # 装配根：LoadConfig、打开两库、Migrate 种子、装配 service/controller
    ├── app.go  config.go  migrate.go  console*.go
    └── sql/*.sql           001 建表 / 002 菜单种子 / 003 基础数据种子
```

依赖方向严格单向：`api/v1 → service → logic → model`；`api/middleware → permission → model`；`internal/app` 负责装配并注入。规则：控制器不碰数据库、service 不出现 `*gin.Context`、logic 不做权限判断、model 不依赖任何上层。

## 3. 数据库与契约的映射

### 3.1 命名映射（DB 为 init.sql，契约以 `web/src/api/system/*/types.ts` 为准）

| DB（init.sql） | 上游契约 | 说明 |
| --- | --- | --- |
| `sa_system_user.realname` | `nickname` | 昵称 |
| `sa_system_user.phone` | `mobile` | 手机号 |
| `sa_system_user.gender`（varchar `'0'/'1'/'2'`） | `gender`（number） | 双向转换 |
| `sa_system_menu.code` | `routeName` | 前端路由名 |
| `sa_system_menu.path` | `path` / `routePath` | 目录用绝对路径，子菜单相对 |
| `sa_system_menu.slug` | `perm` | 按钮权限标识 |
| `sa_system_menu.type`（1/2/3/4） | `type`（C/M/E/B） | 目录/菜单/按钮/外链 |
| `sa_system_menu.is_hidden`（1/2） | `visible`（0/1） | 隐藏与显示的相反语义 |
| `sa_system_menu.is_keep_alive`（1/2） | `keepAlive`（0/1） | 页面缓存 |
| `sa_system_menu.link_url` | `externalUrl` | 外链地址 |
| `sa_system_dict_data.code` | `dictCode` | 字典标识 |
| `sa_system_dict_data.tag_type` | `tagType`（short code） | `[扩展]` 列，短码 N/P/S/W/I/D |
| `sa_system_config.name/key/value` | `configName/configKey/configValue` | 配置项 |
| `sa_system_oper_log.*` | `LogItem.*` | 见下节扩展列 |

分页与响应壳固定：`pageNum`/`pageSize` → `{list,total}`；`{code:"00000",msg,data}`。

### 3.2 为契约新增的扩展列（SQL 中以 `[扩展]` 注释）

* `sa_system_menu`：`is_always_show`、`redirect`、`params`
* `sa_system_dict_data`：`tag_type`
* `sa_system_notice`：`level`、`publish_status`、`target_type`、`target_users`、`publisher_id`、`publish_time`、`revoke_time`
* `sa_system_notice_read`（表）：公告已读记录
* `sa_system_oper_log`：`action_type`、`operator_id`、`device`、`browser`、`os`、`status`、`error_msg`
* `sa_system_meta`（表）：一次性迁移标记（当前用于种子时间订正）

### 3.3 迁移策略

启动时按顺序执行内嵌 SQL，全部语句幂等：

1. `001_schema.sql`：`CREATE TABLE IF NOT EXISTS` + 清理早期模板表（`web_users` 等）；
2. `002_menu_seed.sql`：`INSERT OR IGNORE` 固定主键写入菜单、按钮权限、角色菜单授权；
3. `003_system_seed.sql`：基础数据 + 时区一次性订正 + 写入 `sa_system_meta` 标记；
4. `seed.go`：确保 `admin` 存在（bcrypt），并补齐 `user_role/user_dept/user_post/user_tenant` 关联。

> 新增列时：改 `001_schema.sql`（新库）并在需要时补一次性的 `ALTER TABLE`/订正语句（老库），二者都要幂等。

## 4. 鉴权、权限与审计

```text
登录  → 验证码 → 用户名/密码（bcrypt）→ 签发 JWT(isSuper) + 刷新令牌(SHA-256 指纹)
请求  →  requireAuth（解析 JWT，加载用户）
      →  requirePerm(<slug>)（超管直通；否则查权限集合，越权 403/A0301）
      →  handler
      →  操作日志中间件（非 GET，记录脱敏请求体、耗时、结果）
```

* 权限集合来源：`sa_system_user_role → sa_system_role_menu → sa_system_menu.slug`；按用户缓存 60s，角色/菜单变更时 `invalidateAll()`。
* 数据权限：`sa_system_role.data_scope`（1 全部 / 2 本部门及下属 / 3 本部门 / 4 本人 / 5 自定义）结合 `sa_system_dept.level` 祖级前缀计算可见部门集合，目前作用于用户列表。
* 登录日志与操作日志写库失败只告警（`log/slog`）不阻断主流程。

## 5. 动态菜单链路

```text
sa_system_menu（status=1，type∈{1,2,4}）
  → 按角色过滤（超管取全量）
  → buildRouteTree（O(n) 内存建树，按 sort）
  → GET /api/v1/menus/routes
  → web/src/stores/permission.ts transformRoutes（component="Layout" → 布局；其余 import.meta.glob 映射 views/**）
  → router.addRoute + 侧边栏渲染
```

约定：
* 顶层目录 `component` 固定为 `"Layout"`（否则前端会回退 404 页）；
* 子菜单 `component` 为 `views` 下相对路径（如 `monitor/overview/index`）；
* 参数路由（如服务器详情 `agents/:id`）写 `is_hidden=1`，同时在 `web/src/router/index.ts` 保留静态兜底路由，避免刷新 404。

## 6. 与监控模块的边界（改造红线）

以下内容在本次权限改造中**行为不变**，后续修改需格外谨慎：

* 路由：`/api/v1/monitor/*`、`/api/v1/sse/connect`、`/healthz`
* 代码：`monitor.go`、`captcha.go`、`console*.go`
* 数据：PostgreSQL 监控表与 InfluxDB 均不改结构、不改查询语义
* 前端：`web/src/views/monitor/*`、`constantRoutes`、`/monitor/agents/:id` 静态兜底路由

## 7. 测试与验证

```powershell
cd webserver
go test ./...                 # 单测与集成测试（使用临时 SQLite，不依赖 PG/Influx）
go vet ./...
gofmt -l ./api ./internal ./cmd   # 应无输出
```

测试分布：

| 位置 | 覆盖内容 |
| --- | --- |
| `internal/app/migrate_test.go` | 迁移与种子幂等、内置管理员、**监控中心菜单与子菜单存在性**、角色菜单授权、SQL 语句切分（注释/字符串/分号边界） |
| `api/router/router_test.go` | 探活、错误凭据 A0210、管理员登录、`/users/me` 超管与权限集合、动态菜单含 `/monitor` 与 `/system`、未登录 401/A0230、越权 403/A0301、操作日志落库与**密码脱敏**、监控路由接线（监控库不可用 → 502/B0200、非法 ID → 400/A0001） |

线上冒烟（真实 PostgreSQL + InfluxDB）已覆盖：`/monitor/overview`、`/monitor/agents`、`/monitor/agents/:id`（含 238 条进程快照）、`/monitor/alerts`、`/monitor/maintenance`、`/monitor/services/:id`、`/monitor/containers/:id`、动态菜单、用户/角色/部门/菜单/岗位/字典/配置/公告/日志读接口、岗位写操作与操作日志脱敏。

## 8. 分层重构（已完成）

背景：原单包 `internal/webapp`（27 文件 / 7730 行）同时承担配置、模型、DTO、业务与 HTTP 职责，`dto.go` 已达 1000+ 行。现按「控制器在 `api/`、模型/DTO/服务/领域操作在 `internal/` 各自成包」完成拆分：

- 迁移方式为**分批接力**：先落 `model/dto/apperr/util`，再逐模块下沉 `logic/service`，最后装配 `api/v1 + api/middleware + api/router` 与 `internal/app`；
- 迁移期间旧包保持可用、行为零变更，全部模块完成后才切换 `cmd/webserver` 并删除 `internal/webapp`；
- 安全网：`internal/app` 与 `api/router` 的自动化测试 + 真实服务端到端冒烟（详见 §7）。

### 8.1 目标结构

```text
webserver/
├── api/                                # 全部 Gin 相关代码集中在此
│   ├── v1/                             # 控制器：绑定参数 → 调用 service → 统一响应
│   │   ├── response.go                 # ok/badRequest/notFound/dbError 等响应助手
│   │   └── auth.go user.go role.go dept.go menu.go post.go dict.go config.go notice.go log.go monitor.go
│   ├── middleware/                     # requireAuth、requirePerm、操作日志、访问日志
│   └── router/                         # 路由装配（系统管理路由表 + 监控路由表）
└── internal/
    ├── model/                          # GORM 实体（init.sql 逐字段对应）      ★ 已完成
    ├── dto/                            # 请求参数与视图对象 + 契约转换          ★ 已完成
    ├── apperr/                         # 业务错误类别（不依赖 gin/gorm）        ★ 已完成
    ├── logic/                          # 领域操作：GORM 读写、树构建、聚合
    ├── service/                        # 应用服务：事务编排、校验、审计、组装 VO
    ├── permission/                     # 权限集合与数据权限范围（含缓存）
    ├── captcha/                        # 验证码仓储与图片渲染
    ├── monitor/                        # 监控查询（PostgreSQL + InfluxDB）
    ├── app/                            # 装配根：配置加载、DB 打开、迁移种子、生命周期
    └── sql/                            # 内嵌 DDL 与种子数据
```

### 8.2 依赖方向（必须单向）

```text
api/v1 ──▶ service ──▶ logic ──▶ model
api/middleware ──▶ permission ──▶ model
internal/app 装配 service/logic 并注入 api
model 不依赖任何上层；logic/service 不出现 *gin.Context；internal 不 import api
```

### 8.3 迁移顺序与状态

| 阶段 | 内容 | 状态 |
| --- | --- | --- |
| 1 | `internal/model`、`internal/dto`、`internal/apperr` | ✅ 完成（`go vet` 通过） |
| 2a | `internal/util`（跨层纯工具）、`internal/permission`（权限快照 + 数据范围） | ✅ 完成 |
| 2b | `internal/logic`：support/errors/post/config/dept/role/menu/dict/log/notice/user | ✅ 完成（auth 待迁移） |
| 2c | `internal/service`：common/post/config/dept/role/menu/dict/log/notice/user | ✅ 完成（auth 待迁移） |
| 3a | `api/v1/response.go` + 9 个模块控制器（post/config/dept/role/menu/dict/log/notice/user） | ✅ 完成 |
| 3b | `internal/monitor`（PG + Influx 只读查询，响应码逐字保留）、`internal/eventhub`（SSE 广播）、`api/v1/monitor.go`（含 SSE） | ✅ 完成 |
| 3c | `api/v1/auth.go`、`api/middleware/*`（requireAuth/requirePerm/操作日志/访问日志）、`api/router/*`、`internal/captcha` | ⏳ 待执行 |
| 4 | `internal/app` + `internal/sql`（配置、建库、迁移种子、控制台横幅）、切换 `cmd/webserver`、删除 `internal/webapp`、测试分布到 api/logic、README/AGENT 同步 | ⏳ 待执行 |
| 4 | `internal/app` + `internal/sql`，切换 `cmd/webserver`、删除 `internal/webapp`、测试分布到 api/logic 两层、README/AGENT 同步 | ⏳ 待执行 |

**已迁模块**（logic + service + controller 三层齐备，行为与旧实现逐行对齐）：
`post`、`config`、`dept`、`role`、`menu` —— 覆盖岗位、系统配置、部门、角色（含角色菜单授权与自定义数据权限部门）、菜单（含 C/M/B/E 映射、参数编解码、父级联动与权限缓存失效）。

### 8.3.1 历史记录：迁移顺序（已完成，供回溯）

1. 迁移顺序建议：`menu → dept/role(service+api) → dict → notice → log → user → auth → monitor → captcha`；
2. 每个模块三件套：`internal/logic/<模块>.go`（GORM 读写）→ `internal/service/<模块>.go`（校验 + 组装 VO + 失效权限缓存）→ `api/v1/<模块>.go`（绑定 + 调用 + `OK/Created/Fail`，并在 `Register` 中声明路由与权限标识）；
3. 权限标识与路由路径必须与 `internal/webapp/routes.go` 完全一致（如 `sys:post:create`、`PUT /configs/refresh` 需注册在 `PUT /configs/:id` 之前语义不变）；
4. 全部模块迁完后：`api/router/router.go` 装配 `authed` 组（`middleware.RequireAuth`）并注入 `Guard = middleware.RequirePerm`，`internal/app` 负责建库、迁移种子、装配 service；
5. 最后删 `internal/webapp`，把 `webapp_test.go` 拆分为 `api/v1` 集成测试与 `logic` 单测，跑通后再更新 README 与 AGENT.md。

### 8.4 分层约束（长期有效）

* 旧包 `internal/webapp` 在新结构可切换前保持为唯一实现，**不得边迁移边改行为**；
* 新包中的类型/函数名一律导出（跨包引用），旧包内的未导出命名不再沿用；
* 每阶段结束都要求 `gofmt -l` 无输出、`go vet ./...` 与 `go test ./...` 通过；
* 契约不变：响应壳 `{code,msg,data}`、分页 `{list,total}`、错误码映射仍由 api 层单点决定。
