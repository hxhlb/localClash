# localClash

Local Mihomo runtime wrapper with an MCP management interface for AI-assisted
Clash, Mihomo, and OpenClash workflows.

## Direction

localClash is intended to run near the proxy runtime, such as on a local
machine, NAS, home server, or router. It is not only a remote management helper:
it also owns the local runtime lifecycle for Mihomo. MCP is the management
surface that lets AI agents observe, bootstrap, plan, and confirm operations
against that runtime.

The project is not an admin Web UI. Conversation with an AI agent is the main
management surface. zashboard can still be downloaded and served by Mihomo as a
runtime dashboard, but it is not localClash's configuration UI.

localClash should expose:

- A runtime entrypoint that can ensure prerequisites, render config, validate
  health, and start Mihomo.
- An MCP server as the primary agent management interface.
- CLI commands for bootstrap, debugging, and fallback operation.
- Deterministic renderers for rules, packs, virtual targets, and runtime
  Mihomo configs.
- Read-only diagnostics and runtime inspection for safe agent observation.
- Router adapters for OpenClash workflows, with write operations gated by
  explicit user confirmation.

## Main Bootstrap

Every localClash process builds a shared runtime bootstrap state before serving
CLI commands or MCP tools. This state owns common paths and preflight results
for the Mihomo core, subscription artifacts, rule sources, packs catalog,
generated config, and runtime directory.

Bootstrap failures are recorded as diagnostics instead of preventing MCP from
starting. This lets agents call `subscriptions_status`, `doctor`, or config
tools to explain and repair missing local state. The packs catalog is prepared
at bootstrap time, so `packs_list` and `packs_get` consume the shared catalog
instead of triggering their own rules adaptation or render workflow.

## Runtime Model

localClash's runtime path is the shortest path from local state to a running
Mihomo process:

```text
localclash run
-> ensure core
-> ensure subscription exists
-> ensure rules cache and pack catalog
-> render generated/mihomo.yaml
-> doctor, including Mihomo config validation
-> start Mihomo runtime
```

If no subscription has been configured, runtime startup should stop with a clear
bootstrap message instead of guessing. The user or agent should configure one or
more subscription sources first, then refresh the effective `subscription.yaml`.

Low-level operations such as rule source adaptation, rules fragment rendering,
and raw Mihomo config testing are implementation details of this runtime and
render pipeline. They may remain available as CLI/debug helpers, but they are
not the main MCP workflow for agents.

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
MCP client is not available. The main human path is either `localclash run` for
runtime startup, or conversation through an MCP-capable agent for management.

## MCP Server

Start the local MCP stdio server:

```bash
go run . mcp
```

The MCP server is the primary agent management interface. It exposes bootstrap,
inspection, planning, rendering, health-check, and confirmed runtime-start
tools. It should not expose every internal CLI/debug helper as a product-level
tool. Rules adaptation, rules fragment rendering, and raw Mihomo config testing
are internal pipeline or CLI/debug capabilities, not MCP product tools.

Tool safety levels are part of the tool metadata:

- `safe_read`: observation and diagnostics.
- `safe_write`: writes local generated artifacts or runs local validation.
- `confirm_required`: must not run without an explicit confirmation flow.
- `high_risk`: reserved for operations such as applying router config. The
  first product MCP surface currently exposes no high-risk tools.

The server marks `run_runtime` as `confirm_required`, and assumes the Agent SDK
or MCP client has completed confirmation before calling it. `switch_proxy_group`
and `apply_router_config` are not part of the minimal runtime loop. zashboard
remains Mihomo's runtime dashboard only, not localClash's configuration UI.

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

MCP subscription node inspection tools:

- `subscription_nodes_list`: list safe proxy `name` and `type` summaries from
  the effective subscription.
- `subscription_nodes_search`: search subscription proxy names by literal query
  or regular expression and return safe `name` and `type` summaries.

These tools do not verify network egress location. If a user asks for a region
such as Hong Kong, an agent should treat that as a proxy-name search, for
example matching `香港`, `HK`, or `🇭🇰`, and explain that the result is based only
on subscription proxy names.

MCP packs catalog tools:

- `packs_list`: list and filter adapter-generated pack cache entries from
  `.runtime/rules/packs/*.yaml`.
- `packs_get`: inspect one pack's target, provider summaries, and rule summary
  before enabling it in a selection file.

Pack cache generation is an internal ensure step of runtime startup and config
rendering. Agents should not normally need to call a separate rules adapter
tool.

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

MCP runtime tool:

- `run_runtime`: starts Mihomo from `generated/mihomo.yaml` in the background.

`run_runtime` is `confirm_required`. localClash does not implement an
interactive yes/no prompt inside the tool; the Agent SDK or MCP client must ask
the user for confirmation before calling it. Starting or restarting the proxy
runtime may temporarily interrupt network connectivity. The Agent itself may
depend on the current network or proxy path and could lose its connection after
this operation. `run_runtime` does not modify router/OpenClash config, does not
switch proxy groups, and does not modify system proxy settings.

Minimal MCP closed loop:

1. `subscriptions_refresh`
2. `config_render`
3. `doctor`
4. `run_runtime`

This is the MCP form of the runtime loop. `doctor` should be the health-check
entrypoint, including generated config validation, so agents do not need to call
a separate Mihomo config-test tool in the normal flow.

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
