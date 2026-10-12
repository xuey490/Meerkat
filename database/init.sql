-- ===========================================================================
-- PostgreSQL 备份（结构 + 数据 + 索引 + 序列）
-- 数据库   : postgres
-- 导出时间 : 2026-10-13 00:23:23
-- 生成者   : webserver internal/pgdump
-- 对象统计 : schema 1 / 表 48 / 序列 42 / 索引 135 / 约束 45
-- 数据范围 : 全量数据，但 31 张表按配置只导结构（文件内以「跳过数据」标注）
-- ===========================================================================
-- 导入方式（先在目标实例建好空库，再执行本文件）：
--   psql -h <host> -p 5432 -U <user> -d <db> -v ON_ERROR_STOP=1 -f 本文件
--   或 pgAdmin：选中目标数据库 → Query Tool → 打开本文件 → 执行
--
-- 说明：
--   1. 文件整体包在 BEGIN/COMMIT 中，任何一条语句失败都会整体回滚，不会留下半成品；
--   2. 表以 DROP TABLE IF EXISTS ... CASCADE + CREATE TABLE 重建，会覆盖目标库的同名表；
--   3. 自增列用 GENERATED ... AS IDENTITY 还原，文件末尾的 setval 会把序列推进到
--      属主列的当前最大值，因此导入后可以直接新增数据，不会主键冲突；
--   4. search_path 被显式置空，所有对象都写成全限定名，导入不依赖目标库的 search_path；
--   5. 结构与数据取自同一个 REPEATABLE READ 只读快照，导出期间业务继续写入也不会前后不一致。
-- ===========================================================================

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET client_min_messages = warning;
SET row_security = off;

BEGIN;

--
-- Schema
--

CREATE SCHEMA IF NOT EXISTS "public";

--
-- 清理同名对象（让本文件可重复执行）
--

DROP TABLE IF EXISTS "public"."agent_ip_histories" CASCADE;
DROP TABLE IF EXISTS "public"."agents" CASCADE;
DROP TABLE IF EXISTS "public"."alert_events" CASCADE;
DROP TABLE IF EXISTS "public"."alert_rules" CASCADE;
DROP TABLE IF EXISTS "public"."alerts" CASCADE;
DROP TABLE IF EXISTS "public"."audit_records" CASCADE;
DROP TABLE IF EXISTS "public"."config_documents" CASCADE;
DROP TABLE IF EXISTS "public"."dangerous_commands" CASCADE;
DROP TABLE IF EXISTS "public"."docker_container_states" CASCADE;
DROP TABLE IF EXISTS "public"."maintenance_windows" CASCADE;
DROP TABLE IF EXISTS "public"."metric_receipts" CASCADE;
DROP TABLE IF EXISTS "public"."outbox_events" CASCADE;
DROP TABLE IF EXISTS "public"."rdp_sessions" CASCADE;
DROP TABLE IF EXISTS "public"."remote_endpoints" CASCADE;
DROP TABLE IF EXISTS "public"."sa_job" CASCADE;
DROP TABLE IF EXISTS "public"."sa_job_log" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_attachment" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_category" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_config" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_config_group" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_dept" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_dict_data" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_dict_type" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_login_log" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_menu" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_meta" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_notice" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_notice_read" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_oper_log" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_post" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_refresh_token" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_role" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_role_dept" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_role_menu" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_tenant" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_user" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_user_dept" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_user_menu" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_user_post" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_user_role" CASCADE;
DROP TABLE IF EXISTS "public"."sa_system_user_tenant" CASCADE;
DROP TABLE IF EXISTS "public"."service_probe_states" CASCADE;
DROP TABLE IF EXISTS "public"."ssh_commands" CASCADE;
DROP TABLE IF EXISTS "public"."ssh_sessions" CASCADE;
DROP TABLE IF EXISTS "public"."t_history_record" CASCADE;
DROP TABLE IF EXISTS "public"."t_memory_detail" CASCADE;
DROP TABLE IF EXISTS "public"."t_system_model" CASCADE;
DROP TABLE IF EXISTS "public"."t_system_setting" CASCADE;
DROP SEQUENCE IF EXISTS "public"."agent_ip_histories_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."alert_events_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."alert_rules_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."alerts_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."audit_records_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."config_documents_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."dangerous_commands_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."docker_container_states_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."maintenance_windows_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."metric_receipts_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."outbox_events_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."rdp_sessions_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."remote_endpoints_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_job_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_job_job_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_job_log_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_system_attachment_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_system_category_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_system_config_group_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_system_config_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_system_dept_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_system_dict_data_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_system_dict_type_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_system_login_log_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_system_menu_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_system_notice_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_system_notice_read_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_system_oper_log_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_system_post_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_system_role_dept_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_system_role_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_system_role_menu_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_system_tenant_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_system_user_dept_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_system_user_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_system_user_menu_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_system_user_post_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_system_user_role_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."sa_system_user_tenant_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."service_probe_states_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."ssh_commands_id_seq" CASCADE;
DROP SEQUENCE IF EXISTS "public"."ssh_sessions_id_seq" CASCADE;

--
-- 序列（必须在建表之前：serial 列的默认值引用它）
--

CREATE SEQUENCE "public"."agent_ip_histories_id_seq"
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE "public"."alert_events_id_seq"
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE "public"."alert_rules_id_seq"
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE "public"."alerts_id_seq"
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE "public"."audit_records_id_seq"
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE "public"."config_documents_id_seq"
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE "public"."dangerous_commands_id_seq"
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE "public"."docker_container_states_id_seq"
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE "public"."maintenance_windows_id_seq"
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE "public"."metric_receipts_id_seq"
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE "public"."outbox_events_id_seq"
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE "public"."rdp_sessions_id_seq"
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE "public"."remote_endpoints_id_seq"
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE "public"."sa_job_id_seq"
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE "public"."sa_job_job_id_seq"
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE "public"."sa_job_log_id_seq"
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE "public"."service_probe_states_id_seq"
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE "public"."ssh_commands_id_seq"
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE SEQUENCE "public"."ssh_sessions_id_seq"
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- 表结构
--

-- 表 "public"."agent_ip_histories"
CREATE TABLE "public"."agent_ip_histories" (
    "id" bigint DEFAULT nextval('public.agent_ip_histories_id_seq'::regclass) NOT NULL,
    "agent_id" character varying(128),
    "ip_address" character varying(64),
    "first_seen" timestamp with time zone,
    "last_seen" timestamp with time zone
);
ALTER TABLE ONLY "public"."agent_ip_histories" ADD CONSTRAINT "agent_ip_histories_pkey_1" PRIMARY KEY (id);

-- 表 "public"."agents"
CREATE TABLE "public"."agents" (
    "id" character varying(128) NOT NULL,
    "hostname" character varying(255),
    "last_heartbeat_at" timestamp with time zone,
    "last_metric_at" timestamp with time zone,
    "last_sequence" bigint,
    "created_at" timestamp with time zone,
    "updated_at" timestamp with time zone,
    "current_ip" character varying(64),
    "os" character varying(64),
    "architecture" character varying(32),
    "environment" character varying(64),
    "site" character varying(64),
    "role" character varying(64),
    "labels_json" jsonb,
    "agent_version" character varying(64),
    "telegraf_version" character varying(64),
    "config_version" character varying(128),
    "boot_id" character varying(128),
    "online_state" character varying(16),
    "telegraf_state" character varying(16),
    "last_telegraf_metric" timestamp with time zone,
    "queue_bytes" bigint,
    "dropped_batches" bigint,
    "telegraf_running" boolean,
    "telegraf_pid" bigint,
    "agent_started_at" timestamp with time zone,
    "agent_memory_bytes" bigint,
    "agent_goroutines" bigint,
    "queue_capacity_bytes" bigint,
    "queue_usage_percent" numeric,
    "queue_alert_level" text,
    "os_version" text,
    "system_time" timestamp with time zone,
    "memory_total_bytes" bigint,
    "memory_used_bytes" bigint,
    "process_count" bigint,
    "listening_port_count" bigint,
    "load1" numeric,
    "system_uptime_seconds" bigint,
    "cpu_model" character varying(255),
    "physical_cpu_cores" bigint,
    "logical_cpu_cores" bigint,
    "hardware_temperature_c" numeric,
    "processes_json" jsonb,
    "temperatures_json" jsonb
);
ALTER TABLE ONLY "public"."agents" ADD CONSTRAINT "agents_pkey_1" PRIMARY KEY (id);

-- 表 "public"."alert_events"
CREATE TABLE "public"."alert_events" (
    "id" bigint DEFAULT nextval('public.alert_events_id_seq'::regclass) NOT NULL,
    "alert_id" bigint,
    "agent_id" character varying(128),
    "metric_key" character varying(64),
    "event" character varying(32),
    "from_state" character varying(16),
    "to_state" character varying(16),
    "value" numeric,
    "message" text,
    "created_at" timestamp with time zone
);
ALTER TABLE ONLY "public"."alert_events" ADD CONSTRAINT "alert_events_pkey_1" PRIMARY KEY (id);

-- 表 "public"."alert_rules"
CREATE TABLE "public"."alert_rules" (
    "id" bigint DEFAULT nextval('public.alert_rules_id_seq'::regclass) NOT NULL,
    "name" character varying(255),
    "metric" character varying(128),
    "warning_threshold" numeric,
    "critical_threshold" numeric,
    "trigger_cycles" bigint,
    "recovery_cycles" bigint,
    "enabled" boolean,
    "selector_json" jsonb,
    "created_at" timestamp with time zone,
    "updated_at" timestamp with time zone,
    "metric_key" character varying(64),
    "scope" character varying(16),
    "target" character varying(128),
    "for_seconds" bigint,
    "breach_ratio" numeric,
    "remind_interval_seconds" bigint,
    "cooldown_seconds" bigint,
    "params_json" text,
    "remind_interval_critical_seconds" bigint DEFAULT 0
);
ALTER TABLE ONLY "public"."alert_rules" ADD CONSTRAINT "alert_rules_pkey_1" PRIMARY KEY (id);

-- 表 "public"."alerts"
CREATE TABLE "public"."alerts" (
    "id" bigint DEFAULT nextval('public.alerts_id_seq'::regclass) NOT NULL,
    "rule_id" bigint,
    "agent_id" character varying(128),
    "target_id" character varying(128),
    "dedup_key" character varying(384),
    "state" character varying(16),
    "current_value" numeric,
    "first_fired_at" timestamp with time zone,
    "recovered_at" timestamp with time zone,
    "acknowledged" boolean,
    "silenced_until" timestamp with time zone,
    "created_at" timestamp with time zone,
    "updated_at" timestamp with time zone,
    "metric_key" character varying(64),
    "level" character varying(16),
    "details_json" text,
    "last_seen_at" timestamp with time zone,
    "repeat_count" bigint DEFAULT 0,
    "last_repeated_at" timestamp with time zone,
    "closed_at" timestamp with time zone,
    "close_reason" character varying(16)
);
ALTER TABLE ONLY "public"."alerts" ADD CONSTRAINT "alerts_pkey_1" PRIMARY KEY (id);

-- 表 "public"."audit_records"
CREATE TABLE "public"."audit_records" (
    "id" bigint DEFAULT nextval('public.audit_records_id_seq'::regclass) NOT NULL,
    "actor" character varying(128),
    "action" character varying(128),
    "resource" character varying(128),
    "resource_id" character varying(128),
    "details_json" jsonb,
    "created_at" timestamp with time zone
);
ALTER TABLE ONLY "public"."audit_records" ADD CONSTRAINT "audit_records_pkey_1" PRIMARY KEY (id);

-- 表 "public"."config_documents"
CREATE TABLE "public"."config_documents" (
    "id" bigint DEFAULT nextval('public.config_documents_id_seq'::regclass) NOT NULL,
    "agent_id" character varying(128),
    "version" character varying(128),
    "document_json" jsonb,
    "signature" text,
    "active" boolean,
    "created_at" timestamp with time zone
);
ALTER TABLE ONLY "public"."config_documents" ADD CONSTRAINT "config_documents_pkey_1" PRIMARY KEY (id);

-- 表 "public"."dangerous_commands"
CREATE TABLE "public"."dangerous_commands" (
    "id" bigint DEFAULT nextval('public.dangerous_commands_id_seq'::regclass) NOT NULL,
    "name" character varying(128) NOT NULL,
    "pattern" text NOT NULL,
    "match_type" character varying(16) NOT NULL,
    "enabled" boolean DEFAULT true,
    "description" character varying(255),
    "created_at" timestamp with time zone,
    "updated_at" timestamp with time zone
);
ALTER TABLE ONLY "public"."dangerous_commands" ADD CONSTRAINT "dangerous_commands_pkey_1" PRIMARY KEY (id);

-- 表 "public"."docker_container_states"
CREATE TABLE "public"."docker_container_states" (
    "id" bigint DEFAULT nextval('public.docker_container_states_id_seq'::regclass) NOT NULL,
    "agent_id" character varying(128),
    "container_id" character varying(128),
    "name" character varying(255),
    "image" character varying(255),
    "state" character varying(32),
    "last_seen_at" timestamp with time zone
);
ALTER TABLE ONLY "public"."docker_container_states" ADD CONSTRAINT "docker_container_states_pkey_1" PRIMARY KEY (id);

