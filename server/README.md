# Monitor Server MVP

This directory contains the first server-side ingestion path:

- mTLS gRPC Collector on port `9500`;
- HTTPS management API on port `8080`;
- PostgreSQL asset, heartbeat, metric receipt, and idempotency records;
- PostgreSQL outbox-backed Metric Writer for InfluxDB 2 line-protocol writes;
- State and alert evaluator workers.

## Local setup

1. Copy `configs/server.local.example.yaml` to `configs/server.local.yaml`.
2. Set the PostgreSQL connection values. The local defaults match
   `localhost:5432`, user `postgres`, password `postgres`, database `postgres`.
3. Create an InfluxDB API token with write permission for organization `hkc` and
   bucket `bucket`, then set it for the current PowerShell session:

   ```powershell
   $env:MONITOR_INFLUX_TOKEN = "<your-write-token>"
   ```

4. Start the Collector:

   ```powershell
   go run ./cmd/collector -config configs/server.local.yaml
   ```

InfluxDB 2 uses an API token for writes. The `admin` username and password are
used to sign in to its UI and create that token; they are not used by the Go
write client.

## TLS requirement

The configured server certificate must include a Subject Alternative Name (SAN)
for the Agent's `server_address` hostname. The current local `server.pem` has
no SAN, so it will fail hostname verification in the Agent. Regenerate the
development certificate with a SAN such as `DNS:localhost` before testing.

For local use, configure the Agent with:

```yaml
server_address: "localhost:9500"
insecure_tls: false
```

Its client certificate must be signed by the CA used in `tls.ca_file`.

## Protocol status

The Collector and Agent now use generated strong Protobuf messages from
`api/proto/collector/v1/collector.proto`. Generated Go files are produced with
`protoc-gen-go` and `protoc-gen-go-grpc`; restart both Agent and Collector after
upgrading binaries because the RPC service name is
`monitor.collector.v1.CollectorService`.
