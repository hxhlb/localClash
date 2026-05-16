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
enabled_packs:
  - source: sukkaw
    pack: stream
    target: proxy
```

The first CLI surface is intentionally small:

```bash
go run . rules adapt
go run . rules render --selection localclash-packs.yaml
```

`rules adapt` reads source YAML and writes runtime pack cache. `rules render`
reads that cache plus the selection YAML and renders a rules fragment only. It
does not modify `generated/mihomo.yaml`.
