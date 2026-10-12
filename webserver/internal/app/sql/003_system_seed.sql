-- =============================================================================
-- 系统基础数据种子：租户、角色、部门、岗位、字典、配置。
--
-- 说明：
--   * 租户相关的表（sa_system_tenant / sa_system_user_tenant）按 init.sql 原样导入，
--     但系统不实现任何租户功能，仅作数据保真。
--   * 用户与用户关联数据（sa_system_user / user_role / user_dept / user_post /
--     user_tenant）由 seed.go 生成，因为密码需要用 Go bcrypt 现场计算。
--   * 全部语句幂等：ON CONFLICT DO NOTHING + 固定主键。
-- =============================================================================

-- ---------------------------------------------------------------------------
-- 时区一次性校正（兼容早期种子数据）
--
-- 早期版本的种子 SQL 用 datetime('now')（UTC）写入，后续版本改为按本地时间写入。
-- 为兼容已经初始化的库，这里在标记缺失时把「由种子写入」的表时间整体从 UTC
-- 换算为本地时间，随后写入标记，保证只执行一次。
-- 仅覆盖种子表，程序按本地时间写入的日志、用户数据不受影响。
-- ---------------------------------------------------------------------------
UPDATE sa_system_menu        SET create_time = create_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai', update_time = update_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai'
  WHERE create_time IS NOT NULL AND (SELECT value FROM sa_system_meta WHERE "key" = 'seed_time_localtime') IS NULL;
UPDATE sa_system_post        SET create_time = create_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai', update_time = update_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai'
  WHERE create_time IS NOT NULL AND (SELECT value FROM sa_system_meta WHERE "key" = 'seed_time_localtime') IS NULL;
UPDATE sa_system_dept        SET create_time = create_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai', update_time = update_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai'
  WHERE create_time IS NOT NULL AND (SELECT value FROM sa_system_meta WHERE "key" = 'seed_time_localtime') IS NULL;
UPDATE sa_system_role        SET create_time = create_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai', update_time = update_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai'
  WHERE create_time IS NOT NULL AND (SELECT value FROM sa_system_meta WHERE "key" = 'seed_time_localtime') IS NULL;
UPDATE sa_system_dict_type   SET create_time = create_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai', update_time = update_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai'
  WHERE create_time IS NOT NULL AND (SELECT value FROM sa_system_meta WHERE "key" = 'seed_time_localtime') IS NULL;
UPDATE sa_system_dict_data   SET create_time = create_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai', update_time = update_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai'
  WHERE create_time IS NOT NULL AND (SELECT value FROM sa_system_meta WHERE "key" = 'seed_time_localtime') IS NULL;
UPDATE sa_system_config      SET create_time = create_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai', update_time = update_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai'
  WHERE create_time IS NOT NULL AND (SELECT value FROM sa_system_meta WHERE "key" = 'seed_time_localtime') IS NULL;
UPDATE sa_system_config_group SET create_time = create_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai', update_time = update_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai'
  WHERE create_time IS NOT NULL AND (SELECT value FROM sa_system_meta WHERE "key" = 'seed_time_localtime') IS NULL;
UPDATE sa_system_tenant      SET create_time = create_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai', update_time = update_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai'
  WHERE create_time IS NOT NULL AND (SELECT value FROM sa_system_meta WHERE "key" = 'seed_time_localtime') IS NULL;
UPDATE sa_system_user_role   SET create_time = create_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai', update_time = update_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai'
  WHERE create_time IS NOT NULL AND (SELECT value FROM sa_system_meta WHERE "key" = 'seed_time_localtime') IS NULL;
UPDATE sa_system_user_dept   SET create_time = create_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai', update_time = update_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai'
  WHERE create_time IS NOT NULL AND (SELECT value FROM sa_system_meta WHERE "key" = 'seed_time_localtime') IS NULL;
UPDATE sa_system_user_post   SET create_time = create_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai', update_time = update_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai'
  WHERE create_time IS NOT NULL AND (SELECT value FROM sa_system_meta WHERE "key" = 'seed_time_localtime') IS NULL;
UPDATE sa_system_user_tenant SET create_time = create_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai', update_time = update_time AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai'
  WHERE create_time IS NOT NULL AND (SELECT value FROM sa_system_meta WHERE "key" = 'seed_time_localtime') IS NULL;

-- ---------------------------------------------------------------------------
-- 租户（仅数据保真，不提供租户功能）
-- ---------------------------------------------------------------------------
INSERT INTO sa_system_tenant
  (id, tenant_name, tenant_code, contact_name, contact_phone, contact_email, address, logo_url,
   status, max_users, max_depts, max_roles, remark, created_by, updated_by, create_time, update_time)
VALUES
  (1, '默认租户', 'default', '系统管理员', '13800000000', 'admin@localhost', '', '', 1, 0, 0, 0, '单租户部署，仅作数据保真', 0, 0, localtimestamp, localtimestamp) ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- 角色 sa_system_role
