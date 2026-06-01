# Subscription Inputs

localClash subscription input is a list of whitelisted URIs. Each URI must be
one of:

- `http://` or `https://` remote subscription URI
- MVP proxy URI listed below

Remote subscription responses are parsed as full Mihomo-compatible YAML first.
If the response is not a usable Mihomo subscription YAML document, localClash
then parses it as one proxy URI per line. Inline MVP proxy URI inputs are parsed
directly as one proxy URI per line. In all cases, accepted input is normalized
to a top-level `proxies` list, then stored under `.runtime/subscriptions/` and
merged into the effective `subscription.gob`.

URI deduplication is string-level only. localClash does not decode or compare
proxy server fields, passwords, UUIDs, or node names when removing duplicate URI
inputs.

## Proxy URI Import Scope

Use the Mihomo Meta source checkout at `/Volumes/Data/Github/mihomo-Meta` as
the reference for share-link parsing behavior. The relevant upstream parser is
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

Exclusions and notes:

- `http://` and `https://` are valid Mihomo proxy-node URI schemes only when
  they appear in a clearly delimited proxy-node list. localClash does not treat
  them as proxy URI inputs in the MVP because they are reserved for remote
  subscription URIs.
- `snell://` is not in Mihomo Meta's proxy URI parser. `snell` is a supported
  Mihomo YAML proxy `type`, but localClash should not treat `snell://` as an
  MVP proxy URI input unless a separate parser is intentionally added.
- `mierus://` is supported by Mihomo Meta's proxy URI parser, but it is outside
  the accepted MVP scope for localClash unless product scope changes.
