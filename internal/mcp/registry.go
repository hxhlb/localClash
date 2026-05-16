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
