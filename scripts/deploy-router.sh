#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/deploy-router.sh [flags]

Build localClash for an OpenWrt/Linux router, deploy the binary over SSH, and
install or restart the router MCP HTTP procd service.

Flags:
  --host HOST          SSH target (default: root@192.168.6.1)
  --arch ARCH          Go target arch (default: arm64)
  --remote-bin PATH    Router install path (default: /usr/local/bin/localclash)
  --workdir PATH       Router working directory (default: /root)
  --mcp-addr ADDR      Router MCP bind address (default: 0.0.0.0:8765)
  --mcp-path PATH      Router MCP JSON-RPC path (default: /mcp)
  --skip-tests         Skip go test before building
  -h, --help           Show this help

Environment variables with the LOCALCLASH_ prefix can also override defaults:
LOCALCLASH_ROUTER_SSH, LOCALCLASH_ROUTER_ARCH, LOCALCLASH_REMOTE_BIN,
LOCALCLASH_ROUTER_WORKDIR, LOCALCLASH_MCP_ADDR, LOCALCLASH_MCP_PATH,
LOCALCLASH_SKIP_TESTS, LOCALCLASH_SSH_CONNECT_TIMEOUT,
LOCALCLASH_SSH_LOG_LEVEL.
USAGE
}

log() {
  printf '==> %s\n' "$*"
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

sha256_file() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
    return
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi
  die "missing required command: shasum or sha256sum"
}

reject_unsafe_value() {
  case "$2" in
    *"'"* | *$'\n'* | *$'\r'*)
      die "$1 must not contain quotes or newlines"
      ;;
  esac
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

