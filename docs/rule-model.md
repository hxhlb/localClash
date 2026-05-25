# Rule Model

This document defines the development contract for localClash routing rules.
It turns the current starter policy into an extensible model for the future
web UI, without letting optional customization pollute the safety baseline.

## Product Position

localClash should not be another hand-edited Clash YAML manager. It should
compile localClash-owned policy data into a Mihomo runtime config with:

- deterministic rendering
- readable diffs
- validation before run
- local-only storage
- no cloud dependency
- no sensitive config collection

The web UI must edit localClash policy data, not `generated/mihomo.yaml`
directly.

## Layers

Rules are layered in this order:

```text
1. local safety baseline
2. user explicit overrides
3. optional rule packs
4. base routing preset
5. fallback
```

Mihomo evaluates rules from top to bottom, so higher-priority layers must be
rendered earlier.

## Layer Responsibilities

### 1. Local Safety Baseline

The local safety baseline is built into localClash and cannot be disabled.

It protects local, LAN, mDNS, loopback, link-local, private address, and local
resolver behavior. It must keep local network operation stable even when an
upstream preset, subscription, or optional pack is wrong.

Current examples include:

- `localhost`
- `.local`
- `.lan`
- `.home.arpa`
- loopback ranges
- private IPv4 ranges
- link-local ranges
- local IPv6 ranges
- system DNS policy for local names

This layer is not a place for product categories such as AI, media, games,
developer tools, ads, or company domains.

### 2. User Explicit Overrides

User explicit overrides are direct user decisions. They should have the
highest user-controlled precedence, below only the safety baseline.

Examples:

```yaml
overrides:
  direct:
    domains:
      - nas.home.arpa
      - printer.lan
  proxy:
    domains:
      - example-work-service.com
```

Overrides are for small, concrete fixes. They should not become a hidden
category system.

### 3. Optional Rule Packs

Optional rule packs are the primary web UI customization layer. Users should
be able to enable or disable packs from the UI and choose the target behavior
when a pack supports more than one target.

Examples:

```text
[ ] AI services
[ ] Streaming media
[ ] Ads and tracking
[ ] Developer services
[ ] Games
[ ] Mainland services
```

Rule packs should be declarative files owned by localClash. They can include
domain rules, domain suffix rules, IP CIDR rules, or references to rule
providers when that becomes necessary.

First version schema:

```yaml
id: ai
name: AI Services
description: Route common AI services through a selected target.
version: 1
default_target: proxy
target_options:
  - proxy
  - direct
  - manual
  - smart
rules:
  - domain_suffix: openai.com
  - domain_suffix: chatgpt.com
  - domain_suffix: anthropic.com
```

Local user selection should live in localClash config, for example:

```yaml
base_preset:
  id: loyalsoldier
  mode: whitelist

enabled_rule_packs:
  - id: ai
    target: proxy
  - id: ads
    target: reject
```

The UI should save this localClash config and trigger a render. It should not
patch the generated Mihomo runtime config.

### 4. Base Routing RuntimeProfile

The base routing preset defines the public internet routing philosophy. The
default preset is currently Loyalsoldier.

Loyalsoldier belongs here:

```text
base routing preset = public internet routing base
```

It is not:

- the immutable safety baseline
- an AI/media/game style optional pack
- a user override layer

It is broad enough to decide whether the config is whitelist-first or
blacklist-first. That makes it a base policy choice, not a small add-on.

The web UI should present it separately from rule packs:

```text
Base Policy
(*) Loyalsoldier whitelist
( ) Loyalsoldier blacklist
( ) Minimal direct-first
( ) Custom

Rule Packs
[ ] AI services
[ ] Streaming media
[ ] Ads and tracking
[ ] Developer services
[ ] Games
```

Loyalsoldier should remain replaceable. Future presets can exist beside it,
but none of them may replace the local safety baseline.

### 5. Fallback

Fallback is the final `MATCH` rule emitted by the selected base preset or
custom policy.

Examples:

- whitelist mode: unmatched traffic goes `PROXY`
- blacklist mode: unmatched traffic goes `DIRECT`

Optional packs and overrides must be rendered before fallback.

## Renderer Contract

The renderer should compile inputs into `generated/mihomo.yaml` in this order:

```text
subscription proxies
+ local runtime settings
+ local safety baseline
+ user explicit overrides
+ enabled rule packs
+ selected base routing preset
+ fallback
```

The renderer owns:

