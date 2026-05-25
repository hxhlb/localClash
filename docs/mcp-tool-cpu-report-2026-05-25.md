# MCP Tool CPU Report - 2026-05-25

This report records the real-router MCP tool coverage and CPU observations from
the 2026-05-25 test pass.

Target router:

- Host: `root@192.168.6.1`
- MCP endpoint: `http://192.168.6.1:8765/mcp`
- Tested binary SHA-256:
  `0ade115d24e0d7595018e11ac97094dfcb9b581c320499a954b23b41bc182a21`
- Commit: `85c8f42 Add staged logs for MCP execution tools`

Safety boundary:

- `router_takeover_apply` and `router_takeover_stop` were tested with
  `dry_run=true` only.
- No real router takeover was applied.
- Runtime tests used `/tmp/localclash-mcp-tooltest`, not the active
  `/root/localclash` runtime directory.
- The test runtime was stopped at the end.

## Summary

33 MCP tools were exercised successfully.

The current CPU problem is not evenly distributed across all MCP tools. It is
concentrated in two families:

1. localClash config-resolution paths, especially `localconfig.Resolve`.
2. Mihomo config validation, especially `mihomo -t`.

After test cleanup, the MCP server returned to idle:

- `localclash` state: sleeping
- RSS: about 40 MB
- sample CPU idle: about 93.6%

So this test did not reproduce a persistent idle 100% localClash process. It did
reproduce high CPU during specific MCP operations.

## Tool Coverage

### Fast Read and Builder Tools

These tools completed in about 0.7s to 2.2s and did not show sustained
localClash CPU pressure in the sampled windows:

| Tool | Time |
| --- | ---: |
| `config_configure` | 747ms |
| `tools_list` | 929ms |
| `config_status` | 894ms |
| `nl_file` | 924ms |
| `sed_file` | 899ms |
| `subscriptions_status` | 962ms |
| `subscription_nodes_list` | 1090ms |
| `subscription_nodes_search` | 966ms |
| `runtime_profile_status` | 961ms |
| `runtime_status` | 1236ms |
| `router_takeover_status` | 1687ms |
| `packs_list` | 1570ms |
| `packs_get` | 1429ms |
| `pack_rules_prefetch` | 2223ms |
| `pack_rules_read` | 1520ms |
| `pack_rules_query` | 1531ms |
| `proxy_group_build` | 967ms |
| `policy_group_build` | 910ms |
| `custom_rules_build` | 940ms |
| `rule_provider_build` | 1004ms |
| `subscriptions_configure` | 886ms |
| `doctor` | 1136ms |
| `environment_inspect` | 1332ms |

### Slow Read Tool

`routing_explain` completed successfully but is too expensive for an ordinary
read path.

Observed time:

- First matrix run: 16756ms
- Focused rerun: 26888ms

Focused CPU samples showed `localclash` between about 83% and 136% while the
request was running.

Interpretation:

- `routing_explain` is currently doing heavy config intent resolution work.
- It should not behave like a cheap status/read tool on thin routers.

## Stage-Logged Execution Tools

The new staged task logs worked. Long-running tools now show where time is
spent instead of only reporting final `done`.

### `subscriptions_refresh`

Observed time:

- 21900ms

Important stages:

| Stage | Time |
| --- | ---: |
| `fetch_source` for `sub1` | 2504ms |
| `write_source_artifact` for `sub1` | 143ms |
| `fetch_source` for `sub2` | 2359ms |
| `write_source_artifact` for `sub2` | 129ms |
| `read_artifacts` | 250ms |
| `write_merged_subscription` | 30ms |
| `load_subscription_nodes_after` | 244ms |
| `evaluate_localclash_impact` | 15481ms |

CPU observation:

- `localclash` peaked around 127% during this tool.

Interpretation:

- Network fetch and YAML writes were not the primary bottleneck.
- The expensive stage was post-refresh localClash impact evaluation.
- This points back to config resolution rather than subscription download.

