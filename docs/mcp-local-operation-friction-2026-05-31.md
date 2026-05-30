# MCP Local Operation Friction Report - 2026-05-31

This report records one real localClash MCP maintenance session against the
router endpoint `http://192.168.6.1:8765/mcp`. The goal was small from the
user's point of view: remove the special proxy route for `194.221.250.50`, load
the new runtime config, and restore router network takeover after restart.

The session is useful because it exposed several product-level MCP gaps: delete
operations are not first-class, review diffs are too noisy, restart does not
preserve router takeover, and repair guidance still requires too much agent
coordination.

## Scope And Safety

Target:

- MCP server: `localclash`
- Endpoint: `http://192.168.6.1:8765/mcp`
- Host: OpenWrt router
- Runtime profile: `router`
- Runtime process: `lc-mihomo-smart`

Safety boundary:

- `router_takeover_apply` and `restart_runtime` are `confirm_required`.
- The user explicitly requested `restart_runtime`.
- The user explicitly said network takeover had failed and asked to reapply it.
- No persistent OpenWrt firewall configuration was written; router takeover is
  runtime-only state.

## Operation Timeline

### 1. Identify The Active MCP Target

The local default endpoint was not running:

- `http://127.0.0.1:8765/health` failed to connect.

The router endpoint was active:

- `http://192.168.6.1:8765/health` returned `{"status":"ok"}`.
- MCP `initialize` returned `serverInfo.name=localclash`.
- `tools_list` reported 33 MCP tools.
- `environment_inspect` reported OpenWrt, `procd`, work dir
  `/root/localclash`, and configured localClash state.

Friction:

- The agent had to probe multiple possible endpoints before operating.
- The repo documents both local and router endpoints, but there is no single
  "current target" discovery command for the user's live setup.

Optimization:

- Add a small target discovery helper or documented MCP client profile that can
  say "this is the active router MCP endpoint" before tool calls begin.

### 2. Locate The Route For `194.221.250.50`

`routing_explain` found one active custom route:

```text
custom_rule:ip-194.221.250.50-proxy -> ⚡ 自动选择
```

`config_status` with `detail=true` and `resolve=true` confirmed the durable
intent contained this custom rule:

```json
{
  "id": "ip-194.221.250.50-proxy",
  "target": "⚡ 自动选择",
  "reason": "User requested proxy routing for 194.221.250.50",
  "rules": [
    {
      "type": "ip_cidr",
      "value": "194.221.250.50/32"
    }
  ]
}
```

Friction:

- `config_status detail=true resolve=true` returned a large payload when the
  agent only needed one custom rule.
- `routing_explain` clearly identified the route, but it could only give patch
  guidance for changing the route, not a deletion-ready plan.

Optimization:

- Add a focused route lookup output that includes the exact durable object path,
  for example `custom_rules[id=ip-194.221.250.50-proxy]`.
- Add a small `config_rule_get` or `routing_explain` field with copyable remove
  arguments.

### 3. Remove The Custom Rule

The expected patch-first route was not sufficient:

- `config_patch_create` layers an overlay on top of
  `localclash-intent.json`.
- Overlay lists use merge/upsert semantics by stable ID.
- There is no delete operation for custom rules.

Because of that, the agent used repository-local MCP file tools:

1. `nl_file` read `localclash-intent.json` and returned line numbers plus a
   SHA-256 guard.
2. The custom rule was located at lines 758-768.
3. `sed_file` removed the exact object block and repaired the previous JSON
   comma.
4. `config_render` regenerated `/root/localclash/generated/mihomo.yaml`.

The resulting durable custom rules only contained:

```json
{
  "id": "telegram-geoip",
  "target": "💬 通信服务"
}
```

Verification:

- `routing_explain` for `194.221.250.50` returned `matches: null` and
  `active_routes: null`.
- `config_render` completed with `RuleCount=48`.

Friction:

- Removing one route required falling out of the normal patch workflow.
- The agent had to perform JSON-aware reasoning manually through a text edit.
- `sed_file` returned an oversized whole-file diff, making review harder than
  the actual change.
- A line-oriented delete was not enough because JSON comma repair was needed.
- An attempted generic `replace` could match the wrong earlier `},` because it
  was not line-scoped.

Optimization:

- Add delete semantics to the patch workflow, for example:
  `overlay.remove_custom_rules: ["ip-194.221.250.50-proxy"]`.
- Alternatively add `config_patch_create` mode `desired_state=true`, where list
  fields are replaced rather than merged.
- Add a dedicated safe-write tool such as `custom_rules_remove` that validates
  the resulting intent and emits a small review artifact.
- Make `sed_file` produce contextual diffs and support line-scoped exact
  replacements.
- Prefer structured JSON edits for `localclash-intent.json` over text edits.

### 4. Restart Runtime

The user explicitly requested `restart_runtime`.

The tool performed a fresh config validation before restart:

