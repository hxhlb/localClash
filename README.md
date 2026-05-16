# localClash

Local MCP service for AI-assisted Clash, Mihomo, and OpenClash management.

## Direction

localClash is intended to run near the proxy runtime, such as on a local
machine, NAS, home server, or router. Its primary interface is MCP: AI agents
talk to localClash, localClash observes and manages the local Clash/Mihomo or
OpenClash environment.

The project is not an admin Web UI. Conversation with an AI agent is the main
management surface. zashboard can still be downloaded and served by Mihomo as a
runtime dashboard, but it is not localClash's configuration UI.

localClash should expose:

- An MCP server as the primary agent interface.
- CLI commands for bootstrap, debugging, and fallback operation.
- Deterministic renderers for rules, packs, virtual targets, and runtime
  Mihomo configs.
- Read-only diagnostics and runtime inspection for safe agent observation.
- Router adapters for OpenClash workflows, with write operations gated by
  explicit user confirmation.

## Safety Boundary

AI agents should produce policy intent, plans, and reviewed changes, not edit
active Clash YAML directly. localClash should turn reviewed intent into
Clash/OpenClash artifacts with validation, diff preview, config tests, backups,
and rollback support.

Safe operations include inspection, diagnosis, rendering into generated files,
and configuration tests. Risky operations such as restarting a runtime, changing
live proxy groups, overwriting local selection files, or applying router
configuration must be explicit and auditable.

## Interaction Model

The intended flow is:

```text
user asks an AI agent
-> agent calls localClash MCP tools
-> localClash observes local runtime/config state
-> agent proposes a plan and diff
-> user confirms
-> localClash renders/tests/applies the approved change
```

CLI commands remain useful for local development and for environments where an
MCP client is not available, but they are not the primary product interface.

## MCP Server

Start the local MCP stdio server:

```bash
go run . mcp
```

The MCP server is the primary agent interface. It currently exposes read-only
diagnostic and inspection tools, safe generated-config render/test tools, and
metadata for future confirm-required or high-risk operations.

Tool safety levels are part of the tool metadata:

- `safe_read`: observation and diagnostics.
- `safe_write`: writes local generated artifacts or runs local validation.
- `confirm_required`: must not run without an explicit confirmation flow.
- `high_risk`: reserved for operations such as applying router config.

The initial server deliberately does not execute `run_runtime`,
`switch_proxy_group`, or `apply_router_config`; calls to those tools return a
confirmation-required not-implemented error. zashboard remains Mihomo's runtime
dashboard only, not localClash's configuration UI.

MCP subscription bootstrap tools:

- `subscriptions_status`: inspect whether subscription sources are configured,
  whether per-source runtime artifacts exist, and whether the merged effective
  `subscription.yaml` exists.
- `subscriptions_configure`: save one or more subscription sources into
  `localclash-subscriptions.yaml` without refreshing them.
- `subscriptions_refresh`: refresh configured sources, validate and normalize
  them, write `.runtime/subscriptions/<id>.yaml`, and merge the effective
  `subscription.yaml`.

From a clean setup, an agent should call `subscriptions_status` first. If no
sources are configured, it should ask the user for one or more subscription
URLs, call `subscriptions_configure`, then call `subscriptions_refresh`.
`subscription.yaml` is the merged output used by the existing render pipeline,
not the only source of truth. `localclash-subscriptions.yaml` contains sensitive
subscription URLs and must not be committed.

MCP packs catalog tools:

- `packs_list`: list and filter adapter-generated pack cache entries from
  `.runtime/rules/packs/*.yaml`.
- `packs_get`: inspect one pack's target, provider summaries, and rule summary
  before enabling it in a selection file.

If the pack cache does not exist yet, run `rules_adapt` first.

MCP virtual nodes tools:

- `virtual_nodes_list`: list node-label candidate sets inferred from
  `subscription.yaml` proxy names using selection YAML regexes.
- `virtual_nodes_get`: inspect one node-label candidate set and its safe
  candidate node summaries.

Virtual nodes are localClash compile-time observations only. They are based only
on provider/node names, are not verified GEO regions, and do not use IP lookup,
egress probing, capability probing, or runtime proxy-group creation.

MCP config inspection tools:

