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
