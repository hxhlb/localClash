# Policy Templates

`docs/rule-model.md` is the authoritative development contract for rule
layering, customization, and the Loyalsoldier boundary. This document describes
the current base policy and localClash policy templates.

## Initial Choice

The first policy template uses `Loyalsoldier/clash-rules` as the base ruleset.

Reasons:

- The rule categories are small enough for a user to understand.
- The files map cleanly into Mihomo `rule-providers`.
- The template can stay localClash-owned while rule content remains upstream.
- It includes the recommended whitelist and blacklist rule orders from Loyalsoldier.
- Whitelist mode sends unmatched traffic to proxy.
- Blacklist mode sends unmatched traffic direct.
- Rendered configs prepend a local safety baseline before the upstream policy rules.
- The local safety baseline keeps loopback, private LAN ranges, link-local ranges, and local hostnames direct.
- Rendered configs keep `.local`, `.lan`, `.home.arpa`, and `localhost` DNS resolution on the system resolver instead of remote DoH.

Loyalsoldier is the default base routing preset. It is not the immutable local
safety baseline and it should not be modeled as an optional rule pack.

## Boundary

Do not commit upstream rule content into this repository. A policy template should store:

- upstream references
- group mappings
- rule order
- local override slots

Optional rule packs should be modeled separately from policy templates. Rule
packs are user-selectable add-ons; policy templates define the broad public
internet routing mode such as whitelist-first or blacklist-first.

The renderer should turn the policy into a generated Mihomo runtime config under `generated/`, which is local data and ignored by git.

## localClash Templates

MCP `config_configure` exposes policy templates from disk as base product
configuration. Template manifests live under `policy-templates/`, and the tool
does not render or start runtime. A template may either embed one `config:`
block directly or declare an ordered `patches:` list. Patch-set templates are
merged in manifest order and then written as the durable `localclash.yaml`
intent.

- `minimal`: records a compact durable intent and leaves routing to the local
  safety baseline plus the base policy. This is for users who want manual
  customization.
- `localclash-default`: ACL4SSR-like default for new users. It uses v2fly-dlc
  GEOSITE packs for common categories such as AI, media, communication,
  Telegram, Google, Apple, Microsoft, developer services, games, ads, and China
  direct domains. Telegram also includes explicit IPv4/IPv6 CIDR custom rules,
  because many Telegram clients connect to Telegram IP ranges directly and do
  not always expose a domain that can be caught by `GEOSITE,telegram`.
  The manifest `policy-templates/localclash-default.yaml` is a patch set: each
  file under `policy-templates/localclash-default.d/` contributes one stable
  default patch, such as region exits, communication/social routing, Steam, or
  media groups. The patch files intentionally keep emoji identifiers as YAML
  `\U...` escapes so OpenWrt/BusyBox display locale quirks do not change the
  on-disk template bytes.
  Its Dashboard-facing structure is layered as business group -> exit group ->
  subscription nodes. The base policy provides `⚡ 自动选择` and `🎯 手动选择`;
  the default template adds direct and region exits. For example, `🎮 Steam`
  and `🎮 游戏平台` default to `🌐 全球直连`, `🕹 Bahamut` defaults to
  `🇹🇼 台湾节点`, while ChatGPT and streaming groups default to the Aethersailor
  region order. The final Dashboard group list pins `🎯 手动选择` first and
  `⚡ 自动选择` second. `🎯 手动选择` itself exposes `⚡ 自动选择`, then available
  region groups, then subscription nodes. Region exits are optional so a
  subscription without, for example, Korean nodes does not make first-time
  initialization fail.
  `🤖 ChatGPT` is the OpenAI-specific rule target and is rendered before the
  broader `🧠 AI` category.

MCP `config_status` exposes this template through `intent.packs`,
`intent.policy_groups`, `intent.proxy_groups`, and `overlay.rules`. For a compact
Agent-facing answer, use the read-only `routing_explain` tool; it explains a
service/domain/group query with the matching packs, business policy group,
available exits, optional cached rule evidence, and safe patch path. Agents must
not infer the default template from the truncated
`generated_summary.rules_sample` alone. See `docs/rule-model.md`.

## Starter Base Policy

See `policies/loyalsoldier.yaml`.