- `config_base_inspect`: inspect the generated base config summary. The base
  layer is not modifiable through MCP plan tools.
- `config_overlay_inspect`: inspect the localClash-managed overlay from
  `x-localclash.overlay` metadata.

Config render writes `x-localclash` metadata into generated configs so agents
can distinguish immutable base config from future replaceable overlay config.

MCP config plan tool:

- `config_plan_render`: accepts a complete desired overlay and renders a
  candidate Mihomo config into `.runtime/plans/<plan-id>/`.

The plan renderer writes `mihomo.yaml` and `summary.json` under the plan
directory. It does not overwrite `generated/mihomo.yaml`, does not modify
`localclash-packs.yaml`, does not start or restart Mihomo, and does not apply
router/OpenClash changes. If an agent wants to preserve an existing overlay, it
must first call `config_overlay_inspect` and submit the full desired overlay,
including the retained packs and virtual targets.

For a real MCP client smoke test, use the local `callCopilot` wrapper after the
`localclash` server is registered in the Copilot user MCP config
(`~/.copilot/mcp-config.json`). This is the fixed end-to-end MCP test target for
localClash:

```bash
scripts/test-mcp-callcopilot.sh
```

The script uses `/Volumes/Data/Github/callCopilot/bin/callCopilot` by default
and runs the `ds` model alias. It starts a Copilot CLI session with the user
configured localClash MCP server enabled, calls the `doctor` tool, and expects
`LOCALCLASH_MCP_OK`.

## Local Data

Do not commit downloaded subscriptions, active router profiles, generated configs, or files containing node credentials.

## Core Download

Download the matching Mihomo core for the machine running the command:

```bash
go run . core download
```

By default the command detects the current OS and CPU architecture, selects the matching `MetaCubeX/mihomo` release asset, decompresses it, and writes the binary to `bin/mihomo` or `bin/mihomo.exe`.

To inspect the selected release asset without downloading:

```bash
go run . core download --dry-run
```

To download a core for another target, pass `--os` and `--arch` explicitly:

```bash
go run . core download --os linux --arch arm64 --output bin/clash_meta
```

## Subscription Download

Download a subscription with a Clash-compatible User-Agent:

```bash
go run . subscription download --url "https://example.com/playlist?token=..." --output subscription.yaml --force
```

The default User-Agent is `clash-verge/v1.5.1`, matching the known OpenClash subscription setting. The downloaded subscription file is local data and should not be committed.

## Dashboard

Download the zashboard static UI for Mihomo runtime inspection:

```bash
go run . dashboard download --force
```

The command downloads the default `dist.zip` release asset. The default output is `.runtime/mihomo/ui/zashboard`. Rendered configs set `external-ui: ui/zashboard`, so after `go run . run` the dashboard is available at:

```text
http://127.0.0.1:9090/ui
```

zashboard is useful for viewing Mihomo runtime state and switching groups, but
localClash configuration management is expected to happen through MCP-backed
agent conversation.

## Config Render

Render a runtime Mihomo config from a downloaded subscription source and a local policy:

```bash
go run . config render --force
```

The default render path is `generated/mihomo.yaml`. The renderer treats the subscription as a proxy source and owns the runtime rules, rule providers, and proxy groups locally.

The rule model is documented in `docs/rule-model.md`. In short, localClash
renders a fixed local safety baseline first, then user overrides, optional rule
packs, the selected base routing preset, and finally fallback. Loyalsoldier is
the default base routing preset, not an optional rule pack.

Test the generated config:

```bash
./bin/mihomo -d .runtime/mihomo -f generated/mihomo.yaml -t
```

Run the generated config:

```bash
go run . run
```

By default this is equivalent to:

```bash
./bin/mihomo -d .runtime/mihomo -f generated/mihomo.yaml
```

Mihomo output is also appended to a dated log file under `.runtime/mihomo/logs/`, for example `.runtime/mihomo/logs/mihomo-2026-05-15.log`. Override the path with `--log`. Dated logs are retained for 7 days by default; use `--log-retention` to change this.

## Doctor

Run a read-only diagnostic report for the local core, subscription, generated config, policy, dashboard, rule references, and Mihomo config test:

```bash
go run . doctor
```

Machine-readable output for MCP tools and agent workflows:

```bash
go run . doctor --json
```
