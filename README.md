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
- Deterministic renderers for rules, packs, proxy groups, and runtime
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

Start the local MCP HTTP server:

```bash
go run . mcp
```

By default it listens on `http://127.0.0.1:8765/mcp`, with a health endpoint at
`http://127.0.0.1:8765/health`. Override the bind address or path when needed:

```bash
go run . mcp --addr 127.0.0.1:8766 --path /mcp
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

MCP environment tool:

- `environment_inspect`: inspect host, network evidence, localClash state, and
  OpenClash state without exposing credentials.

This tool reports observed facts and capabilities, not device identity. It does
not return an `is_router` boolean. Agents should reason from evidence such as
service manager, interfaces, routes, DNS/DHCP services, firewall backends,
localClash files, and OpenClash files. Subscription URLs, proxy server
addresses, passwords, UUIDs, WAN credentials, and private keys are redacted or
omitted.

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
  `subscription.yaml`. It also returns proxy-node diffs and, when
  `localclash.yaml` exists, reevaluates saved selectors against the refreshed
  node list.

From a clean setup, an agent should call `subscriptions_status` first. If no
sources are configured, it should ask the user for one or more subscription
URLs, call `subscriptions_configure`, then call `subscriptions_refresh`.
`subscription.yaml` is the merged output used by the render pipeline, not the
only source of truth. `localclash-subscriptions.yaml` contains sensitive
subscription URLs and must not be committed. If a saved selector in
`localclash.yaml` still matches after refresh, localClash updates the selected
nodes, derives `localclash-packs.yaml`, and regenerates `generated/mihomo.yaml`.
If exact `nodes` were selected and one of those nodes disappears, the tool
reports `state: stale_exact_nodes` with `missing_nodes` and leaves the active
generated config unchanged. New nodes are only reported in `node_diff.added`;
they do not trigger repair. If a regex selector no longer matches its minimum
requirements, the tool reports that replanning is required and leaves the active
generated config unchanged.

MCP subscription node inspection tools:

- `subscription_nodes_list`: list safe proxy `name` and `type` summaries from
  the effective subscription.
- `subscription_nodes_search`: search subscription proxy names by literal query
  or regular expression and return safe `name` and `type` summaries plus a
  selector suggestion suitable for `proxy_group_build`.

These tools do not verify network egress location. If a user asks for a region
such as Hong Kong, an agent should treat that as a proxy-name search, for
example matching `香港`, `HK`, or `🇭🇰`, and explain that the result is based only
on subscription proxy names.

MCP packs catalog tools:

- `packs_list`: list and filter adapter-generated pack cache entries from
  `.runtime/rules/packs/*.yaml`.
- `packs_get`: inspect one pack's target, provider summaries, and rule summary
  before enabling it in a selection file.
- `pack_rules_read`: read provider rules for a known pack id, downloading only
  that pack's missing provider-cache entries.
- `pack_rules_prefetch`: download provider rules for selected candidate packs
  into `.runtime/rules/provider-cache/`.
- `pack_rules_query`: search locally cached provider rules for a domain or
  keyword. It does not download provider rules; if cache coverage is incomplete,
  prefetch candidate packs first.

Pack cache generation is an internal ensure step of runtime startup and config
rendering. Agents should not normally need to call a separate rules adapter
tool. Provider rule content is not downloaded for every pack at startup. For a
question such as "does huggingface.co have a pack?", use `pack_rules_query`;
if it reports incomplete cache coverage, use `packs_list` to find semantic
candidates such as `ai`, call `pack_rules_prefetch`, then query again. For a
question such as "what does sukkaw_ai cover?", call `pack_rules_read` directly.
Ronnie's app maintenance packs are exposed as `syncnext_SyncnextProxy` and
`syncnext_SyncnextUnbreak`.

MCP config inspection tools:

- `config_base_inspect`: inspect the generated base config summary. The base
  layer is not modifiable through MCP plan tools.
- `config_intent_inspect`: inspect the durable `localclash.yaml` routing intent,
  including reusable proxy groups, custom rules, and packs. Agents should call
  this before draft rendering when they need to preserve or reuse existing
  localClash intent.
- `config_overlay_inspect`: inspect the localClash-managed overlay from
  `x-localclash.overlay` metadata.

Config render writes `x-localclash` metadata into generated configs so agents
can distinguish immutable base config from future replaceable overlay config.

MCP runtime preset tools:

- `runtime_preset_status`: inspect the active Mihomo preset and a safe summary.
- `runtime_preset_configure`: switch the active preset in `mihomo-preset.yaml`
  to `normal` or `router`, then rerender `generated/mihomo.yaml` when the
  effective subscription is available. It does not start or restart Mihomo and
  does not expose individual DNS, TUN, or firewall switches.

`normal` is the standalone local proxy preset and matches the original generated
Mihomo shell. `router` is a transparent-proxy preset based on the local
OpenClash redir-host-mix reference. Advanced users can edit
`mihomo-preset.yaml` directly, or ask an agent to use `nl_file` and `sed_file`
for explicit line-based edits, but the product MCP path is preset switching.

MCP draft-building tools:

- `proxy_group_build`: build and validate a reusable proxy group target from a
  `name_regex` selector or exact `nodes`. This tool does not persist state; copy
  the returned proxy group into `config_draft_render.overlay.proxy_groups` when a
  draft should use it.
- `custom_rules_build`: build and validate user rules such as domains, domain
  suffixes, or CIDRs that share one target.
- `config_draft_render`: accepts proxy groups, third-party packs, and custom
  rules, then renders candidate `localclash.yaml`, derived
  `localclash-packs.yaml`, and `mihomo.yaml` into `.runtime/drafts/<draft-id>/`.
  MCP `arguments` must be a JSON object, not a JSON-encoded string. If a pack or
  custom rule targets a new proxy group, include that group in
  `overlay.proxy_groups` in the same call.
- `config_draft_apply`: applies a reviewed draft by writing durable
  `localclash.yaml`, deriving `localclash-packs.yaml`, and regenerating
  `generated/mihomo.yaml`. After a successful apply, call
  `config_intent_inspect` to verify the durable proxy groups, custom rules, and
  packs that remain active.

For pack routing such as "Steam through HK", an agent should first call
`config_intent_inspect` to discover reusable proxy groups and existing intent,
then call `subscription_nodes_search`, build or reuse the target with
`proxy_group_build`, inspect the pack with `packs_list` or `packs_get`, and call
`config_draft_render` with the preserved `proxy_groups` and `packs`. For domain
routing such as "huggingface.co through temporary line", inspect intent,
search/build or reuse the proxy group, call `custom_rules_build`, then render a
draft with preserved `proxy_groups` and `custom_rules`. For built-in targets such
as "xxx direct", skip proxy group creation and build custom rules with target
`DIRECT`.

Draft rendering does not overwrite active generated files, start or restart
Mihomo, or apply router/OpenClash changes. After user review,
`config_draft_apply` resolves selectors against the current subscription, backs
up replaced local artifacts, writes `localclash.yaml`, derives
`localclash-packs.yaml`, and regenerates `generated/mihomo.yaml`. It still does
not start or restart Mihomo; use `run_runtime` for that confirmed step. If an
agent wants to preserve existing local intent, it must first inspect current
local state and submit the full desired config, including retained packs,
custom rules, and proxy groups.
The normal reviewed-change loop is:
`config_intent_inspect` → `config_draft_render` → `config_draft_apply` →
`config_intent_inspect`.

MCP runtime tool:

- `run_runtime`: starts Mihomo from `generated/mihomo.yaml` in the background.
  If the effective subscription exists but the generated config is missing,
  localClash renders `generated/mihomo.yaml` before starting runtime.

`run_runtime` is `confirm_required`. localClash does not implement an
interactive yes/no prompt inside the tool; the Agent SDK or MCP client must ask
the user for confirmation before calling it. Starting or restarting the proxy
runtime may temporarily interrupt network connectivity. The Agent itself may
depend on the current network or proxy path and could lose its connection after
this operation. `run_runtime` does not modify router/OpenClash config, does not
switch proxy groups, and does not modify system proxy settings.

Minimal MCP closed loop:

1. `subscriptions_refresh`
2. `run_runtime`
3. `runtime_status`

This is the MCP form of the runtime loop. `doctor` remains the broader
health-check entrypoint, including generated config validation. Agents should use
`config_draft_render` and `config_draft_apply` for reviewed routing changes; raw
`config_render` is a CLI/internal debug capability, not part of the product MCP
surface.

For a local HTTP MCP smoke test, run:

```bash
scripts/test-mcp-http.sh
```

The script starts `go run . mcp`, posts a JSON-RPC `doctor` tool call to the
HTTP endpoint, and expects a response containing `"status":"ok"`.

For a third-party MCP client compatibility smoke test, run:

```bash
scripts/test-mcp-cli.sh
```

This starts a test MCP HTTP listener on `127.0.0.1:18765`, generates a temporary
`mcp-cli` `server_config.json`, verifies `mcp-cli ping`, checks that tool
discovery includes the core localClash tools, and executes `doctor` through
`mcp-cli interactive` with `execute doctor {}`.

To point `mcp-cli` at an already running server, use the checked-in fixture:

```bash
uvx mcp-cli tools \
  --config-file scripts/fixtures/mcp-cli/server_config.json \
  --server localclash \
  --raw
```

For Open WebUI debugging, the helper script can run a logging proxy in front of
the localClash MCP HTTP server:

```bash
python3 scripts/localclash_mcp_openwebui.py serve
```

The public endpoint stays `http://127.0.0.1:8765/mcp`, while the localClash MCP
binary listens on an internal loopback port. The proxy prints each MCP request
and response body to the terminal and appends structured JSONL events to
`.runtime/logs/localclash-mcp-openwebui.jsonl`. Use `--log-file <path>` to
write elsewhere, or `--no-log-redaction` when raw MCP bodies are required for a
local-only diagnosis.

## Local Data

Do not commit downloaded subscriptions, active router profiles, generated
configs, `localclash.yaml`, `localclash-packs.yaml`, or files containing node
credentials.

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

The default render path is `generated/mihomo.yaml`. The renderer treats the
subscription as a proxy source and owns the runtime rules, rule providers, and
proxy groups locally. It also applies the active `mihomo-preset.yaml` runtime
preset; use `go run . config render --preset <path> --force` to test an
alternate preset file. For MCP-managed routing changes, prefer
`config_draft_render` followed by `config_draft_apply`; direct `config render` is
primarily a CLI/debug helper.

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

## Factory Reset

Reset removes local runtime state and user configuration, returning localClash to
an installed-but-unconfigured state:

```bash
go run . reset
```

The command deletes `.runtime/`, `generated/`, `subscription*.yaml`,
`localclash.yaml`, `localclash-packs.yaml`, `localclash-subscriptions.yaml`, and
`mihomo-preset.yaml`. It keeps downloaded binaries in `bin/`, built-in
policies, rule sources, source code, docs, and scripts. By default it prints the
delete plan and requires typing `reset localclash`; use `--dry-run` to inspect
the plan only or `--yes` for non-interactive SSH/script usage. If Mihomo is
running, stop it before resetting.

## Doctor

Run a read-only diagnostic report for the local core, subscription, generated config, policy, dashboard, rule references, and Mihomo config test:

```bash
go run . doctor
```

Machine-readable output for MCP tools and agent workflows:

```bash
go run . doctor --json
```
