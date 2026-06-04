# Proxy Server DNS Policy

Status: investigation note.

This note records a user-facing DNS issue where a subscription provider asks
users to add a `nameserver-policy` entry for private proxy server domains, for
example:

```yaml
dns:
  nameserver-policy:
    "+.cloud-nodes.com":
      - "124.221.68.73:1053"
```

The important localClash question is whether this should be modeled as normal
traffic DNS policy, or as proxy-server DNS policy for `proxies[].server`.

## Problem

Some airport providers use private domains in subscription proxy nodes. Public
DNS cannot resolve those node domains. The provider may publish a private DNS
server that can resolve the node domain suffix.

The affected domain is not necessarily a destination website. In many cases it
is the Mihomo node endpoint under `proxies[].server`. That means the runtime
must resolve the proxy server host before it can dial the selected outbound
proxy.

## Mihomo Behavior

Mihomo stores outbound node endpoints as `server:port` addresses in outbound
adapters. Common adapters such as VMess, Trojan, Shadowsocks, and Socks5 build
their adapter address from the proxy node's `server` and `port`, then dial that
address through the shared dialer.

The shared dialer resolves the address host before connecting. If no resolver is
explicitly supplied, it uses `resolver.ProxyServerHostResolver`. Mihomo sets
`ProxyServerHostResolver` from DNS config:

- If `dns.proxy-server-nameserver` is configured, Mihomo creates a dedicated
  proxy-server resolver from it.
- `dns.proxy-server-nameserver-policy` is parsed with the same policy parser as
  `dns.nameserver-policy`, but applies to that dedicated proxy-server resolver.
- If no `dns.proxy-server-nameserver` is configured, proxy server host
  resolution falls back to the normal resolver, so `dns.nameserver-policy` may
  appear to work in many client configurations.
- Mihomo rejects `proxy-server-nameserver-policy` when
  `proxy-server-nameserver` is empty.

This explains why provider documentation often recommends `nameserver-policy`:
it is a more widely compatible Clash-family snippet. It is not the most precise
placement when Mihomo has a dedicated proxy-server resolver configured.

## localClash Router Default

The localClash router runtime profile currently configures:

```json
{
  "dns": {
    "respect-rules": true,
    "proxy-server-nameserver": [
      "https://dns.alidns.com/dns-query",
      "https://doh.pub/dns-query"
    ],
    "nameserver-policy": {
      "geosite:gfw": [
        "https://cloudflare-dns.com/dns-query#DNSProxy",
        "https://dns.google/dns-query#DNSProxy"
      ]
    }
  }
}
```

Because `proxy-server-nameserver` is non-empty, Mihomo resolves
`proxies[].server` through the dedicated proxy-server resolver. For provider
instructions that target node server domains, localClash should prefer:

```yaml
dns:
  proxy-server-nameserver-policy:
    "+.cloud-nodes.com":
      - "124.221.68.73:1053"
```

This is sufficient for the specific requirement when the target domain suffix
appears in `proxies[].server` and the existing `proxy-server-nameserver` remains
configured.

## Implementation Direction

A future DIY or provider-specific module should distinguish these cases:

- If the provider DNS rule targets node endpoint domains found in
  `proxies[].server`, generate `dns.proxy-server-nameserver-policy`.
- If the provider DNS rule targets ordinary destination domains, generate
  `dns.nameserver-policy`.
- If both are possible, inspect subscription nodes and record why the chosen
  policy is proxy-server DNS or normal DNS.

Generated config must still pass `mihomo config-test`. Runtime verification
should include a Mihomo log or connection attempt showing that a node endpoint
domain was resolved through the expected private DNS path.

