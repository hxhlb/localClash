# localClash

Natural-language traffic policy tooling for Clash, Mihomo, and OpenClash.

## Direction

The project is intended to become a local binary that can expose:

- CLI commands for inspection, planning, compilation, and validation.
- A local HTTP/SSE API for a web UI.
- An MCP server for agent integrations.
- Router adapters for OpenClash workflows.

## Safety Boundary

Models should produce policy intent, not edit Clash YAML directly. A deterministic compiler should turn reviewed intent into Clash/OpenClash artifacts with validation, diff preview, and rollback support.

## Local Data

Do not commit downloaded subscriptions, active router profiles, generated configs, or files containing node credentials.

## Core Download

Download the matching Mihomo core for the machine running the command:

```bash
go run . core download
```

By default the command detects the current OS and CPU architecture, selects the matching `MetaCubeX/mihomo` release asset, decompresses it, and writes the binary to `bin/mihomo` or `bin/mihomo.exe`.

To inspect the selected release asset without downloading:

```bash
go run . core download --dry-run
```

To download a core for another target, pass `--os` and `--arch` explicitly:

```bash
go run . core download --os linux --arch arm64 --output bin/clash_meta
```

## Subscription Download

Download a subscription with a Clash-compatible User-Agent:

```bash
go run . subscription download --url "https://example.com/playlist?token=..." --output subscription.yaml --force
```

The default User-Agent is `clash-verge/v1.5.1`, matching the known OpenClash subscription setting. The downloaded subscription file is local data and should not be committed.

## Dashboard

Download the zashboard static UI for Mihomo:

```bash
go run . dashboard download --force
```

The command downloads the default `dist.zip` release asset. The default output is `.runtime/mihomo/ui/zashboard`. Rendered configs set `external-ui: ui/zashboard`, so after `go run . run` the dashboard is available at:

```text
http://127.0.0.1:9090/ui
```

## Config Render

Render a runtime Mihomo config from a downloaded subscription source and a local policy:

```bash
go run . config render --force
```

The default render path is `generated/mihomo.yaml`. The renderer treats the subscription as a proxy source and owns the runtime rules, rule providers, and proxy groups locally.

Test the generated config:

```bash
./bin/mihomo -d .runtime/mihomo -f generated/mihomo.yaml -t
```

Run the generated config:

```bash
go run . run
```

By default this is equivalent to:

```bash
./bin/mihomo -d .runtime/mihomo -f generated/mihomo.yaml
```

Mihomo output is also appended to a dated log file under `.runtime/mihomo/logs/`, for example `.runtime/mihomo/logs/mihomo-2026-05-15.log`. Override the path with `--log`. Dated logs are retained for 7 days by default; use `--log-retention` to change this.

## Doctor

Run a read-only diagnostic report for the local core, subscription, generated config, policy, dashboard, rule references, and Mihomo config test:

```bash
go run . doctor
```

Machine-readable output for the future web UI:

```bash
go run . doctor --json
```