-- 表 "public"."maintenance_windows"
CREATE TABLE "public"."maintenance_windows" (
    "id" bigint DEFAULT nextval('public.maintenance_windows_id_seq'::regclass) NOT NULL,
    "agent_id" character varying(128),
    "target_id" character varying(128),
    "starts_at" timestamp with time zone,
    "ends_at" timestamp with time zone,
    "reason" text,
    "created_by" character varying(128),
    "created_at" timestamp with time zone
);
ALTER TABLE ONLY "public"."maintenance_windows" ADD CONSTRAINT "maintenance_windows_pkey_1" PRIMARY KEY (id);

-- 表 "public"."metric_receipts"
CREATE TABLE "public"."metric_receipts" (
    "id" bigint DEFAULT nextval('public.metric_receipts_id_seq'::regclass) NOT NULL,
    "agent_id" character varying(128),
    "sequence" bigint,
    "created_at" timestamp with time zone
);
ALTER TABLE ONLY "public"."metric_receipts" ADD CONSTRAINT "metric_receipts_pkey_1" PRIMARY KEY (id);

-- 表 "public"."outbox_events"
CREATE TABLE "public"."outbox_events" (
    "id" bigint DEFAULT nextval('public.outbox_events_id_seq'::regclass) NOT NULL,
    "topic" character varying(64),
    "key" character varying(256),
    "payload_json" jsonb,
    "available_at" timestamp with time zone,
    "claimed_at" timestamp with time zone,
    "created_at" timestamp with time zone
);
ALTER TABLE ONLY "public"."outbox_events" ADD CONSTRAINT "outbox_events_pkey_1" PRIMARY KEY (id);

-- 表 "public"."rdp_sessions"
CREATE TABLE "public"."rdp_sessions" (
    "id" bigint DEFAULT nextval('public.rdp_sessions_id_seq'::regclass) NOT NULL,
    "agent_id" character varying(128),
    "operator" character varying(128),
    "username" character varying(128),
    "guac_connection_id" character varying(128),
    "status" character varying(16),
    "started_at" timestamp with time zone,
    "ended_at" timestamp with time zone,
    "protocol" character varying(16) DEFAULT ''::character varying,
    "client_ip" character varying(64) DEFAULT ''::character varying,
    "close_reason" character varying(255) DEFAULT ''::character varying,
    "recording_path" character varying(512) DEFAULT ''::character varying,
    "bytes_in" bigint DEFAULT 0,
    "bytes_out" bigint DEFAULT 0,
    "duration_sec" bigint DEFAULT 0
);
ALTER TABLE ONLY "public"."rdp_sessions" ADD CONSTRAINT "rdp_sessions_pkey_1" PRIMARY KEY (id);

-- 表 "public"."remote_endpoints"
CREATE TABLE "public"."remote_endpoints" (
    "id" bigint DEFAULT nextval('public.remote_endpoints_id_seq'::regclass) NOT NULL,
    "agent_id" character varying(128),
    "protocol" character varying(16),
    "host" character varying(255),
    "port" bigint,
    "username" character varying(128),
    "auth_type" character varying(16),
    "secret_cipher" text,
    "passphrase_cipher" text,
    "enabled" boolean,
    "remark" character varying(255),
    "created_by" bigint,
    "updated_by" bigint,
    "created_at" timestamp with time zone,
    "updated_at" timestamp with time zone
);
ALTER TABLE ONLY "public"."remote_endpoints" ADD CONSTRAINT "remote_endpoints_pkey_1" PRIMARY KEY (id);

-- 表 "public"."sa_job"
CREATE TABLE "public"."sa_job" (
    "id" bigint DEFAULT nextval('public.sa_job_id_seq'::regclass) NOT NULL,
    "job_name" character varying(64) NOT NULL,
    "job_group" character varying(64) DEFAULT 'DEFAULT'::character varying NOT NULL,
    "invoke_target" character varying(500) NOT NULL,
    "cron_expression" character varying(255),
    "misfire_policy" character varying(20) DEFAULT '3'::character varying,
    "concurrent" character varying(1) DEFAULT '1'::character varying,
    "status" character varying(1) DEFAULT '0'::character varying NOT NULL,
    "remark" character varying(500),
    "del_flag" character varying(1) DEFAULT '0'::character varying NOT NULL,
    "create_by" character varying(64) DEFAULT ''::character varying NOT NULL,
    "create_time" timestamp with time zone,
    "update_by" character varying(64) DEFAULT ''::character varying NOT NULL,
    "update_time" timestamp with time zone,
    "job_id" bigint DEFAULT nextval('public.sa_job_job_id_seq'::regclass) NOT NULL
);
ALTER TABLE ONLY "public"."sa_job" ADD CONSTRAINT "sa_job_pkey_1" PRIMARY KEY (id);

-- 表 "public"."sa_job_log"
CREATE TABLE "public"."sa_job_log" (
    "id" bigint DEFAULT nextval('public.sa_job_log_id_seq'::regclass) NOT NULL,
    "job_name" character varying(64) NOT NULL,
    "job_group" character varying(64) NOT NULL,
    "invoke_target" character varying(500) NOT NULL,
    "job_message" character varying(500),
    "status" character varying(1) DEFAULT '0'::character varying NOT NULL,
    "exception_info" character varying(2000),
    "create_time" timestamp with time zone
);
ALTER TABLE ONLY "public"."sa_job_log" ADD CONSTRAINT "sa_job_log_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_attachment"
CREATE TABLE "public"."sa_system_attachment" (
    "id" integer NOT NULL,
    "category_id" integer DEFAULT 0,
    "storage_mode" integer DEFAULT 1,
    "origin_name" character varying(255),
    "object_name" character varying(255) NOT NULL,
    "hash" character varying(64),
    "mime_type" character varying(255),
    "storage_path" character varying(512) NOT NULL,
    "suffix" character varying(10),
    "size_byte" integer,
    "size_info" character varying(50),
    "url" character varying(1024) NOT NULL,
    "remark" character varying(255),
    "created_by" integer,
    "updated_by" integer,
    "create_time" timestamp without time zone,
    "update_time" timestamp without time zone,
    "delete_time" timestamp without time zone
);
ALTER TABLE "public"."sa_system_attachment" ALTER COLUMN "id" ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME "public"."sa_system_attachment_id_seq"
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);
ALTER TABLE ONLY "public"."sa_system_attachment" ADD CONSTRAINT "sa_system_attachment_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_category"
CREATE TABLE "public"."sa_system_category" (
    "id" integer NOT NULL,
    "parent_id" integer DEFAULT 0 NOT NULL,
    "level" character varying(255),
    "category_name" character varying(100) DEFAULT ''::character varying NOT NULL,
    "sort" integer DEFAULT 0 NOT NULL,
    "status" integer DEFAULT 1,
    "remark" character varying(255),
    "created_by" integer,
    "updated_by" integer,
    "create_time" timestamp without time zone,
    "update_time" timestamp without time zone,
    "delete_time" timestamp without time zone
);
ALTER TABLE "public"."sa_system_category" ALTER COLUMN "id" ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME "public"."sa_system_category_id_seq"
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);
ALTER TABLE ONLY "public"."sa_system_category" ADD CONSTRAINT "sa_system_category_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_config"
CREATE TABLE "public"."sa_system_config" (
    "id" integer NOT NULL,
    "group_id" integer,
    "key" character varying(32) NOT NULL,
    "value" text,
    "name" character varying(255),
    "input_type" character varying(32),
    "config_select_data" character varying(500),
    "sort" integer DEFAULT 0,
    "remark" character varying(255),
    "created_by" integer,
    "updated_by" integer,
    "create_time" timestamp without time zone,
    "update_time" timestamp without time zone,
    "delete_time" timestamp without time zone
);
ALTER TABLE "public"."sa_system_config" ALTER COLUMN "id" ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME "public"."sa_system_config_id_seq"
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);
ALTER TABLE ONLY "public"."sa_system_config" ADD CONSTRAINT "sa_system_config_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_config_group"
CREATE TABLE "public"."sa_system_config_group" (
    "id" integer NOT NULL,
    "name" character varying(50),
    "code" character varying(100),
    "remark" character varying(255),
    "created_by" integer,
    "updated_by" integer,
    "create_time" timestamp without time zone,
    "update_time" timestamp without time zone,
    "delete_time" timestamp without time zone
);
ALTER TABLE "public"."sa_system_config_group" ALTER COLUMN "id" ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME "public"."sa_system_config_group_id_seq"
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);
ALTER TABLE ONLY "public"."sa_system_config_group" ADD CONSTRAINT "sa_system_config_group_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_dept"
CREATE TABLE "public"."sa_system_dept" (
    "id" integer NOT NULL,
    "parent_id" integer DEFAULT 0,
    "name" character varying(64) NOT NULL,
    "code" character varying(64),
    "leader_id" integer,
    "level" character varying(255) DEFAULT ''::character varying,
    "tenant_id" integer DEFAULT 0 NOT NULL,
    "sort" integer DEFAULT 0,
    "status" integer DEFAULT 1,
    "remark" character varying(255),
    "created_by" integer,
    "updated_by" integer,
    "create_time" timestamp without time zone,
    "update_time" timestamp without time zone,
    "delete_time" timestamp without time zone
);
ALTER TABLE "public"."sa_system_dept" ALTER COLUMN "id" ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME "public"."sa_system_dept_id_seq"
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);
ALTER TABLE ONLY "public"."sa_system_dept" ADD CONSTRAINT "sa_system_dept_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_dict_data"
CREATE TABLE "public"."sa_system_dict_data" (
    "id" integer NOT NULL,
    "type_id" integer,
    "label" character varying(50),
    "value" character varying(100),
    "color" character varying(50),
    "code" character varying(100),
    "tag_type" character varying(10),
    "sort" integer DEFAULT 0,
    "status" integer DEFAULT 1,
    "remark" character varying(255),
    "created_by" integer,
    "updated_by" integer,
    "create_time" timestamp without time zone,
    "update_time" timestamp without time zone,
    "delete_time" timestamp without time zone
);
ALTER TABLE "public"."sa_system_dict_data" ALTER COLUMN "id" ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME "public"."sa_system_dict_data_id_seq"
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);
ALTER TABLE ONLY "public"."sa_system_dict_data" ADD CONSTRAINT "sa_system_dict_data_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_dict_type"
CREATE TABLE "public"."sa_system_dict_type" (
    "id" integer NOT NULL,
    "name" character varying(50),
    "code" character varying(100),
    "status" integer DEFAULT 1,
    "remark" character varying(255),
    "created_by" integer,
    "updated_by" integer,
    "create_time" timestamp without time zone,
    "update_time" timestamp without time zone,
    "delete_time" timestamp without time zone
);
ALTER TABLE "public"."sa_system_dict_type" ALTER COLUMN "id" ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME "public"."sa_system_dict_type_id_seq"
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);
ALTER TABLE ONLY "public"."sa_system_dict_type" ADD CONSTRAINT "sa_system_dict_type_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_login_log"
CREATE TABLE "public"."sa_system_login_log" (
    "id" integer NOT NULL,
    "username" character varying(20),
    "ip" character varying(45),
    "ip_location" character varying(255),
    "os" character varying(50),
    "browser" character varying(50),
    "status" integer DEFAULT 1,
    "message" character varying(50),
    "login_time" timestamp without time zone,
    "remark" character varying(255),
    "created_by" integer,
    "updated_by" integer,
    "create_time" timestamp without time zone,
    "update_time" timestamp without time zone,
    "delete_time" timestamp without time zone
);
ALTER TABLE "public"."sa_system_login_log" ALTER COLUMN "id" ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME "public"."sa_system_login_log_id_seq"
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);
ALTER TABLE ONLY "public"."sa_system_login_log" ADD CONSTRAINT "sa_system_login_log_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_menu"
CREATE TABLE "public"."sa_system_menu" (
    "id" integer NOT NULL,
    "parent_id" integer DEFAULT 0,
    "name" character varying(64) NOT NULL,
    "code" character varying(64),
    "slug" character varying(100),
    "type" integer DEFAULT 1 NOT NULL,
    "path" character varying(255),
    "component" character varying(255),
    "method" character varying(10),
    "icon" character varying(64),
    "sort" integer DEFAULT 100,
    "link_url" character varying(255),
    "is_iframe" integer DEFAULT 2,
    "is_keep_alive" integer DEFAULT 2,
    "is_hidden" integer DEFAULT 2,
    "is_fixed_tab" integer DEFAULT 2,
    "is_full_page" integer DEFAULT 2,
    "is_always_show" integer DEFAULT 2,
    "redirect" character varying(255),
    "params" text,
    "generate_id" integer DEFAULT 0,
    "generate_key" character varying(255),
    "status" integer DEFAULT 1,
    "remark" character varying(255),
    "created_by" integer,
    "updated_by" integer,
    "create_time" timestamp without time zone,
    "update_time" timestamp without time zone,
    "delete_time" timestamp without time zone
);
ALTER TABLE "public"."sa_system_menu" ALTER COLUMN "id" ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME "public"."sa_system_menu_id_seq"
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);
ALTER TABLE ONLY "public"."sa_system_menu" ADD CONSTRAINT "sa_system_menu_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_meta"
CREATE TABLE "public"."sa_system_meta" (
    "key" character varying(64) NOT NULL,
    "value" character varying(255),
    "remark" character varying(255),
    "update_time" timestamp without time zone
);
ALTER TABLE ONLY "public"."sa_system_meta" ADD CONSTRAINT "sa_system_meta_pkey" PRIMARY KEY (key);

