#!/usr/bin/env bash
set -euo pipefail

SERVER_NAME="${LOCALCLASH_OBSERVE_SERVER:-localclash}"
MCP_URL="${LOCALCLASH_OBSERVE_MCP_URL:-http://192.168.6.1:8765/mcp}"
MODE="${LOCALCLASH_OBSERVE_MODE:-print}"
DEFAULT_ALLOWED_TOOLS="mcp__${SERVER_NAME}__*"
if [[ "${LOCALCLASH_OBSERVE_ALLOW_ALL:-0}" == "1" ]]; then
  ALLOWED_TOOLS="mcp__${SERVER_NAME}__*"
else
  ALLOWED_TOOLS="${LOCALCLASH_OBSERVE_ALLOWED_TOOLS:-${DEFAULT_ALLOWED_TOOLS}}"
fi
LOG_DIR="${LOCALCLASH_OBSERVE_LOG_DIR:-.runtime/logs}"
CONFIG_FILE="${LOCALCLASH_OBSERVE_CONFIG_FILE:-.runtime/claude-code-localclash-observe.mcp.json}"
DEBUG_FILE="${LOCALCLASH_OBSERVE_DEBUG_FILE:-${LOG_DIR}/claude-code-localclash-observe.log}"
PERMISSION_MODE="${LOCALCLASH_OBSERVE_PERMISSION_MODE:-}"
PROMPT="${*:-Use the localclash MCP tools_list tool and reply with only the number of tools.}"

case "${MODE}" in
  print|interactive) ;;
  *)
    echo "error: LOCALCLASH_OBSERVE_MODE must be print or interactive" >&2
    exit 2
    ;;
esac

mkdir -p "${LOG_DIR}" "$(dirname "${CONFIG_FILE}")"

cat >"${CONFIG_FILE}" <<JSON
{
  "mcpServers": {
    "${SERVER_NAME}": {
      "type": "http",
      "url": "${MCP_URL}"
    }
  }
}
JSON

if ! curl -fsS "${MCP_URL%/mcp}/health" >/dev/null 2>&1; then
  echo "warning: health check failed for ${MCP_URL%/mcp}/health" >&2
fi

base_args=(
  --strict-mcp-config
  --mcp-config "${CONFIG_FILE}"
  --tools ""
  --allowedTools "${ALLOWED_TOOLS}"
  --debug-file "${DEBUG_FILE}"
)

if [[ -n "${PERMISSION_MODE}" ]]; then
  base_args+=(--permission-mode "${PERMISSION_MODE}")
fi

echo "Claude Code localClash observation"
echo "  mcp: ${MCP_URL}"
echo "  config: ${CONFIG_FILE}"
echo "  debug: ${DEBUG_FILE}"
echo "  allowedTools: ${ALLOWED_TOOLS}"
if [[ -n "${PERMISSION_MODE}" ]]; then
  echo "  permissionMode: ${PERMISSION_MODE}"
fi

if [[ "${MODE}" == "interactive" ]]; then
  exec claude "${base_args[@]}" "${PROMPT}"
fi

exec claude \
  --print \
  --output-format json \
  "${base_args[@]}" \
  "${PROMPT}"
