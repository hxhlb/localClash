# Repository Guidelines

## Project Structure & Module Organization

`localClash` is a Go module for managing a local Mihomo runtime through CLI and MCP surfaces. The root `main.go` owns command routing. Feature code lives under `internal/`, grouped by responsibility: `mcp/` for MCP registry/server behavior, `appinit/` for shared bootstrap state, `configrender/` and `configplan/` for generated configs, `rules/` for rule-source adapters and packs, `doctor/` for diagnostics, and download/runtime helpers in `coredownload/`, `subdownload/`, `dashboard/`, and `corerun/`. Repository docs live in `docs/`; static policy inputs are in `policies/` and `rule-sources/`. Treat `.runtime/`, `generated/`, `bin/`, `subscription*.gob`, and `localclash-subscriptions.json` as local artifacts or secrets, not source.

## Build, Test, and Development Commands

- `rtk go test ./...`: run the full Go test suite.
- `rtk go run . mcp`: start the stdio MCP server.
- `rtk go run . doctor` or `rtk go run . doctor --json`: inspect local runtime prerequisites and generated config health.
- `rtk go run . core download --dry-run`: verify Mihomo release asset selection without writing binaries.
- `rtk go run . config render --force`: render `generated/mihomo.yaml` from local subscription, policy, and pack inputs.
- `rtk scripts/test-mcp-callcopilot.sh`: run the end-to-end Copilot MCP smoke test when the local MCP registration is configured.

## Coding Style & Naming Conventions

Use standard Go formatting: tabs via `gofmt`, short package names, and table-driven tests where practical. Keep command behavior explicit and deterministic; prefer typed structs and YAML parsing over ad hoc string manipulation. New MCP tools must include registry metadata, JSON input schema, safety level, server dispatch, and tests.

## Testing Guidelines

Place tests next to implementation as `*_test.go` files. Cover both success paths and safety/error boundaries, especially for config rendering, MCP inputs, filesystem writes, and secret-bearing local data. Run `rtk go test ./...` before handoff; use `doctor --json` when validating runtime-facing changes.

## Commit & Pull Request Guidelines

Recent commits use short imperative subjects such as `Add MCP config plan render tool` and `Trim MCP product tool surface`. Follow that style, keep commits focused, and avoid mixing generated/runtime artifacts with source changes. PRs should describe behavior changes, list verification commands, call out safety-level changes for MCP tools, and include screenshots only for dashboard/UI-facing work.

## Agent-Specific Instructions

Prefix shell commands with `rtk`. For debugging, inspect logs, config state, diagnostics, or MCP responses before changing code. For browser automation, prefer the existing ARC CDP endpoint at `http://localhost:9222` after a quick availability check.