-- 表 "public"."sa_system_notice"
CREATE TABLE "public"."sa_system_notice" (
    "id" integer NOT NULL,
    "title" character varying(100) DEFAULT ''::character varying NOT NULL,
    "type" integer DEFAULT 1 NOT NULL,
    "content" text,
    "status" integer DEFAULT 1 NOT NULL,
    "level" character varying(20),
    "publish_status" integer DEFAULT 0 NOT NULL,
    "target_type" integer DEFAULT 1 NOT NULL,
    "target_users" text,
    "publisher_id" integer,
    "publish_time" timestamp without time zone,
    "revoke_time" timestamp without time zone,
    "remark" character varying(255) DEFAULT ''::character varying NOT NULL,
    "created_by" integer,
    "updated_by" integer,
    "create_time" timestamp without time zone,
    "update_time" timestamp without time zone,
    "delete_time" timestamp without time zone
);
ALTER TABLE "public"."sa_system_notice" ALTER COLUMN "id" ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME "public"."sa_system_notice_id_seq"
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);
ALTER TABLE ONLY "public"."sa_system_notice" ADD CONSTRAINT "sa_system_notice_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_notice_read"
CREATE TABLE "public"."sa_system_notice_read" (
    "id" integer NOT NULL,
    "notice_id" integer NOT NULL,
    "user_id" integer NOT NULL,
    "read_time" timestamp without time zone,
    "create_time" timestamp without time zone
);
ALTER TABLE "public"."sa_system_notice_read" ALTER COLUMN "id" ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME "public"."sa_system_notice_read_id_seq"
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);
ALTER TABLE ONLY "public"."sa_system_notice_read" ADD CONSTRAINT "sa_system_notice_read_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_oper_log"
CREATE TABLE "public"."sa_system_oper_log" (
    "id" integer NOT NULL,
    "username" character varying(20),
    "app" character varying(50),
    "method" character varying(20),
    "router" character varying(500),
    "service_name" character varying(64),
    "ip" character varying(45),
    "ip_location" character varying(255),
    "request_data" text,
    "duration" character varying(20),
    "action_type" character varying(20),
    "operator_id" integer,
    "device" character varying(50),
    "browser" character varying(50),
    "os" character varying(50),
    "status" integer,
    "error_msg" character varying(500),
    "remark" character varying(255),
    "created_by" integer,
    "updated_by" integer,
    "create_time" timestamp without time zone,
    "update_time" timestamp without time zone,
    "delete_time" timestamp without time zone
);
ALTER TABLE "public"."sa_system_oper_log" ALTER COLUMN "id" ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME "public"."sa_system_oper_log_id_seq"
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);
ALTER TABLE ONLY "public"."sa_system_oper_log" ADD CONSTRAINT "sa_system_oper_log_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_post"
CREATE TABLE "public"."sa_system_post" (
    "id" integer NOT NULL,
    "name" character varying(50),
    "code" character varying(100),
    "sort" integer DEFAULT 0,
    "status" integer DEFAULT 1,
    "tenant_id" integer DEFAULT 0 NOT NULL,
    "remark" character varying(255),
    "created_by" integer,
    "updated_by" integer,
    "create_time" timestamp without time zone,
    "update_time" timestamp without time zone,
    "delete_time" timestamp without time zone
);
ALTER TABLE "public"."sa_system_post" ALTER COLUMN "id" ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME "public"."sa_system_post_id_seq"
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);
ALTER TABLE ONLY "public"."sa_system_post" ADD CONSTRAINT "sa_system_post_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_refresh_token"
CREATE TABLE "public"."sa_system_refresh_token" (
    "id" character varying(64) NOT NULL,
    "user_id" integer NOT NULL,
    "token_hash" character varying(128) NOT NULL,
    "expires_at" timestamp without time zone NOT NULL,
    "revoked_at" timestamp without time zone,
    "created_at" timestamp without time zone,
    "ip" character varying(45),
    "ip_location" character varying(255),
    "device" character varying(32),
    "os" character varying(50),
    "browser" character varying(50),
    "last_active_at" timestamp without time zone
);
ALTER TABLE ONLY "public"."sa_system_refresh_token" ADD CONSTRAINT "sa_system_refresh_token_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_role"
CREATE TABLE "public"."sa_system_role" (
    "id" integer NOT NULL,
    "parent_id" integer DEFAULT 0 NOT NULL,
    "name" character varying(64) NOT NULL,
    "code" character varying(64) NOT NULL,
    "level" integer DEFAULT 1,
    "data_scope" integer DEFAULT 1,
    "remark" character varying(255),
    "sort" integer DEFAULT 100,
    "tenant_id" integer DEFAULT 0 NOT NULL,
    "status" integer DEFAULT 1,
    "created_by" integer,
    "updated_by" integer,
    "create_time" timestamp without time zone,
    "update_time" timestamp without time zone,
    "delete_time" timestamp without time zone
);
ALTER TABLE "public"."sa_system_role" ALTER COLUMN "id" ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME "public"."sa_system_role_id_seq"
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);
ALTER TABLE ONLY "public"."sa_system_role" ADD CONSTRAINT "sa_system_role_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_role_dept"
CREATE TABLE "public"."sa_system_role_dept" (
    "id" integer NOT NULL,
    "role_id" integer NOT NULL,
    "dept_id" integer NOT NULL
);
ALTER TABLE "public"."sa_system_role_dept" ALTER COLUMN "id" ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME "public"."sa_system_role_dept_id_seq"
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);
ALTER TABLE ONLY "public"."sa_system_role_dept" ADD CONSTRAINT "sa_system_role_dept_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_role_menu"
CREATE TABLE "public"."sa_system_role_menu" (
    "id" integer NOT NULL,
    "role_id" integer NOT NULL,
    "menu_id" integer NOT NULL
);
ALTER TABLE "public"."sa_system_role_menu" ALTER COLUMN "id" ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME "public"."sa_system_role_menu_id_seq"
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);
ALTER TABLE ONLY "public"."sa_system_role_menu" ADD CONSTRAINT "sa_system_role_menu_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_tenant"
CREATE TABLE "public"."sa_system_tenant" (
    "id" integer NOT NULL,
    "tenant_name" character varying(100) NOT NULL,
    "tenant_code" character varying(50) NOT NULL,
    "contact_name" character varying(50),
    "contact_phone" character varying(20),
    "contact_email" character varying(100),
    "address" character varying(255),
    "logo_url" character varying(255),
    "status" integer DEFAULT 1 NOT NULL,
    "expire_time" timestamp without time zone,
    "max_users" integer DEFAULT 0 NOT NULL,
    "max_depts" integer DEFAULT 0 NOT NULL,
    "max_roles" integer DEFAULT 0 NOT NULL,
    "remark" character varying(500),
    "created_by" integer DEFAULT 0 NOT NULL,
    "updated_by" integer DEFAULT 0 NOT NULL,
    "create_time" timestamp without time zone,
    "update_time" timestamp without time zone,
    "delete_time" timestamp without time zone
);
ALTER TABLE "public"."sa_system_tenant" ALTER COLUMN "id" ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME "public"."sa_system_tenant_id_seq"
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);
ALTER TABLE ONLY "public"."sa_system_tenant" ADD CONSTRAINT "sa_system_tenant_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_user"
CREATE TABLE "public"."sa_system_user" (
    "id" integer NOT NULL,
    "username" character varying(64) NOT NULL,
    "password" character varying(255) NOT NULL,
    "realname" character varying(64),
    "gender" character varying(10) DEFAULT '0'::character varying,
    "avatar" character varying(255) DEFAULT ''::character varying NOT NULL,
    "email" character varying(128),
    "phone" character varying(20),
    "signed" character varying(255),
    "dashboard" character varying(255) DEFAULT 'work'::character varying,
    "dept_id" integer,
    "is_super" integer DEFAULT 0,
    "status" integer DEFAULT 1,
    "remark" character varying(255),
    "login_time" timestamp without time zone,
    "login_ip" character varying(45),
    "created_by" integer,
    "updated_by" integer,
    "create_time" timestamp without time zone,
    "update_time" timestamp without time zone,
    "delete_time" timestamp without time zone
);
ALTER TABLE "public"."sa_system_user" ALTER COLUMN "id" ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME "public"."sa_system_user_id_seq"
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);
ALTER TABLE ONLY "public"."sa_system_user" ADD CONSTRAINT "sa_system_user_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_user_dept"
CREATE TABLE "public"."sa_system_user_dept" (
    "id" integer NOT NULL,
    "tenant_id" integer DEFAULT 0 NOT NULL,
    "user_id" integer NOT NULL,
    "dept_id" integer NOT NULL,
    "created_by" integer,
    "updated_by" integer,
    "create_time" timestamp without time zone,
    "update_time" timestamp without time zone,
    "delete_time" timestamp without time zone
);
ALTER TABLE "public"."sa_system_user_dept" ALTER COLUMN "id" ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME "public"."sa_system_user_dept_id_seq"
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);
ALTER TABLE ONLY "public"."sa_system_user_dept" ADD CONSTRAINT "sa_system_user_dept_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_user_menu"
CREATE TABLE "public"."sa_system_user_menu" (
    "id" integer NOT NULL,
    "user_id" integer NOT NULL,
    "menu_id" integer NOT NULL,
    "tenant_id" integer DEFAULT 0,
    "created_by" integer DEFAULT 0,
    "updated_by" integer DEFAULT 0,
    "status" integer DEFAULT 1 NOT NULL,
    "create_time" timestamp without time zone,
    "update_time" timestamp without time zone,
    "delete_time" timestamp without time zone,
    "is_show" integer DEFAULT 1 NOT NULL,
    "is_create" integer DEFAULT 1 NOT NULL,
    "is_update" integer DEFAULT 1 NOT NULL,
    "is_delete" integer DEFAULT 1 NOT NULL
);
ALTER TABLE "public"."sa_system_user_menu" ALTER COLUMN "id" ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME "public"."sa_system_user_menu_id_seq"
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);
ALTER TABLE ONLY "public"."sa_system_user_menu" ADD CONSTRAINT "sa_system_user_menu_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_user_post"
CREATE TABLE "public"."sa_system_user_post" (
    "id" integer NOT NULL,
    "user_id" integer NOT NULL,
    "post_id" integer NOT NULL,
    "status" integer DEFAULT 1 NOT NULL,
    "created_by" integer DEFAULT 0 NOT NULL,
    "updated_by" integer DEFAULT 0 NOT NULL,
    "create_time" timestamp without time zone,
    "update_time" timestamp without time zone,
    "delete_time" timestamp without time zone,
    "tenant_id" integer DEFAULT 0 NOT NULL
);
ALTER TABLE "public"."sa_system_user_post" ALTER COLUMN "id" ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME "public"."sa_system_user_post_id_seq"
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);
ALTER TABLE ONLY "public"."sa_system_user_post" ADD CONSTRAINT "sa_system_user_post_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_user_role"
CREATE TABLE "public"."sa_system_user_role" (
    "id" integer NOT NULL,
    "user_id" integer NOT NULL,
    "role_id" integer NOT NULL,
    "status" integer DEFAULT 1 NOT NULL,
    "tenant_id" integer DEFAULT 0 NOT NULL,
    "created_by" integer,
    "updated_by" integer,
    "create_time" timestamp without time zone,
    "update_time" timestamp without time zone,
    "delete_time" timestamp without time zone
);
ALTER TABLE "public"."sa_system_user_role" ALTER COLUMN "id" ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME "public"."sa_system_user_role_id_seq"
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);
ALTER TABLE ONLY "public"."sa_system_user_role" ADD CONSTRAINT "sa_system_user_role_pkey" PRIMARY KEY (id);

-- 表 "public"."sa_system_user_tenant"
CREATE TABLE "public"."sa_system_user_tenant" (
    "id" integer NOT NULL,
    "user_id" integer NOT NULL,
    "tenant_id" integer NOT NULL,
    "is_default" integer DEFAULT 0 NOT NULL,
    "join_time" timestamp without time zone,
    "is_super" integer DEFAULT 0 NOT NULL,
    "created_by" integer DEFAULT 0 NOT NULL,
    "updated_by" integer DEFAULT 0 NOT NULL,
    "create_time" timestamp without time zone,
    "update_time" timestamp without time zone,
    "delete_time" timestamp without time zone,
    "status" integer DEFAULT 1 NOT NULL
);
ALTER TABLE "public"."sa_system_user_tenant" ALTER COLUMN "id" ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME "public"."sa_system_user_tenant_id_seq"
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);
ALTER TABLE ONLY "public"."sa_system_user_tenant" ADD CONSTRAINT "sa_system_user_tenant_pkey" PRIMARY KEY (id);

-- 表 "public"."service_probe_states"
CREATE TABLE "public"."service_probe_states" (
    "id" bigint DEFAULT nextval('public.service_probe_states_id_seq'::regclass) NOT NULL,
    "agent_id" character varying(128),
    "target_id" character varying(128),
    "name" character varying(255),
    "state" character varying(16),
    "check_stage" character varying(32),
    "latency_ms" bigint,
    "last_checked_at" timestamp with time zone,
    "last_error" text
);

-- 表 "public"."ssh_commands"
CREATE TABLE "public"."ssh_commands" (
    "id" bigint DEFAULT nextval('public.ssh_commands_id_seq'::regclass) NOT NULL,
    "session_id" bigint,
    "seq" bigint,
    "command" text,
    "recorded_at" timestamp with time zone
);

-- 表 "public"."ssh_sessions"
CREATE TABLE "public"."ssh_sessions" (
    "id" bigint DEFAULT nextval('public.ssh_sessions_id_seq'::regclass) NOT NULL,
    "agent_id" character varying(128),
    "operator" character varying(128),
    "username" character varying(128),
    "client_ip" character varying(64),
    "status" character varying(16),
    "close_reason" character varying(255),
    "recording_path" character varying(512),
    "bytes_in" bigint,
    "bytes_out" bigint,
    "started_at" timestamp with time zone,
    "ended_at" timestamp with time zone
);

