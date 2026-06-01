# Subscription Inputs

localClash currently treats a subscription source as a full Mihomo-compatible
YAML document. The downloaded response must parse as YAML, must be a mapping,
and must include a non-empty top-level `proxies` list. Each proxy entry must be
a mapping with a non-empty `name`. The refresh path then stores per-source
artifacts under `.runtime/subscriptions/` and merges them into the effective
`subscription.gob`.

## Proxy URI Import Scope

Future subscription input refactoring may add proxy URI list import. Use the
Mihomo Meta source checkout at `/Volumes/Data/Github/mihomo-Meta` as the
reference for share-link parsing behavior. The relevant upstream parser is
`common/convert/converter.go`, with shared VMess/VLESS handling in
`common/convert/v.go`.

MVP-supported proxy URI schemes:

- `ss://`
- `ssr://`
- `vmess://`
- `vless://`
- `trojan://`
- `hysteria://`
- `hysteria2://`
- `hy2://`
- `tuic://`
- `anytls://`
- `socks://`
- `socks5://`
- `socks5h://`

MVP exclusions and notes:

- `http://` and `https://` are valid Mihomo proxy-node URI schemes only when
  they appear in a clearly delimited proxy-node list. Exclude them from the MVP
  scanner because arbitrary text commonly contains normal web URLs.
- `snell://` is not in Mihomo Meta's proxy URI parser. `snell` is a supported
  Mihomo YAML proxy `type`, but localClash should not treat `snell://` as an
  MVP proxy URI input unless a separate parser is intentionally added.
- `mierus://` is supported by Mihomo Meta's proxy URI parser, but it is outside
  the accepted MVP scope for localClash unless product scope changes.
