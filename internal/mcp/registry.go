package mcp

import (
	"encoding/json"
	"io"
	"sort"
)

type SafetyLevel string

const (
	SafeRead        SafetyLevel = "safe_read"
	SafeWrite       SafetyLevel = "safe_write"
	ConfirmRequired SafetyLevel = "confirm_required"
	HighRisk        SafetyLevel = "high_risk"
)

type Tool struct {
	Name        string      `json:"name"`
	SafetyLevel SafetyLevel `json:"safety_level"`
	Description string      `json:"description"`
}

type ListedTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	SafetyLevel SafetyLevel    `json:"safety_level"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

type ToolSummary struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	SafetyLevel SafetyLevel `json:"safety_level"`
}

type ToolsListResult struct {
	Tools            []ToolSummary `json:"tools"`
	Count            int           `json:"count"`
	ClientNamingNote string        `json:"client_naming_note,omitempty"`
}

func Registry() []Tool {
	tools := []Tool{
		{Name: "config_base_inspect", SafetyLevel: SafeRead, Description: "Inspect generated base config summary without exposing proxy credentials."},
		{Name: "config_overlay_inspect", SafetyLevel: SafeRead, Description: "Inspect localClash overlay metadata and summaries."},
		{Name: "doctor", SafetyLevel: SafeRead, Description: "Run read-only localClash diagnostics."},
		{Name: "packs_get", SafetyLevel: SafeRead, Description: "Read details for one generated rule pack cache entry."},
		{Name: "packs_list", SafetyLevel: SafeRead, Description: "List and filter generated rule pack cache entries."},
		{Name: "subscription_nodes_list", SafetyLevel: SafeRead, Description: "List safe subscription proxy name/type summaries without exposing connection credentials."},
		{Name: "subscription_nodes_search", SafetyLevel: SafeRead, Description: "Search subscription proxy names and return safe name/type summaries; does not verify network egress location."},
		{Name: "subscriptions_status", SafetyLevel: SafeRead, Description: "Inspect configured subscription sources and local effective subscription state."},
		{Name: "runtime_status", SafetyLevel: SafeRead, Description: "Inspect Mihomo runtime status from the local PID file without changing runtime state."},
		{Name: "tools_list", SafetyLevel: SafeRead, Description: "List localClash MCP tools as ordinary tool output for clients that do not expose MCP registry introspection to the model."},
		{Name: "virtual_nodes_get", SafetyLevel: SafeRead, Description: "Inspect candidates for one node-label virtual node."},
		{Name: "virtual_nodes_list", SafetyLevel: SafeRead, Description: "List node-label virtual nodes inferred from subscription proxy names."},
		{Name: "config_plan_render", SafetyLevel: SafeWrite, Description: "Render a candidate Mihomo config from a complete desired overlay into .runtime/plans."},
		{Name: "config_render", SafetyLevel: SafeWrite, Description: "Render generated Mihomo config from reviewed local inputs."},
		{Name: "subscriptions_configure", SafetyLevel: SafeWrite, Description: "Write local subscription source configuration without refreshing."},
		{Name: "subscriptions_refresh", SafetyLevel: SafeWrite, Description: "Refresh configured subscription sources into local artifacts and effective subscription.yaml."},
		{Name: "run_runtime", SafetyLevel: ConfirmRequired, Description: "Start the Mihomo runtime from generated config. Requires external Agent/MCP client confirmation; starting or restarting the proxy runtime may temporarily interrupt network connectivity, and the Agent itself may be disconnected if it depends on the current network/proxy path."},
		{Name: "stop_runtime", SafetyLevel: ConfirmRequired, Description: "Stop the Mihomo runtime recorded by the local PID file. Requires external Agent/MCP client confirmation because stopping the proxy runtime may interrupt network connectivity."},
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools
}

func WriteRegistry(w io.Writer) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(Registry())
}

func ListedTools() []ListedTool {
	tools := Registry()
	out := make([]ListedTool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, ListedTool{
			Name:        tool.Name,
			Description: tool.Description,
			SafetyLevel: tool.SafetyLevel,
			InputSchema: inputSchemaForTool(tool.Name),
			Annotations: map[string]any{
				"safety_level": tool.SafetyLevel,
			},
		})
	}
	return out
}

func ToolSummaries() ToolsListResult {
	tools := Registry()
	summaries := make([]ToolSummary, 0, len(tools))
	for _, tool := range tools {
		summaries = append(summaries, ToolSummary{
			Name:        tool.Name,
			Description: tool.Description,
			SafetyLevel: tool.SafetyLevel,
		})
	}
	return ToolsListResult{
		Tools:            summaries,
		Count:            len(summaries),
		ClientNamingNote: "Some clients prefix MCP tool names with the server id, for example localclash_doctor.",
	}
}

func inputSchemaForTool(name string) map[string]any {
	switch name {
	case "tools_list":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           map[string]any{},
		}
	case "config_plan_render":
		packIntent := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"id":     map[string]any{"type": "string", "description": "Pack id, for example blackmatrix7_OpenAI or sukkaw_ai_non_ip."},
				"target": map[string]any{"type": "string", "description": "Desired rule target, for example DIRECT, REJECT, PROXY, or AI."},
			},
			"required": []string{"id", "target"},
		}
		virtualTargetIntent := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"id":          map[string]any{"type": "string", "description": "Virtual target id, for example AI."},
				"node_labels": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Node label ids to collect candidates from."},
				"mode":        map[string]any{"type": "string", "enum": []string{"manual", "auto"}, "description": "Materialized runtime group mode."},
			},
			"required": []string{"id", "node_labels", "mode"},
		}
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"plan_name":    map[string]any{"type": "string", "description": "Human-readable plan slug prefix."},
				"subscription": map[string]any{"type": "string", "description": "Subscription YAML path. Defaults to subscription.yaml."},
				"policy":       map[string]any{"type": "string", "description": "Policy YAML path. Defaults to policies/loyalsoldier.yaml."},
				"mode":         map[string]any{"type": "string", "description": "Policy render mode. Defaults to the policy default."},
				"rules_cache":  map[string]any{"type": "string", "description": "Pack cache directory. Defaults to .runtime/rules/packs."},
				"output_dir":   map[string]any{"type": "string", "description": "Plan artifact root. Defaults to .runtime/plans."},
				"test":         map[string]any{"type": "boolean", "description": "Run Mihomo config test. Defaults to true."},
				"overlay": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"packs":           map[string]any{"type": "array", "items": packIntent},
						"virtual_targets": map[string]any{"type": "array", "items": virtualTargetIntent},
					},
					"required": []string{"packs"},
				},
			},
			"required": []string{"overlay"},
		}
	case "config_base_inspect", "config_overlay_inspect":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config": map[string]any{"type": "string", "description": "Mihomo config YAML path. Defaults to generated/mihomo.yaml."},
				"limit":  map[string]any{"type": "integer", "minimum": 1, "description": "Maximum summary entries per section."},
			},
		}
	case "run_runtime":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config":      map[string]any{"type": "string", "description": "Mihomo generated config path. Defaults to generated/mihomo.yaml."},
				"runtime_dir": map[string]any{"type": "string", "description": "Mihomo runtime data directory. Defaults to .runtime/mihomo."},
				"core":        map[string]any{"type": "string", "description": "Mihomo core binary path. Defaults to bin/mihomo."},
				"foreground":  map[string]any{"type": "boolean", "description": "Foreground mode is not supported by MCP run_runtime; use CLI run for foreground execution."},
				"log_file":    map[string]any{"type": "string", "description": "Runtime log file. Defaults to .runtime/mihomo/mihomo.log."},
			},
		}
	case "runtime_status":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config":      map[string]any{"type": "string", "description": "Mihomo generated config path. Defaults to generated/mihomo.yaml."},
				"runtime_dir": map[string]any{"type": "string", "description": "Mihomo runtime data directory. Defaults to .runtime/mihomo."},
				"log_file":    map[string]any{"type": "string", "description": "Runtime log file. Defaults to .runtime/mihomo/mihomo.log."},
			},
		}
	case "stop_runtime":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"runtime_dir": map[string]any{"type": "string", "description": "Mihomo runtime data directory. Defaults to .runtime/mihomo."},
				"timeout_ms":  map[string]any{"type": "integer", "minimum": 0, "description": "Milliseconds to wait after SIGTERM before reporting timeout. Defaults to 5000."},
				"force":       map[string]any{"type": "boolean", "description": "Send SIGKILL if the runtime does not exit before timeout. Defaults to false."},
			},
		}
	case "subscriptions_status":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config":      map[string]any{"type": "string", "description": "Subscription sources config path. Defaults to localclash-subscriptions.yaml."},
				"merged":      map[string]any{"type": "string", "description": "Effective merged subscription path. Defaults to subscription.yaml."},
				"runtime_dir": map[string]any{"type": "string", "description": "Runtime source artifact directory. Defaults to .runtime/subscriptions."},
			},
		}
	case "subscription_nodes_list":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"subscription": map[string]any{"type": "string", "description": "Subscription YAML path. Defaults to subscription.yaml."},
				"limit":        map[string]any{"type": "integer", "minimum": 0, "description": "Maximum returned proxy summaries. Defaults to 100."},
			},
		}
	case "subscription_nodes_search":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"subscription":   map[string]any{"type": "string", "description": "Subscription YAML path. Defaults to subscription.yaml."},
				"query":          map[string]any{"type": "string", "description": "Literal substring to match against subscription proxy names."},
				"patterns":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Regular expressions to match against subscription proxy names."},
				"case_sensitive": map[string]any{"type": "boolean", "description": "Use case-sensitive query and pattern matching. Defaults to false."},
				"limit":          map[string]any{"type": "integer", "minimum": 0, "description": "Maximum returned proxy summaries. Defaults to 50."},
			},
		}
	case "subscriptions_configure":
		source := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"id":  map[string]any{"type": "string", "description": "Source id. Only letters, digits, underscore, and hyphen are allowed."},
				"url": map[string]any{"type": "string", "description": "HTTP or HTTPS Clash/Mihomo subscription URL."},
			},
			"required": []string{"id", "url"},
		}
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config":  map[string]any{"type": "string", "description": "Subscription sources config path. Defaults to localclash-subscriptions.yaml."},
				"sources": map[string]any{"type": "array", "items": source, "description": "Complete subscription source list to write."},
				"replace": map[string]any{"type": "boolean", "description": "Replace existing sources. Defaults to true; false is not supported in the first version."},
			},
			"required": []string{"sources"},
		}
	case "subscriptions_refresh":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config":      map[string]any{"type": "string", "description": "Subscription sources config path. Defaults to localclash-subscriptions.yaml."},
				"ids":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional source ids to refresh. Defaults to all."},
				"runtime_dir": map[string]any{"type": "string", "description": "Runtime source artifact directory. Defaults to .runtime/subscriptions."},
				"merged":      map[string]any{"type": "string", "description": "Effective merged subscription path. Defaults to subscription.yaml."},
				"force":       map[string]any{"type": "boolean", "description": "Reserved compatibility flag. Existing artifacts are replaced."},
				"user_agent":  map[string]any{"type": "string", "description": "Subscription request User-Agent. Defaults to a Clash/Mihomo-like user agent."},
			},
		}
	case "packs_list":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"source": map[string]any{"type": "string", "description": "Exact pack source, for example sukkaw or blackmatrix7."},
				"name":   map[string]any{"type": "string", "description": "Case-insensitive substring filter for pack name or id."},
				"target": map[string]any{"type": "string", "description": "Exact target filter, for example DIRECT, REJECT, or AI."},
				"limit":  map[string]any{"type": "integer", "minimum": 1},
			},
		}
	case "packs_get":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "Catalog pack id, for example blackmatrix7_OpenAI."},
			},
			"required": []string{"id"},
		}
	case "virtual_nodes_list":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"subscription":  map[string]any{"type": "string", "description": "Subscription YAML path. Defaults to subscription.yaml."},
				"selection":     map[string]any{"type": "string", "description": "Packs selection YAML path. Defaults to localclash-packs.yaml with example fallback."},
				"include_empty": map[string]any{"type": "boolean", "description": "Include labels with no matched proxy names."},
				"sample_limit":  map[string]any{"type": "integer", "minimum": 0, "description": "Maximum sample nodes per virtual node. Defaults to 5."},
			},
		}
	case "virtual_nodes_get":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"id":           map[string]any{"type": "string", "description": "Node label id, for example SG, JP, or US."},
				"subscription": map[string]any{"type": "string", "description": "Subscription YAML path. Defaults to subscription.yaml."},
				"selection":    map[string]any{"type": "string", "description": "Packs selection YAML path. Defaults to localclash-packs.yaml with example fallback."},
				"limit":        map[string]any{"type": "integer", "minimum": 0, "description": "Maximum returned candidate nodes. Defaults to 50."},
			},
			"required": []string{"id"},
		}
	default:
		return map[string]any{
			"type":                 "object",
			"additionalProperties": true,
		}
	}
}
