package mcp

import "testing"

func TestRegistryIncludesSafetyLevels(t *testing.T) {
	tools := Registry()
	byName := map[string]Tool{}
	for _, tool := range tools {
		byName[tool.Name] = tool
	}

	if byName["doctor"].SafetyLevel != SafeRead {
		t.Fatalf("doctor safety = %q, want %q", byName["doctor"].SafetyLevel, SafeRead)
	}
	if byName["packs_list"].SafetyLevel != SafeRead {
		t.Fatalf("packs_list safety = %q, want %q", byName["packs_list"].SafetyLevel, SafeRead)
	}
	if byName["packs_get"].SafetyLevel != SafeRead {
		t.Fatalf("packs_get safety = %q, want %q", byName["packs_get"].SafetyLevel, SafeRead)
	}
	if byName["config_render"].SafetyLevel != SafeWrite {
		t.Fatalf("config_render safety = %q, want %q", byName["config_render"].SafetyLevel, SafeWrite)
	}
	if byName["run_runtime"].SafetyLevel != ConfirmRequired {
		t.Fatalf("run_runtime safety = %q, want %q", byName["run_runtime"].SafetyLevel, ConfirmRequired)
	}
	if byName["apply_router_config"].SafetyLevel != HighRisk {
		t.Fatalf("apply_router_config safety = %q, want %q", byName["apply_router_config"].SafetyLevel, HighRisk)
	}
}
