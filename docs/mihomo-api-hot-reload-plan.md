# Mihomo API Hot Reload Development Plan

Status: draft.

This document splits the subscription apply improvement into separate
development tasks. The goal is to make config activation observable and explicit:

```text
subscription refresh -> config render -> mihomo config test -> config promote -> hot reload
```

The current LuCI "save and apply subscription" flow saves subscription sources,
refreshes artifacts, and renders a generated Mihomo config. It does not make a
running Mihomo process load the new config. The new flow must close that gap
without hiding failures behind an implicit process restart.

## Design Rules

- Do not add implicit fallback from hot reload to process restart.
- Do not hot reload a config that has not passed an explicit Mihomo config test.
- Do not trust a previous test by timestamp alone. Re-hash the current config
  bytes and compare them with the tested config hash.
- Keep Mihomo controller access fixed to the configured local runtime endpoint.
  A generic Mihomo API MCP tool must not become a generic HTTP client.
- Keep subscription URIs, Mihomo secrets, and full proxy dumps out of normal
  logs and source docs.

## Config Path Relocation

The effective runtime config should move from:

```text
generated/mihomo.yaml
```

to a path under the Mihomo home directory / SAFE_PATHS:

```text
.runtime/mihomo/config.yaml
```

Render should use a candidate/promote model:

```text
.runtime/mihomo/config.candidate.yaml
.runtime/mihomo/config.yaml
```

The candidate config is the output of render. `mihomo_config_test` validates the
candidate. Only a passing candidate is promoted to `config.yaml`. Runtime start,
process restart, and API hot reload use the promoted config.

Required code areas:

- `internal/appinit`: default generated config path and runtime state paths.
- `internal/configrender`: output path defaults and render status.
- `internal/corerun`: runtime start/restart default config path.
- `internal/mcp`: tool defaults and restart/hot reload behavior.
- LuCI helper: subscription setup and runtime control paths.
- Docs/tests that still name `generated/mihomo.yaml`.

## Mihomo API MCP Tool

Add one generic Mihomo API proxy tool:

```text
mihomo_api_request
```

This is intentionally generic, but still bounded to the active local Mihomo
runtime controller.

Input schema:

```json
{
  "method": "GET",
  "path": "/proxies",
  "query": {
    "name": "value"
  },
  "body": {},
  "timeout_ms": 5000
}
```

Contract:

- `method` accepts `GET`, `POST`, `PUT`, `PATCH`, and `DELETE`.
- `path` must be an absolute API path beginning with `/`.
- Full URLs are rejected.
- Host, port, and bearer secret are resolved from localClash runtime state or
  runtime profile, not from user input.
- Requests always use a bounded timeout.
- Responses are size capped. Oversized responses fail explicitly or return a
  truncated flag only if the tool contract says truncation is allowed.
- Streaming endpoints such as `/logs` need an explicit bounded mode before they
  are enabled.

Safety level: `SafeWrite`. Even a generic `GET` capable tool can also call
state-changing Mihomo endpoints when the method/path changes.

Initial use cases:

- `GET /configs`
- `GET /proxies`
- `GET /rules`
- `GET /proxies/{name}/delay`
- `PUT /configs` for hot reload

`/logs` is not part of the first generic HTTP request contract because it is a
streaming endpoint. It needs a bounded log collection contract instead of a
normal request/response result.

## Mihomo Logs Streaming

Mihomo exposes `/logs` as a long-running stream. In Mihomo Meta, the same route
supports ordinary HTTP streaming and WebSocket upgrade. Browser WebSocket
clients cannot set custom authorization headers reliably, so Mihomo also
accepts the controller token in the WebSocket query string.

Example shape, with the token intentionally redacted:

```text
ws://100.122.122.35:9090/logs?token=<redacted>&level=info
```

localClash should not require users or agents to pass this full URL. The MCP
tool should resolve the controller endpoint and secret from runtime state, then
construct the WebSocket request internally.

Add a bounded logs tool:

