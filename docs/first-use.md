# First Use

This guide gets a fresh localClash checkout to a running local Mihomo runtime.
Run commands from the repository root. In Codex/local automation sessions, prefix
commands with `rtk`; otherwise `go run . ...` is enough.

## Prerequisites

- Go is installed and can build this module.
- You have a Clash/Mihomo subscription URL.
- The subscription URL is secret. Do not commit `subscription*.gob`,
  `localclash-subscriptions.json`, `localclash-intent.json`, or `.runtime/`.

## 1. Download the Host Core

Preview the selected host core:

```bash
go run . core download --dry-run
```

Download it:

```bash
go run . core download --force
```

On macOS arm64 this writes `bin/darwin-arm64/mihomo-meta`. The default host
target downloads only the host `meta` core; it does not download Linux Smart
cores.

## 2. Download the Dashboard

```bash
go run . dashboard download --force
```

This installs zashboard under `.runtime/mihomo/ui/zashboard`. Generated configs
use `external-ui: ui/zashboard`.

## 3. Add a Subscription

For a quick CLI-only first run:

```bash
go run . subscription download --url "https://example.com/sub?token=..." --output subscription.gob --force
```

For the MCP-managed path, start the server and ask the agent to use
`subscriptions_configure`, then `subscriptions_refresh`:

```bash
go run . mcp
```

The MCP path stores source URLs in `localclash-subscriptions.json` and produces
the merged `subscription.gob`. When more than one source is configured, merged
proxy names are prefixed with `[source-id]` so Dashboard and MCP selectors can
show where each node came from. A single source keeps the provider's original
node names unless a local duplicate still needs disambiguation.

## 4. Render and Validate Config

```bash
go run . config render --force
go run . doctor
```

`config render` writes `generated/mihomo.yaml`. `doctor` checks local files,
dashboard state, proxy-group references, and runs Mihomo config validation.

## 5. Start Mihomo

```bash
go run . run
```

Logs are written under `.runtime/mihomo/logs/`. After startup, open:

```text
http://127.0.0.1:9090/ui
```

## Router or Smart Core

Router deployment must be explicit:

```bash
go run . core download --target router --arch arm64 --force
```

This downloads Linux router cores to `bin/linux-arm64/mihomo-meta` and
`bin/linux-arm64/mihomo-smart`. Use MCP `environment_inspect` before router
changes, then `config_configure` with `runtime_profile: router` and
`core: smart` when that is the intended runtime.

## Factory Reset

To return to an installed-but-unconfigured state:

```bash
go run . reset
```

The reset command deletes local runtime state and user configuration, but keeps
downloaded binaries, source files, policies, rule sources, and docs.
