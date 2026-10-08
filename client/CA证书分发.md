从代码可以看出，`certgen` 通过命令行参数控制 Server 证书的 SAN（主体备用名称）：

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `-out` | `certs` | 输出目录 |
| `-dns` | `localhost,monitor.company.example` | Server 证书 DNS SAN |
| `-ip` | `127.0.0.1,::1` | Server 证书 IP SAN |

生成文件共 6 个：

- `ca.pem` / `ca-key.pem` — CA 根证书和私钥
- `server.pem` / `server-key.pem` — 服务端证书（SAN 含你指定的 IP/DNS）
- `client.pem` / `client-key.pem` — 客户端证书（所有 Agent 共用）

---

## 用法示例

### 1. 在 client 目录下运行（推荐）

```bash
cd /path/to/monitor/client

# 指定你的中心机 IP（如 192.168.1.100）
go run ./cmd/certgen -out ../scripts/cert -ip "127.0.0.1,192.168.1.100" -dns "localhost,monitor.company.example"
```

或指定公网 IP：

```bash
go run ./cmd/certgen -out ../scripts/cert -ip "127.0.0.1,123.45.67.89,192.168.1.12" -dns "localhost,monitor.example.com"
```

### 2. 输出位置

证书会生成到 `scripts/cert/`：

```text
monitor/scripts/cert/
├── ca.pem
├── ca-key.pem      ← CA 私钥，不能分发到 Agent
├── server.pem
├── server-key.pem  ← 仅放中心 Collector 机
├── client.pem
└── client-key.pem  ← 复制到所有 Agent
```

### 3. 手动分发

**中心机（Collector）需要：**
- `ca.pem`
- `server.pem`
- `server-key.pem`

**被监控机（Agent）需要：**
- `ca.pem`
- `client.pem`
- `client-key.pem`

---

## 关键注意事项

1. **Server 证书的 SAN 必须包含 Collector 的监听地址**。如果 Agent 通过 `192.168.1.100:9500` 连接，那 Server 证书必须有 `192.168.1.100` 这个 IP SAN。

2. **`-ip` 参数只影响 server 证书**。client 证书没有 SAN 限制，所有 Agent 可以共用同一张 `client.pem`。

3. **`ca-key.pem` 是 CA 根私钥**，只能在生成证书的环境中保留，不要复制到任何服务器或提交到 git。

4. 如果有多台中心机需要各自的服务端证书，可以运行多次（但通常只需要一套 CA，然后分别签发 server 证书）。

5. Windows 上同样用法，路径换一下即可：

```powershell
cd C:\Users\Administrator\Desktop\monitor\client
go run .\cmd\certgen -out ..\scripts\cert -ip "127.0.0.1,192.168.1.100" -dns "localhost,monitor.company.example"
```