根据 `README.md`、`client/` 目录结构以及部署文档，单独部署 **agent-monitor** 到 Linux 的最小化步骤如下（不使用一键脚本）：

---

## Linux Agent 最小部署清单

### 1. 准备二进制文件

在编译机（或本机，如果已装 Go 1.27+）执行：

```bash
cd /path/to/monitor/client
export CGO_ENABLED=0
go build -trimpath -ldflags "-s -w" -o monitor-agent ./cmd/monitor-agent
```

然后将 `monitor-agent` 复制到目标服务器。

---

### 2. 目标服务器创建目录与用户

```bash
# 创建专用用户
sudo useradd -r -s /usr/sbin/nologin monitor-agent 2>/dev/null || true

# 创建配置、状态、日志目录
sudo install -d -m 0700 /etc/monitor-agent /var/lib/monitor-agent /var/log/monitor-agent
sudo install -d -m 0750 /var/lib/telegraf/buffer

# 安装 monitor-agent
sudo install -Dm755 monitor-agent /usr/local/bin/monitor-agent
```

---

### 3. 安装 Telegraf（系统包）

```bash
# Ubuntu/Debian
sudo apt-get update
sudo apt-get install -y telegraf

# 或从 InfluxData 官方源安装新版
```

---

### 4. 准备 mTLS 证书

将中心端签发的证书复制到目标服务器（测试环境可共用一套 `client.pem`）：

```bash
sudo install -Dm600 ca.pem      /etc/monitor-agent/ca.pem
sudo install -Dm600 client.pem  /etc/monitor-agent/client.pem
sudo install -Dm600 client-key.pem /etc/monitor-agent/client-key.pem
```

---

### 5. 配置 `agent.yaml`

复制模板并修改：

```bash
sudo cp /path/to/monitor/client/configs/agent.example.yaml /etc/monitor-agent/agent.yaml
# 修改关键字段
```

必改项：

| 字段 | 说明 |
| --- | --- |
| `agent_id` | 全局唯一标识（如 ULID） |
| `server_address` | 中心 Collector 地址，如 `192.168.1.100:9500` |
| `ca_file` / `cert_file` / `key_file` | 证书路径（上一步的落位路径） |
| `labels` | `environment`、`site`、`role` 等标签 |

示例核心配置：

```yaml
agent_id: "01JXXXXXXX"
labels:
  environment: production
  site: shanghai
  role: web
listen_address: "127.0.0.1:9510"
server_address: "192.168.1.100:9500"
ca_file: "/etc/monitor-agent/ca.pem"
cert_file: "/etc/monitor-agent/client.pem"
key_file: "/etc/monitor-agent/client-key.pem"
state_directory: "/var/lib/monitor-agent"
health_address: "127.0.0.1:9511"
telegraf_binary: "/usr/bin/telegraf"
telegraf_service: "telegraf"
auto_recover_telegraf: true
```

---

### 6. 配置 `telegraf.conf`

基于模板生成配置，替换环境变量：

```bash
sudo cp /opt/monitor/client/telegraf/telegraf.conf.template /etc/telegraf/telegraf.conf

sudo telegraf --config /www/wwwroot/Telegraf/telegraf-linux.conf
```

模板中使用了变量 `${AGENT_ID}`、`${ENVIRONMENT}`、`${SITE}`、`${ROLE}`，可通过 systemd 的 `Environment=` 注入，或直接用 `sed` 替换为固定值。

如果不用环境变量渲染，直接替换：

```bash
sudo sed -i \
  -e 's/${AGENT_ID}/01JXXXXXXX/g' \
  -e 's/${ENVIRONMENT}/production/g' \
  -e 's/${SITE}/shanghai/g' \
  -e 's/${ROLE}/web/g' \
  /etc/telegraf/telegraf.conf
```

> 注意：模板中 MySQL/Redis/PostgreSQL 的连接串默认使用环境变量，如果不需要这些插件，可注释掉对应段落。

验证配置：

```bash
sudo telegraf --config /etc/telegraf/telegraf.conf --test
```

---

### 7. systemd 服务文件

启动命令： ./monitor-agent --config /www/wwwroot/client/configs/agent.yaml

```bash
sudo cp /path/to/monitor/client/packaging/monitor-agent.service /etc/systemd/system/
sudo systemctl daemon-reload
```

`monitor-agent.service` 内容要点（已内置在仓库中）：

```ini
[Unit]
Description=Monitor Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=monitor-agent
Group=monitor-agent
ExecStart=/usr/local/bin/monitor-agent --config /etc/monitor-agent/agent.yaml
Restart=always
RestartSec=5
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/monitor-agent /var/log/monitor-agent

[Install]
WantedBy=multi-user.target
```

---

### 8. 启动并验证

```bash
# 先启动 Agent
sudo systemctl enable --now monitor-agent

# 再启动 Telegraf
sudo systemctl enable --now telegraf

# 查看状态
sudo systemctl status monitor-agent telegraf

# 查看日志
sudo journalctl -u monitor-agent -u telegraf -n 100 -f

# 验证 Agent 健康端口
curl -sS http://127.0.0.1:9511/healthz

# 验证 Telegraf -> Agent 通路（期望 204）
curl -i -X POST http://127.0.0.1:9510/v1/telegraf/metrics \
  -H "Content-Type: text/plain" \
  --data-binary 'probe,source=manual value=1i'
```

---

### 总结：最小部署只需要

| 组件 | 来源 |
| --- | --- |
| `monitor-agent` 二进制 | `go build` 或从 `dist/linux-amd64` 复制 |
| `telegraf` | `apt-get install telegraf` |
| 3 张证书 | `ca.pem` + `client.pem` + `client-key.pem` |
| 1 个配置文件 | `/etc/monitor-agent/agent.yaml` |
| 1 个 Telegraf 配置 | `/etc/telegraf/telegraf.conf` |
| systemd 服务文件 | `client/packaging/monitor-agent.service` |

不需要：PostgreSQL、InfluxDB、Collector、webserver、Node.js、Go（仅编译机需要）。