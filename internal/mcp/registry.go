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

func Registry() []Tool {
	tools := []Tool{
		{Name: "config_base_inspect", SafetyLevel: SafeRead, Description: "Inspect generated base config summary without exposing proxy credentials."},
		{Name: "config_overlay_inspect", SafetyLevel: SafeRead, Description: "Inspect localClash overlay metadata and summaries."},
		{Name: "doctor", SafetyLevel: SafeRead, Description: "Run read-only localClash diagnostics."},
		{Name: "inspect_generated_config", SafetyLevel: SafeRead, Description: "Inspect generated Mihomo config structure without applying changes."},
		{Name: "packs_get", SafetyLevel: SafeRead, Description: "Read details for one generated rule pack cache entry."},
		{Name: "packs_list", SafetyLevel: SafeRead, Description: "List and filter generated rule pack cache entries."},
		{Name: "rules_adapt", SafetyLevel: SafeRead, Description: "Adapt rule sources into runtime pack cache."},
		{Name: "rules_render", SafetyLevel: SafeRead, Description: "Render selected rule packs into a rules fragment."},
		{Name: "virtual_nodes_get", SafetyLevel: SafeRead, Description: "Inspect candidates for one node-label virtual node."},
		{Name: "virtual_nodes_list", SafetyLevel: SafeRead, Description: "List node-label virtual nodes inferred from subscription proxy names."},
		{Name: "config_plan_render", SafetyLevel: SafeWrite, Description: "Render a candidate Mihomo config from a complete desired overlay into .runtime/plans."},
		{Name: "config_render", SafetyLevel: SafeWrite, Description: "Render generated Mihomo config from reviewed local inputs."},
		{Name: "config_test", SafetyLevel: SafeWrite, Description: "Run Mihomo config validation against generated config."},
		{Name: "run_runtime", SafetyLevel: ConfirmRequired, Description: "Start Mihomo runtime from generated config."},
		{Name: "switch_proxy_group", SafetyLevel: ConfirmRequired, Description: "Change active runtime proxy group selection."},
		{Name: "apply_router_config", SafetyLevel: HighRisk, Description: "Apply generated config to a router/OpenClash target."},
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

func inputSchemaForTool(name string) map[string]any {
	switch name {
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
