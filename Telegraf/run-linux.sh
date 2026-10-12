#!/usr/bin/env bash
# Linux counterpart to Telegraf/run.txt (PowerShell).
# Sets MONITOR_* env, per-agent buffer dir, single-instance lock, then telegraf.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
self_root="$(cd "$script_dir/.." && pwd)"

if [ -z "${MONITOR_ROOT:-}" ]; then
  if [ -f "$script_dir/telegraf-linux.conf" ]; then
    MONITOR_ROOT="$self_root"
  else
    echo "MONITOR_ROOT is unset. Run scripts/linux/agent/start.sh from the package root." >&2
    exit 1
  fi
elif [ ! -d "$MONITOR_ROOT/Telegraf" ] || [ ! -d "$MONITOR_ROOT/client" ]; then
  # MONITOR_ROOT 有值但与实际目录结构不符（例如被 monitor.env 覆盖成了空目录或 "/"，
  # 表现为「missing telegraf config: //Telegraf/telegraf-linux.conf」）。
  # 用脚本自身位置（<包根>/Telegraf/run-linux.sh）推断包根并纠正。
  echo "[WARN] MONITOR_ROOT='$MONITOR_ROOT' 不像包根目录，改用脚本位置推断：'$self_root'" >&2
  MONITOR_ROOT="$self_root"
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
[ -f "$config" ] || { echo "missing telegraf config: $config (MONITOR_ROOT='$MONITOR_ROOT')" >&2; exit 1; }

buffer_root="$MONITOR_ROOT/Telegraf/buffer"
buffer="$buffer_root/$MONITOR_AGENT_ID"
mkdir -p "$buffer"

lock="/tmp/monitor-telegraf-${MONITOR_AGENT_ID}.lock"
exec 9>"$lock"
if ! flock -n 9; then
  echo "Telegraf is already running for agent $MONITOR_AGENT_ID (lock $lock)" >&2
  exit 1
fi

# --non-strict-env-handling 只在 Telegraf 1.19~1.2x 存在，之后被移除。传入未知参数会让
# Telegraf 直接以「flag provided but not defined」退出，表现为「启动后立即消失」。
# 这里探测 help 输出再决定是否传；用不带横线的关键字匹配，兼容单横线/双横线两种写法。
telegraf_args=(--config "$config")
if "$telegraf_bin" --help 2>&1 | grep -q 'non-strict-env-handling'; then
  telegraf_args+=(--non-strict-env-handling)
else
  echo "telegraf $(basename "$telegraf_bin") 不支持 --non-strict-env-handling，已自动跳过该参数" >&2
fi

echo "telegraf: $telegraf_bin ${telegraf_args[*]} ${*:-} (agent_id=$MONITOR_AGENT_ID, root=$MONITOR_ROOT)" >&2

# 允许透传额外参数，例如 `bash Telegraf/run-linux.sh --test` 只做配置校验并打印一轮指标后退出。
# 注意用 "$#" 判断而不是直接 "${@}"，以兼容 set -u 下较老的 bash。
if [ "$#" -gt 0 ]; then
  exec "$telegraf_bin" "${telegraf_args[@]}" "$@"
fi
exec "$telegraf_bin" "${telegraf_args[@]}"
