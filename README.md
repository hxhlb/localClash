# localClash

Local Mihomo runtime wrapper with an MCP management interface for AI-assisted
Clash, Mihomo, and router workflows.

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
- Router adapters for OpenWrt workflows, with write operations gated by
  explicit user confirmation.

New users should start with [First Use](docs/first-use.md) for the shortest
path from a fresh checkout to a running local Mihomo runtime.

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
Clash/Mihomo artifacts with validation, diff preview, config tests, backups,
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

Deploy the router MCP server:

```bash
scripts/deploy-router.sh --host root@192.168.6.1
```

The script builds `bin/linux-arm64/localclash`, installs it to the router at
`/usr/local/bin/localclash`, installs `/usr/bin/localclash` as a wrapper that
enters `/root/localclash`, installs the OpenWrt procd service
`/etc/init.d/localclash-mcp`, and runs MCP from the same isolated working
directory by default. On first deployment to that directory it copies existing
localClash files from `/root` when the target file is missing, installs missing
base assets from `policies/` and `rule-sources/` without overwriting existing
files, and restarts the MCP HTTP server on `http://192.168.6.1:8765/mcp`. After
deployment it follows the router MCP log with `tail -f` until interrupted with
`Ctrl+C`; use `--no-tail` for non-interactive automation.

Tool safety levels are part of the tool metadata:

- `safe_read`: observation and diagnostics.
- `safe_write`: writes local generated artifacts or runs local validation.
- `confirm_required`: must not run without an explicit confirmation flow.
- `high_risk`: reserved for operations such as applying router config. The
  first product MCP surface currently exposes no high-risk tools.

MCP environment tool:

- `environment_inspect`: inspect host, network evidence, localClash state, and
  local runtime readiness without exposing credentials.

This tool reports observed facts and capabilities, not device identity. It does
not return an `is_router` boolean. Agents should reason from evidence such as
service manager, interfaces, routes, DNS/DHCP services, firewall backends,
and localClash files. Subscription URLs, proxy server addresses, passwords,
UUIDs, WAN credentials, and private keys are redacted or omitted.

The server marks `run_runtime` as `confirm_required`, and assumes the Agent SDK
or MCP client has completed confirmation before calling it. Router traffic
takeover is a separate confirmed step from starting Mihomo. zashboard remains
Mihomo's runtime dashboard only, not localClash's configuration UI.

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

MCP config model:

- `localclash.yaml` is the source of truth.
- `generated/mihomo.yaml` is a build artifact.
- `.runtime/patches/<patch-id>/` contains review artifacts.

MCP config tools:

- `config_status`: inspect source-of-truth state, generated config presence,
  render readiness, generated summaries, overlay metadata, and pending patches.
- `config_render`: rebuild `generated/mihomo.yaml` from the current durable
  `localclash.yaml`, subscription, policy, and runtime profile. If
  `localclash.yaml` does not exist, it renders the base config without an
  overlay. It ignores patches and does not start Mihomo.
- `config_patch_create`: create a reviewable patch with candidate
  `localclash.yaml` and `mihomo.yaml` under `.runtime/patches/<patch-id>/`.
- `config_patch_apply`: apply a reviewed patch by writing `localclash.yaml`,
  deriving `localclash-packs.yaml`, and regenerating `generated/mihomo.yaml`.

Config render writes `x-localclash` metadata into generated configs so agents
can distinguish immutable base config from localClash-managed overlay config.

MCP runtime profile tools:

- `runtime_profile_status`: inspect the active mode, core, core path, and safe
  Mihomo summary.
- `runtime_profile_configure`: switch `mode` (`normal` or `router`) and/or
  `core` (`meta` or `smart`) in `localclash-runtime.yaml`, then rerender
  `generated/mihomo.yaml` when the effective subscription is available. It does
  not start or restart Mihomo and does not edit profile contents.

`normal` is the standalone local proxy profile and matches the original generated
Mihomo shell. `router` is a transparent-proxy profile based on the local
router redir-host-mix reference. Profile contents are ordinary YAML files under
`profiles/`: `normal.yaml` and `router.yaml` are user-owned, while
`normal.default.yaml` and `router.default.yaml` are templates. On first use,
localClash writes the `.default.yaml` files and copies missing user profiles from
them. Advanced users can edit `profiles/*.yaml` directly, or ask an agent to use
`nl_file` and `sed_file` for explicit line-based edits. The product MCP path is
still profile switching, not granular DNS/TUN patching.

MCP patch-building tools:

