#!/usr/bin/env bash
set -euo pipefail

ADDR="${LOCALCLASH_MCP_ADDR:-127.0.0.1:8765}"
PATH_PREFIX="${LOCALCLASH_MCP_PATH:-/mcp}"
LOG_FILE="$(mktemp "${TMPDIR:-/tmp}/localclash-mcp-http.XXXXXX.log")"

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

for _ in $(seq 1 50); do
  if curl -fsS "http://${ADDR}/health" >/dev/null 2>&1; then
    break
  fi
  if ! kill -0 "${SERVER_PID}" 2>/dev/null; then
    cat "${LOG_FILE}" >&2
    exit 1
  fi
  sleep 0.1
done

response="$(
  curl -fsS \
    -H 'Content-Type: application/json' \
    -X POST "http://${ADDR}${PATH_PREFIX}" \
    --data '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"doctor","arguments":{}}}'
)"

printf '%s\n' "${response}"

if ! grep -q '"status":"ok"' <<<"${response}"; then
  echo "error: MCP HTTP doctor smoke test did not return status ok" >&2
  exit 1
fi
