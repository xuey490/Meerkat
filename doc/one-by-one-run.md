你现在的进程参数已经和一键脚本一致，手动启动就是把 `scripts/linux/server/start.sh` 和 `scripts/linux/agent/start.sh` 里做的事拆开、按顺序自己执行。

---

## 启动顺序

**中心机（Server）**：PostgreSQL → InfluxDB → Collector → webserver  

**被监控机（Agent）**：monitor-agent → Telegraf（Agent 要先监听 `:9510`）

下面路径按你的部署 **`MONITOR_ROOT=/opt/monitor`** 写；若机器是 arm64，把 `linux-amd64` 换成 `linux-arm64`。

---

## 中心机：手动启动

### 0. 可选：加载环境变量

```bash
export MONITOR_ROOT=/opt/monitor
# 若 setup 写过：
[ -f /etc/monitor/monitor.env ] && set -a && . /etc/monitor/monitor.env && set +a
```

### 1. PostgreSQL（脚本里会先起这个）

```bash
sudo systemctl start postgresql   # 或 postgresql-16
ss -lnt | grep ':5432'
```

### 2. InfluxDB（`influxd`）

一键脚本等价于：**无额外参数**，用 PATH 里的二进制（你这边是 `/usr/bin/influxd`）：

```bash
# 前台排障
/usr/bin/influxd

# 后台（与 start.sh 相同）
nohup /usr/bin/influxd >/tmp/influxd.monitor.log 2>&1 &
```

若用仓库自带包：

```bash
nohup /opt/monitor/influxdb2/influxdb2_linux_amd64/influxd >/tmp/influxd.monitor.log 2>&1 &
# 或 export INFLUXD_BIN=... 后执行同上
```

数据目录、bolt 等由 **Influx 安装方式 / 环境**决定（系统包一般是 `/var/lib/influxdb2`），不是 Monitor 再传 `-config`。

探活：`curl -s http://127.0.0.1:8086/health`

### 3. Collector

Go 程序只有 **`-config`** 一个常用参数：

```bash
/opt/monitor/dist/linux-amd64/collector \
  -config /opt/monitor/server/configs/server.local.yaml
```

后台：

```bash
nohup /opt/monitor/dist/linux-amd64/collector \
  -config /opt/monitor/server/configs/server.local.yaml \
  >/tmp/monitor-collector.log 2>&1 &
```

监听：**gRPC `:9500`**，管理 HTTPS 一般在 **`8080`**（见 README 端口表）。

### 4. webserver

同样只有 **`-config`**；首次建 admin 用户时会读 **`WEBSERVER_ADMIN_PASSWORD`**（脚本未设时默认 `Monitor123!`）：

```bash
export WEBSERVER_ADMIN_PASSWORD='Monitor123!'   # 按你实际密码改
nohup /opt/monitor/dist/linux-amd64/webserver \
  -config /opt/monitor/webserver/configs/webserver.yaml \
  >/tmp/monitor-webserver.log 2>&1 &
```

探活：`curl -s http://127.0.0.1:8090/healthz`

---

## 被监控机：手动启动 Agent + Telegraf

### 1. monitor-agent

参数是 **`--config`**（双横线，和你 `pgrep` 一致）：

```bash
export MONITOR_ROOT=/opt/monitor

nohup /opt/monitor/dist/linux-amd64/monitor-agent \
  --config /opt/monitor/client/configs/agent.yaml \
  >/tmp/monitor-agent.log 2>&1 &
```

确认 **`:9510`** 起来后再启 Telegraf：

```bash
ss -lnt | grep ':9510'
curl -s http://127.0.0.1:9511/healthz
```

`monitor-agent` 还有 **`-u`**（Windows 隐藏控制台），Linux 上一般不用。

### 2. Telegraf

不要裸跑 `telegraf`，要用 **`Telegraf/run-linux.sh`**（会设 `MONITOR_*`、buffer 目录、单实例锁），和 `start.sh` 一致：

```bash
export MONITOR_ROOT=/opt/monitor

# 与 agent.yaml 对齐（start.sh 会从 yaml 读；手动时建议自己 export）
export MONITOR_AGENT_ID="$(grep -E '^agent_id:' /opt/monitor/client/configs/agent.yaml | head -1 | sed -E 's/^[^:]+:\s*"?([^"#]+)"?.*/\1/')"
# 可选：MONITOR_ENVIRONMENT / MONITOR_SITE / MONITOR_ROLE（labels 段）

nohup /opt/monitor/Telegraf/run-linux.sh >/tmp/telegraf.log 2>&1 &
```

等价于直接调用（`run-linux.sh` 最后一行）：

```bash
/usr/bin/telegraf \
  --config /opt/monitor/Telegraf/telegraf-linux.conf \
  --non-strict-env-handling
```

但缺少 `MONITOR_ROOT`、`MONITOR_AGENT_ID` 等环境时，配置里的 `${MONITOR_*}` 会不对。

可选覆盖：

- `TELEGRAF_BIN=/usr/bin/telegraf`
- `TELEGRAF_CONFIG=/opt/monitor/Telegraf/telegraf-linux.conf`

---

## 和一键脚本的对应关系

| 组件 | 二进制 | 参数 / 环境 |
|------|--------|-------------|
| influxd | `/usr/bin/influxd` | 无（或 `INFLUXD_BIN`） |
| collector | `dist/linux-amd64/collector` | `-config server/configs/server.local.yaml` |
| webserver | `dist/linux-amd64/webserver` | `-config webserver/configs/webserver.yaml` + `WEBSERVER_ADMIN_PASSWORD` |
| monitor-agent | `dist/linux-amd64/monitor-agent` | `--config client/configs/agent.yaml` |
| telegraf | `Telegraf/run-linux.sh` | `MONITOR_ROOT` + 可选 `MONITOR_AGENT_ID` 等 |

环境变量覆盖（与脚本相同）：`COLLECTOR_BIN`、`COLLECTOR_CONFIG`、`WEBSERVER_BIN`、`WEBSERVER_CONFIG`、`AGENT_BIN`、`AGENT_CFG`。

---

## 排障习惯

- **前台跑**：去掉 `nohup ... &`，在当前终端看日志。
- **先停再手启**：`/opt/monitor/scripts/linux/server/stop.sh`、`/opt/monitor/scripts/linux/agent/stop.sh`，避免端口占用。
- **日志**：`/tmp/influxd.monitor.log`、`/tmp/monitor-collector.log`、`/tmp/monitor-webserver.log`、`/tmp/monitor-agent.log`、`/tmp/telegraf.log`。

若中心机和 Agent 在同一台 `/opt/monitor` 上，仍建议顺序：**PG → influxd → collector → webserver → monitor-agent → Telegraf**。