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
	if byName["router_takeover_status"].SafetyLevel != SafeRead {
		t.Fatalf("router_takeover_status safety = %q, want %q", byName["router_takeover_status"].SafetyLevel, SafeRead)
	}
	if byName["routing_explain"].SafetyLevel != SafeRead {
		t.Fatalf("routing_explain safety = %q, want %q", byName["routing_explain"].SafetyLevel, SafeRead)
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
	if !strings.Contains(byName["config_patch_apply"].Description, ".runtime/mihomo/config.yaml") {
		t.Fatalf("config_patch_apply description should mention generated config: %q", byName["config_patch_apply"].Description)
	}
	if byName["config_patch_draft"].SafetyLevel != SafeWrite {
		t.Fatalf("config_patch_draft safety = %q, want %q", byName["config_patch_draft"].SafetyLevel, SafeWrite)
	}
	if byName["config_patch_get"].SafetyLevel != SafeRead {
		t.Fatalf("config_patch_get safety = %q, want %q", byName["config_patch_get"].SafetyLevel, SafeRead)
	}
	if byName["config_render"].SafetyLevel != SafeWrite {
		t.Fatalf("config_render safety = %q, want %q", byName["config_render"].SafetyLevel, SafeWrite)
	}
	if byName["mihomo_logs_read"].SafetyLevel != SafeRead {
		t.Fatalf("mihomo_logs_read safety = %q, want %q", byName["mihomo_logs_read"].SafetyLevel, SafeRead)
	}
	if byName["mihomo_connections_read"].SafetyLevel != SafeRead {
		t.Fatalf("mihomo_connections_read safety = %q, want %q", byName["mihomo_connections_read"].SafetyLevel, SafeRead)
	}
	if !strings.Contains(byName["mihomo_connections_read"].Description, "currently tracked active connections only") || !strings.Contains(byName["mihomo_connections_read"].Description, "absence of a domain is not proof") {
		t.Fatalf("mihomo_connections_read description missing active-connection boundary: %q", byName["mihomo_connections_read"].Description)
	}
	if !strings.Contains(byName["routing_explain"].Description, "config/intent evidence") || !strings.Contains(byName["routing_explain"].Description, "not proof that Mihomo has loaded") {
		t.Fatalf("routing_explain description missing intent/runtime boundary: %q", byName["routing_explain"].Description)
	}
	if byName["mihomo_api_request"].SafetyLevel != SafeWrite {
		t.Fatalf("mihomo_api_request safety = %q, want %q", byName["mihomo_api_request"].SafetyLevel, SafeWrite)
	}
	if !strings.Contains(byName["mihomo_api_request"].Description, "/providers/rules") || !strings.Contains(byName["mihomo_api_request"].Description, "prefer mihomo_connections_read") {
		t.Fatalf("mihomo_api_request description missing recommended runtime paths: %q", byName["mihomo_api_request"].Description)
	}
	if byName["mihomo_config_test"].SafetyLevel != SafeWrite {
		t.Fatalf("mihomo_config_test safety = %q, want %q", byName["mihomo_config_test"].SafetyLevel, SafeWrite)
	}
	if byName["proxy_group_build"].SafetyLevel != SafeWrite {
		t.Fatalf("proxy_group_build safety = %q, want %q", byName["proxy_group_build"].SafetyLevel, SafeWrite)
	}
	if byName["policy_group_build"].SafetyLevel != SafeWrite {
		t.Fatalf("policy_group_build safety = %q, want %q", byName["policy_group_build"].SafetyLevel, SafeWrite)
	}
	if byName["custom_rules_build"].SafetyLevel != SafeWrite {
		t.Fatalf("custom_rules_build safety = %q, want %q", byName["custom_rules_build"].SafetyLevel, SafeWrite)
	}
	if byName["rule_provider_build"].SafetyLevel != SafeWrite {
		t.Fatalf("rule_provider_build safety = %q, want %q", byName["rule_provider_build"].SafetyLevel, SafeWrite)
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
	if byName["restart_runtime"].SafetyLevel != ConfirmRequired {
		t.Fatalf("restart_runtime safety = %q, want %q", byName["restart_runtime"].SafetyLevel, ConfirmRequired)
	}
	if byName["stop_runtime"].SafetyLevel != ConfirmRequired {
		t.Fatalf("stop_runtime safety = %q, want %q", byName["stop_runtime"].SafetyLevel, ConfirmRequired)
	}
	if !strings.Contains(byName["stop_runtime"].Description, "router_takeover_stop") || !strings.Contains(byName["stop_runtime"].Description, "force=true") {
		t.Fatalf("stop_runtime description missing takeover guard guidance: %q", byName["stop_runtime"].Description)
	}
	if byName["router_takeover_apply"].SafetyLevel != ConfirmRequired {
		t.Fatalf("router_takeover_apply safety = %q, want %q", byName["router_takeover_apply"].SafetyLevel, ConfirmRequired)
	}
	if byName["router_takeover_stop"].SafetyLevel != ConfirmRequired {
		t.Fatalf("router_takeover_stop safety = %q, want %q", byName["router_takeover_stop"].SafetyLevel, ConfirmRequired)
	}
	if !strings.Contains(byName["run_runtime"].Description, "network connectivity") || !strings.Contains(byName["run_runtime"].Description, "Agent itself") {
		t.Fatalf("run_runtime description missing network risk: %q", byName["run_runtime"].Description)
	}
	if !strings.Contains(byName["restart_runtime"].Description, "hot reload") || !strings.Contains(byName["restart_runtime"].Description, "process restart") {
		t.Fatalf("restart_runtime description missing reload strategy guidance: %q", byName["restart_runtime"].Description)
	}
	for _, name := range []string{"config_base_inspect", "config_intent_inspect", "config_overlay_inspect", "config_draft_apply", "config_draft_render", "config_test", "config_plan_apply", "config_plan_render", "inspect_generated_config", "rules_adapt", "rules_render", "switch_proxy_group", "apply_router_config"} {
		if byName[name].Name != "" {
			t.Fatalf("removed tool %q should not be registered", name)
		}
	}
}

func TestRuntimeSchemasExposeForceConfigTest(t *testing.T) {
	for _, name := range []string{"run_runtime", "restart_runtime"} {
		schema := inputSchemaForTool(name)
		properties := schema["properties"].(map[string]any)
		field, ok := properties["force_config_test"].(map[string]any)
		if !ok {
			t.Fatalf("%s schema missing force_config_test: %+v", name, properties)
		}
		if field["type"] != "boolean" {
			t.Fatalf("%s force_config_test schema = %+v, want boolean", name, field)
		}
	}
}
