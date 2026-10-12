-- =============================================================================
-- 菜单种子：把原先写死在代码里的监控菜单与系统管理菜单落库（monitor + system）。
--
-- 字段约定（对齐前端 src/api/system/menu/types.ts 与 stores/permission.ts）：
--   type      1 目录 / 2 菜单 / 3 按钮 / 4 外链
--   path      前端路由地址：顶层目录用绝对路径（/monitor），子菜单用相对路径（overview）
--   component 顶层目录固定写 Layout；子菜单写视图路径（monitor/overview/index）
--   code      路由名（前端 routeName，需在同一应用内唯一）
--   slug      权限标识（前端 perm，v-hasPerm 使用，如 sys:user:create）
--   is_hidden 1 隐藏 / 2 显示；is_keep_alive 1 缓存 / 2 不缓存
--
-- 全部语句幂等：ON CONFLICT DO NOTHING + 固定主键，可随服务启动重复执行。
-- =============================================================================

-- ---------------------------------------------------------------------------
-- 监控中心（原有监控模块菜单，一个都不能少）
-- ---------------------------------------------------------------------------
INSERT INTO sa_system_menu
  (id, parent_id, name, code, slug, type, path, component, method, icon, sort, link_url,
   is_iframe, is_keep_alive, is_hidden, is_fixed_tab, is_full_page, is_always_show, redirect, params,
   generate_id, generate_key, status, remark, create_time, update_time)
