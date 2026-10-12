cd webserver
go run ./tools/pgdump -out ../database/init.sql                      # 全量
go run ./tools/pgdump -schemas public -out dump.sql                  # 只导指定 schema
go run ./tools/pgdump -exclude-data sa_system_oper_log,sa_system_login_log -out lean.sql   # 日志表只导结构
go run ./tools/pgdump -no-data -out schema-only.sql                  # 只导结构

cd webserver
$ex = 'outbox_events,sa_system_oper_log,sa_system_login_log,sa_system_refresh_token,sa_system_notice_read,alert_events,alerts,metric_receipts,agent_ip_histories,agents,remote_endpoints,ssh_sessions,ssh_commands,rdp_sessions,sa_job_log,docker_container_states,audit_records,maintenance_windows,config_documents,service_probe_states,sa_system_attachment,t_history_record,t_memory_detail,t_system_model,t_system_setting,sa_system_user,sa_system_user_role,sa_system_user_dept,sa_system_user_post,sa_system_user_tenant,sa_system_user_menu'

go run ./tools/pgdump -out ../database/init.sql -exclude-data $ex   # 当前这份干净范本
go run ./tools/pgdump -out ../database/init.sql                    # 全量快照（带所有数据）
go run ./tools/pgdump -out ../database/init.sql -no-data           # 只导结构






导入（先在目标实例建空库）：
& "E:\PostgreSQL\18\bin\psql.exe" -h <host> -U postgres -d <新库> -v ON_ERROR_STOP=1 -f database\init.sql
