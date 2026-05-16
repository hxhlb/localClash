#!/usr/bin/env bash
set -euo pipefail

CALLCOPILOT_BIN="${CALLCOPILOT_BIN:-/Volumes/Data/Github/callCopilot/bin/callCopilot}"
MODEL="${CALLCOPILOT_MCP_TEST_MODEL:-ds}"

if [[ ! -x "${CALLCOPILOT_BIN}" ]]; then
  echo "error: callCopilot executable not found: ${CALLCOPILOT_BIN}" >&2
  exit 1
fi

output="$(
  "${CALLCOPILOT_BIN}" "${MODEL}" -- \
    -s \
    --disable-builtin-mcps \
    --disable-mcp-server chrome-devtools \
    --disable-mcp-server Sentry \
    --disable-mcp-server cupertino \
    --disable-mcp-server things \
    --disable-mcp-server notion \
    --disable-mcp-server searxng \
    --allow-tool=localclash \
    --max-autopilot-continues 1 \
    -p 'Call the MCP tool localclash doctor once. If its JSON result has a top-level status field, reply exactly LOCALCLASH_MCP_OK. Otherwise reply exactly LOCALCLASH_MCP_FAIL. Do not inspect files or run shell commands.'
)"

printf '%s\n' "${output}"

if ! grep -q 'LOCALCLASH_MCP_OK' <<<"${output}"; then
  echo "error: callCopilot MCP smoke test did not return LOCALCLASH_MCP_OK" >&2
  exit 1
fi
