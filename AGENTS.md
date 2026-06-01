# Repository Guidelines

## Project Structure & Module Organization

`localClash` is a Go module for managing a local Mihomo runtime through CLI and MCP surfaces. The root `main.go` owns command routing. Feature code lives under `internal/`, grouped by responsibility: `mcp/` for MCP registry/server behavior, `appinit/` for shared bootstrap state, `configrender/` and `configplan/` for generated configs, `rules/` for rule-source adapters and packs, `doctor/` for diagnostics, and download/runtime helpers in `coredownload/`, `subdownload/`, `dashboard/`, and `corerun/`. Repository docs live in `docs/`; static policy-template inputs are in `policy-templates/` and static rule-source inputs are in `rule-sources/`. Treat `.runtime/`, `generated/`, `bin/`, `subscription*.gob`, and `localclash-subscriptions.json` as local artifacts or secrets, not source.

## LuCI Project Relationship

`/Volumes/Data/Github/localclash-luci` is the sibling OpenWrt LuCI surface for this core project. Keep ownership explicit: this repository owns the Go runtime, product-level CLI/MCP API, subscriptions, config rendering, runtime lifecycle, release manifests, and router-safe diagnostics; the LuCI repository owns UI, package, ACL, menu, and helper files that call those product surfaces. Start future LuCI maintenance tasks from this core project so agents first verify the current core contract, release/manifest state, and runtime behavior before changing the LuCI UI/package layer. When edits must land in `../localclash-luci`, keep them scoped there and do not move runtime logic, generated runtime artifacts, or Mihomo/dashboard assets into the LuCI package.

## Mihomo Core Source Checkouts

The local source checkout for the Mihomo Meta core is `/Volumes/Data/Github/mihomo-Meta`. The local source checkout for the Mihomo Alpha Smart core is `/Volumes/Data/Github/mihomo-Alpha`. Use these sibling repositories for source inspection, upstream behavior checks, and core-specific debugging only; localClash still owns runtime selection, config rendering, lifecycle commands, and managed binary paths such as `bin/*/lc-mihomo-meta` and `bin/*/lc-mihomo-smart`.

## Build, Test, and Development Commands

- `rtk go test ./...`: run the full Go test suite.
- `rtk go run . mcp`: start the stdio MCP server.
- `rtk go run . doctor` or `rtk go run . doctor --json`: inspect local runtime prerequisites and generated config health.
- `rtk go run . core download --dry-run`: verify Mihomo release asset selection without writing binaries.
- `rtk go run . config render --force`: render `generated/mihomo.yaml` from local subscription, policy-template intent, and pack inputs.
- `rtk scripts/test-mcp-callcopilot.sh`: run the end-to-end Copilot MCP smoke test when the local MCP registration is configured.

## Coding Style & Naming Conventions

Use standard Go formatting: tabs via `gofmt`, short package names, and table-driven tests where practical. Keep command behavior explicit and deterministic; prefer typed structs and YAML parsing over ad hoc string manipulation. New MCP tools must include registry metadata, JSON input schema, safety level, server dispatch, and tests.

## Testing Guidelines

Place tests next to implementation as `*_test.go` files. Cover both success paths and safety/error boundaries, especially for config rendering, MCP inputs, filesystem writes, and secret-bearing local data. Run `rtk go test ./...` before handoff; use `doctor --json` when validating runtime-facing changes.

## Commit & Pull Request Guidelines

Recent commits use short imperative subjects such as `Add MCP config plan render tool` and `Trim MCP product tool surface`. Follow that style, keep commits focused, and avoid mixing generated/runtime artifacts with source changes. PRs should describe behavior changes, list verification commands, call out safety-level changes for MCP tools, and include screenshots only for dashboard/UI-facing work.

## Agent-Specific Instructions

Prefix shell commands with `rtk`. For debugging, inspect logs, config state, diagnostics, or MCP responses before changing code. For browser automation, prefer the existing ARC CDP endpoint at `http://localhost:9222` after a quick availability check.