- `proxy_group_build`: build and validate a reusable proxy group target from a
  `name_regex` selector or exact `nodes`. This tool does not persist state; copy
  the returned proxy group into `config_patch_create.overlay.proxy_groups` when a
  patch should use it.
- `custom_rules_build`: build and validate user rules such as domains, domain
  suffixes, or CIDRs that share one target.
- `rule_provider_build`: build and validate a user-supplied external Mihomo
  rule-provider, such as `US-Proxy` from a raw GitHub URL, before adding it to
  `config_patch_create.overlay.rule_providers`.
- `config_patch_create`: accepts proxy groups, third-party packs, custom rules,
  and external rule-providers, then renders candidate `localclash.yaml`, derived
  `localclash-packs.yaml`, and `mihomo.yaml` into `.runtime/patches/<patch-id>/`.
  MCP `arguments` must be a JSON object, not a JSON-encoded string. If a pack or
  custom rule or external provider targets a new proxy group, include that group in
  `overlay.proxy_groups` in the same call.
- `config_patch_apply`: applies a reviewed patch by writing durable
  `localclash.yaml`, deriving `localclash-packs.yaml`, and regenerating
  `generated/mihomo.yaml`.

For pack routing such as "Steam through HK", an agent should first call
`config_status` to discover reusable proxy groups and current durable state,
then call `subscription_nodes_search`, build or reuse the target with
`proxy_group_build`, inspect the pack with `packs_list` or `packs_get`, and call
`config_patch_create` with the desired `proxy_groups` and `packs`. For domain
routing such as "huggingface.co through temporary line", inspect status,
search/build or reuse the proxy group, call `custom_rules_build`, then create a
patch with desired `proxy_groups` and `custom_rules`. For built-in targets such
as "xxx direct", skip proxy group creation and build custom rules with target
`DIRECT`. For external provider routing such as adding a raw `US-Proxy`
rule-provider URL, call `rule_provider_build`, then create a patch with desired
`rule_providers`; do not edit `generated/mihomo.yaml` directly.

Patch creation does not overwrite active generated files, start or restart
Mihomo, or apply router system changes. After user review,
`config_patch_apply` resolves selectors against the current subscription, backs
up replaced local artifacts, writes `localclash.yaml`, derives
`localclash-packs.yaml`, and regenerates `generated/mihomo.yaml`. It still does
not start or restart Mihomo; use `run_runtime` for that confirmed step. If an
agent wants to preserve existing local state, it must first call `config_status`
and submit the full desired config, including retained packs, custom rules,
external rule-providers, and proxy groups.
The normal reviewed-change loop is:
`config_status` → `config_patch_create` → `config_patch_apply` →
`config_status`.

MCP runtime tool:

- `run_runtime`: starts Mihomo from `generated/mihomo.yaml` in the background.
  If the effective subscription exists but the generated config is missing,
  localClash renders `generated/mihomo.yaml` before starting runtime.
- `restart_runtime`: validates/renders config, stops the recorded Mihomo
  process if needed, and starts it again in one confirmed call. Use this when
  Mihomo is already running and the agent may lose connectivity between a
  separate `stop_runtime` and `run_runtime`.
- `stop_runtime`: stops Mihomo only when it is not still required by active
  router takeover. If `router_takeover_status.effective` is true, call
  `router_takeover_stop` first, or pass `force: true` only after explicit user
  confirmation.

`run_runtime` and `restart_runtime` are `confirm_required`. localClash does not
implement an interactive yes/no prompt inside the tool; the Agent SDK or MCP
client must ask the user for confirmation before calling it. Starting or
restarting the proxy runtime may temporarily interrupt network connectivity.
The Agent itself may
depend on the current network or proxy path and could lose its connection after
this operation. These tools do not install router takeover rules, switch proxy
groups, or modify system proxy settings.

Router profile takeover tools:

- `router_takeover_status`: inspect localClash-owned OpenWrt takeover runtime
  state.
- `router_takeover_apply`: after `run_runtime`, install localClash-owned
  Redir-Host Mix runtime rules: TCP redir-host, DNS hijack, fwmark route, and
  TUN forwarding. This must not write persistent firewall configuration.
- `router_takeover_stop`: remove localClash-owned takeover rules without
  stopping Mihomo.

These tools are for `router` profile mode. In `normal` mode, agents should use
only `config_render` and `run_runtime`; `router_takeover_apply` will refuse to
apply until the runtime profile is switched to `router`. Router takeover rules
are runtime state; reboot clears them, and `router_takeover_stop` removes the
localClash-owned rules explicitly.

