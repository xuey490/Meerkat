#!/usr/bin/env bash
# Linux counterpart to Telegraf/run.txt (PowerShell).
# Sets MONITOR_* env, per-agent buffer dir, single-instance lock, then telegraf.
set -euo pipefail

if [ -z "${MONITOR_ROOT:-}" ]; then
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  if [ -f "$script_dir/telegraf-linux.conf" ]; then
    MONITOR_ROOT="$(cd "$script_dir/.." && pwd)"
  else
    echo "MONITOR_ROOT is unset. Run scripts/linux/agent/start.sh from the package root." >&2
    exit 1
  fi
fi
export MONITOR_ROOT="${MONITOR_ROOT//\\//}"

yaml_scalar() {
  local key="$1" file="$2"
  [ -f "$file" ] || return 0
  grep -E "^${key}:" "$file" 2>/dev/null | head -1 | sed -E 's/^[^:]+:\s*"?([^"#]+)"?\s*$/\1/' | tr -d ' ' || true
}

yaml_label() {
  local key="$1" file="$2"
  [ -f "$file" ] || return 0
  awk -v k="$key" '$0 ~ "^  " k ":" { gsub(/^.*: *"?|"?$/, ""); print; exit }' "$file" 2>/dev/null
}

placeholder_id() {
  case "$1" in
    ""|replace-with-stable-ulid|local-windows-002|local-linux-001) return 0 ;;
    *) return 1 ;;
  esac
}

agent_cfg="${AGENT_CFG:-$MONITOR_ROOT/client/configs/agent.yaml}"
if [ ! -f "$agent_cfg" ]; then
  for c in "$MONITOR_ROOT/client/configs/agent.example.yaml" "$MONITOR_ROOT/client/configs/agent.windows.yaml"; do
    [ -f "$c" ] && { agent_cfg="$c"; break; }
  done
fi

if placeholder_id "${MONITOR_AGENT_ID:-}"; then
  unset MONITOR_AGENT_ID
  id="$(yaml_scalar agent_id "$agent_cfg")"
  placeholder_id "$id" || MONITOR_AGENT_ID="$id"
fi
if [ -z "${MONITOR_ENVIRONMENT:-}" ]; then
  val="$(yaml_label environment "$agent_cfg")"
  MONITOR_ENVIRONMENT="${val:-test}"
fi
if [ -z "${MONITOR_SITE:-}" ]; then
  val="$(yaml_label site "$agent_cfg")"
  MONITOR_SITE="${val:-local}"
fi
if [ -z "${MONITOR_ROLE:-}" ]; then
  val="$(yaml_label role "$agent_cfg")"
  MONITOR_ROLE="${val:-linux}"
fi
if placeholder_id "${MONITOR_AGENT_ID:-}"; then
  echo "agent_id missing in $agent_cfg. Start monitor-agent once so it writes a stable id." >&2
  exit 1
fi

: "${MONITOR_MYSQL_DSN:=root:root@tcp(127.0.0.1:3306)/?parseTime=true}"
: "${MONITOR_REDIS_URL:=tcp://127.0.0.1:6379}"
: "${MONITOR_POSTGRES_ADDRESS:=postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable}"

export MONITOR_AGENT_ID MONITOR_ENVIRONMENT MONITOR_SITE MONITOR_ROLE
export MONITOR_MYSQL_DSN MONITOR_REDIS_URL MONITOR_POSTGRES_ADDRESS

telegraf_bin="${TELEGRAF_BIN:-}"
if [ -z "$telegraf_bin" ]; then
  for c in "$MONITOR_ROOT/Telegraf/telegraf" "$(command -v telegraf 2>/dev/null || true)"; do
    [ -n "$c" ] && [ -x "$c" ] && { telegraf_bin="$c"; break; }
  done
fi
[ -n "$telegraf_bin" ] || { echo "telegraf binary not found under $MONITOR_ROOT/Telegraf or PATH" >&2; exit 1; }

config="${TELEGRAF_CONFIG:-$MONITOR_ROOT/Telegraf/telegraf-linux.conf}"
[ -f "$config" ] || { echo "missing telegraf config: $config" >&2; exit 1; }

buffer_root="$MONITOR_ROOT/Telegraf/buffer"
buffer="$buffer_root/$MONITOR_AGENT_ID"
mkdir -p "$buffer"

lock="/tmp/monitor-telegraf-${MONITOR_AGENT_ID}.lock"
exec 9>"$lock"
if ! flock -n 9; then
  echo "Telegraf is already running for agent $MONITOR_AGENT_ID (lock $lock)" >&2
  exit 1
fi

exec "$telegraf_bin" --config "$config" --non-strict-env-handling
