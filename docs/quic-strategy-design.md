# localClash QUIC Phase 1 Strategy Design

## Summary

Phase 1 adds a Mihomo rule-layer QUIC main strategy so UDP destination port 443 is visible and switchable in Dashboard before ordinary business/domain rules.

This phase does not add firewall-level UDP/443 blocking. Firewall forced blocking is intentionally out of scope until a separate design review is completed.

## Goal

- Add a Dashboard-visible main policy group: `🚦 QUIC`.
- Default UDP/443 behavior to `REJECT` so HTTP/3/QUIC can fall back to TCP/TLS or HTTP/2.
- Allow manual Dashboard switching to existing manual, automatic, regional, or direct exits.
- Keep existing YouTube/Google/template rules intact for TCP/TLS, HTTP/2 fallback, and non-UDP/443 traffic.

## Policy Group

`🚦 QUIC` is a `select` group rendered from durable localClash policy intent:

```yaml
proxy-groups:
  - name: 🚦 QUIC
    type: select
    proxies:
      - REJECT
      - 🎯 手动选择
      - ⚡ 自动选择
      - 🇭🇰 香港节点
      - 🇯🇵 日本节点
      - 🇺🇸 美国节点
      - DIRECT
```

`REJECT` must remain the first candidate because Mihomo uses the first candidate as the default.

## Rule Order

The rendered rule is inserted after local safety baseline rules and before existing business/template rules:

```yaml
rules:
  # local safety baseline first

  - AND,((NETWORK,UDP),(DST-PORT,443)),🚦 QUIC

  # existing rules continue below
  - GEOSITE,youtube,📺 YouTube
  - GEOSITE,google,🔎 Google
  ...
  - MATCH,DIRECT
```

This is an additional high-priority transport rule. It does not replace `GEOSITE,youtube` or other existing rules.
The final fallback stays `DIRECT` so router deployments remain blacklist-based
and unclassified game accelerator traffic is not captured by a catch-all proxy
group.

## Durable Model

Phase 1 adds `transport_rules` to the localClash intent and selection model. This keeps `AND/NETWORK/DST-PORT` rules separate from domain/CIDR/GEOIP custom rules.

The default template now carries:

```json
{
  "id": "quic-udp-443-main",
  "network": "UDP",
  "dst_port": 443,
  "target": "🚦 QUIC"
}
```

## Behavior

- `REJECT`: block UDP/443 and force QUIC downgrade.
- `🎯 手动选择`, `⚡ 自动选择`, and regional nodes: route UDP/443 through the selected strategy.
- `DIRECT`: allow UDP/443 direct connection.
- `GEOSITE,youtube`: still handles TCP/TLS, HTTP/2 downgrade, and non-UDP/443 YouTube traffic.

## Test Coverage

- Default template contains `🚦 QUIC` and the first candidate is `REJECT`.
- The transport rule renders as `AND,((NETWORK,UDP),(DST-PORT,443)),🚦 QUIC`.
- The QUIC rule renders after the local safety baseline and before existing template pack rules.
- `transport_rules` is represented in durable intent, config plan overlays, config status intent output, generated metadata, and routing explanations.