router_ssh="${LOCALCLASH_ROUTER_SSH:-root@192.168.6.1}"
target_os="linux"
target_arch="${LOCALCLASH_ROUTER_ARCH:-arm64}"
remote_bin="${LOCALCLASH_REMOTE_BIN:-/usr/local/bin/localclash}"
remote_workdir="${LOCALCLASH_ROUTER_WORKDIR:-/root}"
mcp_addr="${LOCALCLASH_MCP_ADDR:-0.0.0.0:8765}"
mcp_path="${LOCALCLASH_MCP_PATH:-/mcp}"
skip_tests="${LOCALCLASH_SKIP_TESTS:-0}"
ssh_connect_timeout="${LOCALCLASH_SSH_CONNECT_TIMEOUT:-8}"
ssh_log_level="${LOCALCLASH_SSH_LOG_LEVEL:-ERROR}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --host | --ssh)
      [[ $# -ge 2 ]] || die "$1 requires a value"
      router_ssh="$2"
      shift 2
      ;;
    --arch)
      [[ $# -ge 2 ]] || die "$1 requires a value"
      target_arch="$2"
      shift 2
      ;;
    --remote-bin)
      [[ $# -ge 2 ]] || die "$1 requires a value"
      remote_bin="$2"
      shift 2
      ;;
    --workdir)
      [[ $# -ge 2 ]] || die "$1 requires a value"
      remote_workdir="$2"
      shift 2
      ;;
    --mcp-addr)
      [[ $# -ge 2 ]] || die "$1 requires a value"
      mcp_addr="$2"
      shift 2
      ;;
    --mcp-path)
      [[ $# -ge 2 ]] || die "$1 requires a value"
      mcp_path="$2"
      shift 2
      ;;
    --skip-tests)
      skip_tests="1"
      shift
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

remote_link="/usr/bin/localclash"
service_path="/etc/init.d/localclash-mcp"
mcp_log="${remote_workdir}/.runtime/logs/localclash-mcp.log"
local_bin="bin/${target_os}-${target_arch}/localclash"
remote_tmp="/tmp/localclash.new.$$"
remote_init_tmp="/tmp/localclash-mcp.init.$$"

reject_unsafe_value "remote_bin" "${remote_bin}"
reject_unsafe_value "remote_workdir" "${remote_workdir}"
reject_unsafe_value "mcp_addr" "${mcp_addr}"
reject_unsafe_value "mcp_path" "${mcp_path}"
reject_unsafe_value "mcp_log" "${mcp_log}"

require_command go
require_command ssh
require_command scp

ssh_opts=(
  -o BatchMode=yes
  -o ConnectTimeout="${ssh_connect_timeout}"
  -o LogLevel="${ssh_log_level}"
)

if [[ "${mcp_addr}" != *:* ]]; then
  die "--mcp-addr must include a port, for example 0.0.0.0:8765"
fi
mcp_port="${mcp_addr##*:}"
router_host="${router_ssh#*@}"
router_host="${router_host%%:*}"

log "checking router ${router_ssh}"
router_arch="$(
  ssh "${ssh_opts[@]}" "${router_ssh}" 'uname -m' 2>/dev/null \
    || die "cannot connect to ${router_ssh}"
)"
case "${router_arch}:${target_arch}" in
  aarch64:arm64 | arm64:arm64)
    ;;
  *)
    die "router arch is ${router_arch}, but build target is ${target_arch}; pass --arch if this is intentional"
    ;;
esac

if [[ "${skip_tests}" != "1" ]]; then
  log "running go tests"
  go test ./...
fi

log "building ${target_os}/${target_arch} binary at ${local_bin}"
mkdir -p "$(dirname "${local_bin}")"
GOOS="${target_os}" GOARCH="${target_arch}" CGO_ENABLED=0 \
  go build -trimpath -ldflags '-s -w' -o "${local_bin}" .

local_sha="$(sha256_file "${local_bin}")"
log "uploading binary to ${router_ssh}:${remote_tmp}"
scp "${ssh_opts[@]}" "${local_bin}" "${router_ssh}:${remote_tmp}"

log "installing binary at ${remote_bin}"
ssh "${ssh_opts[@]}" "${router_ssh}" 'sh -s' -- "${remote_tmp}" "${remote_bin}" "${remote_link}" <<'EOS'
set -eu
remote_tmp="$1"
remote_bin="$2"
remote_link="$3"

mkdir -p "$(dirname "$remote_bin")"
if [ -e "$remote_bin" ]; then
  cp -p "$remote_bin" "$remote_bin.bak.$(date +%Y%m%d%H%M%S)"
fi
cp "$remote_tmp" "$remote_bin.tmp"
chmod 0755 "$remote_bin.tmp"
mv "$remote_bin.tmp" "$remote_bin"
rm -f "$remote_tmp"
ln -sf "$remote_bin" "$remote_link"
"$remote_bin" --help >/dev/null
sha256sum "$remote_bin"
EOS

init_file="$(mktemp "${TMPDIR:-/tmp}/localclash-mcp.init.XXXXXX")"
cleanup() {
  rm -f "${init_file}"
}
trap cleanup EXIT

cat >"${init_file}" <<EOF
#!/bin/sh /etc/rc.common

START=95
STOP=10
USE_PROCD=1

COMMAND='${remote_bin}'
WORKDIR='${remote_workdir}'
MCP_ADDR='${mcp_addr}'
MCP_PATH='${mcp_path}'
LOG_FILE='${mcp_log}'

start_service() {
  mkdir -p "\$(dirname "\${LOG_FILE}")"
  procd_open_instance
  procd_set_param command /bin/sh -c "cd \"\${WORKDIR}\" && exec \"\${COMMAND}\" mcp --addr \"\${MCP_ADDR}\" --path \"\${MCP_PATH}\" >>\"\${LOG_FILE}\" 2>&1"
  procd_set_param respawn 3600 5 5
  procd_close_instance
}
EOF

log "installing procd service ${service_path}"
scp "${ssh_opts[@]}" "${init_file}" "${router_ssh}:${remote_init_tmp}"
ssh "${ssh_opts[@]}" "${router_ssh}" 'sh -s' -- "${remote_init_tmp}" "${service_path}" "${remote_workdir}" "${mcp_log}" <<'EOS'
set -eu
remote_init_tmp="$1"
service_path="$2"
remote_workdir="$3"
mcp_log="$4"

test -f /etc/rc.common
mkdir -p "$remote_workdir/.runtime/logs"
touch "$mcp_log"
mv "$remote_init_tmp" "$service_path"
chmod 0755 "$service_path"
"$service_path" enable
"$service_path" restart || {
  "$service_path" stop || true
  "$service_path" start
}
EOS

log "waiting for router MCP health on port ${mcp_port}"
ssh "${ssh_opts[@]}" "${router_ssh}" 'sh -s' -- "${mcp_port}" "${mcp_log}" <<'EOS'
set -eu
port="$1"
mcp_log="$2"
url="http://127.0.0.1:${port}/health"
i=0
while [ "$i" -lt 30 ]; do
  if command -v curl >/dev/null 2>&1 && curl -fsS "$url" >/dev/null 2>&1; then
    exit 0
  fi
  if command -v wget >/dev/null 2>&1 && wget -qO- "$url" >/dev/null 2>&1; then
    exit 0
  fi
  if command -v uclient-fetch >/dev/null 2>&1 && uclient-fetch -qO- "$url" >/dev/null 2>&1; then
    exit 0
  fi
  i=$((i + 1))
  sleep 1
done
echo "MCP health check failed: $url" >&2
tail -n 80 "$mcp_log" 2>/dev/null || true
exit 1
EOS

if command -v curl >/dev/null 2>&1; then
  log "checking LAN health http://${router_host}:${mcp_port}/health"
  curl -fsS "http://${router_host}:${mcp_port}/health" >/dev/null
fi

remote_sha="$(
  ssh "${ssh_opts[@]}" "${router_ssh}" 'sh -s' -- "${remote_bin}" <<'EOS' | awk '{print $1}'
set -eu
sha256sum "$1"
EOS
)"
if [[ "${remote_sha}" != "${local_sha}" ]]; then
  die "remote sha256 ${remote_sha} does not match local ${local_sha}"
fi

log "router deployment complete"
printf 'binary: %s:%s\n' "${router_ssh}" "${remote_bin}"
printf 'service: %s:%s\n' "${router_ssh}" "${service_path}"
printf 'mcp: http://%s:%s%s\n' "${router_host}" "${mcp_port}" "${mcp_path}"
printf 'health: http://%s:%s/health\n' "${router_host}" "${mcp_port}"
