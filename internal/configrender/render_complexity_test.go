package configrender

import (
	"path/filepath"
	"testing"

	rulespkg "localclash/internal/rules"
)

func TestRenderEmitsSelectionComplexityCounters(t *testing.T) {
	dir := t.TempDir()
	source := map[string]any{
		"proxies": []any{
			map[string]any{"name": "HK 01", "type": "ss"},
			map[string]any{"name": "JP 01", "type": "ss"},
		},
	}
	policyPath := filepath.Join(dir, "policy.json")
	writeFile(t, policyPath, `groups:
  manual: 🎯 手动选择
  auto: ⚡ 自动选择
  direct: DIRECT
provider_mapping:
  default:
    path: default.yaml
    behavior: domain
modes:
  default: whitelist
  whitelist:
    fallback: direct
    rules:
      - match: true
        target: manual
  blacklist:
    fallback: direct
`)
	runtimePath := filepath.Join(dir, "runtime.json")
	writeFile(t, runtimePath, `version: 1
mode: router
core: meta
cores:
  meta:
    path: bin/mihomo
meta:
  config_path: generated/mihomo.yaml
`)
	rulesCache := filepath.Join(dir, "rules")
	writeRenderPackIndex(t, rulesCache)
	selection := rulespkg.Selection{
		Version: 1,
		ProxyGroups: map[string]rulespkg.ProxyGroup{
			"HK": {Manual: true, Nodes: []string{"HK 01"}},
		},
		EnabledPack: []rulespkg.SelectedPack{{Source: "blackmatrix7", Pack: "OpenAI", Target: "HK"}},
	}
	var events []StageEvent

	_, err := Render(Options{
		SourcePath:         filepath.Join(dir, "subscription.gob"),
		Source:             source,
		PolicyPath:         policyPath,
		OutputPath:         filepath.Join(dir, "generated", "mihomo.yaml"),
		Selection:          &selection,
		RulesCacheDir:      rulesCache,
		RuntimeProfilePath: runtimePath,
		Force:              true,
		OnStage: func(event StageEvent) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	event := findRenderStageEvent(t, events, "render_pack_selection", "done")
	assertRenderStageField(t, event, "proxy_names", 2)
	assertRenderStageField(t, event, "proxy_groups", 1)
	assertRenderStageField(t, event, "enabled_packs", 1)
	assertRenderStageField(t, event, "rendered_rules", 1)
}

func findRenderStageEvent(t *testing.T, events []StageEvent, stage, event string) StageEvent {
	t.Helper()
	for _, got := range events {
		if got.Stage == stage && got.Event == event {
			return got
		}
	}
	t.Fatalf("missing stage event %s/%s in %+v", stage, event, events)
	return StageEvent{}
}

func assertRenderStageField(t *testing.T, event StageEvent, key string, want int) {
	t.Helper()
	got, ok := event.Fields[key].(int)
	if !ok {
		t.Fatalf("%s field = %#v, want int %d in %+v", key, event.Fields[key], want, event.Fields)
	}
	if got != want {
		t.Fatalf("%s = %d, want %d in %+v", key, got, want, event.Fields)
	}
}
