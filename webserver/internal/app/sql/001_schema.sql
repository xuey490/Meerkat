-- =============================================================================
-- Monitor Webserver 权限库结构（SQLite）
--
-- 事实来源：database/init.sql（MySQL 8 导出的 sa_system_* 系列表）。
-- 翻译规则：
--   1. 逐字段保留 init.sql 的列，包含全部租户字段（tenant_id 等）与审计字段
--      （created_by/updated_by/create_time/update_time/delete_time）；
--      多租户功能不实现，租户列只按 init.sql 的默认值写入，不参与查询条件。
--   2. 类型适配：bigint/int AUTO_INCREMENT -> INTEGER PRIMARY KEY AUTOINCREMENT；
--      datetime/timestamp/tinyint/smallint -> DATETIME/INTEGER；text/longtext -> TEXT。
--   3. 少量列是为了对齐前端既有契约而新增的扩展列，注释中以 [扩展] 标出。
--   4. 所有语句幂等：建表用 IF NOT EXISTS，种子数据用 INSERT OR IGNORE。
-- =============================================================================

-- ---------------------------------------------------------------------------
-- 旧的模板表（web_users/web_roles/web_menus/...）已由 sa_system_* 取代，启动时清理。
-- ---------------------------------------------------------------------------
DROP TABLE IF EXISTS web_users;
DROP TABLE IF EXISTS web_roles;
DROP TABLE IF EXISTS web_menus;
DROP TABLE IF EXISTS web_user_roles;
DROP TABLE IF EXISTS web_refresh_tokens;
DROP TABLE IF EXISTS web_audit_records;

-- ---------------------------------------------------------------------------
-- 元数据表 sa_system_meta（[扩展]）：记录一次性迁移标记，避免重复执行数据订正。
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sa_system_meta (
  "key"   VARCHAR(64) PRIMARY KEY,
  value   VARCHAR(255),
  remark  VARCHAR(255),
  update_time DATETIME
);

