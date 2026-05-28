# Router Incident Register

This document records router-facing usability and performance incidents that
must be investigated with evidence. Do not treat post-removal or wrong-window
samples as proof for an incident.

## 2026-05-29 DHCP Hostname DNS Hijack Regression

Observed symptom:

- LAN hostnames learned by OpenWrt DHCP, for example `Ronnie-PC`, could not be
  pinged by name while localClash router takeover was active.
- The same host was reachable by private IP, so the failure was not ICMP or
  basic LAN routing.

Evidence:

- The router DHCP lease table contained `Ronnie-PC` with a `192.168.6.x`
  address.
- `ping 192.168.6.x` from a LAN client succeeded.
- `nslookup Ronnie-PC 192.168.6.1` from the router returned the DHCP address,
  proving dnsmasq could answer when queried through the router LAN address.
- `dig @192.168.6.1 Ronnie-PC A` from a LAN client returned `NXDOMAIN`.
- The nft `localClash DNS hijack` counter increased during that LAN-client DNS
  query, proving the query was redirected to Mihomo DNS before dnsmasq could
  answer it.
- The active Mihomo DNS config listened on `0.0.0.0:7874` and used public DoH
  upstreams plus `geosite:gfw` policy, but did not have DHCP lease awareness or
  a local dnsmasq policy for DHCP hostnames.

Current explanation:

- Router takeover installs a broad prerouting DNS hijack:
  `meta l4proto { tcp, udp } th dport 53 redirect to :7874`.
- That rule captures client DNS queries even when the destination is the router
  LAN DNS service at `192.168.6.1:53`.
- Mihomo receives `Ronnie-PC` / `.lan` lookups but cannot answer from dnsmasq's
  DHCP lease table, so the client sees `NXDOMAIN`.

Product requirement:

- Router takeover must preserve OpenWrt local resolver behavior for DHCP
  hostnames, `.lan`, `.local`, `.home.arpa`, reverse private zones, and other
  LAN-local names.
- DNS hijack must not turn local hostname lookups into public-DNS lookups.
- A future fix should either bypass router-destined DNS traffic before the
  hijack rule or configure Mihomo DNS to forward local zones to the router's
  local resolver. The implementation must be validated from a LAN client, not
  only from the router shell.

Required verification for the fix:

- From a LAN client, `dig @192.168.6.1 Ronnie-PC A` returns the DHCP address.
- From a LAN client, `ping Ronnie-PC` resolves and reaches the same private IP.
- The verification records whether the query bypassed Mihomo DNS or was
  forwarded through Mihomo to dnsmasq.
- Public DNS hijack for ordinary client traffic still works after the local
  hostname path is restored.

## 2026-05-25 CPU and Runtime Incidents

### localClash CPU Occupancy

Observed symptom:

- On the real router, localClash was reported to sometimes hold CPU near 100%.
- The router became difficult to operate, and localClash had to be removed to
  restore normal network usage.

Evidence boundary:

- A CPU sample taken after localClash had already been removed or stopped is not
  valid evidence for this incident.
- Docker OpenWrt did not reproduce the same severe CPU behavior, so the issue is
  likely tied to real-router workload, hardware, process supervision, traffic,
  filesystem, DNS, or request behavior.

Open questions:

- Which process name and PID owned the CPU during the incident?
- Was the hot path MCP HTTP request handling, config rendering, subscription
  refresh, runtime control, DNS interaction, file IO, or a retry loop?
- Did LuCI ubus requests wait on localClash long enough to stack pressure on a
  slow router?

Required evidence for the next reproduction:

- timestamped `top` or `ps` samples with PID, command, `%CPU`, `%MEM`, RSS, and
  full command line
- localClash MCP request summaries with tool name, duration, result, and error
- runtime state transition logs around start, stop, restart, takeover apply, and
  takeover stop
- process supervision logs showing restarts, exits, or respawn loops

### Mihomo CPU and Warning Volume

Observed symptom:

- Mihomo CPU was reported to reach about 14% on the real router.
- The router also showed a large volume of Mihomo warning logs during the
  localClash-managed network period.

Evidence boundary:

- The warning batch was not fully captured before the network environment was
  switched back to OpenClash.
- A previous partial local sample saw warning classes around direct match
  reports, Telegram IP timeouts, and `8.8.8.8:853` DNS connection failures, but
  that sample must be treated as partial evidence, not a complete diagnosis.

Open questions:

- Are warnings caused by rule mismatch, unreachable upstream DNS, direct routing
  of blocked destinations, retry amplification, or dashboard/API polling?
- Does warning rate correlate with Mihomo CPU, localClash CPU, or DNS latency?
- Are the warnings materially affecting forwarding latency, or only producing
  logging overhead?

