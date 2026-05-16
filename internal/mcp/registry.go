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
		{Name: "doctor", SafetyLevel: SafeRead, Description: "Run read-only localClash diagnostics."},
		{Name: "inspect_generated_config", SafetyLevel: SafeRead, Description: "Inspect generated Mihomo config structure without applying changes."},
		{Name: "rules_adapt", SafetyLevel: SafeRead, Description: "Adapt rule sources into runtime pack cache."},
		{Name: "rules_render", SafetyLevel: SafeRead, Description: "Render selected rule packs into a rules fragment."},
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
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": true,
			},
			Annotations: map[string]any{
				"safety_level": tool.SafetyLevel,
			},
		})
	}
	return out
}
