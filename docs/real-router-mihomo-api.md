# 192.168.6.1 Real Router Mihomo API

This note records the Mihomo external-controller access pattern observed from:

```text
/Volumes/Data/Github/macOSAgentBot/applogs/20260528-202058.log
```

The response fragments below are historical samples from that log. They document
the API shape and useful fields, but they do not represent the latest live state
of the router. Re-query `192.168.6.1:9090` before using counts, selected proxy
groups, hit counters, or runtime config values as current facts.

## Access

The real router Mihomo controller was exposed at:

```text
http://192.168.6.1:9090
```

zashboard was served from the same controller:

```text
http://192.168.6.1:9090/ui/zashboard/
```

The captured requests used Mihomo's default example secret:

```text
Authorization: Bearer 123456
```

`123456` is safe to show as the default Mihomo key used in these examples. If a
runtime profile changes the Mihomo `secret`, replace it in the header.

Minimal curl form:

```bash
MIHOMO_API=http://192.168.6.1:9090
MIHOMO_SECRET=123456

curl -sS -H "Authorization: Bearer ${MIHOMO_SECRET}" \
  "${MIHOMO_API}/configs"
```

The browser-captured Safari requests also included `Accept`, `Cookie`,
`Referer`, `User-Agent`, language, compression, and keep-alive headers. Those
headers are useful evidence of how zashboard called the API, but the essential
scriptable part is the controller URL plus the bearer token.

## Useful Read Calls

```bash
curl -sS -H "Authorization: Bearer ${MIHOMO_SECRET}" \
  "${MIHOMO_API}/configs"

curl -sS -H "Authorization: Bearer ${MIHOMO_SECRET}" \
  "${MIHOMO_API}/proxies"

curl -sS -H "Authorization: Bearer ${MIHOMO_SECRET}" \
  "${MIHOMO_API}/rules"
```

With `jq`, quick live checks are:

```bash
curl -sS -H "Authorization: Bearer ${MIHOMO_SECRET}" \
  "${MIHOMO_API}/proxies" | jq '.proxies | length'

curl -sS -H "Authorization: Bearer ${MIHOMO_SECRET}" \
  "${MIHOMO_API}/rules" | jq '.rules | length'
```

## GET /configs

`/configs` returns the effective Mihomo runtime settings exposed by the
controller. The captured sample had 41 top-level keys.

Selected historical sample values:

```json
{
  "port": 7890,
  "socks-port": 7891,
  "redir-port": 7892,
  "mixed-port": 7893,
  "tproxy-port": 7895,
  "allow-lan": true,
  "bind-address": "*",
  "mode": "rule",
  "log-level": "warning",
  "ipv6": true,
  "interface-name": "pppoe-wan",
  "geodata-mode": true,
  "geodata-loader": "standard",
  "geosite-matcher": "succinct",
  "tcp-concurrent": true,
  "find-process-mode": "off",
  "sniffing": true,
  "global-ua": "clash.meta/alpha-smart-g565047e"
}
```

The same sample showed TUN enabled:

```json
{
  "tun": {
    "enable": true,
    "device": "utun",
    "stack": "Mixed",
    "dns-hijack": ["127.0.0.1:53"],
    "auto-route": false,
    "auto-detect-interface": false,
    "inet4-address": ["198.18.0.1/30"],
    "inet6-address": ["fdfe:dcba:9876::1/126"],
    "endpoint-independent-nat": true
  }
}
```

Do not treat these config values as current router truth. They are the values in
the captured log response.

## GET /proxies

`/proxies` returns a JSON object keyed by proxy or group name:

```json
{
  "proxies": {
    "DIRECT": {
      "alive": true,
      "name": "DIRECT",
      "type": "Direct",
      "udp": true
    },
    "GLOBAL": {
      "alive": true,
      "all": ["DIRECT", "REJECT", "..."],
      "name": "GLOBAL",
      "now": "DIRECT",
      "type": "Selector",
      "udp": true
    },
    "♻️ 自动选择": {
      "alive": true,
      "all": ["..."],
      "name": "♻️ 自动选择",
      "now": "Smart - Select",
      "testUrl": "https://cp.cloudflare.com/generate_204",
      "type": "Smart",
      "useLightGBM": true
    }
  }
}
```

Captured sample summary:

```text
proxy entries: 116
types:
  Vless: 40
  Shadowsocks: 35
  Selector: 29
  Smart: 7
  Compatible: 1
  Direct: 1
  Pass: 1
  Reject: 1
  RejectDrop: 1
```

Group examples from the same sample:

```text
GLOBAL                  Selector  now=DIRECT           candidates=112
♻️ 自动选择             Smart     now=Smart - Select   candidates=75
🇭🇰 香港节点            Smart     now=Smart - Select   candidates=15
🇯🇵 日本节点            Smart     now=Smart - Select   candidates=15
🇺🇸 美国节点            Smart     now=Smart - Select   candidates=14
🎥 Netflix              Selector  now=🇭🇰 香港节点      candidates=83
🎯 全球直连             Selector  now=DIRECT           candidates=1
```

The full response can include subscription node names, latency history, and
provider-specific data. Avoid copying a full `/proxies` dump into source docs
unless the node list has been intentionally reviewed for publication.

## GET /rules

`/rules` returns the active Mihomo rule list as an ordered array. Each entry
contains the evaluation index, rule type, payload, target proxy/group, size, and
hit/miss counters.

Captured sample summary:

```text
rules: 740
types:
  DomainSuffix: 631
  GeoSite: 32
  IPCIDR: 29
  ProcessName: 25
  GeoIP: 7
  RuleSet: 6
  DstPort: 5
  Domain: 3
  Match: 1
  SrcPort: 1
```

Start of the captured rule list:

```json
{
  "index": 0,
  "type": "GeoSite",
  "payload": "google-cn",
  "proxy": "🇬 谷歌服务",
  "size": 112,
  "extra": {
    "disabled": false,
    "hitCount": 128,
    "hitAt": "2026-05-25T21:37:50.340338924+08:00",
    "missCount": 79625,
    "missAt": "2026-05-25T21:47:21.674236428+08:00"
  }
}
```

Other early rules from the same sample:

```text
1  RuleSet  直連              -> 🎯 全球直连
2  RuleSet  SyncnextUnbreak   -> 🎯 全球直连
3  RuleSet  inline AI Suite   -> 🇺🇸 美国节点
4  RuleSet  US-Proxy          -> 🇺🇸 美国节点
5  RuleSet  大流量            -> 🎥 Netflix
6  RuleSet  SyncnextProxy     -> 🚀 手动选择
7  GeoSite  category-public-tracker -> DIRECT
8  GeoSite  private           -> 🎯 全球直连
9  GeoIP    private           -> 🎯 全球直连
```

End of the captured rule list:

```text
735  ProcessName  UUBooster      -> DIRECT
736  ProcessName  uugamebooster  -> DIRECT
737  DstPort      80             -> 🐟 漏网之鱼
738  DstPort      443            -> 🐟 漏网之鱼
739  Match                       -> DIRECT
```

The `extra.hitCount`, `extra.hitAt`, `extra.missCount`, and `extra.missAt`
fields are runtime counters. In the captured sample their timestamps were
already from `2026-05-25`, so they are especially unsafe to reuse as latest
router state without a fresh API call.
