# Policy Templates

`docs/rule-model.md` is the authoritative development contract for rule
layering, customization, and target ownership. This document describes the
current disk-backed localClash policy templates.

## Boundary

Policy templates are localClash-owned durable intent, not generated Mihomo YAML.
Template manifests live under `policy-templates/`, and `config_configure` writes
their resolved intent into `localclash.json`. The renderer then combines that
intent with the effective subscription and runtime profile.

Do not model a removed upstream preset as hidden renderer behavior. Broad default
behavior must appear as explicit patch files in the selected template manifest.

## Built-In Templates

- `minimal`: loads only `policy-templates/minimal.json`. It defines the compact
  default graph for advanced manual customization and does not auto-load the
  built-in patch set.
- `localclash-default`: loads all built-in patches listed by
  `policy-templates/localclash-default.json`. Each ordered file under
  `policy-templates/localclash-default.d/` contributes one stable default patch,
  such as region exits, communication/social routing, Steam, media groups, games,
  and fallback behavior.

Both templates are patch-layered product configuration. Neither depends on a
separate preset file outside `policy-templates/`.

## Default Structure

The default Dashboard-facing structure is layered as:

```text
business group -> exit group -> subscription nodes
```

`minimal` defines `⚡ 自动选择`, `🎯 手动选择`, and `DNSProxy` in the minimal
strategy layer. `DNSProxy` exits through `⚡ 自动选择`, so router DNS `#DNSProxy`
references have a concrete target even without loading the default patch set.

`localclash-default` adds direct and regional exits plus business routing groups.
Region exits are optional so subscriptions without a given region can still
initialize. Patch files intentionally keep emoji identifiers as YAML `\U...`
escapes so OpenWrt/BusyBox display locale quirks do not change on-disk template
bytes.

MCP `config_status` exposes the active template through `intent.packs`,
`intent.policy_groups`, `intent.proxy_groups`, and `overlay.rules`. For compact
Agent-facing routing discovery, use the read-only `routing_explain` tool.