Minimal MCP closed loop:

1. `subscriptions_refresh`
2. `config_status`
3. `config_render` if `generated/mihomo.yaml` is missing or stale
4. `run_runtime`, or `restart_runtime` if Mihomo is already running
5. `runtime_status`

Router MCP closed loop:

1. `runtime_profile_configure` with `mode: router`
2. `config_render`
3. `run_runtime`, or `restart_runtime` if Mihomo is already running
4. `router_takeover_apply`
5. `router_takeover_status`

This is the MCP form of the runtime loop. `doctor` remains the broader
health-check entrypoint, including generated config validation. Agents should use
`config_patch_create` and `config_patch_apply` for reviewed routing changes, and
`config_render` for plain rebuilds of the generated Mihomo config.

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

Do not commit downloaded subscriptions, user-edited `profiles/*.yaml`, generated
configs, `localclash.yaml`, `localclash-packs.yaml`, or files containing node
credentials. Committed `.default.yaml` profile templates are source files.

## Core Download

Download the current host Mihomo Meta core:

```bash
go run . core download
```

By default the command targets the current host and downloads only the host
`meta` core from `MetaCubeX/mihomo`, for example
`bin/darwin-arm64/mihomo-meta` on macOS arm64. It does not silently download a
Linux Smart core on macOS.

To inspect the selected release asset without downloading:

```bash
go run . core download --dry-run
```

To download router cores, make the router target explicit. This downloads Linux
`meta` and `smart` cores for the requested architecture:

```bash
go run . core download --target router --arch arm64 --force
```

To download one exact flavor or custom output path:

```bash
go run . core download --target router --flavor smart --arch arm64 --output bin/linux-arm64/mihomo-smart
```

## Subscription Download

Download a subscription with a Clash-compatible User-Agent:

```bash
go run . subscription download --url "https://example.com/playlist?token=..." --output subscription.yaml --force
```

The default User-Agent is `clash-verge/v1.5.1`. The downloaded subscription file is local data and should not be committed.

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
proxy groups locally. It also applies the active mode from
`localclash-runtime.yaml`, which points to editable profile YAML files under
`profiles/`; use `go run . config render --runtime-profile <path> --force` to
test an alternate runtime selector. For MCP-managed routing changes, prefer
`config_patch_create` followed by `config_patch_apply`; for plain MCP rebuilds,
use `config_render`.

The rule model is documented in `docs/rule-model.md`. In short, localClash
renders a fixed local safety baseline first, then user overrides, optional rule
packs, the selected base routing preset, and finally fallback. Loyalsoldier is
the default base routing preset, not an optional rule pack.

Test the generated config:

```bash
./bin/darwin-arm64/mihomo-meta -d .runtime/mihomo -f generated/mihomo.yaml -t
```

Run the generated config:

```bash
go run . run
```

By default this is equivalent to:

```bash
./bin/darwin-arm64/mihomo-meta -d .runtime/mihomo -f generated/mihomo.yaml
```

Mihomo output is also appended to a dated log file under `.runtime/mihomo/logs/`, for example `.runtime/mihomo/logs/mihomo-2026-05-15.log`. Override the path with `--log`. Dated logs are retained for 7 days by default; use `--log-retention` to change this.

Check or stop the background runtime started through MCP:

```bash
go run . status
go run . stop
go run . restart
```

`status` reads `.runtime/mihomo/mihomo.pid` and reports the generated config,
log file, external controller, and dashboard URL when available. Use
`go run . status --json` for scripts. `stop` sends SIGTERM to the recorded
process and removes stale PID files; use `--force` to send SIGKILL if the
runtime does not stop before `--timeout`. `restart` validates the generated
config before stopping the old process, then starts a new background runtime.
The MCP `stop_runtime` tool adds an Agent safety guard: it refuses to stop
Mihomo while localClash router takeover is effective unless `force: true` is
explicitly supplied.

## Factory Reset

Reset removes local runtime state and user configuration, returning localClash to
an installed-but-unconfigured state:

```bash
go run . reset
```

The command deletes `.runtime/`, `generated/`, `profiles/`, `subscription*.yaml`,
`localclash.yaml`, `localclash-packs.yaml`, `localclash-subscriptions.yaml`, and
`localclash-runtime.yaml`. It keeps downloaded binaries in `bin/`, built-in
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

## License

localClash is released under the MIT License. See [LICENSE](LICENSE).
