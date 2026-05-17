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
	if byName["environment_inspect"].SafetyLevel != SafeRead {
		t.Fatalf("environment_inspect safety = %q, want %q", byName["environment_inspect"].SafetyLevel, SafeRead)
	}
	if byName["config_status"].SafetyLevel != SafeRead {
		t.Fatalf("config_status safety = %q, want %q", byName["config_status"].SafetyLevel, SafeRead)
	}
	if byName["nl_file"].SafetyLevel != SafeRead {
		t.Fatalf("nl_file safety = %q, want %q", byName["nl_file"].SafetyLevel, SafeRead)
	}
	if byName["packs_list"].SafetyLevel != SafeRead {
		t.Fatalf("packs_list safety = %q, want %q", byName["packs_list"].SafetyLevel, SafeRead)
	}
	if byName["packs_get"].SafetyLevel != SafeRead {
		t.Fatalf("packs_get safety = %q, want %q", byName["packs_get"].SafetyLevel, SafeRead)
	}
	if byName["pack_rules_query"].SafetyLevel != SafeRead {
		t.Fatalf("pack_rules_query safety = %q, want %q", byName["pack_rules_query"].SafetyLevel, SafeRead)
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
	if byName["config_patch_apply"].SafetyLevel != SafeWrite {
		t.Fatalf("config_patch_apply safety = %q, want %q", byName["config_patch_apply"].SafetyLevel, SafeWrite)
	}
	if !strings.Contains(byName["config_patch_apply"].Description, "generated/mihomo.yaml") {
		t.Fatalf("config_patch_apply description should mention generated config: %q", byName["config_patch_apply"].Description)
	}
	if byName["config_patch_create"].SafetyLevel != SafeWrite {
		t.Fatalf("config_patch_create safety = %q, want %q", byName["config_patch_create"].SafetyLevel, SafeWrite)
	}
	if byName["config_render"].SafetyLevel != SafeWrite {
		t.Fatalf("config_render safety = %q, want %q", byName["config_render"].SafetyLevel, SafeWrite)
	}
	if byName["proxy_group_build"].SafetyLevel != SafeWrite {
		t.Fatalf("proxy_group_build safety = %q, want %q", byName["proxy_group_build"].SafetyLevel, SafeWrite)
	}
	if byName["custom_rules_build"].SafetyLevel != SafeWrite {
		t.Fatalf("custom_rules_build safety = %q, want %q", byName["custom_rules_build"].SafetyLevel, SafeWrite)
	}
	if byName["pack_rules_prefetch"].SafetyLevel != SafeWrite {
		t.Fatalf("pack_rules_prefetch safety = %q, want %q", byName["pack_rules_prefetch"].SafetyLevel, SafeWrite)
	}
	if byName["pack_rules_read"].SafetyLevel != SafeWrite {
		t.Fatalf("pack_rules_read safety = %q, want %q", byName["pack_rules_read"].SafetyLevel, SafeWrite)
	}
	if byName["sed_file"].SafetyLevel != SafeWrite {
		t.Fatalf("sed_file safety = %q, want %q", byName["sed_file"].SafetyLevel, SafeWrite)
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
	for _, name := range []string{"config_base_inspect", "config_intent_inspect", "config_overlay_inspect", "config_draft_apply", "config_draft_render", "config_test", "config_plan_apply", "config_plan_render", "inspect_generated_config", "rules_adapt", "rules_render", "switch_proxy_group", "apply_router_config"} {
		if byName[name].Name != "" {
			t.Fatalf("removed tool %q should not be registered", name)
		}
	}
}