-- 表 "public"."t_history_record"
CREATE TABLE "public"."t_history_record" (
    "id" character varying(50) NOT NULL,
    "source" character varying(20) DEFAULT NULL::character varying,
    "pattern" character varying(20) DEFAULT NULL::character varying,
    "name" character varying(100) DEFAULT NULL::character varying,
    "create_time" timestamp without time zone
);
ALTER TABLE ONLY "public"."t_history_record" ADD CONSTRAINT "t_history_record_pkey" PRIMARY KEY (id);

-- 表 "public"."t_memory_detail"
CREATE TABLE "public"."t_memory_detail" (
    "id" character varying(50) NOT NULL,
    "source_id" character varying(50) DEFAULT NULL::character varying,
    "type" character varying(10) DEFAULT NULL::character varying,
    "content" text,
    "create_time" timestamp without time zone
);
ALTER TABLE ONLY "public"."t_memory_detail" ADD CONSTRAINT "t_memory_detail_pkey" PRIMARY KEY (id);

-- 表 "public"."t_system_model"
CREATE TABLE "public"."t_system_model" (
    "id" character varying(50) NOT NULL,
    "model_name" character varying(50) DEFAULT NULL::character varying,
    "base_url" character varying(100) DEFAULT NULL::character varying,
    "api_key" character varying(100) DEFAULT NULL::character varying,
    "type" character varying(20) DEFAULT NULL::character varying,
    "source" character varying(20) DEFAULT NULL::character varying,
    "create_time" timestamp without time zone
);
ALTER TABLE ONLY "public"."t_system_model" ADD CONSTRAINT "t_system_model_pkey" PRIMARY KEY (id);

-- 表 "public"."t_system_setting"
CREATE TABLE "public"."t_system_setting" (
    "id" character varying(50) NOT NULL,
    "source" character varying(20) DEFAULT NULL::character varying,
    "content" json,
    "create_time" timestamp without time zone
);
ALTER TABLE ONLY "public"."t_system_setting" ADD CONSTRAINT "t_system_setting_pkey" PRIMARY KEY (id);

--
-- 数据
--

-- 表 "public"."agent_ip_histories"：按配置只导结构，跳过数据

-- 表 "public"."agents"：按配置只导结构，跳过数据

-- 表 "public"."alert_events"：按配置只导结构，跳过数据

-- 表 "public"."alert_rules" 的数据
INSERT INTO "public"."alert_rules" ("id", "name", "metric", "warning_threshold", "critical_threshold", "trigger_cycles", "recovery_cycles", "enabled", "selector_json", "created_at", "updated_at", "metric_key", "scope", "target", "for_seconds", "breach_ratio", "remind_interval_seconds", "cooldown_seconds", "params_json", "remind_interval_critical_seconds") VALUES
    (71, '', 'cpu_usage', 85, 95, 0, 0, true, '{}'::jsonb, '2026-10-11 11:25:51.34+08:00', '2026-10-11 11:25:51.34+08:00', 'cpu_usage', 'global', '', 300, 0.9, 1800, 600, '', 600),
    (72, '', 'mem_usage', 15, 95, 0, 0, true, '{}'::jsonb, '2026-10-11 11:25:51.341+08:00', '2026-10-11 11:25:51.341+08:00', 'mem_usage', 'global', '', 300, 0.9, 1800, 600, '', 600),
    (73, '', 'load_per_core', 1.2, 2, 0, 0, true, '{}'::jsonb, '2026-10-11 11:25:51.341+08:00', '2026-10-11 11:25:51.341+08:00', 'load_per_core', 'global', '', 300, 0.8, 1800, 600, '', 600),
    (74, '', 'disk_usage', 80, 92, 0, 0, true, '{}'::jsonb, '2026-10-11 11:25:51.341+08:00', '2026-10-11 11:25:51.341+08:00', 'disk_usage', 'global', '', 300, 0.9, 1800, 600, '{"ignoreFstypes":["tmpfs","devtmpfs","overlay","squashfs","ramfs","autofs","nfs","nfs4","cifs"]}', 600);

-- 表 "public"."alerts"：按配置只导结构，跳过数据

-- 表 "public"."audit_records"：按配置只导结构，跳过数据

-- 表 "public"."config_documents"：按配置只导结构，跳过数据

-- 表 "public"."dangerous_commands" 的数据
INSERT INTO "public"."dangerous_commands" ("id", "name", "pattern", "match_type", "enabled", "description", "created_at", "updated_at") VALUES
    (15, '写入磁盘设备', '>\s*/dev/[sh]d[a-z]?\d*', 'regex', true, '禁止向磁盘设备写入数据', '2026-10-07 18:26:20.29+08:00', '2026-10-07 18:26:20.29+08:00'),
    (16, 'Fork炸弹', '\w*\s*$$\s*\{[^}]*\|[^}]*&[^}]*\}', 'regex', true, '禁止执行 fork 炸弹', '2026-10-07 18:26:20.29+08:00', '2026-10-07 18:26:20.29+08:00'),
    (17, '系统关机', 'shutdown', 'prefix', true, '禁止执行系统关机命令', '2026-10-07 18:26:20.29+08:00', '2026-10-07 18:26:20.29+08:00'),
    (18, '系统重启', 'reboot', 'prefix', true, '禁止执行系统重启命令', '2026-10-07 18:26:20.29+08:00', '2026-10-07 18:26:20.29+08:00'),
    (19, '停止系统', 'halt', 'exact', true, '禁止执行停止系统命令', '2026-10-07 18:26:20.29+08:00', '2026-10-07 18:26:20.29+08:00'),
    (20, '关闭电源', 'poweroff', 'prefix', true, '禁止执行关闭电源命令', '2026-10-07 18:26:20.29+08:00', '2026-10-07 18:26:20.29+08:00'),
    (21, '切换运行级别0', 'init 0', 'exact', true, '禁止切换到关机运行级别', '2026-10-07 18:26:20.29+08:00', '2026-10-07 18:26:20.29+08:00'),
    (22, '移动根目录到黑洞', '(?:sudo\s+)?mv(?:\s+-[a-zA-Z]+)*\s+/\s+/dev/null', 'regex', true, '禁止将根目录移动到 /dev/null', '2026-10-07 18:26:20.29+08:00', '2026-10-07 18:26:20.29+08:00'),
    (23, '递归修改根目录权限', 'chmod(?:\s+-[a-zA-Z]+)*\s+-R[a-zA-Z]*\s+000\s+/', 'regex', true, '禁止递归修改根目录权限为 000', '2026-10-07 18:26:20.29+08:00', '2026-10-07 18:26:20.29+08:00'),
    (24, '系统关机(systemctl)', '(?:sudo\s+)?systemctl(?:\s+-[-\w]+)*\s+(?:poweroff|halt|reboot|shutdown)', 'regex', true, '禁止通过 systemctl 执行关机/重启操作', '2026-10-07 18:26:20.29+08:00', '2026-10-07 18:26:20.29+08:00');

-- 表 "public"."docker_container_states"：按配置只导结构，跳过数据

-- 表 "public"."maintenance_windows"：按配置只导结构，跳过数据

-- 表 "public"."metric_receipts"：按配置只导结构，跳过数据

-- 表 "public"."outbox_events"：按配置只导结构，跳过数据

-- 表 "public"."rdp_sessions"：按配置只导结构，跳过数据

-- 表 "public"."remote_endpoints"：按配置只导结构，跳过数据

-- 表 "public"."sa_job" 的数据
INSERT INTO "public"."sa_job" ("id", "job_name", "job_group", "invoke_target", "cron_expression", "misfire_policy", "concurrent", "status", "remark", "del_flag", "create_by", "create_time", "update_by", "update_time", "job_id") VALUES
    (1, 'cleaninfluxdb', 'DEFAULT', 'cleanInfluxDB', '0 0 2 * * ?', '1', '1', '0', '', '0', 'admin', '2026-10-12 11:34:34.68253+08:00', '', '2026-10-12 11:34:34.68253+08:00', 1),
    (2, 'bakupdatabse', 'DEFAULT', 'backupPostgres', '0 0 3 * * ?', '1', '1', '0', '', '0', 'admin', '2026-10-12 11:34:54.750524+08:00', '', '2026-10-12 11:34:54.750524+08:00', 2);

-- 表 "public"."sa_job_log"：按配置只导结构，跳过数据

-- 表 "public"."sa_system_attachment"：按配置只导结构，跳过数据

-- 表 "public"."sa_system_category" 的数据
INSERT INTO "public"."sa_system_category" ("id", "parent_id", "level", "category_name", "sort", "status", "remark", "created_by", "updated_by", "create_time", "update_time", "delete_time") VALUES
    (1, 0, '0,', '全部分类', 100, 1, '默认根分类', 1, 1, '2026-10-06 18:35:20', '2026-10-06 18:35:20', NULL),
    (2, 1, '0,1,', '图片分类', 100, 1, '', 1, 1, '2026-10-06 18:35:20', '2026-10-06 18:35:20', NULL),
    (3, 1, '0,1,', '文件分类', 100, 1, '', 1, 1, '2026-10-06 18:35:20', '2026-10-06 18:35:20', NULL),
    (4, 1, '0,1,', '系统图片', 100, 1, '头像等系统内置图片', 1, 1, '2026-10-06 18:35:20', '2026-10-06 18:35:20', NULL),
    (5, 1, '0,1,', '其他分类', 100, 1, '', 1, 1, '2026-10-06 18:35:20', '2026-10-06 18:35:20', NULL);