--   data_scope：1 全部 / 2 本部门及下属 / 3 本部门 / 4 仅本人 / 5 自定义
-- ---------------------------------------------------------------------------
INSERT INTO sa_system_role
  (id, parent_id, name, code, level, data_scope, remark, sort, tenant_id, status,
   created_by, updated_by, create_time, update_time)
VALUES
  (1, 0, '超级管理员', 'super_admin', 100, 1, '系统内置角色，拥有全部权限', 1, 0, 1, 1, 1, localtimestamp, localtimestamp),
  (2, 0, '运维人员',   'ops',         50,  2, '负责服务器监控与日志查看',   2, 0, 1, 1, 1, localtimestamp, localtimestamp),
  (3, 0, '只读访客',   'viewer',      10,  4, '仅可查看监控数据',           3, 0, 1, 1, 1, localtimestamp, localtimestamp) ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- 部门 sa_system_dept（level 为祖级列表，便于查子孙）
-- ---------------------------------------------------------------------------
INSERT INTO sa_system_dept
  (id, parent_id, name, code, leader_id, level, tenant_id, sort, status, remark,
   created_by, updated_by, create_time, update_time)
VALUES
  (1, 0, '监控平台',   'MONITOR',  NULL, '0,',     0, 1, 1, '', 1, 1, localtimestamp, localtimestamp),
  (2, 1, '平台运维部', 'OPS',      NULL, '0,1,',   0, 1, 1, '', 1, 1, localtimestamp, localtimestamp),
  (3, 1, '研发中心',   'RD',       NULL, '0,1,',   0, 2, 1, '', 1, 1, localtimestamp, localtimestamp),
  (4, 3, '后端组',     'BACKEND',  NULL, '0,1,3,', 0, 1, 1, '', 1, 1, localtimestamp, localtimestamp),
  (5, 3, '前端组',     'FRONTEND', NULL, '0,1,3,', 0, 2, 1, '', 1, 1, localtimestamp, localtimestamp) ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- 岗位 sa_system_post
-- ---------------------------------------------------------------------------
INSERT INTO sa_system_post
  (id, name, code, sort, status, tenant_id, remark, created_by, updated_by, create_time, update_time)
VALUES
  (1, '系统管理员', 'sysadmin', 1, 1, 0, '', 1, 1, localtimestamp, localtimestamp),
  (2, '运维工程师', 'ops',      2, 1, 0, '', 1, 1, localtimestamp, localtimestamp),
  (3, '开发工程师', 'dev',      3, 1, 0, '', 1, 1, localtimestamp, localtimestamp) ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- 字典类型 sa_system_dict_type
-- ---------------------------------------------------------------------------
INSERT INTO sa_system_dict_type
  (id, name, code, status, remark, created_by, updated_by, create_time, update_time)
VALUES
  (1, '性别',       'gender',      1, '', 1, 1, localtimestamp, localtimestamp),
  (2, '数据状态',   'data_status', 1, '', 1, 1, localtimestamp, localtimestamp),
  (3, '是否',       'yes_or_no',   1, '', 1, 1, localtimestamp, localtimestamp),
  (4, '公告类型',   'notice_type', 1, '', 1, 1, localtimestamp, localtimestamp),
  (5, '公告级别',   'notice_level', 1, '', 1, 1, localtimestamp, localtimestamp),
  (6, '日志状态',   'log_status',  1, '', 1, 1, localtimestamp, localtimestamp) ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- 字典数据 sa_system_dict_data（code 为字典标识，tag_type 为标签样式短码）
-- ---------------------------------------------------------------------------
INSERT INTO sa_system_dict_data
  (id, type_id, label, value, color, code, tag_type, sort, status, remark,
   created_by, updated_by, create_time, update_time)
VALUES
  (1, 1, '男',   '1', '#5d87ff', 'gender', 'P', 1, 1, '', 1, 1, localtimestamp, localtimestamp),
  (2, 1, '女',   '2', '#ff4500', 'gender', 'D', 2, 1, '', 1, 1, localtimestamp, localtimestamp),
  (3, 1, '未知', '0', '#909399', 'gender', 'N', 3, 1, '', 1, 1, localtimestamp, localtimestamp),

  (4, 2, '正常', '1', '#60c041', 'data_status', 'S', 1, 1, '', 1, 1, localtimestamp, localtimestamp),
  (5, 2, '停用', '0', '#ff4d4f', 'data_status', 'D', 2, 1, '', 1, 1, localtimestamp, localtimestamp),

  (6, 3, '是',   '1', '#60c041', 'yes_or_no', 'S', 1, 1, '', 1, 1, localtimestamp, localtimestamp),
  (7, 3, '否',   '0', '#ff4d4f', 'yes_or_no', 'D', 2, 1, '', 1, 1, localtimestamp, localtimestamp),

  (8, 4, '通知', '1', '#00ced1', 'notice_type', 'I', 1, 1, '', 1, 1, localtimestamp, localtimestamp),
  (9, 4, '公告', '2', '#5d87ff', 'notice_type', 'P', 2, 1, '', 1, 1, localtimestamp, localtimestamp),

  (10, 5, '普通', 'normal',  '#909399', 'notice_level', 'N', 1, 1, '', 1, 1, localtimestamp, localtimestamp),
  (11, 5, '重要', 'high',    '#ff8c00', 'notice_level', 'W', 2, 1, '', 1, 1, localtimestamp, localtimestamp),
  (12, 5, '紧急', 'urgent',  '#ff4d4f', 'notice_level', 'D', 3, 1, '', 1, 1, localtimestamp, localtimestamp),

  (13, 6, '成功', '1', '#60c041', 'log_status', 'S', 1, 1, '', 1, 1, localtimestamp, localtimestamp),
  (14, 6, '失败', '0', '#ff4d4f', 'log_status', 'D', 2, 1, '', 1, 1, localtimestamp, localtimestamp) ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- 配置分组 sa_system_config_group / 配置项 sa_system_config
