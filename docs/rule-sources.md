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
node_labels:
  JP:
    match:
      - "🇯🇵"
      - "日本"
      - "\\bJP\\b"

virtual_targets:
  AI:
    candidates:
      labels: [JP]
    auto: true
    manual: true
    direct: false

enabled_packs:
  - source: sukkaw
    pack: ai
    target: AI
```

`node_labels` are localClash compile-time candidate sets. They are based only on
provider/node names from `subscription.yaml`, not verified egress regions.
localClash does not use server IP geolocation, hostname geolocation, outbound
probing, or capability probing for this first version. Labels such as `JP` or
`US` are not Clash runtime proxy-groups by themselves; they are only used when
a virtual target such as `AI` asks for candidate nodes.

The first CLI surface is intentionally small:

```bash
go run . rules adapt
go run . rules render --selection localclash-packs.yaml
```

`rules adapt` reads source YAML and writes runtime pack cache. `rules render`
reads that cache plus the selection YAML and renders rule-provider, proxy-group,
and rule fragments only. It does not modify `generated/mihomo.yaml`.