VALUES
  (100, 0,   '监控中心', 'Monitor',            '',                    1, '/monitor',           'Layout',                    '', 'monitor',  10, '', 2, 2, 2, 2, 2, 1, '', '[]', 0, NULL, 1, '服务器 Agent 监控入口', localtimestamp, localtimestamp),
  (101, 100, '监控总览', 'MonitorOverview',    'monitor:overview',    2, 'overview',           'monitor/overview/index',    '', 'monitor',  11, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (102, 100, '服务器列表', 'MonitorAgents',    'monitor:agent:list',  2, 'agents',             'monitor/agent/index',       '', 'table',    12, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (103, 100, '服务器详情', 'MonitorAgentDetail', 'monitor:agent:detail', 2, 'agents/:id',      'monitor/agent-detail/index', '', 'document', 13, '', 2, 2, 1, 2, 2, 2, '', '[]', 0, NULL, 1, '参数路由，菜单中隐藏', localtimestamp, localtimestamp),
  (104, 100, '告警中心', 'MonitorAlerts',      'monitor:alert:list',  2, 'alerts',             'monitor/alert/index',       '', 'shield',   14, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (105, 100, '会话审计', 'MonitorAudit',       'monitor:audit:view',  2, 'audit',              'monitor/audit/index',       '', 'history',  15, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, 'SSH/RDP 会话记录与录像回放', localtimestamp, localtimestamp),
  (106, 100, '高危命令', 'MonitorDangerousCommand', 'monitor:dangerous-command:list', 2, 'dangerous-command', 'monitor/dangerous-command/index', '', 'warning', 16, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, 'SSH 高危命令拦截规则管理', localtimestamp, localtimestamp),
  (107, 100, '计划任务', 'MonitorJob', 'monitor:job:list', 2, 'job', 'monitor/job/index', '', 'time', 17, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '定时任务调度与管理', localtimestamp, localtimestamp),
  (108, 100, '任务日志', 'MonitorJobLog', 'monitor:job-log:list', 2, 'job-log', 'monitor/job/log', '', 'document', 18, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '计划任务执行日志', localtimestamp, localtimestamp),
  (109, 100, '告警规则', 'MonitorAlertRule', 'monitor:alert:rule:list', 2, 'alert-rule', 'monitor/alert-rule/index', '', 'bell', 19, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '资源告警阈值：全局默认与按角色配置', localtimestamp, localtimestamp),
  (110, 100, '远程桌面会话', 'MonitorRdpSession', 'monitor:rdp:view', 2, 'rdp-sessions', 'monitor/remote-session/index', '', 'monitor', 20, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, 'RDP / VNC 远程会话记录与录像回放', localtimestamp, localtimestamp) ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- 系统管理
-- ---------------------------------------------------------------------------
INSERT INTO sa_system_menu
  (id, parent_id, name, code, slug, type, path, component, method, icon, sort, link_url,
   is_iframe, is_keep_alive, is_hidden, is_fixed_tab, is_full_page, is_always_show, redirect, params,
   generate_id, generate_key, status, remark, create_time, update_time)
VALUES
  (200, 0,   '系统管理', 'System',     '',                  1, '/system', 'Layout',                  '', 'system',   20, '', 2, 2, 2, 2, 2, 1, '', '[]', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (201, 200, '用户管理', 'User',       'sys:user:list',     2, 'user',    'system/user/index',       '', 'group',    21, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (202, 200, '部门管理', 'Dept',       'sys:dept:list',     2, 'dept',    'system/dept/index',       '', 'tree',     22, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (203, 200, '角色管理', 'Role',       'sys:role:list',     2, 'role',    'system/role/index',       '', 'role',     23, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (204, 200, '岗位管理', 'Post',       'sys:post:list',     2, 'post',    'system/post/index',       '', 'security', 24, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (205, 200, '菜单管理', 'Menu',       'sys:menu:list',     2, 'menu',    'system/menu/index',       '', 'menu',     25, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (206, 200, '字典管理', 'Dict',       'sys:dict:list',     2, 'dict',    'system/dict/index',       '', 'dict',     26, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (207, 200, '系统配置', 'SysConfig',  'sys:config:list',   2, 'config',  'system/config/index',     '', 'setting',  27, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (208, 200, '通知公告', 'Notice',     'sys:notice:list',   2, 'notice',  'system/notice/index',     '', 'file',     28, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (209, 200, '操作日志', 'OperLog',    'sys:log:list',      2, 'log',     'system/log/index',        '', 'code',     29, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (210, 200, '登录日志', 'LoginLog',   'sys:login-log:list', 2, 'login-log', 'system/login-log/index', '', 'api',    30, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (211, 200, '附件管理', 'Attachment', 'sys:attachment:list', 2, 'attachment', 'system/attachment/index', '', 'upload', 31, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '附件分类树 + 附件上传', localtimestamp, localtimestamp),
  (212, 200, '缓存管理', 'Cache', 'sys:cache:list', 2, 'cache', 'system/cache/index', '', 'coin', 32, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, 'Redis / 进程内缓存浏览与清理', localtimestamp, localtimestamp),
  (213, 200, '在线用户', 'Online', 'sys:online:list', 2, 'online', 'system/online/index', '', 'connection', 33, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '登录会话列表与强制下线', localtimestamp, localtimestamp) ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- 按钮权限（type=3，slug 与前端 v-hasPerm 字面量一致）
-- ---------------------------------------------------------------------------
INSERT INTO sa_system_menu
  (id, parent_id, name, code, slug, type, path, component, method, icon, sort, link_url,
   is_iframe, is_keep_alive, is_hidden, is_fixed_tab, is_full_page, is_always_show, redirect, params,
   generate_id, generate_key, status, remark, create_time, update_time)
VALUES
  (1021, 102, '连接配置', 'MonitorAccessManage', 'monitor:access:manage', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '编辑服务器 SSH/RDP 连接信息', localtimestamp, localtimestamp),
  (1022, 102, 'SSH连接', 'MonitorSSHConnect', 'monitor:ssh:connect', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '发起 SSH 终端连接', localtimestamp, localtimestamp),
  (1023, 102, 'RDP连接', 'MonitorRDPConnect', 'monitor:rdp:connect', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '发起 RDP 远程桌面连接', localtimestamp, localtimestamp),
  (1024, 102, '告警设置', 'MonitorAlertRuleUpdate', 'monitor:alert:rule:update', 3, '', '', '', '', 4, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '设置该服务器的资源告警阈值（单机覆盖）', localtimestamp, localtimestamp),
  (1025, 102, '结束进程', 'MonitorSSHProcessKill', 'monitor:ssh:process:kill', 3, '', '', '', '', 5, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '在 SSH 终端的系统进程面板中结束进程（高危）', localtimestamp, localtimestamp),
  (1026, 102, '重启进程', 'MonitorSSHProcessRestart', 'monitor:ssh:process:restart', 3, '', '', '', '', 6, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '重启 systemd 托管的进程（高危）', localtimestamp, localtimestamp),
  (1061, 106, '新增规则', 'DangerousCommandCreate', 'monitor:dangerous-command:create', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '新增高危命令拦截规则', localtimestamp, localtimestamp),
  (1062, 106, '修改规则', 'DangerousCommandUpdate', 'monitor:dangerous-command:update', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '修改高危命令拦截规则', localtimestamp, localtimestamp),
  (1063, 106, '删除规则', 'DangerousCommandDelete', 'monitor:dangerous-command:delete', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '删除高危命令拦截规则', localtimestamp, localtimestamp),
  (1071, 107, '新增任务', 'JobCreate', 'monitor:job:create', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '新增计划任务', localtimestamp, localtimestamp),
  (1072, 107, '修改任务', 'JobUpdate', 'monitor:job:update', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '修改计划任务', localtimestamp, localtimestamp),
  (1073, 107, '删除任务', 'JobDelete', 'monitor:job:delete', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '删除计划任务', localtimestamp, localtimestamp),
  (1074, 107, '手动执行', 'JobRun', 'monitor:job:run', 3, '', '', '', '', 4, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '手动执行计划任务', localtimestamp, localtimestamp),
  (1081, 108, '清空日志', 'JobLogClean', 'monitor:job-log:delete', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '清空任务执行日志', localtimestamp, localtimestamp),
  (2011, 201, '用户新增', 'UserCreate', 'sys:user:create', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2012, 201, '用户修改', 'UserUpdate', 'sys:user:update', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2013, 201, '用户删除', 'UserDelete', 'sys:user:delete', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2014, 201, '重置密码', 'UserResetPassword', 'sys:user:reset-password', 3, '', '', '', '', 4, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2015, 201, '用户导出', 'UserExport', 'sys:user:export', 3, '', '', '', '', 5, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2016, 201, '用户导入', 'UserImport', 'sys:user:import', 3, '', '', '', '', 6, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2017, 201, '菜单设置', 'UserAssignMenu', 'sys:user:assign-menu', 3, '', '', '', '', 7, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '为用户单独授权可见菜单', localtimestamp, localtimestamp),

  (2021, 202, '部门新增', 'DeptCreate', 'sys:dept:create', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2022, 202, '部门修改', 'DeptUpdate', 'sys:dept:update', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2023, 202, '部门删除', 'DeptDelete', 'sys:dept:delete', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),

  (2031, 203, '角色新增', 'RoleCreate', 'sys:role:create', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2032, 203, '角色修改', 'RoleUpdate', 'sys:role:update', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2033, 203, '角色删除', 'RoleDelete', 'sys:role:delete', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2034, 203, '菜单设置', 'RoleAssignMenu', 'sys:role:assign-menu', 3, '', '', '', '', 4, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '为角色授权可见菜单', localtimestamp, localtimestamp),

  (2041, 204, '岗位新增', 'PostCreate', 'sys:post:create', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2042, 204, '岗位修改', 'PostUpdate', 'sys:post:update', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2043, 204, '岗位删除', 'PostDelete', 'sys:post:delete', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),

  (2051, 205, '菜单新增', 'MenuCreate', 'sys:menu:create', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2052, 205, '菜单修改', 'MenuUpdate', 'sys:menu:update', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2053, 205, '菜单删除', 'MenuDelete', 'sys:menu:delete', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),

  (2061, 206, '字典新增', 'DictCreate', 'sys:dict:create', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2062, 206, '字典修改', 'DictUpdate', 'sys:dict:update', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2063, 206, '字典删除', 'DictDelete', 'sys:dict:delete', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),

  (2071, 207, '配置新增', 'ConfigCreate', 'sys:config:create', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2072, 207, '配置修改', 'ConfigUpdate', 'sys:config:update', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2073, 207, '配置删除', 'ConfigDelete', 'sys:config:delete', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2074, 207, '刷新缓存', 'ConfigRefresh', 'sys:config:refresh', 3, '', '', '', '', 4, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),

  (2091, 209, '日志删除', 'OperLogDelete', 'sys:log:delete', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2101, 210, '登录日志删除', 'LoginLogDelete', 'sys:login-log:delete', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),

  (2081, 208, '公告新增', 'NoticeCreate', 'sys:notice:create', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2082, 208, '公告修改', 'NoticeUpdate', 'sys:notice:update', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2083, 208, '公告删除', 'NoticeDelete', 'sys:notice:delete', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2084, 208, '公告发布', 'NoticePublish', 'sys:notice:publish', 3, '', '', '', '', 4, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2085, 208, '公告撤回', 'NoticeRevoke', 'sys:notice:revoke', 3, '', '', '', '', 5, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),

  (2111, 211, '分类新增', 'AttachmentCategoryCreate', 'sys:attachment:category:create', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2112, 211, '分类修改', 'AttachmentCategoryUpdate', 'sys:attachment:category:update', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2113, 211, '分类删除', 'AttachmentCategoryDelete', 'sys:attachment:category:delete', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2114, 211, '附件上传', 'AttachmentUpload', 'sys:attachment:upload', 3, '', '', '', '', 4, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2115, 211, '附件修改', 'AttachmentUpdate', 'sys:attachment:update', 3, '', '', '', '', 5, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),
  (2116, 211, '附件删除', 'AttachmentDelete', 'sys:attachment:delete', 3, '', '', '', '', 6, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),

  (2121, 212, '缓存清理', 'CacheClear', 'sys:cache:clear', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),

  (2131, 213, '强制下线', 'OnlineKick', 'sys:online:kick', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', localtimestamp, localtimestamp),

  (1027, 102, '文件读取', 'SftpFileRead', 'monitor:sftp:read', 3, '', '', '', '', 7, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, 'SSH 终端 SFTP 在线读取远程文件', localtimestamp, localtimestamp),
  (1028, 102, '文件写入', 'SftpFileWrite', 'monitor:sftp:write', 3, '', '', '', '', 8, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, 'SSH 终端 SFTP 在线写入远程文件', localtimestamp, localtimestamp),
  -- 服务操作是继「结束进程」之后的又一类高危操作：不会复用 monitor:ssh:connect，
  -- 能打开终端不等于应该能停服务。服务端的硬拦截见 internal/remote/servicelist.go。
  (1029, 102, '服务操作', 'MonitorSSHServiceManage', 'monitor:ssh:service:manage', 3, '', '', '', '', 9, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, 'SSH 控制台系统服务面板的启停/重启/开机自启（高危）', localtimestamp, localtimestamp),
  -- 清理采集数据：删除该主机在时序库中的全部指标与 PostgreSQL 的采集回执（metric_receipts）。
  -- 不可逆且影响历史曲线，因此不复用任何查看类标识。
  (1030, 102, '清理数据', 'MonitorMetricsClean', 'monitor:metrics:clean', 3, '', '', '', '', 10, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '清理该服务器在时序库中的采集指标与采集回执（不可逆）', localtimestamp, localtimestamp) ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- 列表权限（type=3，只读授权入口）
--   模块菜单节点（type=2）本身已带 sys:<模块>:list，但开启「父子联动」勾选模块时
--   会把该模块下所有按钮一并勾上，做不到「只给查看权限」；关掉父子联动再单独勾模块
--   虽然也能实现，但每次授权都要多一步、且不直观。
--   这里给每个模块补一个「列表」按钮作为单点勾选入口：父子联动开启时只勾它，
--   模块菜单节点变为半选并随链路入库，最终就是「能进页面、看不到操作按钮」的只读授权。
--   id 规则：模块菜单 id 末位补 0（201 → 2010）；sort=0 使其排在其他按钮之前。
--   注意：它与模块菜单节点的 slug 重复（库内无 slug 唯一索引，代码也无唯一性校验，
--   权限集合按 map 去重），因此不要把 sys:<模块>:list 拿去给列表接口加鉴权，
--   否则从未勾过该按钮的既有角色会突然 403。
-- ---------------------------------------------------------------------------
INSERT INTO sa_system_menu
  (id, parent_id, name, code, slug, type, path, component, method, icon, sort, link_url,
   is_iframe, is_keep_alive, is_hidden, is_fixed_tab, is_full_page, is_always_show, redirect, params,
   generate_id, generate_key, status, remark, create_time, update_time)
VALUES
  (2010, 201, '列表', 'UserList',       'sys:user:list',       3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', localtimestamp, localtimestamp),
  (2020, 202, '列表', 'DeptList',       'sys:dept:list',       3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', localtimestamp, localtimestamp),
  (2030, 203, '列表', 'RoleList',       'sys:role:list',       3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', localtimestamp, localtimestamp),
  (2040, 204, '列表', 'PostList',       'sys:post:list',       3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', localtimestamp, localtimestamp),
  (2050, 205, '列表', 'MenuList',       'sys:menu:list',       3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', localtimestamp, localtimestamp),
  (2060, 206, '列表', 'DictList',       'sys:dict:list',       3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', localtimestamp, localtimestamp),
  (2070, 207, '列表', 'ConfigList',     'sys:config:list',     3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', localtimestamp, localtimestamp),
  (2080, 208, '列表', 'NoticeList',     'sys:notice:list',     3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', localtimestamp, localtimestamp),
  (2090, 209, '列表', 'OperLogList',    'sys:log:list',        3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', localtimestamp, localtimestamp),
  (2100, 210, '列表', 'LoginLogList',   'sys:login-log:list',  3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', localtimestamp, localtimestamp),
  (2110, 211, '列表', 'AttachmentList', 'sys:attachment:list', 3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', localtimestamp, localtimestamp),
  (2120, 212, '列表', 'CacheList',      'sys:cache:list',      3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', localtimestamp, localtimestamp),
  (2130, 213, '列表', 'OnlineList',     'sys:online:list',     3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', localtimestamp, localtimestamp) ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- 列表清理权限（type=3）
--   四个审计/日志类列表支持「多选 → 批量清理」：告警记录、SSH 会话审计、
--   任务日志、远程桌面会话。清理不可逆，因此各自独立权限标识，不复用 read/view
--   （只读角色不该因为能看列表就能删记录）。
--   「任务日志」与既有的「清空日志」（1081）共用 monitor:job-log:delete：
--   同一模块的同类操作不再拆标识；库内 slug 允许重复，权限集合按 map 去重。
-- ---------------------------------------------------------------------------
INSERT INTO sa_system_menu
  (id, parent_id, name, code, slug, type, path, component, method, icon, sort, link_url,
   is_iframe, is_keep_alive, is_hidden, is_fixed_tab, is_full_page, is_always_show, redirect, params,
   generate_id, generate_key, status, remark, create_time, update_time)
VALUES
  (1041, 104, '清理告警', 'MonitorAlertDelete', 'monitor:alert:delete', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '批量清理告警记录（仍在活动的告警会被同时结束）', localtimestamp, localtimestamp),
  (1051, 105, '清理会话', 'MonitorAuditDelete', 'monitor:audit:delete', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '批量清理 SSH 会话审计记录（含命令流与录像）', localtimestamp, localtimestamp),
  (1082, 108, '清理日志', 'JobLogDelete', 'monitor:job-log:delete', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '批量清理选中的任务执行日志', localtimestamp, localtimestamp),
  (1101, 110, '清理会话', 'MonitorRdpSessionDelete', 'monitor:rdp:delete', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '批量清理远程桌面会话记录（含录像）', localtimestamp, localtimestamp) ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- 角色菜单授权
--   角色 1 超级管理员：全部菜单
--   角色 2 运维人员：监控中心 + 日志
--   角色 3 只读访客：监控中心（只读）
--
--   注意：上面的清理按钮默认不授予角色 2/3（与既有「清空日志」1081 的取舍一致）：
--   删除类权限需要管理员在「角色管理 → 菜单设置」里显式勾选，超管自动全量。
-- ---------------------------------------------------------------------------
INSERT INTO sa_system_role_menu (role_id, menu_id) SELECT 1, id FROM sa_system_menu ON CONFLICT DO NOTHING;
INSERT INTO sa_system_role_menu (role_id, menu_id) VALUES
  (2, 100), (2, 101), (2, 102), (2, 103), (2, 104), (2, 106), (2, 107), (2, 108),
  (2, 200), (2, 209), (2, 210), (2, 213),
  (3, 100), (3, 101), (3, 102), (3, 103), (3, 104), (3, 106), (3, 107), (3, 108),
  (2, 1027), (2, 1028) ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- 内置菜单 slug 回填（幂等；只在为空时写入，绝不覆盖人工修改）
--
--   为什么需要：上面的种子用的是 ON CONFLICT DO NOTHING —— 已存在的行不会被更新，于是
--   早期插入的行，后来在种子里补上的 slug 永远到不了老库。症状很隐蔽：
--   「侧边栏能看到菜单，点进去却提示权限不足」——因为菜单可见性按**菜单 ID**判定
--   （sa_system_role_menu / sa_system_user_menu），而接口鉴权按 **slug** 判定
--   （roleSlugs 会过滤掉空 slug），两者用的是同一批菜单行、口径却不同。
--   附件管理、缓存管理、在线用户三个菜单就是这么漏掉的。
--
--   维护约定：新增内置菜单（带 slug）时，同步在下面补一行；
--   菜单节点的列表权限与同模块的「列表」按钮共用同一个 slug，因此用 IN 合并。
-- ---------------------------------------------------------------------------
UPDATE sa_system_menu SET slug = 'monitor:overview' WHERE id = 101 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:agent:list' WHERE id = 102 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:agent:detail' WHERE id = 103 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:alert:list' WHERE id = 104 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:audit:view' WHERE id = 105 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:dangerous-command:list' WHERE id = 106 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:job:list' WHERE id = 107 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:job-log:list' WHERE id = 108 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:alert:rule:list' WHERE id = 109 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:access:manage' WHERE id = 1021 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:ssh:connect' WHERE id = 1022 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:rdp:connect' WHERE id = 1023 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:alert:rule:update' WHERE id = 1024 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:ssh:process:kill' WHERE id = 1025 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:ssh:process:restart' WHERE id = 1026 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:sftp:read' WHERE id = 1027 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:sftp:write' WHERE id = 1028 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:ssh:service:manage' WHERE id = 1029 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:metrics:clean' WHERE id = 1030 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:rdp:view' WHERE id = 110 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:dangerous-command:create' WHERE id = 1061 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:dangerous-command:update' WHERE id = 1062 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:dangerous-command:delete' WHERE id = 1063 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:job:create' WHERE id = 1071 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:job:update' WHERE id = 1072 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:job:delete' WHERE id = 1073 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:job:run' WHERE id = 1074 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:job-log:delete' WHERE id = 1081 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:user:list' WHERE id IN (201, 2010) AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:dept:list' WHERE id IN (202, 2020) AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:role:list' WHERE id IN (203, 2030) AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:post:list' WHERE id IN (204, 2040) AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:menu:list' WHERE id IN (205, 2050) AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:dict:list' WHERE id IN (206, 2060) AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:config:list' WHERE id IN (207, 2070) AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:notice:list' WHERE id IN (208, 2080) AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:log:list' WHERE id IN (209, 2090) AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:login-log:list' WHERE id IN (210, 2100) AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:attachment:list' WHERE id IN (211, 2110) AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:cache:list' WHERE id IN (212, 2120) AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:online:list' WHERE id IN (213, 2130) AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:user:create' WHERE id = 2011 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:user:update' WHERE id = 2012 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:user:delete' WHERE id = 2013 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:user:reset-password' WHERE id = 2014 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:user:export' WHERE id = 2015 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:user:import' WHERE id = 2016 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:user:assign-menu' WHERE id = 2017 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:dept:create' WHERE id = 2021 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:dept:update' WHERE id = 2022 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:dept:delete' WHERE id = 2023 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:role:create' WHERE id = 2031 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:role:update' WHERE id = 2032 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:role:delete' WHERE id = 2033 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:role:assign-menu' WHERE id = 2034 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:post:create' WHERE id = 2041 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:post:update' WHERE id = 2042 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:post:delete' WHERE id = 2043 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:menu:create' WHERE id = 2051 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:menu:update' WHERE id = 2052 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:menu:delete' WHERE id = 2053 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:dict:create' WHERE id = 2061 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:dict:update' WHERE id = 2062 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:dict:delete' WHERE id = 2063 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:config:create' WHERE id = 2071 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:config:update' WHERE id = 2072 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:config:delete' WHERE id = 2073 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:config:refresh' WHERE id = 2074 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:notice:create' WHERE id = 2081 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:notice:update' WHERE id = 2082 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:notice:delete' WHERE id = 2083 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:notice:publish' WHERE id = 2084 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:notice:revoke' WHERE id = 2085 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:log:delete' WHERE id = 2091 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:login-log:delete' WHERE id = 2101 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:attachment:category:create' WHERE id = 2111 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:attachment:category:update' WHERE id = 2112 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:attachment:category:delete' WHERE id = 2113 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:attachment:upload' WHERE id = 2114 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:attachment:update' WHERE id = 2115 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:attachment:delete' WHERE id = 2116 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:cache:clear' WHERE id = 2121 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'sys:online:kick' WHERE id = 2131 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:alert:delete' WHERE id = 1041 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:audit:delete' WHERE id = 1051 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:job-log:delete' WHERE id = 1082 AND (slug IS NULL OR slug = '');
UPDATE sa_system_menu SET slug = 'monitor:rdp:delete' WHERE id = 1101 AND (slug IS NULL OR slug = '');