-- ---------------------------------------------------------------------------
INSERT INTO sa_system_config_group
  (id, name, code, remark, created_by, updated_by, create_time, update_time)
VALUES
  (1, '站点配置', 'site_config', '', 1, 1, localtimestamp, localtimestamp),
  (2, '上传配置', 'upload_config', '', 1, 1, localtimestamp, localtimestamp),
  (3, '告警配置', 'alert_config', '告警数据保留策略，由计划任务 cleanAlertData 读取', 1, 1, localtimestamp, localtimestamp) ON CONFLICT DO NOTHING;

INSERT INTO sa_system_config
  (id, group_id, "key", value, name, input_type, config_select_data, sort, remark,
   created_by, updated_by, create_time, update_time)
VALUES
  (1, 1, 'site_name',          '服务器监控运维平台', '网站名称',   'input',    NULL, 1, '', 1, 1, localtimestamp, localtimestamp),
  (2, 1, 'site_desc',          'Agent 采集 + 集中监控的后台管理端', '网站描述', 'textarea', NULL, 2, '', 1, 1, localtimestamp, localtimestamp),
  (3, 1, 'site_copyright',     'Copyright © 2026 Monitor Team', '版权信息', 'textarea', NULL, 3, '', 1, 1, localtimestamp, localtimestamp),
  (4, 1, 'site_record_number', '', '网站备案号', 'input', NULL, 4, '', 1, 1, localtimestamp, localtimestamp),
  (5, 2, 'upload_mode',        '1', '上传模式', 'select',
      '[{"label":"本地上传","value":"1"},{"label":"阿里云OSS","value":"2"},{"label":"腾讯云COS","value":"4"}]',
      5, '', 1, 1, localtimestamp, localtimestamp),
  (6, 2, 'upload_size',        '52428800', '上传大小', 'input', NULL, 6, '单位 Byte，1MB=1024*1024Byte', 1, 1, localtimestamp, localtimestamp),
  (7, 2, 'upload_allow_file',  'txt,doc,docx,xls,xlsx,ppt,pptx,rar,zip,7z,pdf,md,jpg,png,jpeg', '文件类型', 'input', NULL, 7, '', 1, 1, localtimestamp, localtimestamp),
  (8, 2, 'upload_allow_image', 'jpg,jpeg,png,gif,svg,bmp', '图片类型', 'input', NULL, 8, '', 1, 1, localtimestamp, localtimestamp),
  -- 告警数据保留策略：由计划任务 cleanAlertData 读取，改键名需同步
  -- internal/job/service.go 的 ConfigKeyAlert*RetentionDays 常量。
  -- 取值 0 或负数表示「该条策略不清理」。
  (9, 3, 'alert_retention_events_days', '90', '告警事件保留天数', 'input', NULL, 9,
      '删除 alert_events 中超过该天数的事件；0 表示不清理', 1, 1, localtimestamp, localtimestamp),
  (10, 3, 'alert_retention_alerts_days', '180', '已结束告警保留天数', 'input', NULL, 10,
      '删除 alerts 中结束时间超过该天数的告警；0 表示不清理', 1, 1, localtimestamp, localtimestamp) ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- 附件分类（数据源自 database/init.sql 的 sa_system_category）
--   level 是祖先链（"0,1,"），与 init.sql 的组集关系统一口径。
-- ---------------------------------------------------------------------------
INSERT INTO sa_system_category
  (id, parent_id, level, category_name, sort, status, remark, created_by, updated_by, create_time, update_time)
VALUES
  (1, 0, '0,',   '全部分类', 100, 1, '默认根分类',         1, 1, localtimestamp, localtimestamp),
  (2, 1, '0,1,', '图片分类', 100, 1, '',                   1, 1, localtimestamp, localtimestamp),
  (3, 1, '0,1,', '文件分类', 100, 1, '',                   1, 1, localtimestamp, localtimestamp),
  (4, 1, '0,1,', '系统图片', 100, 1, '头像等系统内置图片', 1, 1, localtimestamp, localtimestamp),
  (5, 1, '0,1,', '其他分类', 100, 1, '',                   1, 1, localtimestamp, localtimestamp) ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- 记录种子写入完成标记：后续启动不再执行上面的时区校正。
-- ---------------------------------------------------------------------------
INSERT INTO sa_system_meta ("key", value, remark, update_time)
VALUES ('seed_time_localtime', 'done', '种子数据时间已按本地时区写入', localtimestamp) ON CONFLICT DO NOTHING;