-- 表 "public"."sa_system_config" 的数据
INSERT INTO "public"."sa_system_config" ("id", "group_id", "key", "value", "name", "input_type", "config_select_data", "sort", "remark", "created_by", "updated_by", "create_time", "update_time", "delete_time") VALUES
    (1, 1, 'site_name', '服务器监控运维平台', '网站名称', 'input', NULL, 1, NULL, 1, 1, '2026-10-01 11:01:18', '2026-10-06 19:51:53.287', NULL),
    (2, 1, 'site_desc', 'Agent 采集 + 集中监控的后台管理端', '网站描述', 'textarea', NULL, 2, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (3, 1, 'site_copyright', 'Copyright © 2026 Monitor Team', '版权信息', 'textarea', NULL, 3, NULL, 1, 1, '2026-10-01 11:01:18', '2026-10-06 21:36:15.333', NULL),
    (4, 1, 'site_record_number', '粤备ICP-00001', '网站备案号', 'input', NULL, 4, NULL, 1, 1, '2026-10-01 11:01:18', '2026-10-06 17:23:31.758', NULL),
    (5, 2, 'upload_mode', '1', '上传模式', 'select', '[{"label":"本地上传","value":"1"},{"label":"阿里云OSS","value":"2"},{"label":"腾讯云COS","value":"4"}]', 5, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (6, 2, 'upload_size', '52428800', '上传大小', 'input', NULL, 6, '单位 Byte，1MB=1024*1024Byte', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (7, 2, 'upload_allow_file', 'txt,doc,docx,xls,xlsx,ppt,pptx,rar,zip,7z,pdf,md,jpg,png,jpeg', '文件类型', 'input', NULL, 7, NULL, 1, 1, '2026-10-01 11:01:18', '2026-10-06 21:36:17.38', NULL),
    (8, 2, 'upload_allow_image', 'jpg,jpeg,png,gif,svg,bmp', '图片类型', 'input', NULL, 8, NULL, 1, 1, '2026-10-01 11:01:18', '2026-10-06 21:36:20.156', NULL),
    (9, 3, 'alert_retention_events_days', '90', '告警事件保留天数', 'input', NULL, 9, '删除 alert_events 中超过该天数的事件；0 表示不清理', 1, 1, '2026-10-11 13:00:27.243955', '2026-10-11 13:00:27.243955', NULL),
    (10, 3, 'alert_retention_alerts_days', '180', '已结束告警保留天数', 'input', NULL, 10, '删除 alerts 中结束时间超过该天数的告警；0 表示不清理', 1, 1, '2026-10-11 13:00:27.243955', '2026-10-11 13:00:27.243955', NULL);

-- 表 "public"."sa_system_config_group" 的数据
INSERT INTO "public"."sa_system_config_group" ("id", "name", "code", "remark", "created_by", "updated_by", "create_time", "update_time", "delete_time") VALUES
    (1, '站点配置', 'site_config', '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2, '上传配置', 'upload_config', '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (3, '告警配置', 'alert_config', '告警数据保留策略，由计划任务 cleanAlertData 读取', 1, 1, '2026-10-11 13:00:27.243955', '2026-10-11 13:00:27.243955', NULL);

-- 表 "public"."sa_system_dept" 的数据
INSERT INTO "public"."sa_system_dept" ("id", "parent_id", "name", "code", "leader_id", "level", "tenant_id", "sort", "status", "remark", "created_by", "updated_by", "create_time", "update_time", "delete_time") VALUES
    (1, 0, '监控平台', 'MONITOR', NULL, '0,', 0, 1, 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2, 1, '平台运维部', 'OPS', NULL, '0,1,', 0, 1, 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (3, 1, '研发中心', 'RD', NULL, '0,1,', 0, 2, 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (4, 3, '后端组', 'BACKEND', NULL, '0,1,3,', 0, 1, 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (5, 3, '前端组', 'FRONTEND', NULL, '0,1,3,', 0, 2, 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL);

-- 表 "public"."sa_system_dict_data" 的数据
INSERT INTO "public"."sa_system_dict_data" ("id", "type_id", "label", "value", "color", "code", "tag_type", "sort", "status", "remark", "created_by", "updated_by", "create_time", "update_time", "delete_time") VALUES
    (1, 1, '男', '1', '#5d87ff', 'gender', 'N', 1, 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-06 19:19:16.823', NULL),
    (2, 1, '女', '2', '#ff4500', 'gender', 'D', 2, 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (3, 1, '未知', '0', '#909399', 'gender', 'N', 3, 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (4, 2, '正常', '1', '#60c041', 'data_status', 'S', 1, 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (5, 2, '停用', '0', '#ff4d4f', 'data_status', 'D', 2, 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (6, 3, '是', '1', '#60c041', 'yes_or_no', 'S', 1, 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (7, 3, '否', '0', '#ff4d4f', 'yes_or_no', 'D', 2, 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (8, 4, '通知', '1', '#00ced1', 'notice_type', 'I', 1, 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (9, 4, '公告', '2', '#5d87ff', 'notice_type', 'P', 2, 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (10, 5, '普通', 'normal', '#909399', 'notice_level', 'N', 1, 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (11, 5, '重要', 'high', '#ff8c00', 'notice_level', 'W', 2, 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (12, 5, '紧急', 'urgent', '#ff4d4f', 'notice_level', 'D', 3, 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (13, 6, '成功', '1', '#60c041', 'log_status', 'S', 1, 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (14, 6, '失败', '0', '#ff4d4f', 'log_status', 'D', 2, 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL);

-- 表 "public"."sa_system_dict_type" 的数据
INSERT INTO "public"."sa_system_dict_type" ("id", "name", "code", "status", "remark", "created_by", "updated_by", "create_time", "update_time", "delete_time") VALUES
    (1, '性别', 'gender', 1, NULL, 1, 1, '2026-10-01 11:01:18', '2026-10-06 19:15:56.673', NULL),
    (2, '数据状态', 'data_status', 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (3, '是否', 'yes_or_no', 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (4, '公告类型', 'notice_type', 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (5, '公告级别', 'notice_level', 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (6, '日志状态', 'log_status', 1, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL);

-- 表 "public"."sa_system_login_log"：按配置只导结构，跳过数据

-- 表 "public"."sa_system_menu" 的数据
INSERT INTO "public"."sa_system_menu" ("id", "parent_id", "name", "code", "slug", "type", "path", "component", "method", "icon", "sort", "link_url", "is_iframe", "is_keep_alive", "is_hidden", "is_fixed_tab", "is_full_page", "is_always_show", "redirect", "params", "generate_id", "generate_key", "status", "remark", "created_by", "updated_by", "create_time", "update_time", "delete_time") VALUES
    (100, 0, '监控中心', 'Monitor', '', 1, '/monitor', 'Layout', '', 'monitor', 10, '', 2, 2, 2, 2, 2, 1, '', '[]', 0, NULL, 1, '服务器 Agent 监控入口', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (101, 100, '监控总览', 'MonitorOverview', 'monitor:overview', 2, 'overview', 'monitor/overview/index', '', 'monitor', 11, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (102, 100, '服务器列表', 'MonitorAgents', 'monitor:agent:list', 2, 'agents', 'monitor/agent/index', '', 'table', 12, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (103, 100, '服务器详情', 'MonitorAgentDetail', 'monitor:agent:detail', 2, 'agents/:id', 'monitor/agent-detail/index', '', 'document', 13, '', 2, 2, 1, 2, 2, 2, '', '[]', 0, NULL, 1, '参数路由，菜单中隐藏', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (104, 100, '告警中心', 'MonitorAlerts', 'monitor:alert:list', 2, 'alerts', 'monitor/alert/index', '', 'shield', 14, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (105, 100, '会话审计', NULL, 'monitor:audit:view', 2, 'audit', 'monitor/audit/index', NULL, 'el-icon-Calendar', 15, NULL, 2, 2, 2, 2, 2, 2, NULL, '[]', 0, NULL, 1, 'SSH/RDP 会话记录与录像回放', NULL, 1, '2026-10-06 22:32:31', '2026-10-07 18:19:11.208', NULL),
    (106, 100, '高危命令', NULL, 'monitor:dangerous-command:list', 2, 'dangerous-command', 'monitor/dangerous-command/index', NULL, 'el-icon-WalletFilled', 16, NULL, 2, 2, 2, 2, 2, 2, NULL, '[]', 0, NULL, 1, 'SSH 高危命令拦截规则管理', NULL, 1, '2026-10-07 13:59:58', '2026-10-07 18:19:04.5', NULL),
    (107, 100, '计划任务', NULL, 'monitor:job:list', 2, 'job', 'monitor/job/index', NULL, 'el-icon-Clock', 17, NULL, 2, 2, 2, 2, 2, 2, NULL, '[]', 0, NULL, 1, '定时任务调度与管理', NULL, 1, '2026-10-07 19:46:24', '2026-10-07 20:43:00.817', NULL),
    (108, 100, '任务日志', 'MonitorJobLog', 'monitor:job-log:list', 2, 'job-log', 'monitor/job/log', '', 'document', 18, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '计划任务执行日志', NULL, NULL, '2026-10-07 19:46:24', '2026-10-07 19:46:24', NULL),
    (109, 100, '告警规则', 'MonitorAlertRule', 'monitor:alert:rule:list', 2, 'alert-rule', 'monitor/alert-rule/index', '', 'bell', 19, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '资源告警阈值：全局默认与按角色配置', NULL, NULL, '2026-10-08 23:58:48', '2026-10-08 23:58:48', NULL),
    (200, 0, '系统管理', 'System', '', 1, '/system', 'Layout', '', 'system', 20, '', 2, 2, 2, 2, 2, 1, '', '[]', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (201, 200, '用户管理', 'User', 'sys:user:list', 2, 'user', 'system/user/index', '', 'group', 21, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (202, 200, '部门管理', 'Dept', 'sys:dept:list', 2, 'dept', 'system/dept/index', '', 'tree', 22, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (203, 200, '角色管理', 'Role', 'sys:role:list', 2, 'role', 'system/role/index', '', 'role', 23, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (204, 200, '岗位管理', 'Post', 'sys:post:list', 2, 'post', 'system/post/index', '', 'security', 24, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (205, 200, '菜单管理', 'Menu', 'sys:menu:list', 2, 'menu', 'system/menu/index', '', 'menu', 25, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (206, 200, '字典管理', 'Dict', 'sys:dict:list', 2, 'dict', 'system/dict/index', '', 'dict', 26, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (207, 200, '系统配置', 'SysConfig', 'sys:config:list', 2, 'config', 'system/config/index', '', 'setting', 27, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (208, 200, '通知公告', 'Notice', 'sys:notice:list', 2, 'notice', 'system/notice/index', '', 'file', 28, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (209, 200, '操作日志', 'OperLog', 'sys:log:list', 2, 'log', 'system/log/index', '', 'code', 29, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (210, 200, '登录日志', 'LoginLog', 'sys:login-log:list', 2, 'login-log', 'system/login-log/index', '', 'api', 30, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (211, 200, '附件管理', NULL, 'sys:attachment:list', 2, 'attachment', 'system/attachment/index', NULL, 'el-icon-Paperclip', 31, NULL, 2, 2, 2, 2, 2, 2, NULL, '[]', 0, NULL, 1, '附件分类树 + 附件上传', NULL, 1, '2026-10-06 18:35:20', '2026-10-06 18:49:35.661', NULL),
    (212, 200, '缓存管理', NULL, 'sys:cache:list', 2, 'cache', 'system/cache/index', NULL, 'el-icon-SuitcaseLine', 32, NULL, 2, 2, 2, 2, 2, 2, NULL, '[]', 0, NULL, 1, 'Redis / 进程内缓存浏览与清理', NULL, 1, '2026-10-06 18:59:03', '2026-10-06 19:13:44.513', NULL),
    (213, 200, '在线用户', NULL, 'sys:online:list', 2, 'online', 'system/online/index', NULL, 'el-icon-UserFilled', 33, NULL, 2, 2, 2, 2, 2, 2, NULL, '[]', 0, NULL, 1, '登录会话列表与强制下线', NULL, 1, '2026-10-06 19:44:37', '2026-10-06 20:00:33.699', NULL),
    (1021, 102, '连接配置', 'MonitorAccessManage', 'monitor:access:manage', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '编辑服务器 SSH/RDP 连接信息', NULL, NULL, '2026-10-06 22:32:31', '2026-10-06 22:32:31', NULL),
    (1022, 102, 'SSH连接', 'MonitorSSHConnect', 'monitor:ssh:connect', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '发起 SSH 终端连接', NULL, NULL, '2026-10-06 22:32:31', '2026-10-06 22:32:31', NULL),
    (1023, 102, 'RDP连接', 'MonitorRDPConnect', 'monitor:rdp:connect', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '发起 RDP 远程桌面连接', NULL, NULL, '2026-10-06 22:32:31', '2026-10-06 22:32:31', NULL),
    (1024, 102, '告警设置', 'MonitorAlertRuleUpdate', 'monitor:alert:rule:update', 3, '', '', '', '', 4, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '设置该服务器的资源告警阈值（单机覆盖）', NULL, NULL, '2026-10-08 23:58:48', '2026-10-08 23:58:48', NULL),
    (1061, 106, '新增规则', 'DangerousCommandCreate', 'monitor:dangerous-command:create', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '新增高危命令拦截规则', NULL, NULL, '2026-10-07 13:59:58', '2026-10-07 13:59:58', NULL),
    (1062, 106, '修改规则', 'DangerousCommandUpdate', 'monitor:dangerous-command:update', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '修改高危命令拦截规则', NULL, NULL, '2026-10-07 13:59:58', '2026-10-07 13:59:58', NULL),
    (1063, 106, '删除规则', 'DangerousCommandDelete', 'monitor:dangerous-command:delete', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '删除高危命令拦截规则', NULL, NULL, '2026-10-07 13:59:58', '2026-10-07 13:59:58', NULL),
    (1071, 107, '新增任务', 'JobCreate', 'monitor:job:create', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '新增计划任务', NULL, NULL, '2026-10-07 19:46:24', '2026-10-07 19:46:24', NULL),
    (1072, 107, '修改任务', 'JobUpdate', 'monitor:job:update', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '修改计划任务', NULL, NULL, '2026-10-07 19:46:24', '2026-10-07 19:46:24', NULL),
    (1073, 107, '删除任务', 'JobDelete', 'monitor:job:delete', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '删除计划任务', NULL, NULL, '2026-10-07 19:46:24', '2026-10-07 19:46:24', NULL),
    (1074, 107, '手动执行', 'JobRun', 'monitor:job:run', 3, '', '', '', '', 4, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '手动执行计划任务', NULL, NULL, '2026-10-07 19:46:24', '2026-10-07 19:46:24', NULL),
    (1081, 108, '清空日志', 'JobLogClean', 'monitor:job-log:delete', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '清空任务执行日志', NULL, NULL, '2026-10-07 19:46:24', '2026-10-07 19:46:24', NULL),
    (2010, 201, '列表', 'UserList', 'sys:user:list', 3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', NULL, NULL, '2026-10-06 21:16:34', '2026-10-06 21:16:34', NULL),
    (2011, 201, '用户新增', 'UserCreate', 'sys:user:create', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2012, 201, '用户修改', 'UserUpdate', 'sys:user:update', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2013, 201, '用户删除', 'UserDelete', 'sys:user:delete', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2014, 201, '重置密码', 'UserResetPassword', 'sys:user:reset-password', 3, '', '', '', '', 4, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2015, 201, '用户导出', 'UserExport', 'sys:user:export', 3, '', '', '', '', 5, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2016, 201, '用户导入', 'UserImport', 'sys:user:import', 3, '', '', '', '', 6, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2017, 201, '菜单设置', 'UserAssignMenu', 'sys:user:assign-menu', 3, '', '', '', '', 7, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '为用户单独授权可见菜单', NULL, NULL, '2026-10-06 20:47:53', '2026-10-06 20:47:53', NULL),
    (2020, 202, '列表', 'DeptList', 'sys:dept:list', 3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', NULL, NULL, '2026-10-06 21:16:34', '2026-10-06 21:16:34', NULL),
    (2021, 202, '部门新增', 'DeptCreate', 'sys:dept:create', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2022, 202, '部门修改', 'DeptUpdate', 'sys:dept:update', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2023, 202, '部门删除', 'DeptDelete', 'sys:dept:delete', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2030, 203, '列表', 'RoleList', 'sys:role:list', 3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', NULL, NULL, '2026-10-06 21:16:34', '2026-10-06 21:16:34', NULL),
    (2031, 203, '角色新增', 'RoleCreate', 'sys:role:create', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2032, 203, '角色修改', 'RoleUpdate', 'sys:role:update', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2033, 203, '角色删除', 'RoleDelete', 'sys:role:delete', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2034, 203, '分配菜单', 'RoleAssignMenu', 'sys:role:assign-menu', 3, '', '', '', '', 4, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2040, 204, '列表', 'PostList', 'sys:post:list', 3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', NULL, NULL, '2026-10-06 21:16:34', '2026-10-06 21:16:34', NULL),
    (2041, 204, '岗位新增', 'PostCreate', 'sys:post:create', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2042, 204, '岗位修改', 'PostUpdate', 'sys:post:update', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2043, 204, '岗位删除', 'PostDelete', 'sys:post:delete', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2050, 205, '列表', 'MenuList', 'sys:menu:list', 3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', NULL, NULL, '2026-10-06 21:16:34', '2026-10-06 21:16:34', NULL),
    (2051, 205, '菜单新增', 'MenuCreate', 'sys:menu:create', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2052, 205, '菜单修改', 'MenuUpdate', 'sys:menu:update', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2053, 205, '菜单删除', 'MenuDelete', 'sys:menu:delete', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2060, 206, '列表', 'DictList', 'sys:dict:list', 3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', NULL, NULL, '2026-10-06 21:16:34', '2026-10-06 21:16:34', NULL),
    (2061, 206, '字典新增', 'DictCreate', 'sys:dict:create', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2062, 206, '字典修改', 'DictUpdate', 'sys:dict:update', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2063, 206, '字典删除', 'DictDelete', 'sys:dict:delete', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2070, 207, '列表', 'ConfigList', 'sys:config:list', 3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', NULL, NULL, '2026-10-06 21:16:34', '2026-10-06 21:16:34', NULL),
    (2071, 207, '配置新增', 'ConfigCreate', 'sys:config:create', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2072, 207, '配置修改', 'ConfigUpdate', 'sys:config:update', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2073, 207, '配置删除', 'ConfigDelete', 'sys:config:delete', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2074, 207, '刷新缓存', 'ConfigRefresh', 'sys:config:refresh', 3, '', '', '', '', 4, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2080, 208, '列表', 'NoticeList', 'sys:notice:list', 3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', NULL, NULL, '2026-10-06 21:16:34', '2026-10-06 21:16:34', NULL),
    (2081, 208, '公告新增', 'NoticeCreate', 'sys:notice:create', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2082, 208, '公告修改', 'NoticeUpdate', 'sys:notice:update', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2083, 208, '公告删除', 'NoticeDelete', 'sys:notice:delete', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2084, 208, '公告发布', 'NoticePublish', 'sys:notice:publish', 3, '', '', '', '', 4, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2085, 208, '公告撤回', 'NoticeRevoke', 'sys:notice:revoke', 3, '', '', '', '', 5, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2090, 209, '列表', 'OperLogList', 'sys:log:list', 3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', NULL, NULL, '2026-10-06 21:16:34', '2026-10-06 21:16:34', NULL),
    (2091, 209, '日志删除', 'OperLogDelete', 'sys:log:delete', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2100, 210, '列表', 'LoginLogList', 'sys:login-log:list', 3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', NULL, NULL, '2026-10-06 21:16:34', '2026-10-06 21:16:34', NULL),
    (2101, 210, '登录日志删除', 'LoginLogDelete', 'sys:login-log:delete', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (2110, 211, '列表', 'AttachmentList', 'sys:attachment:list', 3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', NULL, NULL, '2026-10-06 21:16:34', '2026-10-06 21:16:34', NULL),
    (2111, 211, '分类新增', 'AttachmentCategoryCreate', 'sys:attachment:category:create', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-06 18:35:20', '2026-10-06 18:35:20', NULL),
    (2112, 211, '分类修改', 'AttachmentCategoryUpdate', 'sys:attachment:category:update', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-06 18:35:20', '2026-10-06 18:35:20', NULL),
    (2113, 211, '分类删除', 'AttachmentCategoryDelete', 'sys:attachment:category:delete', 3, '', '', '', '', 3, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-06 18:35:20', '2026-10-06 18:35:20', NULL),
    (2114, 211, '附件上传', 'AttachmentUpload', 'sys:attachment:upload', 3, '', '', '', '', 4, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-06 18:35:20', '2026-10-06 18:35:20', NULL),
    (2115, 211, '附件修改', 'AttachmentUpdate', 'sys:attachment:update', 3, '', '', '', '', 5, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-06 18:35:20', '2026-10-06 18:35:20', NULL),
    (2116, 211, '附件删除', 'AttachmentDelete', 'sys:attachment:delete', 3, '', '', '', '', 6, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-06 18:35:20', '2026-10-06 18:35:20', NULL),
    (2120, 212, '列表', 'CacheList', 'sys:cache:list', 3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', NULL, NULL, '2026-10-06 21:16:34', '2026-10-06 21:16:34', NULL),
    (2121, 212, '缓存清理', 'CacheClear', 'sys:cache:clear', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-06 18:59:03', '2026-10-06 18:59:03', NULL),
    (2130, 213, '列表', 'OnlineList', 'sys:online:list', 3, '', '', '', '', 0, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '只读授权入口', NULL, NULL, '2026-10-06 21:16:34', '2026-10-06 21:16:34', NULL),
    (2131, 213, '强制下线', 'OnlineKick', 'sys:online:kick', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '', NULL, NULL, '2026-10-06 19:44:37', '2026-10-06 19:44:37', NULL),
    (1025, 102, '结束进程', 'MonitorSSHProcessKill', 'monitor:ssh:process:kill', 3, '', '', '', '', 5, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '在 SSH 终端的系统进程面板中结束进程（高危）', NULL, NULL, '2026-10-09 13:24:44.94', '2026-10-09 13:24:44.94', NULL),
    (1026, 102, '重启进程', 'MonitorSSHProcessRestart', 'monitor:ssh:process:restart', 3, '', '', '', '', 6, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '重启 systemd 托管的进程（高危）', NULL, NULL, '2026-10-09 13:29:31.961', '2026-10-09 13:29:31.961', NULL),
    (110, 100, '远程桌面会话', 'MonitorRdpSession', 'monitor:rdp:view', 2, 'rdp-sessions', 'monitor/remote-session/index', '', 'monitor', 20, '', 2, 2, 2, 2, 2, 2, '', '[]', 0, NULL, 1, 'RDP / VNC 远程会话记录与录像回放', NULL, NULL, '2026-10-09 20:22:38.324', '2026-10-09 20:22:38.324', NULL),
    (1027, 102, '文件读取', 'SftpFileRead', 'monitor:sftp:read', 3, '', '', '', '', 7, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, 'SSH 终端 SFTP 在线读取远程文件', NULL, NULL, '2026-10-12 23:49:17.075994', '2026-10-12 23:49:17.075994', NULL),
    (1028, 102, '文件写入', 'SftpFileWrite', 'monitor:sftp:write', 3, '', '', '', '', 8, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, 'SSH 终端 SFTP 在线写入远程文件', NULL, NULL, '2026-10-12 23:49:17.075994', '2026-10-12 23:49:17.075994', NULL),
    (1029, 102, '服务操作', 'MonitorSSHServiceManage', 'monitor:ssh:service:manage', 3, '', '', '', '', 9, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, 'SSH 控制台系统服务面板的启停/重启/开机自启（高危）', NULL, NULL, '2026-10-12 23:49:17.075994', '2026-10-12 23:49:17.075994', NULL),
    (1030, 102, '清理数据', 'MonitorMetricsClean', 'monitor:metrics:clean', 3, '', '', '', '', 10, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '清理该服务器在时序库中的采集指标与采集回执（不可逆）', NULL, NULL, '2026-10-12 23:49:17.075994', '2026-10-12 23:49:17.075994', NULL),
    (1041, 104, '清理告警', 'MonitorAlertDelete', 'monitor:alert:delete', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '批量清理告警记录（仍在活动的告警会被同时结束）', NULL, NULL, '2026-10-12 23:49:17.075994', '2026-10-12 23:49:17.075994', NULL),
    (1051, 105, '清理会话', 'MonitorAuditDelete', 'monitor:audit:delete', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '批量清理 SSH 会话审计记录（含命令流与录像）', NULL, NULL, '2026-10-12 23:49:17.075994', '2026-10-12 23:49:17.075994', NULL),
    (1082, 108, '清理日志', 'JobLogDelete', 'monitor:job-log:delete', 3, '', '', '', '', 2, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '批量清理选中的任务执行日志', NULL, NULL, '2026-10-12 23:49:17.075994', '2026-10-12 23:49:17.075994', NULL),
    (1101, 110, '清理会话', 'MonitorRdpSessionDelete', 'monitor:rdp:delete', 3, '', '', '', '', 1, '', 2, 2, 2, 2, 2, 2, '', '', 0, NULL, 1, '批量清理远程桌面会话记录（含录像）', NULL, NULL, '2026-10-12 23:49:17.075994', '2026-10-12 23:49:17.075994', NULL);

-- 表 "public"."sa_system_meta" 的数据
INSERT INTO "public"."sa_system_meta" ("key", "value", "remark", "update_time") VALUES
    ('seed_time_localtime', 'done', '种子数据时间已按本地时区写入', '2026-10-01 13:39:24');

-- 表 "public"."sa_system_notice" 的数据
INSERT INTO "public"."sa_system_notice" ("id", "title", "type", "content", "status", "level", "publish_status", "target_type", "target_users", "publisher_id", "publish_time", "revoke_time", "remark", "created_by", "updated_by", "create_time", "update_time", "delete_time") VALUES
    (1, '??????', 1, 'content', 1, 'L', 0, 1, NULL, NULL, NULL, NULL, '', 1, 1, '2026-10-06 18:09:59.17', '2026-10-06 18:09:59.17', '2026-10-06 18:10:06.481'),
    (2, 'xss-test', 1, 'hello<img src=x alert(2)>world', 1, 'L', 0, 1, NULL, NULL, NULL, NULL, '', 1, 1, '2026-10-06 20:12:35.912', '2026-10-06 20:12:35.912', '2026-10-06 20:13:43.39');

-- 表 "public"."sa_system_notice_read"：按配置只导结构，跳过数据

-- 表 "public"."sa_system_oper_log"：按配置只导结构，跳过数据

-- 表 "public"."sa_system_post" 的数据
INSERT INTO "public"."sa_system_post" ("id", "name", "code", "sort", "status", "tenant_id", "remark", "created_by", "updated_by", "create_time", "update_time", "delete_time") VALUES
    (1, '系统管理员', 'sysadmin', 1, 1, 0, NULL, 1, 1, '2026-10-01 11:01:18', '2026-10-06 19:14:53.612', NULL),
    (2, '运维工程师', 'ops', 2, 1, 0, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (3, '开发工程师', 'dev', 3, 1, 0, '', 1, 1, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL),
    (4, 'smoke', 'smoke_test', 9, 1, 0, NULL, NULL, NULL, NULL, NULL, '2026-10-01 12:35:21.594'),
    (5, 'smoke', 'smoke2', 9, 1, 0, NULL, 1, 1, '2026-10-06 18:00:13.271', '2026-10-06 18:00:13.271', '2026-10-06 18:00:13.294');

-- 表 "public"."sa_system_refresh_token"：按配置只导结构，跳过数据

-- 表 "public"."sa_system_role" 的数据
INSERT INTO "public"."sa_system_role" ("id", "parent_id", "name", "code", "level", "data_scope", "remark", "sort", "tenant_id", "status", "created_by", "updated_by", "create_time", "update_time", "delete_time") VALUES
    (1, 0, '超级管理员', 'super_admin', 100, 1, '系统内置角色，拥有全部权限', 1, 0, 1, 1, 1, '2026-10-01 11:01:18', '2026-10-06 18:03:26.393', NULL),
    (2, 0, '运维人员', 'ops', 50, 2, '负责服务器监控与日志查看', 2, 0, 1, 1, 1, '2026-10-01 11:01:18', '2026-10-06 21:30:42.225', NULL),
    (3, 0, '只读访客', 'viewer', 10, 4, '仅可查看监控数据', 3, 0, 1, 1, 1, '2026-10-01 11:01:18', '2026-10-06 21:07:10.589', NULL);

-- 表 "public"."sa_system_role_dept" 的数据

-- 表 "public"."sa_system_role_menu" 的数据
INSERT INTO "public"."sa_system_role_menu" ("id", "role_id", "menu_id") VALUES
    (1, 1, 100),
    (2, 1, 101),
    (3, 1, 102),
    (4, 1, 103),
    (5, 1, 104),
    (6, 1, 200),
    (7, 1, 201),
    (8, 1, 202),
    (9, 1, 203),
    (10, 1, 204),
    (11, 1, 205),
    (12, 1, 206),
    (13, 1, 207),
    (14, 1, 208),
    (15, 1, 209),
    (16, 1, 210),
    (17, 1, 2011),
    (18, 1, 2012),
    (19, 1, 2013),
    (20, 1, 2014),
    (21, 1, 2015),
    (22, 1, 2016),
    (23, 1, 2021),
    (24, 1, 2022),
    (25, 1, 2023),
    (26, 1, 2031),
    (27, 1, 2032),
    (28, 1, 2033),
    (29, 1, 2034),
    (30, 1, 2041),
    (31, 1, 2042),
    (32, 1, 2043),
    (33, 1, 2051),
    (34, 1, 2052),
    (35, 1, 2053),
    (36, 1, 2061),
    (37, 1, 2062),
    (38, 1, 2063),
    (39, 1, 2071),
    (40, 1, 2072),
    (41, 1, 2073),
    (42, 1, 2074),
    (43, 1, 2081),
    (44, 1, 2082),
    (45, 1, 2083),
    (46, 1, 2084),
    (47, 1, 2085),
    (48, 1, 2091),
    (49, 1, 2101),
    (1071, 1, 211),
    (1105, 1, 2111),
    (1106, 1, 2112),
    (1107, 1, 2113),
    (1108, 1, 2114),
    (1109, 1, 2115),
    (1110, 1, 2116),
    (1210, 1, 212),
    (1250, 1, 2121),
    (1637, 1, 213),
    (1678, 1, 2131),
    (2828, 1, 2017),
    (3030, 2, 100),
    (3031, 2, 101),
    (3032, 2, 102),
    (3033, 2, 103),
    (3034, 2, 104),
    (3035, 2, 200),
    (3036, 2, 209),
    (3037, 2, 210),
    (3038, 2, 213),
    (3414, 3, 100),
    (3415, 3, 101),
    (3416, 3, 102),
    (3417, 3, 103),
    (3418, 3, 104),
    (3419, 3, 200),
    (3420, 3, 209),
    (3421, 3, 2091),
    (3422, 3, 210),
    (3423, 3, 2101),
    (3424, 3, 213),
    (3425, 3, 2131),
    (3520, 1, 2010),
    (3528, 1, 2020),
    (3532, 1, 2030),
    (3537, 1, 2040),
    (3541, 1, 2050),
    (3545, 1, 2060),
    (3549, 1, 2070),
    (3554, 1, 2080),
    (3560, 1, 2090),
    (3562, 1, 2100),
    (3564, 1, 2110),
    (3571, 1, 2120),
    (3573, 1, 2130),
    (4122, 1, 105),
    (4137, 1, 1021),
    (4138, 1, 1022),
    (4139, 1, 1023),
    (5871, 1, 106),
    (5889, 1, 1061),
    (5890, 1, 1062),
    (5891, 1, 1063),
    (5952, 2, 106),
    (5962, 3, 106),
    (6460, 1, 107),
    (6461, 1, 108),
    (6482, 1, 1071),
    (6483, 1, 1072),
    (6484, 1, 1073),
    (6485, 1, 1074),
    (6486, 1, 1081),
    (6548, 2, 107),
    (6549, 2, 108),
    (6560, 3, 107),
    (6561, 3, 108),
    (7988, 1, 109),
    (8006, 1, 1024),
    (8098, 1, 1025),
    (8191, 1, 1026),
    (8285, 1, 110),
    (8380, 1, 1027),
    (8381, 1, 1028),
    (8382, 1, 1029),
    (8383, 1, 1030),
    (8384, 1, 1041),
    (8385, 1, 1051),
    (8386, 1, 1082),
    (8387, 1, 1101),
    (8408, 2, 1027),
    (8409, 2, 1028);

-- 表 "public"."sa_system_tenant" 的数据
INSERT INTO "public"."sa_system_tenant" ("id", "tenant_name", "tenant_code", "contact_name", "contact_phone", "contact_email", "address", "logo_url", "status", "expire_time", "max_users", "max_depts", "max_roles", "remark", "created_by", "updated_by", "create_time", "update_time", "delete_time") VALUES
    (1, '默认租户', 'default', '系统管理员', '13800000000', 'admin@localhost', '', '', 1, NULL, 0, 0, 0, '单租户部署，仅作数据保真', 0, 0, '2026-10-01 11:01:18', '2026-10-01 11:01:18', NULL);

-- 表 "public"."sa_system_user"：按配置只导结构，跳过数据

-- 表 "public"."sa_system_user_dept"：按配置只导结构，跳过数据

-- 表 "public"."sa_system_user_menu"：按配置只导结构，跳过数据

-- 表 "public"."sa_system_user_post"：按配置只导结构，跳过数据

-- 表 "public"."sa_system_user_role"：按配置只导结构，跳过数据

-- 表 "public"."sa_system_user_tenant"：按配置只导结构，跳过数据

-- 表 "public"."service_probe_states"：按配置只导结构，跳过数据

-- 表 "public"."ssh_commands"：按配置只导结构，跳过数据

-- 表 "public"."ssh_sessions"：按配置只导结构，跳过数据

-- 表 "public"."t_history_record"：按配置只导结构，跳过数据

-- 表 "public"."t_memory_detail"：按配置只导结构，跳过数据

-- 表 "public"."t_system_model"：按配置只导结构，跳过数据

-- 表 "public"."t_system_setting"：按配置只导结构，跳过数据

--
-- 索引
--

CREATE INDEX idx_agent_ip_histories_agent_id ON public.agent_ip_histories USING btree (agent_id);
CREATE INDEX idx_agent_ip_histories_agent_id_1 ON public.agent_ip_histories USING btree (agent_id);
CREATE INDEX idx_agent_ip_histories_ip_address ON public.agent_ip_histories USING btree (ip_address);
CREATE INDEX idx_agent_ip_histories_ip_address_1 ON public.agent_ip_histories USING btree (ip_address);
CREATE INDEX idx_agents_boot_id ON public.agents USING btree (boot_id);
CREATE INDEX idx_agents_boot_id_1 ON public.agents USING btree (boot_id);
CREATE INDEX idx_agents_environment ON public.agents USING btree (environment);
CREATE INDEX idx_agents_environment_1 ON public.agents USING btree (environment);
CREATE INDEX idx_agents_last_heartbeat_at ON public.agents USING btree (last_heartbeat_at);
CREATE INDEX idx_agents_last_heartbeat_at_1 ON public.agents USING btree (last_heartbeat_at);
CREATE INDEX idx_agents_last_metric_at ON public.agents USING btree (last_metric_at);
CREATE INDEX idx_agents_last_metric_at_1 ON public.agents USING btree (last_metric_at);
CREATE INDEX idx_agents_last_telegraf_metric ON public.agents USING btree (last_telegraf_metric);
CREATE INDEX idx_agents_last_telegraf_metric_1 ON public.agents USING btree (last_telegraf_metric);
CREATE INDEX idx_agents_online_state ON public.agents USING btree (online_state);
CREATE INDEX idx_agents_online_state_1 ON public.agents USING btree (online_state);
CREATE INDEX idx_agents_role ON public.agents USING btree (role);
CREATE INDEX idx_agents_role_1 ON public.agents USING btree (role);
CREATE INDEX idx_agents_site ON public.agents USING btree (site);
CREATE INDEX idx_agents_site_1 ON public.agents USING btree (site);
CREATE INDEX idx_agents_telegraf_state ON public.agents USING btree (telegraf_state);
CREATE INDEX idx_agents_telegraf_state_1 ON public.agents USING btree (telegraf_state);
CREATE INDEX idx_alert_events_agent_id ON public.alert_events USING btree (agent_id);
CREATE INDEX idx_alert_events_agent_id_1 ON public.alert_events USING btree (agent_id);
CREATE INDEX idx_alert_events_alert_id ON public.alert_events USING btree (alert_id);
CREATE INDEX idx_alert_events_alert_id_1 ON public.alert_events USING btree (alert_id);
CREATE INDEX idx_alert_events_created_at ON public.alert_events USING btree (created_at);
CREATE INDEX idx_alert_events_created_at_1 ON public.alert_events USING btree (created_at);
CREATE INDEX idx_alert_events_metric_key ON public.alert_events USING btree (metric_key);
CREATE INDEX idx_alert_events_metric_key_1 ON public.alert_events USING btree (metric_key);
CREATE INDEX idx_alert_rules_enabled ON public.alert_rules USING btree (enabled);
CREATE INDEX idx_alert_rules_enabled_1 ON public.alert_rules USING btree (enabled);
CREATE INDEX idx_alert_rules_metric ON public.alert_rules USING btree (metric);
CREATE INDEX idx_alert_rules_metric_1 ON public.alert_rules USING btree (metric);
CREATE INDEX idx_alert_rules_metric_key ON public.alert_rules USING btree (metric_key);
CREATE INDEX idx_alert_rules_metric_key_1 ON public.alert_rules USING btree (metric_key);
CREATE INDEX idx_alert_rules_scope ON public.alert_rules USING btree (scope);
CREATE INDEX idx_alert_rules_scope_1 ON public.alert_rules USING btree (scope);
CREATE INDEX idx_alert_rules_target ON public.alert_rules USING btree (target);
CREATE INDEX idx_alert_rules_target_1 ON public.alert_rules USING btree (target);
CREATE INDEX idx_alerts_agent_id ON public.alerts USING btree (agent_id);
CREATE INDEX idx_alerts_agent_id_1 ON public.alerts USING btree (agent_id);
CREATE INDEX idx_alerts_closed_at ON public.alerts USING btree (closed_at);
CREATE INDEX idx_alerts_closed_at_1 ON public.alerts USING btree (closed_at);
CREATE INDEX idx_alerts_dedup_key ON public.alerts USING btree (dedup_key);
CREATE INDEX idx_alerts_dedup_key_1 ON public.alerts USING btree (dedup_key);
CREATE INDEX idx_alerts_metric_key ON public.alerts USING btree (metric_key);
CREATE INDEX idx_alerts_metric_key_1 ON public.alerts USING btree (metric_key);
CREATE INDEX idx_alerts_rule_id ON public.alerts USING btree (rule_id);
CREATE INDEX idx_alerts_rule_id_1 ON public.alerts USING btree (rule_id);
CREATE INDEX idx_alerts_state ON public.alerts USING btree (state);
CREATE INDEX idx_alerts_state_1 ON public.alerts USING btree (state);
CREATE INDEX idx_alerts_target_id ON public.alerts USING btree (target_id);
CREATE INDEX idx_alerts_target_id_1 ON public.alerts USING btree (target_id);
CREATE UNIQUE INDEX uk_alerts_active ON public.alerts USING btree (dedup_key) WHERE (closed_at IS NULL);
CREATE INDEX idx_audit_records_action ON public.audit_records USING btree (action);
CREATE INDEX idx_audit_records_action_1 ON public.audit_records USING btree (action);
CREATE INDEX idx_audit_records_actor ON public.audit_records USING btree (actor);
CREATE INDEX idx_audit_records_actor_1 ON public.audit_records USING btree (actor);
CREATE INDEX idx_audit_records_created_at ON public.audit_records USING btree (created_at);
CREATE INDEX idx_audit_records_created_at_1 ON public.audit_records USING btree (created_at);
CREATE INDEX idx_audit_records_resource ON public.audit_records USING btree (resource);
CREATE INDEX idx_audit_records_resource_1 ON public.audit_records USING btree (resource);
CREATE INDEX idx_config_documents_active ON public.config_documents USING btree (active);
CREATE INDEX idx_config_documents_active_1 ON public.config_documents USING btree (active);
CREATE INDEX idx_config_documents_agent_id ON public.config_documents USING btree (agent_id);
CREATE INDEX idx_config_documents_agent_id_1 ON public.config_documents USING btree (agent_id);
CREATE INDEX idx_config_documents_version ON public.config_documents USING btree (version);
CREATE INDEX idx_config_documents_version_1 ON public.config_documents USING btree (version);
CREATE INDEX idx_docker_container_states_agent_id ON public.docker_container_states USING btree (agent_id);
CREATE INDEX idx_docker_container_states_agent_id_1 ON public.docker_container_states USING btree (agent_id);
CREATE INDEX idx_docker_container_states_container_id ON public.docker_container_states USING btree (container_id);
CREATE INDEX idx_docker_container_states_container_id_1 ON public.docker_container_states USING btree (container_id);
CREATE INDEX idx_docker_container_states_last_seen_at ON public.docker_container_states USING btree (last_seen_at);
CREATE INDEX idx_docker_container_states_last_seen_at_1 ON public.docker_container_states USING btree (last_seen_at);
CREATE INDEX idx_docker_container_states_state ON public.docker_container_states USING btree (state);
CREATE INDEX idx_docker_container_states_state_1 ON public.docker_container_states USING btree (state);
CREATE INDEX idx_maintenance_windows_agent_id ON public.maintenance_windows USING btree (agent_id);
CREATE INDEX idx_maintenance_windows_agent_id_1 ON public.maintenance_windows USING btree (agent_id);
CREATE INDEX idx_maintenance_windows_ends_at ON public.maintenance_windows USING btree (ends_at);
CREATE INDEX idx_maintenance_windows_ends_at_1 ON public.maintenance_windows USING btree (ends_at);
CREATE INDEX idx_maintenance_windows_starts_at ON public.maintenance_windows USING btree (starts_at);
CREATE INDEX idx_maintenance_windows_starts_at_1 ON public.maintenance_windows USING btree (starts_at);
CREATE INDEX idx_maintenance_windows_target_id ON public.maintenance_windows USING btree (target_id);
CREATE INDEX idx_maintenance_windows_target_id_1 ON public.maintenance_windows USING btree (target_id);
CREATE UNIQUE INDEX idx_metric_receipt ON public.metric_receipts USING btree (agent_id, sequence);
CREATE UNIQUE INDEX idx_metric_receipt_1 ON public.metric_receipts USING btree (agent_id, sequence);
CREATE INDEX idx_outbox_events_available_at ON public.outbox_events USING btree (available_at);
CREATE INDEX idx_outbox_events_available_at_1 ON public.outbox_events USING btree (available_at);
CREATE INDEX idx_outbox_events_created_at ON public.outbox_events USING btree (created_at);
CREATE INDEX idx_outbox_events_created_at_1 ON public.outbox_events USING btree (created_at);
CREATE INDEX idx_outbox_events_key ON public.outbox_events USING btree (key);
CREATE INDEX idx_outbox_events_key_1 ON public.outbox_events USING btree (key);
CREATE INDEX idx_outbox_events_topic ON public.outbox_events USING btree (topic);
CREATE INDEX idx_outbox_events_topic_1 ON public.outbox_events USING btree (topic);
CREATE UNIQUE INDEX uk_agent_protocol ON public.remote_endpoints USING btree (agent_id, protocol);
CREATE UNIQUE INDEX uk_agent_protocol_1 ON public.remote_endpoints USING btree (agent_id, protocol);
CREATE INDEX ix_sa_system_attachment_category_id ON public.sa_system_attachment USING btree (category_id);
CREATE INDEX ix_sa_system_attachment_delete_time ON public.sa_system_attachment USING btree (delete_time);
CREATE INDEX ix_sa_system_category_parent_id ON public.sa_system_category USING btree (parent_id);
CREATE UNIQUE INDEX ux_sa_system_config_key ON public.sa_system_config USING btree (key);
CREATE INDEX ix_sa_system_dept_delete_time ON public.sa_system_dept USING btree (delete_time);
CREATE INDEX ix_sa_system_dept_parent_id ON public.sa_system_dept USING btree (parent_id);
CREATE INDEX ix_sa_system_dict_data_code ON public.sa_system_dict_data USING btree (code);
CREATE UNIQUE INDEX ux_sa_system_dict_type_code ON public.sa_system_dict_type USING btree (code);
CREATE INDEX ix_sa_system_login_log_login_time ON public.sa_system_login_log USING btree (login_time);
CREATE INDEX ix_sa_system_menu_delete_time ON public.sa_system_menu USING btree (delete_time);
CREATE INDEX ix_sa_system_menu_parent_id ON public.sa_system_menu USING btree (parent_id);
CREATE UNIQUE INDEX ux_sa_system_notice_read ON public.sa_system_notice_read USING btree (notice_id, user_id);
CREATE INDEX ix_sa_system_oper_log_create_time ON public.sa_system_oper_log USING btree (create_time);
CREATE INDEX ix_sa_system_oper_log_username ON public.sa_system_oper_log USING btree (username);
CREATE INDEX ix_sa_system_post_delete_time ON public.sa_system_post USING btree (delete_time);
CREATE INDEX ix_sa_system_refresh_token_expires_at ON public.sa_system_refresh_token USING btree (expires_at);
CREATE INDEX ix_sa_system_refresh_token_revoked_at ON public.sa_system_refresh_token USING btree (revoked_at);
CREATE INDEX ix_sa_system_refresh_token_user_id ON public.sa_system_refresh_token USING btree (user_id);
CREATE UNIQUE INDEX ux_sa_system_refresh_token_hash ON public.sa_system_refresh_token USING btree (token_hash);
CREATE INDEX ix_sa_system_role_delete_time ON public.sa_system_role USING btree (delete_time);
CREATE UNIQUE INDEX ux_sa_system_role_code ON public.sa_system_role USING btree (code);
CREATE UNIQUE INDEX ux_sa_system_role_dept ON public.sa_system_role_dept USING btree (role_id, dept_id);
CREATE INDEX ix_sa_system_role_menu_role_id ON public.sa_system_role_menu USING btree (role_id);
CREATE UNIQUE INDEX ux_sa_system_role_menu ON public.sa_system_role_menu USING btree (role_id, menu_id);
CREATE INDEX ix_sa_system_user_delete_time ON public.sa_system_user USING btree (delete_time);
CREATE INDEX ix_sa_system_user_dept_id ON public.sa_system_user USING btree (dept_id);
CREATE UNIQUE INDEX ux_sa_system_user_username ON public.sa_system_user USING btree (username);
CREATE UNIQUE INDEX ux_sa_system_user_dept ON public.sa_system_user_dept USING btree (user_id, dept_id);
CREATE UNIQUE INDEX ux_sa_system_user_menu ON public.sa_system_user_menu USING btree (user_id, menu_id, tenant_id);
CREATE UNIQUE INDEX ux_sa_system_user_post ON public.sa_system_user_post USING btree (user_id, post_id);
CREATE INDEX ix_sa_system_user_role_user_id ON public.sa_system_user_role USING btree (user_id);
CREATE UNIQUE INDEX ux_sa_system_user_role ON public.sa_system_user_role USING btree (user_id, role_id);
CREATE UNIQUE INDEX ux_sa_system_user_tenant ON public.sa_system_user_tenant USING btree (user_id, tenant_id);
CREATE INDEX idx_service_probe_states_agent_id ON public.service_probe_states USING btree (agent_id);
CREATE INDEX idx_service_probe_states_last_checked_at ON public.service_probe_states USING btree (last_checked_at);
CREATE INDEX idx_service_probe_states_state ON public.service_probe_states USING btree (state);
CREATE INDEX idx_service_probe_states_target_id ON public.service_probe_states USING btree (target_id);
CREATE INDEX idx_ssh_commands_session_id ON public.ssh_commands USING btree (session_id);

--
-- 序列归属
--

ALTER SEQUENCE "public"."agent_ip_histories_id_seq" OWNED BY "public"."agent_ip_histories"."id";
ALTER SEQUENCE "public"."alert_events_id_seq" OWNED BY "public"."alert_events"."id";
ALTER SEQUENCE "public"."alert_rules_id_seq" OWNED BY "public"."alert_rules"."id";
ALTER SEQUENCE "public"."alerts_id_seq" OWNED BY "public"."alerts"."id";
ALTER SEQUENCE "public"."audit_records_id_seq" OWNED BY "public"."audit_records"."id";
ALTER SEQUENCE "public"."config_documents_id_seq" OWNED BY "public"."config_documents"."id";
ALTER SEQUENCE "public"."dangerous_commands_id_seq" OWNED BY "public"."dangerous_commands"."id";
ALTER SEQUENCE "public"."docker_container_states_id_seq" OWNED BY "public"."docker_container_states"."id";
ALTER SEQUENCE "public"."maintenance_windows_id_seq" OWNED BY "public"."maintenance_windows"."id";
ALTER SEQUENCE "public"."metric_receipts_id_seq" OWNED BY "public"."metric_receipts"."id";
ALTER SEQUENCE "public"."outbox_events_id_seq" OWNED BY "public"."outbox_events"."id";
ALTER SEQUENCE "public"."rdp_sessions_id_seq" OWNED BY "public"."rdp_sessions"."id";
ALTER SEQUENCE "public"."remote_endpoints_id_seq" OWNED BY "public"."remote_endpoints"."id";
ALTER SEQUENCE "public"."sa_job_id_seq" OWNED BY "public"."sa_job"."id";
ALTER SEQUENCE "public"."sa_job_job_id_seq" OWNED BY "public"."sa_job"."job_id";
ALTER SEQUENCE "public"."sa_job_log_id_seq" OWNED BY "public"."sa_job_log"."id";
ALTER SEQUENCE "public"."service_probe_states_id_seq" OWNED BY "public"."service_probe_states"."id";
ALTER SEQUENCE "public"."ssh_commands_id_seq" OWNED BY "public"."ssh_commands"."id";
ALTER SEQUENCE "public"."ssh_sessions_id_seq" OWNED BY "public"."ssh_sessions"."id";

--
-- 序列当前值（按属主列的最大值对齐）
--

SELECT setval('"public"."agent_ip_histories_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."agent_ip_histories"), 1), (SELECT COUNT(*) FROM "public"."agent_ip_histories") > 0);
SELECT setval('"public"."alert_events_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."alert_events"), 1), (SELECT COUNT(*) FROM "public"."alert_events") > 0);
SELECT setval('"public"."alert_rules_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."alert_rules"), 1), (SELECT COUNT(*) FROM "public"."alert_rules") > 0);
SELECT setval('"public"."alerts_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."alerts"), 1), (SELECT COUNT(*) FROM "public"."alerts") > 0);
SELECT setval('"public"."audit_records_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."audit_records"), 1), (SELECT COUNT(*) FROM "public"."audit_records") > 0);
SELECT setval('"public"."config_documents_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."config_documents"), 1), (SELECT COUNT(*) FROM "public"."config_documents") > 0);
SELECT setval('"public"."dangerous_commands_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."dangerous_commands"), 1), (SELECT COUNT(*) FROM "public"."dangerous_commands") > 0);
SELECT setval('"public"."docker_container_states_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."docker_container_states"), 1), (SELECT COUNT(*) FROM "public"."docker_container_states") > 0);
SELECT setval('"public"."maintenance_windows_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."maintenance_windows"), 1), (SELECT COUNT(*) FROM "public"."maintenance_windows") > 0);
SELECT setval('"public"."metric_receipts_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."metric_receipts"), 1), (SELECT COUNT(*) FROM "public"."metric_receipts") > 0);
SELECT setval('"public"."outbox_events_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."outbox_events"), 1), (SELECT COUNT(*) FROM "public"."outbox_events") > 0);
SELECT setval('"public"."rdp_sessions_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."rdp_sessions"), 1), (SELECT COUNT(*) FROM "public"."rdp_sessions") > 0);
SELECT setval('"public"."remote_endpoints_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."remote_endpoints"), 1), (SELECT COUNT(*) FROM "public"."remote_endpoints") > 0);
SELECT setval('"public"."sa_job_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_job"), 1), (SELECT COUNT(*) FROM "public"."sa_job") > 0);
SELECT setval('"public"."sa_job_job_id_seq"', COALESCE((SELECT MAX("job_id") FROM "public"."sa_job"), 1), (SELECT COUNT(*) FROM "public"."sa_job") > 0);
SELECT setval('"public"."sa_job_log_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_job_log"), 1), (SELECT COUNT(*) FROM "public"."sa_job_log") > 0);
SELECT setval('"public"."sa_system_attachment_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_system_attachment"), 1), (SELECT COUNT(*) FROM "public"."sa_system_attachment") > 0);
SELECT setval('"public"."sa_system_category_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_system_category"), 1), (SELECT COUNT(*) FROM "public"."sa_system_category") > 0);
SELECT setval('"public"."sa_system_config_group_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_system_config_group"), 1), (SELECT COUNT(*) FROM "public"."sa_system_config_group") > 0);
SELECT setval('"public"."sa_system_config_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_system_config"), 1), (SELECT COUNT(*) FROM "public"."sa_system_config") > 0);
SELECT setval('"public"."sa_system_dept_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_system_dept"), 1), (SELECT COUNT(*) FROM "public"."sa_system_dept") > 0);
SELECT setval('"public"."sa_system_dict_data_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_system_dict_data"), 1), (SELECT COUNT(*) FROM "public"."sa_system_dict_data") > 0);
SELECT setval('"public"."sa_system_dict_type_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_system_dict_type"), 1), (SELECT COUNT(*) FROM "public"."sa_system_dict_type") > 0);
SELECT setval('"public"."sa_system_login_log_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_system_login_log"), 1), (SELECT COUNT(*) FROM "public"."sa_system_login_log") > 0);
SELECT setval('"public"."sa_system_menu_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_system_menu"), 1), (SELECT COUNT(*) FROM "public"."sa_system_menu") > 0);
SELECT setval('"public"."sa_system_notice_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_system_notice"), 1), (SELECT COUNT(*) FROM "public"."sa_system_notice") > 0);
SELECT setval('"public"."sa_system_notice_read_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_system_notice_read"), 1), (SELECT COUNT(*) FROM "public"."sa_system_notice_read") > 0);
SELECT setval('"public"."sa_system_oper_log_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_system_oper_log"), 1), (SELECT COUNT(*) FROM "public"."sa_system_oper_log") > 0);
SELECT setval('"public"."sa_system_post_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_system_post"), 1), (SELECT COUNT(*) FROM "public"."sa_system_post") > 0);
SELECT setval('"public"."sa_system_role_dept_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_system_role_dept"), 1), (SELECT COUNT(*) FROM "public"."sa_system_role_dept") > 0);
SELECT setval('"public"."sa_system_role_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_system_role"), 1), (SELECT COUNT(*) FROM "public"."sa_system_role") > 0);
SELECT setval('"public"."sa_system_role_menu_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_system_role_menu"), 1), (SELECT COUNT(*) FROM "public"."sa_system_role_menu") > 0);
SELECT setval('"public"."sa_system_tenant_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_system_tenant"), 1), (SELECT COUNT(*) FROM "public"."sa_system_tenant") > 0);
SELECT setval('"public"."sa_system_user_dept_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_system_user_dept"), 1), (SELECT COUNT(*) FROM "public"."sa_system_user_dept") > 0);
SELECT setval('"public"."sa_system_user_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_system_user"), 1), (SELECT COUNT(*) FROM "public"."sa_system_user") > 0);
SELECT setval('"public"."sa_system_user_menu_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_system_user_menu"), 1), (SELECT COUNT(*) FROM "public"."sa_system_user_menu") > 0);
SELECT setval('"public"."sa_system_user_post_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_system_user_post"), 1), (SELECT COUNT(*) FROM "public"."sa_system_user_post") > 0);
SELECT setval('"public"."sa_system_user_role_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_system_user_role"), 1), (SELECT COUNT(*) FROM "public"."sa_system_user_role") > 0);
SELECT setval('"public"."sa_system_user_tenant_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."sa_system_user_tenant"), 1), (SELECT COUNT(*) FROM "public"."sa_system_user_tenant") > 0);
SELECT setval('"public"."service_probe_states_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."service_probe_states"), 1), (SELECT COUNT(*) FROM "public"."service_probe_states") > 0);
SELECT setval('"public"."ssh_commands_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."ssh_commands"), 1), (SELECT COUNT(*) FROM "public"."ssh_commands") > 0);
SELECT setval('"public"."ssh_sessions_id_seq"', COALESCE((SELECT MAX("id") FROM "public"."ssh_sessions"), 1), (SELECT COUNT(*) FROM "public"."ssh_sessions") > 0);

COMMIT;

-- 还原会话的 search_path：文件开头把它置空是为了让所有对象都按全限定名解析，
-- 但设置是会话级的，在 pgAdmin 的 Query Tool 里执行后会一直生效，
-- 之后手写的未限定表名 SQL 会莫名其妙地报「关系不存在」。
RESET search_path;

-- ===========================================================================
-- 备份结束：schema 1 / 表 48 / 序列 42 / 索引 135 / 约束 45 / 数据 304 行
-- ===========================================================================