### `config_render`

Observed time:

- 16828ms

Important stages:

| Stage | Time |
| --- | ---: |
| `resolve_localclash_config` | 15024ms |
| `render_generated_config` | 526ms |
| `render_generated_config.render_pack_selection` | 471ms |
| `render_generated_config.write_output` | 29ms |

CPU observation:

- `localclash` peaked around 108%.

Interpretation:

- Rendering itself is relatively cheap.
- Resolving `localclash.yaml` into selected packs, proxy groups, policy groups,
  and custom rules is the dominant cost.

### `config_patch_create`

Observed time:

- 16905ms

Important stages:

| Stage | Time |
| --- | ---: |
| `resolve_candidate_config` | 15051ms |
| `render_candidate` | 568ms |
| `render_candidate.render_pack_selection` | 513ms |
| `write_summary` | 1ms |

CPU observation:

- `localclash` peaked around 125%.

Interpretation:

- Patch creation has the same resolution bottleneck as `config_render`.
- Candidate render and summary writes are not the problem.

### `config_patch_apply`

Observed time:

- 16681ms

Important stages:

| Stage | Time |
| --- | ---: |
| `resolve_apply_config` | 14964ms |
| `render_candidate` | 531ms |
| `backup_apply_targets` | 1ms |
| `write_active_config` | 14ms |

CPU observation:

- `localclash` peaked around 118%.

Interpretation:

- Apply has the same resolution bottleneck.
- The file write and backup operations are not material contributors.

### `run_runtime`

Observed time:

- 37273ms

Important stages:

| Stage | Time |
| --- | ---: |
| `config_test` | 35941ms |
| `start_process` | 1ms |

CPU observation:

- `mihomo-meta` peaked around 183%.

Interpretation:

- Runtime start is fast after validation.
- The slow operation is Mihomo config validation.

### `restart_runtime`

Observed time:

- 33316ms

Important stages:

| Stage | Time |
| --- | ---: |
| `config_test` | 32527ms |
| `stop` | 1ms |
| `start` | 2ms |
| `status` | 1ms |

CPU observation:

- `mihomo-meta` peaked around 192%.

Interpretation:

- Restart is no longer a mystery path.
- Stop/start/status are effectively negligible.
- `mihomo -t` is the dominant cost.

### Router Takeover Dry Runs

Observed times:

- `router_takeover_apply`: 1890ms
- `router_takeover_stop`: 1875ms

Important stage:

- `read_runtime_profile`: 3ms

Interpretation:

- In `dry_run=true` with `runtime_profile=normal`, these tools return early.
- This does not validate real firewall mutation cost.
- Real takeover testing still belongs in isolated OpenWrt only.

### `stop_runtime`

Observed time:

- 1877ms

Important stages:

| Stage | Time |
| --- | ---: |
| `takeover_guard_status` | 744ms |
| `stop_runtime` | 31ms |

Interpretation:

- Stop itself is cheap.
- Guard status is measurable but not severe in this run.

## CPU Findings

### localClash CPU

Observed hot paths:

- `routing_explain`
- `subscriptions_refresh` stage `evaluate_localclash_impact`
- `config_render` stage `resolve_localclash_config`
- `config_patch_create` stage `resolve_candidate_config`
- `config_patch_apply` stage `resolve_apply_config`

Working hypothesis:

- `localconfig.Resolve` is doing repeated expensive work over the same
  subscription/config/rule-pack data.
- The cost is large enough to occupy more than one CPU worth of time on the
  router.

This is now bounded enough to stop treating the issue as "MCP server is
generally expensive".

### Mihomo CPU

Observed hot paths:

- `run_runtime` stage `config_test`
- `restart_runtime` stage `config_test`

Working hypothesis:

- Mihomo config validation is expensive for the generated config, subscription
  size, and rule/provider structure.
- localClash currently pays this cost before runtime start/restart.

