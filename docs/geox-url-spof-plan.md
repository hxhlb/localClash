# GEO Data URL And Auto-Update Contract

This document records the current localClash contract for Mihomo GEO data
resources. It is no longer an open implementation plan: the default runtime
profiles already enable Mihomo native GEO auto-update and point `geox-url` at
one selected GitHub mirror.

## Current Contract

localClash manages GEO data through two separate paths:

- Release/base-asset bootstrap: `scripts/build-release-assets.sh` downloads
  `Country.mmdb`, `geoip.dat`, `geosite.dat`, and `ASN.mmdb`, then packages
  them into `localclash-base-assets.tar.gz`.
- Runtime refresh: generated Mihomo config includes `geo-auto-update: true` and
  `geo-update-interval: 24`, so Mihomo can refresh those files after startup.

The default profile contract is covered by
`internal/runtimeprofile/profile_test.go` and applies to both
`internal/runtimeprofile/profiles/normal.default.json` and
`internal/runtimeprofile/profiles/router.default.json`:

```yaml
geodata-mode: true
geodata-loader: memconservative
geo-auto-update: true
geo-update-interval: 24
etag-support: true
geox-url:
  geoip: "https://gh-proxy.com/https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat"
  geosite: "https://gh-proxy.com/https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat"
  mmdb: "https://gh-proxy.com/https://raw.githubusercontent.com/alecthw/mmdb_china_ip_list/release/Country.mmdb"
  asn: "https://gh-proxy.com/https://github.com/xishang0128/geoip/releases/latest/download/GeoLite2-ASN.mmdb"
```

The release builder still uses multiple mirrors while creating base assets.
That build-time fallback chain is not exposed to Mihomo. The rendered runtime
config writes a single URL per Mihomo `geox-url` key because Mihomo accepts only
one URL for each resource type.

## Boundaries

localClash does not run a local HTTP mirror/proxy service for GEO data. The
core-owned output is `.runtime/mihomo/config.yaml`; any mirror choice must be resolved
before writing that file.

Mihomo behavior:

- Missing or corrupt GEO files can trigger synchronous download during GEO rule
  initialization.
- `geo-auto-update: true` registers a background update loop using
  `geo-update-interval`.
- Successful GEO updates refresh Mihomo GEO matchers/readers; they do not
  restart the Mihomo process or re-apply the entire generated config.
- Existing connections should not be assumed to be re-routed. New matching work
  uses refreshed data after Mihomo rebuilds its GEO readers/caches.

## Release Assets

`localclash-base-assets.tar.gz` contains:

```text
policy-templates/
rule-sources/
.runtime/mihomo/Country.mmdb
.runtime/mihomo/geoip.dat
.runtime/mihomo/geosite.dat
.runtime/mihomo/ASN.mmdb
```

The LuCI helper installs those base assets from the core release manifest before
first-use config rendering. `internal/baseassets.Status` treats all four GEO
files, the default policy template, default patch files, and at least one rule
source JSON as required base assets.

## Remaining Product Work

The current implementation intentionally keeps runtime `geox-url` simple and
deterministic. Future work, if needed, should stay explicit:

- Add doctor checks for missing, empty, or unreadable GEO files under
  `.runtime/mihomo/`.
- Add a dedicated `geodata update` or `geodata repair` command only if it can
  share the same resource metadata as release/base-asset bootstrap.
- If render-time mirror selection is added, expose the selected URL and reason
  in status/doctor output instead of silently replacing upstream URLs.

## Verification

Use these checks after changing GEO defaults or release assets:

```bash
rtk go test ./internal/runtimeprofile/... ./internal/baseassets/...
rtk go run . config render --force
rtk go run . doctor --json
```
