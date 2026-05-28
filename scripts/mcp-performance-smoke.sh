#!/usr/bin/env bash
set -euo pipefail

ADDR="${LOCALCLASH_MCP_ADDR:-127.0.0.1:8765}"
PATH_PREFIX="${LOCALCLASH_MCP_PATH:-/mcp}"
OUT="${LOCALCLASH_PERF_OUT:-.runtime/diagnostics/mcp-performance-$(date -u +%Y%m%dT%H%M%SZ).json}"
LOG_FILE="$(mktemp "${TMPDIR:-/tmp}/localclash-mcp-perf.XXXXXX.log")"
TOOLS="${LOCALCLASH_PERF_TOOLS:-config_status routing_explain config_render}"

mkdir -p "$(dirname "${OUT}")"

go run . mcp --addr "${ADDR}" --path "${PATH_PREFIX}" >"${LOG_FILE}" 2>&1 &
SERVER_PID=$!

cleanup() {
  if kill -0 "${SERVER_PID}" 2>/dev/null; then
    kill "${SERVER_PID}" 2>/dev/null || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
  rm -f "${LOG_FILE}"
}
trap cleanup EXIT

for _ in $(seq 1 80); do
  if curl -fsS "http://${ADDR}/health" >/dev/null 2>&1; then
    break
  fi
  if ! kill -0 "${SERVER_PID}" 2>/dev/null; then
    cat "${LOG_FILE}" >&2
    exit 1
  fi
  sleep 0.1
done

json_escape() {
  awk '
    BEGIN { ORS="" }
    {
      gsub(/\\/, "\\\\")
      gsub(/"/, "\\\"")
      gsub(/\r/, "\\r")
      if (NR > 1) {
        printf "\\n"
      }
      printf "%s", $0
    }
  '
}

epoch_ms() {
  local value
  value="$(date +%s%3N 2>/dev/null || true)"
  if [[ "${value}" =~ ^[0-9]+$ ]]; then
    printf '%s\n' "${value}"
    return
  fi
  printf '%s000\n' "$(date +%s)"
}

latest_task_log() {
  local tool="$1"
  local dir=".runtime/mcp-tasks"
  if [ ! -d "${dir}" ]; then
    return 0
  fi
  ls -t "${dir}"/*-"${tool}"-*.log 2>/dev/null | head -n 1 || true
}

process_snapshot() {
  local pattern="$1"
  if ps ax -o pid=,pcpu=,pmem=,comm= >/dev/null 2>&1; then
    ps ax -o pid=,pcpu=,pmem=,comm= | awk -v pat="${pattern}" 'BEGIN { IGNORECASE=1 } $0 ~ pat { print }'
  else
    ps w 2>/dev/null | grep -Ei "${pattern}" | grep -v grep || true
  fi
}

call_tool() {
  local id="$1"
  local tool="$2"
  local args="$3"
  local started ended duration response status task_log stage_events
  started="$(epoch_ms)"
  set +e
  response="$(
    curl -fsS \
      -H 'Content-Type: application/json' \
      -X POST "http://${ADDR}${PATH_PREFIX}" \
      --data "{\"jsonrpc\":\"2.0\",\"id\":${id},\"method\":\"tools/call\",\"params\":{\"name\":\"${tool}\",\"arguments\":${args}}}" 2>&1
  )"
  status=$?
  set -e
  ended="$(epoch_ms)"
  duration=$((ended - started))
  task_log="$(latest_task_log "${tool}")"
  stage_events=""
  if [ -n "${task_log}" ]; then
    stage_events="$(grep '"event":"stage_' "${task_log}" || true)"
  fi
  printf '{"tool":"%s","status":%d,"duration_ms":%d,"response_bytes":%d,"task_log":"%s","stage_events_jsonl":"%s","response":"%s"}\n' \
    "${tool}" "${status}" "${duration}" "${#response}" \
    "$(printf '%s' "${task_log}" | json_escape)" \
    "$(printf '%s' "${stage_events}" | json_escape)" \
    "$(printf '%s' "${response}" | json_escape)"
}

tmp_results="$(mktemp "${TMPDIR:-/tmp}/localclash-mcp-perf-results.XXXXXX.jsonl")"
trap 'rm -f "${tmp_results}"; cleanup' EXIT

id=1
for tool in ${TOOLS}; do
  case "${tool}" in
    routing_explain)
      args='{"query":"Steam","evidence":false}'
      ;;
    config_render)
      args='{"wait":true,"force":true}'
      ;;
    *)
      args='{}'
      ;;
  esac
  call_tool "${id}" "${tool}" "${args}" >>"${tmp_results}"
  id=$((id + 1))
done

cpu_snapshot="$(
  ps -o pid=,pcpu=,pmem=,comm= -p "${SERVER_PID}" 2>/dev/null | sed 's/^ *//'
)"
localclash_cpu_snapshot="$(
  {
    printf '%s\n' "${cpu_snapshot}"
    process_snapshot 'localclash'
  } | awk 'NF && !seen[$0]++'
)"
mihomo_cpu_snapshot="$(process_snapshot 'mihomo')"
clash_cpu_snapshot="$(process_snapshot '(^|/)(clash|clash-meta)( |$)')"

{
  printf '{\n'
  printf '  "generated_at": "%s",\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf '  "addr": "%s",\n' "${ADDR}"
  printf '  "path": "%s",\n' "${PATH_PREFIX}"
  printf '  "server_pid": %d,\n' "${SERVER_PID}"
  printf '  "cpu_snapshot": "%s",\n' "$(printf '%s' "${cpu_snapshot}" | json_escape)"
  printf '  "cpu_snapshots": {\n'
  printf '    "localclash": "%s",\n' "$(printf '%s' "${localclash_cpu_snapshot}" | json_escape)"
  printf '    "mihomo": "%s",\n' "$(printf '%s' "${mihomo_cpu_snapshot}" | json_escape)"
  printf '    "background_clash": "%s"\n' "$(printf '%s' "${clash_cpu_snapshot}" | json_escape)"
  printf '  },\n'
  printf '  "service_log": ".runtime/logs/mcp-http.jsonl",\n'
  printf '  "tools": [\n'
  awk 'NR>1{printf ",\n"} {printf "    "$0}' "${tmp_results}"
  printf '\n  ]\n'
  printf '}\n'
} >"${OUT}"

printf '%s\n' "${OUT}"