```text
mihomo_logs_read
```

Input schema:

```json
{
  "level": "info",
  "format": "default",
  "transport": "websocket",
  "duration_ms": 3000,
  "max_lines": 200,
  "max_bytes": 131072
}
```

Contract:

- `level` accepts Mihomo log levels such as `debug`, `info`, `warning`,
  `error`, and `silent`, matching Mihomo's supported levels.
- `format` accepts `default` or `structured`.
- `transport` accepts `websocket` or `http_stream`. Default should be
  `websocket` for parity with zashboard/browser behavior.
- The tool always ends by `duration_ms`, `max_lines`, or `max_bytes`.
- The tool never returns or logs the Mihomo token.
- The input rejects full `ws://` or `http://` URLs. Endpoint and token are local
  runtime state, not caller input.
- If the controller secret is missing or the runtime is not running, fail
  explicitly.
- If the WebSocket handshake fails, do not silently fall back to HTTP streaming.
  The caller can retry with `transport=http_stream`.

Safety level: `SafeRead`. The tool observes runtime logs only and does not
change Mihomo state, but it can expose runtime metadata. Keep returned lines
bounded and avoid including secrets in test fixtures.

Implementation notes:

- A normal `net/http` client is enough for HTTP streaming mode.
- WebSocket mode can use a small dependency, or a minimal client implementation
  if dependency policy prefers avoiding another module. Do not shell out to
  `websocat` or `curl` in production code.
- The result should include `transport`, `level`, `format`, `line_count`,
  `byte_count`, `truncated`, and `elapsed_ms`.
- The collected log lines should be parsed as JSON objects when possible; raw
  lines can be returned only with an explicit parse error field.

## Mihomo Config Test Tool

Add an explicit config test tool:

```text
mihomo_config_test
```

This tool is the only MCP entry point that runs `mihomo -t`.

Input schema:

```json
{
  "config": ".runtime/mihomo/config.candidate.yaml",
  "runtime_dir": ".runtime/mihomo",
  "core": "",
  "record": true
}
```

Contract:

- Resolve `core`, `runtime_dir`, and `config` from active runtime profile when
  omitted.
- Run Mihomo validation against the exact config bytes being tested.
- Compute `config_sha256` from the tested config file.
- Return pass/fail, exit code, elapsed time, core path, runtime dir, config path,
  and a bounded stderr/stdout summary.
- When `record=true`, write an attestation file for the passed config.
- On failure, do not write a passing attestation.

Suggested attestation path:

```text
.runtime/mihomo/config-test-attestation.json
```

Suggested attestation payload:

```json
{
  "version": 1,
  "config": "/root/localclash/.runtime/mihomo/config.candidate.yaml",
  "promoted_config": "/root/localclash/.runtime/mihomo/config.yaml",
  "runtime_dir": "/root/localclash/.runtime/mihomo",
  "core": "/root/localclash/bin/mihomo-smart",
  "config_sha256": "hex",
  "passed": true,
  "tested_at": "2026-06-01T00:00:00Z"
}
```

Safety level: `SafeWrite` if `record=true` is the default, because it writes an
attestation file. The validation itself does not change runtime state.

Shared implementation should live outside MCP so both MCP and LuCI/core CLI can
use the same test and hash logic.

## Runtime Reload / Restart Changes

Upgrade `restart_runtime` with explicit strategies:

```json
{
  "strategy": "hot_reload",
  "config_sha256": "",
  "attestation": ".runtime/mihomo/config-test-attestation.json",
  "timeout_ms": 10000
}
```

Strategies:

- `hot_reload`: default. Verify hash, then call Mihomo API `PUT /configs`.
- `process_restart`: stop/start the Mihomo process.

`restart_runtime` must not run `mihomo -t`.

Hot reload contract:

1. Read the promoted config.
2. Compute current config SHA256.
3. Load the last passed attestation, or use an explicit `config_sha256` input.
4. Fail if the current config hash does not match the tested hash.
5. Fail if the runtime is not running.
6. Call Mihomo API `PUT /configs` with the promoted config path.
7. Re-query runtime/API status and report the applied strategy.