- proxy groups
- optional policy groups that expose business-layer choices before selecting
  proxy-group exits
- rule provider definitions
- rule order
- local DNS safety policy
- generated runtime output

The subscription is only a proxy source. It must not be treated as the owner of
runtime rules.

## Current Implementation State

Current code already has:

- built-in local safety baseline in `internal/configrender/render.go`
- Loyalsoldier policy preset in `policies/loyalsoldier.yaml`
- whitelist and blacklist modes
- generated Mihomo config under `generated/`
- doctor checks for baseline injection, rule targets, provider references, and
  `mihomo -t`

Current code now has:

- a localClash user config file model
- a `policy_template` field for durable config intent
- a `config_configure` MCP tool for base product configuration: core,
  runtime profile, and policy template
- disk-backed `minimal` and `localclash-default` policy templates under
  `policy-templates/`; `localclash-default` is a patch-set manifest whose
  ordered files under `policy-templates/localclash-default.d/` are merged during
  initialization into the same durable `localclash.yaml` intent model that MCP
  patches use
- default patch files for region exits, direct baselines, communication/social
  routing, AI/developer routing, Steam, media/platform routing, games, and tail
  fallback routing
- renderer support for selected third-party packs
- renderer support for inline `custom_rules`
- renderer support for user-supplied external `rule_providers`
- MCP patch tools for proxy groups, custom rules, external rule-providers,
  reviewed config apply, and atomic generated config rendering

Current code still does not yet have:

- standalone local rule pack files
- UI support for base policy and rule pack selection
- doctor checks for custom rule or external provider schema and target
  references

## MCP Routing Discovery

`config_status` exposes the factual source of truth for default routing:

- `intent.proxy_groups` lists reusable exit groups such as region selectors and
  direct exits
- `intent.policy_groups` lists business-layer Dashboard groups and their exits
- `intent.packs` lists active rule packs and their targets
- `overlay.rules` shows rendered localClash-managed rule targets

That is enough for a careful Agent to discover that `localclash-default` is a
business -> exit -> node model created by default patches, for example
`default.steam.v1` contributing `v2fly_dlc_steam` targeting `🎮 Steam`, whose
exits include direct, manual, automatic, and regional groups.

Agents should not infer active default rules from
`generated_summary.rules_sample` alone because that sample is intentionally
truncated and often dominated by the local safety baseline.

Use the read-only MCP `routing_explain` tool for compact routing discovery.
It reads durable `localclash.yaml` intent and returns matching packs, policy
groups, reusable exit groups, optional cached provider-rule evidence, and the
safe reviewed patch path. Example queries:

- `routing_explain(query: "Steam")`: explains the active Steam pack, the
  Dashboard-facing Steam policy group, and its exits.
- `routing_explain(query: "ChatGPT through Singapore")`: surfaces matching
  business groups and reusable Singapore exits so an Agent can build a reviewed
  policy-group patch.
- `routing_explain(query: "openai.com")`: can include cached provider-rule
  matches when provider-cache coverage exists; if cache is incomplete, the tool
  still reports durable intent and says which prefetch/read path to use.

`routing_explain` is not a mutation tool. For changes, follow its
`patch_guidance`: `config_status` -> optional `proxy_group_build` /
`policy_group_build` -> `config_patch_create` -> review -> `config_patch_apply`
-> verification.

## Development Sequence

Build this in small steps:

1. Extend MCP patch tools until agents can express common routing intent without
   editing YAML directly.
2. Add read-only MCP routing discovery tools so Agents can inspect default
   business groups without parsing the full `config_status` payload.
3. Add declarative `rule-packs/*.yaml` for localClash-owned reusable packs.
4. Add doctor checks for pack parsing, custom rule validity, target validity,
   and missing providers.
5. Add CLI flags for config path and dry-run diff.
6. Expose the same model through the local web UI.

Do not start by adding many pack contents. First make the mechanism correct.

## Acceptance Criteria

A correct implementation must satisfy:

- local safety baseline is always rendered first and cannot be disabled
- user overrides render before optional packs
- optional packs render before the base routing preset
- Loyalsoldier is selectable as a base preset, not as an optional pack
- whitelist and blacklist remain base policy modes
- generated config is reproducible from localClash-owned inputs
- UI changes are stored in localClash config, not in generated Mihomo YAML
- doctor can explain missing files, invalid rules, missing targets, missing
  providers, and failed `mihomo -t`
- sensitive local files remain ignored by git