Required evidence for the next reproduction:

- warning rate by class over time
- Mihomo CPU samples in the same timestamp window
- active generated `mihomo.yaml` rule count and provider count
- DNS upstream errors and rule match samples for Telegram and other affected
  services

Collection entrypoint:

- `scripts/collect-mihomo-warnings.py` streams
  `http://192.168.6.1:9090/logs?level=info` by default and writes full log,
  warning subset, snapshot, summary, event, and error JSONL artifacts under
  `.runtime/diagnostics/`.
- Use `--level warning` only when the collection target is warning volume alone.
  Use the default `info` level when warning context and runtime state-transition
  lines are needed in the same window.
- The script is read-only against the Mihomo controller. Add
  `--ssh-host root@192.168.6.1` only when process samples are needed in the
  same time window.

### Smart Config-Test Isolation

Observed symptom:

- On the real router, Smart core config validation could report
  `[Smart] DB Cache file load failed` while the active transparent-proxy runtime
  was already serving traffic.
- The active Smart process used a relative runtime directory:
  `-d .runtime/mihomo` from `/root/localclash`, and held
  `.runtime/mihomo/cache.db` open.
- `runtime_status` could report the live process as not using the configured
  runtime directory when comparing configured absolute paths with the relative
  command-line `-d` value.

Safety boundary:

- Do not run `mihomo -t` directly against the live `.runtime/mihomo` directory
  while the router network depends on localClash.
- Do not restart, stop, or start the runtime merely to validate a candidate
  config during this incident class.
- Config validation should use an isolated temporary runtime directory populated
  only with validation artifacts such as `Model.bin`, geodata/mmdb files, and
  rule-provider data. Live `cache.db`, PID files, logs, and UI assets are not
  validation inputs and must not be copied.

Follow-up:

- Fix runtime status path matching separately by resolving process cwd plus
  relative `-d` arguments before deciding whether a live process belongs to the
  configured runtime directory.

### OpenClash Baseline

Observed baseline:

- The user reported OpenClash-managed Clash usually runs around 6% to 10% CPU,
  with occasional spikes around 56%.

Evidence boundary:

- A single sample outside the incident window is not enough to compare
  localClash with OpenClash.

Required baseline:

- collect 5 to 10 minutes of process samples under the same traffic pattern
- record process command, CPU, memory, and warning/error rate
- compare with localClash using the same router, subscription, traffic, and DNS
  state

### Telegram Regression

Observed symptom:

- An older localClash core-only or minimal configuration path could cover
  Telegram automatically.
- The newer ACL4SSR-like default configuration failed for Telegram in real use.

Current explanation:

- The new default relied on GEOSITE category routing for Telegram. Telegram
  clients can connect directly to Telegram IP ranges without exposing a domain
  or SNI that `GEOSITE,telegram` can match.
- The default template now adds a `GEOIP,telegram` custom rule targeting the
  communication policy group. Isolated `mihomo -t` validation on v1.19.25
  loaded the Telegram GeoIP rule with 12 records.

Required verification:

- render the default patch set
- confirm generated Mihomo rules include `GEOIP,telegram` before fallback
- run Telegram traffic and confirm it matches the expected communication group

### Logging Gap

Observed gap:

- localClash incidents could not be answered from durable logs on the real
  router.
- `/Volumes/Data/Github/localClash/.runtime/logs/claude-code-localclash-observe.log`
  is a Claude Code client debug log. It records Claude/MCP client setup and
  transport behavior, not localClash server-side runtime decisions.

Existing logging:

- The MCP HTTP server already writes concise `mcp_http` request summaries to
  stderr, including method, path, tool, redacted arguments, HTTP status,
  response status, and duration.
- `scripts/deploy-router.sh` installs a procd service that redirects MCP
  stdout/stderr to `.runtime/logs/localclash-mcp.log`.

Integration gap:

- The LuCI-installed service path does not currently persist MCP stdout/stderr
  to a bounded router log, so the existing MCP request logs can be lost.
- Runtime operations, config rendering, takeover state transitions, and Mihomo
  warning summaries need durable router-side observability during development.

Product requirement:

- Development and diagnosis builds should make router-side evidence easy to
  collect.
- Release defaults must stay cheap for thin clients: no unbounded hot-path logs,
  no verbose logs by default, and no expensive polling.

### Duplicate Log-File Direction

Decision:

- Do not add a second generic MCP `--log-file` mechanism to the core CLI just to
  solve this incident.
- The better fix is to make deployment paths consistently preserve existing
  stderr request logs and add targeted, bounded observability for runtime and
  config operations.

Reason:

- MCP service logging already exists at the server stderr layer.
- The observed gap is deployment integration and missing runtime diagnostics,
  not a lack of another CLI flag.