Failure examples:

```text
config hash mismatch: current config differs from last mihomo_config_test result
runtime is not running; hot_reload requires an active Mihomo controller
Mihomo API reload failed: config path is outside SAFE_PATHS
```

No implicit fallback:

- If hot reload fails, do not automatically process-restart.
- If the hash check fails, do not run `mihomo -t` inside `restart_runtime`.
- If the API rejects the config path, fail and report the rejected path.

## LuCI Save And Apply Subscription Flow

The LuCI helper currently runs:

```text
subscription set
subscription refresh
config render
```

It should become:

```text
subscription set
subscription refresh
config render --output .runtime/mihomo/config.candidate.yaml
mihomo_config_test --config .runtime/mihomo/config.candidate.yaml --record
promote candidate to .runtime/mihomo/config.yaml
if runtime running: restart_runtime --strategy hot_reload
if runtime not running: report config_ready
```

The helper should continue to provide task logs for each stage. If hot reload
fails, the UI should say that the subscription was refreshed and the config test
passed, but runtime reload failed. It must not present that as full success.

## Task Breakdown

1. Add a shared Mihomo API client.
   - Resolve controller URL and secret from runtime profile/status.
   - Reject full URLs and external hosts.
   - Add unit tests for path validation, auth header construction, timeout, and
     response handling.

2. Add `mihomo_api_request`.
   - Register MCP metadata and schema.
   - Mark as `SafeWrite`.
   - Add tests for safe path/method validation and JSON result shape.

3. Add `mihomo_logs_read`.
   - Implement bounded WebSocket log collection.
   - Add HTTP streaming mode as an explicit transport, not a fallback.
   - Add tests for duration, max lines, max bytes, token redaction, bad level,
     and handshake failure.

4. Add shared Mihomo config test implementation.
   - Implement config hashing and validation command execution.
   - Write passing attestation only after a successful test.
   - Add tests for pass, fail, missing config, hash stability, and no
     attestation-on-failure.

5. Add `mihomo_config_test` MCP and core CLI entry point.
   - MCP wraps the shared implementation.
   - CLI lets LuCI helper use the same capability without talking to MCP.
   - Add docs and tests for both surfaces.

6. Relocate runtime config output.
   - Update defaults from `generated/mihomo.yaml` to `.runtime/mihomo/config.yaml`.
   - Introduce candidate output for render/apply flows.
   - Update config status, runtime status, tests, and docs.

7. Upgrade `restart_runtime`.
   - Add strategy input with default `hot_reload`.
   - Remove embedded config-test behavior from restart path.
   - Verify hash before hot reload.
   - Add process restart strategy as explicit opt-in.

8. Update LuCI helper and UI messages.
   - Use candidate render, config test, promote, then hot reload when runtime is
     already running.
   - Preserve task logs and explicit partial-failure states.
   - Avoid logging raw subscription URIs or Mihomo secret values.

9. Verify on a router.
   - Save a subscription containing a known proxy URI node.
   - Confirm source artifact and promoted config contain the node.
   - Run `mihomo_config_test` and record the config hash.
   - Run hot reload through `restart_runtime`.
   - Query Mihomo API `/proxies` and delay-test the expected node.
   - Run bounded `mihomo_logs_read` with WebSocket transport and confirm it
     returns recent info-level log events without exposing the token.
   - Confirm no process restart occurred during hot reload.

## Open Decisions

- Whether `config render` should always render candidate first, or only in
  runtime-facing apply flows.
- Whether the attestation belongs under `.runtime/mihomo/` or a separate
  `.runtime/attestations/` directory.
- Whether `mihomo_logs_read` should ship in the same milestone as
  `mihomo_api_request`, or as the next milestone after hot reload.
- Whether LuCI should auto hot reload after every successful subscription apply,
  or expose a checkbox for "apply to running runtime".
