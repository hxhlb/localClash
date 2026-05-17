package mcp

import (
	"strings"
	"testing"
)

func TestRegistryIncludesSafetyLevels(t *testing.T) {
	tools := Registry()
	byName := map[string]Tool{}
	for _, tool := range tools {
		byName[tool.Name] = tool
	}

	if byName["doctor"].SafetyLevel != SafeRead {
		t.Fatalf("doctor safety = %q, want %q", byName["doctor"].SafetyLevel, SafeRead)
	}
	if byName["config_base_inspect"].SafetyLevel != SafeRead {
		t.Fatalf("config_base_inspect safety = %q, want %q", byName["config_base_inspect"].SafetyLevel, SafeRead)
	}
	if byName["config_overlay_inspect"].SafetyLevel != SafeRead {
		t.Fatalf("config_overlay_inspect safety = %q, want %q", byName["config_overlay_inspect"].SafetyLevel, SafeRead)
	}
	if byName["packs_list"].SafetyLevel != SafeRead {
		t.Fatalf("packs_list safety = %q, want %q", byName["packs_list"].SafetyLevel, SafeRead)
	}
	if byName["packs_get"].SafetyLevel != SafeRead {
		t.Fatalf("packs_get safety = %q, want %q", byName["packs_get"].SafetyLevel, SafeRead)
	}
	if byName["subscriptions_status"].SafetyLevel != SafeRead {
		t.Fatalf("subscriptions_status safety = %q, want %q", byName["subscriptions_status"].SafetyLevel, SafeRead)
	}
	if byName["runtime_status"].SafetyLevel != SafeRead {
		t.Fatalf("runtime_status safety = %q, want %q", byName["runtime_status"].SafetyLevel, SafeRead)
	}
	if byName["tools_list"].SafetyLevel != SafeRead {
		t.Fatalf("tools_list safety = %q, want %q", byName["tools_list"].SafetyLevel, SafeRead)
	}
	if byName["subscription_nodes_list"].SafetyLevel != SafeRead {
		t.Fatalf("subscription_nodes_list safety = %q, want %q", byName["subscription_nodes_list"].SafetyLevel, SafeRead)
	}
	if byName["subscription_nodes_search"].SafetyLevel != SafeRead {
		t.Fatalf("subscription_nodes_search safety = %q, want %q", byName["subscription_nodes_search"].SafetyLevel, SafeRead)
	}
	if byName["virtual_nodes_list"].SafetyLevel != SafeRead {
		t.Fatalf("virtual_nodes_list safety = %q, want %q", byName["virtual_nodes_list"].SafetyLevel, SafeRead)
	}
	if byName["virtual_nodes_get"].SafetyLevel != SafeRead {
		t.Fatalf("virtual_nodes_get safety = %q, want %q", byName["virtual_nodes_get"].SafetyLevel, SafeRead)
	}
	if byName["config_plan_render"].SafetyLevel != SafeWrite {
		t.Fatalf("config_plan_render safety = %q, want %q", byName["config_plan_render"].SafetyLevel, SafeWrite)
	}
	if byName["config_render"].SafetyLevel != SafeWrite {
		t.Fatalf("config_render safety = %q, want %q", byName["config_render"].SafetyLevel, SafeWrite)
	}
	if byName["subscriptions_configure"].SafetyLevel != SafeWrite {
		t.Fatalf("subscriptions_configure safety = %q, want %q", byName["subscriptions_configure"].SafetyLevel, SafeWrite)
	}
	if byName["subscriptions_refresh"].SafetyLevel != SafeWrite {
		t.Fatalf("subscriptions_refresh safety = %q, want %q", byName["subscriptions_refresh"].SafetyLevel, SafeWrite)
	}
	if byName["run_runtime"].SafetyLevel != ConfirmRequired {
		t.Fatalf("run_runtime safety = %q, want %q", byName["run_runtime"].SafetyLevel, ConfirmRequired)
	}
	if byName["stop_runtime"].SafetyLevel != ConfirmRequired {
		t.Fatalf("stop_runtime safety = %q, want %q", byName["stop_runtime"].SafetyLevel, ConfirmRequired)
	}
	if !strings.Contains(byName["run_runtime"].Description, "network connectivity") || !strings.Contains(byName["run_runtime"].Description, "Agent itself") {
		t.Fatalf("run_runtime description missing network risk: %q", byName["run_runtime"].Description)
	}
	for _, name := range []string{"config_test", "inspect_generated_config", "rules_adapt", "rules_render", "switch_proxy_group", "apply_router_config"} {
		if byName[name].Name != "" {
			t.Fatalf("removed tool %q should not be registered", name)
		}
	}
}