-- ---------------------------------------------------------------------------
-- 用户表 sa_system_user
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sa_system_user (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  username    VARCHAR(64)  NOT NULL,
  password    VARCHAR(255) NOT NULL,
  realname    VARCHAR(64),
  gender      VARCHAR(10)  DEFAULT '0',
  avatar      VARCHAR(255) NOT NULL DEFAULT '',
  email       VARCHAR(128),
  phone       VARCHAR(20),
  signed      VARCHAR(255),
  dashboard   VARCHAR(255) DEFAULT 'work',
  dept_id     INTEGER,
  is_super    INTEGER DEFAULT 0,
  status      INTEGER DEFAULT 1,
  remark      VARCHAR(255),
  login_time  DATETIME,
  login_ip    VARCHAR(45),
  created_by  INTEGER,
  updated_by  INTEGER,
  create_time DATETIME,
  update_time DATETIME,
  delete_time DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS ux_sa_system_user_username ON sa_system_user (username);
CREATE INDEX IF NOT EXISTS ix_sa_system_user_delete_time ON sa_system_user (delete_time);
CREATE INDEX IF NOT EXISTS ix_sa_system_user_dept_id ON sa_system_user (dept_id);

-- ---------------------------------------------------------------------------
-- 角色表 sa_system_role
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sa_system_role (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  parent_id   INTEGER NOT NULL DEFAULT 0,
  name        VARCHAR(64) NOT NULL,
  code        VARCHAR(64) NOT NULL,
  level       INTEGER DEFAULT 1,
  data_scope  INTEGER DEFAULT 1,
  remark      VARCHAR(255),
  sort        INTEGER DEFAULT 100,
  tenant_id   INTEGER NOT NULL DEFAULT 0,
  status      INTEGER DEFAULT 1,
  created_by  INTEGER,
  updated_by  INTEGER,
  create_time DATETIME,
  update_time DATETIME,
  delete_time DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS ux_sa_system_role_code ON sa_system_role (code);
CREATE INDEX IF NOT EXISTS ix_sa_system_role_delete_time ON sa_system_role (delete_time);

-- ---------------------------------------------------------------------------
-- 部门表 sa_system_dept
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sa_system_dept (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  parent_id   INTEGER DEFAULT 0,
  name        VARCHAR(64) NOT NULL,
  code        VARCHAR(64),
  leader_id   INTEGER,
  level       VARCHAR(255) DEFAULT '',
  tenant_id   INTEGER NOT NULL DEFAULT 0,
  sort        INTEGER DEFAULT 0,
  status      INTEGER DEFAULT 1,
  remark      VARCHAR(255),
  created_by  INTEGER,
  updated_by  INTEGER,
  create_time DATETIME,
  update_time DATETIME,
  delete_time DATETIME
);
CREATE INDEX IF NOT EXISTS ix_sa_system_dept_parent_id ON sa_system_dept (parent_id);
CREATE INDEX IF NOT EXISTS ix_sa_system_dept_delete_time ON sa_system_dept (delete_time);

-- ---------------------------------------------------------------------------
-- 菜单表 sa_system_menu
--   type：1 目录 / 2 菜单 / 3 按钮(API) / 4 外链
--   is_hidden：1 隐藏 / 2 显示；is_keep_alive / is_iframe 等：1 是 / 2 否
--   slug：权限标识（前端 perm），code：组件名（前端 routeName）
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sa_system_menu (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  parent_id      INTEGER DEFAULT 0,
  name           VARCHAR(64) NOT NULL,
  code           VARCHAR(64),
  slug           VARCHAR(100),
  type           INTEGER NOT NULL DEFAULT 1,
  path           VARCHAR(255),
  component      VARCHAR(255),
  method         VARCHAR(10),
  icon           VARCHAR(64),
  sort           INTEGER DEFAULT 100,
  link_url       VARCHAR(255),
  is_iframe      INTEGER DEFAULT 2,
  is_keep_alive  INTEGER DEFAULT 2,
  is_hidden      INTEGER DEFAULT 2,
  is_fixed_tab   INTEGER DEFAULT 2,
  is_full_page   INTEGER DEFAULT 2,
  is_always_show INTEGER DEFAULT 2, -- [扩展] 目录仅一个子路由时是否始终显示
  redirect       VARCHAR(255),      -- [扩展] 目录重定向地址
  params         TEXT,              -- [扩展] 路由参数，JSON 数组 [{"key":"","value":""}]
  generate_id    INTEGER DEFAULT 0,
  generate_key   VARCHAR(255),
  status         INTEGER DEFAULT 1,
  remark         VARCHAR(255),
  created_by     INTEGER,
  updated_by     INTEGER,
  create_time    DATETIME,
  update_time    DATETIME,
  delete_time    DATETIME
);
CREATE INDEX IF NOT EXISTS ix_sa_system_menu_parent_id ON sa_system_menu (parent_id);
CREATE INDEX IF NOT EXISTS ix_sa_system_menu_delete_time ON sa_system_menu (delete_time);

-- ---------------------------------------------------------------------------
-- 关联表：用户-角色 / 角色-菜单 / 角色-部门 / 用户-部门 / 用户-岗位 / 用户-菜单
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sa_system_user_role (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL,
  role_id     INTEGER NOT NULL,
  status      INTEGER NOT NULL DEFAULT 1,
  tenant_id   INTEGER NOT NULL DEFAULT 0,
  created_by  INTEGER,
  updated_by  INTEGER,
  create_time DATETIME,
  update_time DATETIME,
  delete_time DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS ux_sa_system_user_role ON sa_system_user_role (user_id, role_id);
CREATE INDEX IF NOT EXISTS ix_sa_system_user_role_user_id ON sa_system_user_role (user_id);

CREATE TABLE IF NOT EXISTS sa_system_role_menu (
  id      INTEGER PRIMARY KEY AUTOINCREMENT,
  role_id INTEGER NOT NULL,
  menu_id INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS ux_sa_system_role_menu ON sa_system_role_menu (role_id, menu_id);
CREATE INDEX IF NOT EXISTS ix_sa_system_role_menu_role_id ON sa_system_role_menu (role_id);

CREATE TABLE IF NOT EXISTS sa_system_role_dept (
  id      INTEGER PRIMARY KEY AUTOINCREMENT,
  role_id INTEGER NOT NULL,
  dept_id INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS ux_sa_system_role_dept ON sa_system_role_dept (role_id, dept_id);

CREATE TABLE IF NOT EXISTS sa_system_user_dept (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  tenant_id   INTEGER NOT NULL DEFAULT 0,
  user_id     INTEGER NOT NULL,
  dept_id     INTEGER NOT NULL,
  created_by  INTEGER,
  updated_by  INTEGER,
  create_time DATETIME,
  update_time DATETIME,
  delete_time DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS ux_sa_system_user_dept ON sa_system_user_dept (user_id, dept_id);

CREATE TABLE IF NOT EXISTS sa_system_user_post (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL,
  post_id     INTEGER NOT NULL,
  status      INTEGER NOT NULL DEFAULT 1,
  created_by  INTEGER NOT NULL DEFAULT 0,
  updated_by  INTEGER NOT NULL DEFAULT 0,
  create_time DATETIME,
  update_time DATETIME,
  delete_time DATETIME,
  tenant_id   INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS ux_sa_system_user_post ON sa_system_user_post (user_id, post_id);

CREATE TABLE IF NOT EXISTS sa_system_user_menu (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL,
  menu_id     INTEGER NOT NULL,
  tenant_id   INTEGER DEFAULT 0,
  created_by  INTEGER DEFAULT 0,
  updated_by  INTEGER DEFAULT 0,
  status      INTEGER NOT NULL DEFAULT 1,
  create_time DATETIME,
  update_time DATETIME,
  delete_time DATETIME,
  is_show     INTEGER NOT NULL DEFAULT 1,
  is_create   INTEGER NOT NULL DEFAULT 1,
  is_update   INTEGER NOT NULL DEFAULT 1,
  is_delete   INTEGER NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX IF NOT EXISTS ux_sa_system_user_menu ON sa_system_user_menu (user_id, menu_id, tenant_id);

-- ---------------------------------------------------------------------------
-- 岗位表 sa_system_post
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sa_system_post (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        VARCHAR(50),
  code        VARCHAR(100),
  sort        INTEGER DEFAULT 0,
  status      INTEGER DEFAULT 1,
  tenant_id   INTEGER NOT NULL DEFAULT 0,
  remark      VARCHAR(255),
  created_by  INTEGER,
  updated_by  INTEGER,
  create_time DATETIME,
  update_time DATETIME,
  delete_time DATETIME
);
CREATE INDEX IF NOT EXISTS ix_sa_system_post_delete_time ON sa_system_post (delete_time);

-- ---------------------------------------------------------------------------
-- 字典：类型 sa_system_dict_type / 数据 sa_system_dict_data
--   dict_data.tag_type [扩展]：前端标签样式短码 N/P/S/W/I/D
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sa_system_dict_type (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        VARCHAR(50),
  code        VARCHAR(100),
  status      INTEGER DEFAULT 1,
  remark      VARCHAR(255),
  created_by  INTEGER,
  updated_by  INTEGER,
  create_time DATETIME,
  update_time DATETIME,
  delete_time DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS ux_sa_system_dict_type_code ON sa_system_dict_type (code);

CREATE TABLE IF NOT EXISTS sa_system_dict_data (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  type_id     INTEGER,
  label       VARCHAR(50),
  value       VARCHAR(100),
  color       VARCHAR(50),
  code        VARCHAR(100),
  tag_type    VARCHAR(10), -- [扩展] 标签样式短码
  sort        INTEGER DEFAULT 0,
  status      INTEGER DEFAULT 1,
  remark      VARCHAR(255),
  created_by  INTEGER,
  updated_by  INTEGER,
  create_time DATETIME,
  update_time DATETIME,
  delete_time DATETIME
);
CREATE INDEX IF NOT EXISTS ix_sa_system_dict_data_code ON sa_system_dict_data (code);

-- ---------------------------------------------------------------------------
-- 系统配置：分组 sa_system_config_group / 配置项 sa_system_config
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sa_system_config_group (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        VARCHAR(50),
  code        VARCHAR(100),
  remark      VARCHAR(255),
  created_by  INTEGER,
  updated_by  INTEGER,
  create_time DATETIME,
  update_time DATETIME,
  delete_time DATETIME
);

CREATE TABLE IF NOT EXISTS sa_system_config (
  id                  INTEGER PRIMARY KEY AUTOINCREMENT,
  group_id            INTEGER,
  "key"               VARCHAR(32) NOT NULL,
  value               TEXT,
  name                VARCHAR(255),
  input_type          VARCHAR(32),
  config_select_data  VARCHAR(500),
  sort                INTEGER DEFAULT 0,
  remark              VARCHAR(255),
  created_by          INTEGER,
  updated_by          INTEGER,
  create_time         DATETIME,
  update_time         DATETIME,
  delete_time         DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS ux_sa_system_config_key ON sa_system_config ("key");

-- ---------------------------------------------------------------------------
-- 通知公告 sa_system_notice
--   publish_status [扩展]：0 草稿 / 1 已发布 / 2 已撤回
--   target_type / target_users / publisher_id / publish_time / revoke_time
--   level [扩展]：公告级别
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sa_system_notice (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  title          VARCHAR(100) NOT NULL DEFAULT '',
  type           INTEGER NOT NULL DEFAULT 1,
  content        TEXT,
  status         INTEGER NOT NULL DEFAULT 1,
  level          VARCHAR(20),  -- [扩展]
  publish_status INTEGER NOT NULL DEFAULT 0, -- [扩展]
  target_type    INTEGER NOT NULL DEFAULT 1, -- [扩展] 1 全部 / 2 指定用户
  target_users   TEXT,                        -- [扩展] 指定用户 ID，逗号分隔
  publisher_id   INTEGER,                     -- [扩展]
  publish_time   DATETIME,                    -- [扩展]
  revoke_time    DATETIME,                    -- [扩展]
  remark         VARCHAR(255) NOT NULL DEFAULT '',
  created_by     INTEGER,
  updated_by     INTEGER,
  create_time    DATETIME,
  update_time    DATETIME,
  delete_time    DATETIME
);

-- 公告已读记录（[扩展]）
CREATE TABLE IF NOT EXISTS sa_system_notice_read (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  notice_id   INTEGER NOT NULL,
  user_id     INTEGER NOT NULL,
  read_time   DATETIME,
  create_time DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS ux_sa_system_notice_read ON sa_system_notice_read (notice_id, user_id);

-- ---------------------------------------------------------------------------
-- 登录日志 sa_system_login_log
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sa_system_login_log (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  username    VARCHAR(20),
  ip          VARCHAR(45),
  ip_location VARCHAR(255),
  os          VARCHAR(50),
  browser     VARCHAR(50),
  status      INTEGER DEFAULT 1,
  message     VARCHAR(50),
  login_time  DATETIME,
  remark      VARCHAR(255),
  created_by  INTEGER,
  updated_by  INTEGER,
  create_time DATETIME,
  update_time DATETIME,
  delete_time DATETIME
);
CREATE INDEX IF NOT EXISTS ix_sa_system_login_log_login_time ON sa_system_login_log (login_time);

-- ---------------------------------------------------------------------------
-- 操作日志 sa_system_oper_log
--   action_type / operator_id / device / browser / os / status / error_msg
--   为对齐前端操作日志页新增（[扩展]）；duration 存毫秒文本。
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sa_system_oper_log (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  username     VARCHAR(20),
  app          VARCHAR(50),
  method       VARCHAR(20),
  router       VARCHAR(500),
  service_name VARCHAR(30),
  ip           VARCHAR(45),
  ip_location  VARCHAR(255),
  request_data TEXT,
  duration     VARCHAR(20),
  action_type  VARCHAR(20),  -- [扩展]
  operator_id  INTEGER,      -- [扩展]
  device       VARCHAR(50),  -- [扩展]
  browser      VARCHAR(50),  -- [扩展]
  os           VARCHAR(50),  -- [扩展]
  status       INTEGER,      -- [扩展] 1 成功 / 0 失败
  error_msg    VARCHAR(500), -- [扩展]
  remark       VARCHAR(255),
  created_by   INTEGER,
  updated_by   INTEGER,
  create_time  DATETIME,
  update_time  DATETIME,
  delete_time  DATETIME
);
CREATE INDEX IF NOT EXISTS ix_sa_system_oper_log_create_time ON sa_system_oper_log (create_time);
CREATE INDEX IF NOT EXISTS ix_sa_system_oper_log_username ON sa_system_oper_log (username);

-- ---------------------------------------------------------------------------
-- 租户表 sa_system_tenant / 用户租户关联 sa_system_user_tenant
--   仅按 init.sql 建表与导入数据，不提供任何租户功能与接口。
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sa_system_tenant (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  tenant_name   VARCHAR(100) NOT NULL,
  tenant_code   VARCHAR(50) NOT NULL,
  contact_name  VARCHAR(50),
  contact_phone VARCHAR(20),
  contact_email VARCHAR(100),
  address       VARCHAR(255),
  logo_url      VARCHAR(255),
  status        INTEGER NOT NULL DEFAULT 1,
  expire_time   DATETIME,
  max_users     INTEGER NOT NULL DEFAULT 0,
  max_depts     INTEGER NOT NULL DEFAULT 0,
  max_roles     INTEGER NOT NULL DEFAULT 0,
  remark        VARCHAR(500),
  created_by    INTEGER NOT NULL DEFAULT 0,
  updated_by    INTEGER NOT NULL DEFAULT 0,
  create_time   DATETIME,
  update_time   DATETIME,
  delete_time   DATETIME
);

CREATE TABLE IF NOT EXISTS sa_system_user_tenant (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL,
  tenant_id   INTEGER NOT NULL,
  is_default  INTEGER NOT NULL DEFAULT 0,
  join_time   DATETIME,
  is_super    INTEGER NOT NULL DEFAULT 0,
  created_by  INTEGER NOT NULL DEFAULT 0,
  updated_by  INTEGER NOT NULL DEFAULT 0,
  create_time DATETIME,
  update_time DATETIME,
  delete_time DATETIME,
  status      INTEGER NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX IF NOT EXISTS ux_sa_system_user_tenant ON sa_system_user_tenant (user_id, tenant_id);

-- ---------------------------------------------------------------------------
-- 刷新令牌 sa_system_refresh_token（替代旧的 web_refresh_tokens）
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sa_system_refresh_token (
  id         VARCHAR(64) PRIMARY KEY,
  user_id    INTEGER NOT NULL,
  token_hash VARCHAR(128) NOT NULL,
  expires_at DATETIME NOT NULL,
  revoked_at DATETIME,
  created_at DATETIME,
  -- [扩展] 会话元信息：在线用户列表展示（归属地 / 终端 / 最近活跃）
  ip             VARCHAR(45),
  ip_location    VARCHAR(255),
  device         VARCHAR(32),
  os             VARCHAR(50),
  browser        VARCHAR(50),
  last_active_at DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS ux_sa_system_refresh_token_hash ON sa_system_refresh_token (token_hash);
CREATE INDEX IF NOT EXISTS ix_sa_system_refresh_token_user_id ON sa_system_refresh_token (user_id);
CREATE INDEX IF NOT EXISTS ix_sa_system_refresh_token_expires_at ON sa_system_refresh_token (expires_at);
CREATE INDEX IF NOT EXISTS ix_sa_system_refresh_token_revoked_at ON sa_system_refresh_token (revoked_at);

-- ---------------------------------------------------------------------------
-- 附件分类表 sa_system_category
--   逐字段照搬 init.sql（无租户字段）；level 保存祖先链（如 "0,1,"），
--   用于按分类树快速筛选，父分类变化时由 logic 层同步维护。
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sa_system_category (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  parent_id     INTEGER NOT NULL DEFAULT 0,
  level         VARCHAR(255),
  category_name VARCHAR(100) NOT NULL DEFAULT '',
  sort          INTEGER NOT NULL DEFAULT 0,
  status        INTEGER DEFAULT 1,
  remark        VARCHAR(255),
  created_by    INTEGER,
  updated_by    INTEGER,
  create_time   DATETIME,
  update_time   DATETIME,
  delete_time   DATETIME
);
CREATE INDEX IF NOT EXISTS ix_sa_system_category_parent_id ON sa_system_category (parent_id);

-- ---------------------------------------------------------------------------
-- 附件表 sa_system_attachment
--   storage_path 为相对上传根目录的路径（upload/2026/10/06/x.jpg），
--   url 为可直接访问的地址（/upload/2026/10/06/x.jpg）；
--   origin_name 是用户可见文件名，重命名即改它。
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sa_system_attachment (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  category_id  INTEGER DEFAULT 0,
  storage_mode INTEGER DEFAULT 1,
  origin_name  VARCHAR(255),
  object_name  VARCHAR(255) NOT NULL,
  hash         VARCHAR(64),
  mime_type    VARCHAR(255),
  storage_path VARCHAR(512) NOT NULL,
  suffix       VARCHAR(10),
  size_byte    INTEGER,
  size_info    VARCHAR(50),
  url          VARCHAR(1024) NOT NULL,
  remark       VARCHAR(255),
  created_by   INTEGER,
  updated_by   INTEGER,
  create_time  DATETIME,
  update_time  DATETIME,
  delete_time  DATETIME
);
CREATE INDEX IF NOT EXISTS ix_sa_system_attachment_category_id ON sa_system_attachment (category_id);
CREATE INDEX IF NOT EXISTS ix_sa_system_attachment_delete_time ON sa_system_attachment (delete_time);