- Config: `/root/localclash/generated/mihomo.yaml`
- Core: `/root/localclash/bin/linux-arm64/lc-mihomo-smart`
- Config test: passed
- Validation duration: about 15.4 seconds

Runtime transition:

- Old PID: `6930`
- New PID: `29115`
- Process name: `lc-mihomo-smart`

Verification:

- `runtime_status` reported `running=true`, PID `29115`.
- `routing_explain` for `194.221.250.50` still returned no active route.

Friction:

- `restart_runtime` correctly warns that router profile restart does not capture
  router traffic, but the follow-up is still manual.
- The user had to notice that network takeover was no longer effective.
- Config validation cost is noticeable on the router even for a small routing
  deletion.

Optimization:

- When runtime profile is `router`, `restart_runtime` should inspect takeover
  state before stopping Mihomo and report whether takeover was previously
  effective.
- If takeover was effective before restart, `restart_runtime` should either
  reapply it after restart with explicit confirmation support, or return a
  first-class pending action that clients can present as a single confirmable
  continuation.
- Keep the existing config validation cache path, but expose clearer timing and
  cache-hit guidance in the final tool summary.

### 5. Repair Router Takeover

After restart, the user reported that network takeover failed.

`router_takeover_status` showed:

- `profile_mode=router`
- `runtime_running=true`
- `effective=false`
- `fwmark_route_v4=false`
- nft chains, TCP redirect, UDP/ICMP TUN mark, DNS hijack, and local DNS bypass
  were still installed.

The failure was therefore narrow: the IPv4 fwmark route was missing.

The user explicitly requested reapply, so the agent called
`router_takeover_apply`.

Result:

- `applied=true`
- `effective=true`
- `fwmark_route_v4=true`
- `tun_device=utun`
- `dns_port=7874`
- `redir_port=7892`

Independent verification:

- A fresh `router_takeover_status` reported `effective=true`.
- All takeover checks were `ok`.
- `runtime_status` still reported PID `29115` running.

Friction:

- The status tool made the missing route clear, but repair still required a
  separate confirmed tool call.
- The failure mode is predictable after runtime restart, so it should not rely
  on user observation.

Optimization:

- Add `router_takeover_repair` or `router_takeover_apply` mode that reports
  exactly which checks were repaired.
- Add a `restart_runtime` post-check for router profiles that automatically
  calls `router_takeover_status` and includes a single actionable result:
  `takeover_effective`, `missing_checks`, and `recommended_tool_args`.
- Persist enough localClash-owned takeover state under `/tmp/localclash` to
  distinguish "user intentionally stopped takeover" from "restart caused runtime
  takeover drift".

## Main Product Gaps

### 1. Delete Is Not A First-Class Intent Operation

The current patch workflow is good for adding or updating routing intent. It is
weak for removal. A user asked to remove one IP route, but the agent had to use
file editing tools.

Required direction:

- Support deletion inside reviewed config patches.
- Keep deletion reviewable and applyable through the same patch boundary as
  additions.
- Avoid direct edits to `localclash-intent.json` for routine route removal.

### 2. File Tool Diffs Are Too Noisy For Small JSON Changes

`sed_file` was useful as an escape hatch, but the returned diff was difficult to
review because it expanded far beyond the changed custom rule.

Required direction:

- Return compact contextual diffs.
- Support line-scoped exact replacement.
- Add structured JSON patch tools for localClash-owned JSON files.

### 3. Query Tools Need Copyable Next Actions

`routing_explain` identified the active custom route clearly, but did not return
a deletion plan.

Required direction:

- For each active route match, include its durable source path and suggested
  patch/remove arguments.
- Make route removal discoverable without requiring the agent to inspect
  `localclash-intent.json` manually.

### 4. Router Restart And Takeover Are Coupled In Practice

The runtime and takeover tools are separate for safety, but in router mode a
restart can leave takeover partially ineffective. The user experiences that as
"network takeover failed".

Required direction:

- Preserve the safety boundary, but make the coupled operational loop explicit.
- `restart_runtime` should surface takeover drift immediately.
- A confirmed "restart and restore takeover" path should exist for router mode.

## Suggested Tool Improvements

Near-term:

- Add `remove_custom_rules` support to `config_patch_create`.
- Add contextual diff output to `sed_file`.
- Add `routing_explain.matches[].remove_tool_args`.
- Make `restart_runtime` include a router takeover post-check when
  `runtime_profile=router`.

Medium-term:

- Add `config_patch_create` delete operations for packs, policy groups, proxy
  groups, transport rules, custom rules, enabled local rule packs, and external
  rule providers.
- Add a structured JSON edit tool for localClash-owned config files with schema
  validation before write.
- Add `router_takeover_repair` as a confirmed tool that repairs only missing
  localClash-owned runtime state and reports repaired checks.

Long-term:

- Model router runtime as a small state machine:
  `config rendered -> runtime running -> takeover effective`.
- Let MCP clients ask for one reviewed operation, such as "remove route and
  reload router mode", while localClash expands it into safe, confirmable
  phases.

