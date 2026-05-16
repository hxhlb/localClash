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

### 4. Base Routing Preset

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

Current code does not yet have:

- a localClash user config file
- standalone rule pack files
- renderer support for selected packs
- UI support for base policy and rule pack selection
- doctor checks for pack schema or pack target references

## Development Sequence

Build this in small steps:

1. Define the localClash user config file.
2. Add declarative `rule-packs/*.yaml`.
3. Teach the renderer to insert enabled packs before the base preset.
4. Add doctor checks for pack parsing, target validity, and missing providers.
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