This is a separate problem from localClash's own config-resolution CPU.

### OpenClash / Background Noise

The router's existing OpenClash `clash` process showed background spikes during
sampling, sometimes above 90%. This is not proof that localClash caused those
spikes.

The useful comparison is:

- localClash CPU during specific MCP calls
- localClash idle CPU after those calls finish
- Mihomo CPU during config validation or runtime warm-up

## Product Impact

The current staged logs are a necessary improvement because they let an Agent
answer:

- which stage is currently running
- which stage failed
- how long each stage took
- whether the long wait is localClash resolution or Mihomo validation

But staged logs are not enough by themselves. Several user-facing operations are
still too expensive for a thin-client router:

- "explain routing" can take over 20 seconds.
- "render config" takes about 17 seconds mostly before actual render.
- "patch create/apply" takes about 17 seconds mostly before actual render/write.
- "run/restart runtime" takes over 30 seconds mostly in Mihomo `-t`.

## Recommended Discussion Points

### 1. Optimize or Cache `localconfig.Resolve`

Likely work:

- Profile `localconfig.Resolve` directly with a copied real-router config.
- Add per-stage timing inside `localconfig.Resolve`.
- Identify whether the cost is subscription parsing, selector matching,
  policy-group expansion, rule-pack resolution, YAML marshal/unmarshal, or
  repeated file reads.
- Reuse resolved subscription node indexes across:
  - `routing_explain`
  - `config_render`
  - `config_patch_create`
  - `config_patch_apply`
  - `subscriptions_refresh` impact evaluation

Success target:

- Common resolve path under 2s on the real router.

### 2. Split Cheap Status From Full Audit

`config_status` is already lightweight by default. The same principle should
apply to `routing_explain`.

Possible approach:

- `routing_explain` default mode should use existing durable metadata and avoid
  full selector resolution.
- Add `detail=true` or `resolve=true` for expensive full proof.
- Return a clear `resolve_skipped` marker when it avoids heavy work.

Success target:

- Basic routing explanation under 2s.
- Full evidence mode can remain slower but must be explicit.

### 3. Reconsider When `mihomo -t` Is Required

The current preflight is safe but expensive.

Possible approach:

- Keep `mihomo -t` for config changes.
- Skip repeated `-t` when generated config hash has already passed validation.
- Store validation metadata:
  - config SHA-256
  - core type/version
  - validation time
  - result
- `restart_runtime` can reuse validation if generated config and core did not
  change.

Success target:

- Restart unchanged runtime in under 3s.
- First validation after config change can still take 30s, but the task log must
  say it is validating Mihomo config.

### 4. Keep Runtime Mutation Separate From Router Takeover

This remains correct:

- `run_runtime` starts Mihomo only.
- `restart_runtime` restarts Mihomo only.
- `router_takeover_apply` mutates firewall/DNS/policy-routing state.
- `router_takeover_stop` reverts takeover state.

Do not collapse these into one opaque action without stage logs.

### 5. Add a Repeatable Performance Harness

The ad hoc script should become a repo script.

Suggested script:

- `scripts/mcp-tool-perf-smoke.mjs`

It should collect:

- tool name
- elapsed time
- task status
- stage timings
- `localclash` max CPU
- `mihomo` max CPU
- `clash` background max CPU
- output JSON artifact

Success target:

- One command can reproduce this report against a router or test OpenWrt VM.

## Immediate Next Step

The highest-value next engineering step is to instrument and profile
`localconfig.Resolve`.

Reason:

- It explains the `routing_explain`, `subscriptions_refresh`,
  `config_render`, `config_patch_create`, and `config_patch_apply` CPU spikes.
- It is localClash-owned code.
- Optimizing it will improve both Agent experience and LuCI responsiveness.

The second step is validation-cache design for Mihomo `-t`, because that is the
dominant cost in `run_runtime` and `restart_runtime`.
