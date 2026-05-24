# Rule Sources

Rule source files are adapter inputs, not pack catalogs.

Each file under `rule-sources/` should stay minimal:

```yaml
id: sukkaw
adapter: sukkaw
url: https://github.com/SukkaW/Surge
base_url: https://ruleset.skk.moe
```

The adapter owns source-specific transformation. It discovers or derives packs
and writes a runtime cache under `.runtime/rules/packs/`.

User selection belongs in a separate packs selection YAML:

```yaml
proxy_groups:
  HK:
    nodes:
      - "🇭🇰香港01 | HK"
      - "🇭🇰香港02 | HK"
    manual: true
    direct: false

policy_groups:
  Steam:
    exits:
      - HK
      - DIRECT
    manual: true

enabled_packs:
  - source: blackmatrix7
    pack: Steam
    target: Steam
```

`proxy_groups` materialize to Clash/Mihomo runtime proxy-groups. `nodes` must
be exact proxy names from `subscription.yaml`; use `subscription_nodes_search`
to find candidate names first. localClash does not verify egress regions with
IP lookup, hostname geolocation, outbound probing, or capability probing.
Choose either `auto: true` or `manual: true`; enabling both is rejected because
it would create competing runtime groups for the same target.

`policy_groups` are the optional business layer for ACL4SSR-style UX. Rules and
packs can target a visible group such as `Steam`; that group then offers exits
such as `HK`, `JP`, `AUTO`, or `DIRECT` in Dashboard. Non-built-in exits must
refer to `proxy_groups`; policy groups do not directly select subscription
nodes.

The first CLI surface is intentionally small:

```bash
go run . rules adapt
go run . rules render --selection localclash-packs.yaml
```

`rules adapt` reads source YAML and writes runtime pack cache. `rules render`
reads that cache plus the selection YAML and renders rule-provider, proxy-group,
and rule fragments only. It does not modify `generated/mihomo.yaml`.
