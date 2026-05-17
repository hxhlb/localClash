package configplan

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"localclash/internal/rules"

	"gopkg.in/yaml.v3"
)

func TestRenderBuiltInTargetPlanWritesArtifacts(t *testing.T) {
	paths := writePlanFixture(t)
	generated := filepath.Join(paths.dir, "generated", "mihomo.yaml")
	writeFile(t, generated, "sentinel: keep\n")

	result, err := Render(context.Background(), Options{
		PlanName:     "gaming-direct",
		Subscription: paths.subscription,
		Policy:       paths.policy,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		Test:         false,
		Now:          fixedPlanTime(),
		Overlay: OverlayIntent{
			Packs: []OverlayPackIntent{
				{ID: "blackmatrix7_Steam", Target: "DIRECT"},
				{ID: "blackmatrix7_Epic", Target: "DIRECT"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.PlanID != "gaming-direct-20260516-130000" {
		t.Fatalf("plan id = %q", result.PlanID)
	}
	assertFileExists(t, result.Output)
	assertFileExists(t, result.SummaryPath)
	if got := readFile(t, generated); got != "sentinel: keep\n" {
		t.Fatalf("generated config was overwritten: %q", got)
	}
	if result.Changes.RuleProvidersAdded != 2 || result.Changes.ProxyGroupsAdded != 0 || result.Changes.RulesAdded != 2 {
		t.Fatalf("changes = %+v, want 2 providers, 0 groups, 2 rules", result.Changes)
	}
	config := readYAMLMap(t, result.Output)
	metadata := config["x-localclash"].(map[string]any)
	overlay := metadata["overlay"].(map[string]any)
	if len(overlay["packs"].([]any)) != 2 {
		t.Fatalf("overlay packs = %v, want 2", overlay["packs"])
	}
	if len(overlay["proxy_groups"].([]any)) != 0 {
		t.Fatalf("overlay proxy_groups = %v, want empty", overlay["proxy_groups"])
	}
	assertMetadataHasNoSensitiveFields(t, metadata)
	assertSummaryJSON(t, result.SummaryPath)
}

func TestRenderPlanIDDoesNotOverwriteExistingPlan(t *testing.T) {
	paths := writePlanFixture(t)
	firstPlanDir := filepath.Join(paths.planDir, "gaming-direct-20260516-130000")
	writeFile(t, filepath.Join(firstPlanDir, "summary.json"), `{"sentinel":true}`)

	result, err := Render(context.Background(), Options{
		PlanName:     "gaming-direct",
		Subscription: paths.subscription,
		Policy:       paths.policy,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		Test:         false,
		Now:          fixedPlanTime(),
		Overlay: OverlayIntent{
			Packs: []OverlayPackIntent{
				{ID: "blackmatrix7_Steam", Target: "DIRECT"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.PlanID != "gaming-direct-20260516-130000-2" {
		t.Fatalf("plan id = %q, want collision suffix", result.PlanID)
	}
	if got := readFile(t, filepath.Join(firstPlanDir, "summary.json")); got != `{"sentinel":true}` {
		t.Fatalf("existing plan was overwritten: %q", got)
	}
}

func TestRenderProxyGroupPlan(t *testing.T) {
	paths := writePlanFixture(t)

	result, err := Render(context.Background(), Options{
		PlanName:     "ai-sg-jp-us",
		Subscription: paths.subscription,
		Policy:       paths.policy,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		Test:         false,
		Now:          fixedPlanTime(),
		Overlay: OverlayIntent{
			Packs: []OverlayPackIntent{
				{ID: "blackmatrix7_OpenAI", Target: "AI"},
				{ID: "sukkaw_ai_non_ip", Target: "AI"},
			},
			ProxyGroups: []OverlayProxyGroupIntent{
				{ID: "AI", Nodes: []string{"SG 01", "JP Tokyo 01", "US 01"}, Mode: "manual"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Overlay.ProxyGroups) != 1 {
		t.Fatalf("proxy groups = %+v, want one", result.Overlay.ProxyGroups)
	}
	if result.Overlay.ProxyGroups[0].NodeCount != 3 {
		t.Fatalf("proxy group node count = %d, want 3", result.Overlay.ProxyGroups[0].NodeCount)
	}
	if result.Changes.RuleProvidersAdded != 2 || result.Changes.ProxyGroupsAdded != 1 || result.Changes.RulesAdded != 2 {
		t.Fatalf("changes = %+v, want 2 providers, 1 group, 2 rules", result.Changes)
	}
	config := readYAMLMap(t, result.Output)
	if !proxyGroupNames(config)["AI"] {
		t.Fatal("candidate config missing AI proxy group")
	}
	metadata := config["x-localclash"].(map[string]any)
	overlay := metadata["overlay"].(map[string]any)
	proxyGroups := overlay["proxy_groups"].([]any)
	if got := proxyGroups[0].(map[string]any)["mode"]; got != "manual" {
		t.Fatalf("proxy group mode = %v, want manual", got)
	}
	assertMetadataHasNoSensitiveFields(t, metadata)
}

func TestApplyPlanWritesSelectionAndGeneratedConfig(t *testing.T) {
	paths := writePlanFixture(t)
	generated := filepath.Join(paths.dir, "generated", "mihomo.yaml")
	selectionPath := filepath.Join(paths.dir, "localclash-packs.yaml")
	writeFile(t, generated, "sentinel: old generated\n")

	plan, err := Render(context.Background(), Options{
		PlanName:     "ai-sg",
		Subscription: paths.subscription,
		Policy:       paths.policy,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		Test:         false,
		Now:          fixedPlanTime(),
		Overlay: OverlayIntent{
			Packs: []OverlayPackIntent{{ID: "blackmatrix7_OpenAI", Target: "AI"}},
			ProxyGroups: []OverlayProxyGroupIntent{
				{ID: "AI", Nodes: []string{"SG 01"}, Mode: "manual"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Apply(context.Background(), ApplyOptions{
		PlanID:        plan.PlanID,
		PlansDir:      paths.planDir,
		Subscription:  paths.subscription,
		Policy:        paths.policy,
		RulesCache:    paths.cacheDir,
		SelectionPath: selectionPath,
		OutputPath:    generated,
		Test:          false,
		Now:           fixedPlanTime(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || !result.Valid {
		t.Fatalf("apply result = %+v, want applied valid plan", result)
	}
	if len(result.Backups) != 2 {
		t.Fatalf("backups = %+v, want selection and generated backups", result.Backups)
	}
	selection, err := rules.LoadSelection(selectionPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.EnabledPack) != 1 || selection.EnabledPack[0].Source != "blackmatrix7" || selection.EnabledPack[0].Pack != "OpenAI" || selection.EnabledPack[0].Target != "AI" {
		t.Fatalf("selection enabled packs = %+v", selection.EnabledPack)
	}
	if got := selection.ProxyGroups["AI"].Nodes; len(got) != 1 || got[0] != "SG 01" {
		t.Fatalf("AI nodes = %+v, want SG 01", got)
	}
	config := readYAMLMap(t, generated)
	if !proxyGroupNames(config)["AI"] {
		t.Fatal("generated config missing applied AI proxy group")
	}
	if strings.Contains(readFile(t, generated), "sentinel") {
		t.Fatalf("generated config was not replaced: %s", readFile(t, generated))
	}
}

func TestRenderUnknownPackIDReturnsError(t *testing.T) {
	paths := writePlanFixture(t)

	_, err := Render(context.Background(), Options{
		Subscription: paths.subscription,
		Policy:       paths.policy,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		Test:         false,
		Overlay: OverlayIntent{
			Packs: []OverlayPackIntent{{ID: "missing_pack", Target: "DIRECT"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "missing_pack") {
		t.Fatalf("error = %v, want missing pack error", err)
	}
}

func TestRenderMissingProxyGroupReturnsError(t *testing.T) {
	paths := writePlanFixture(t)

	_, err := Render(context.Background(), Options{
		Subscription: paths.subscription,
		Policy:       paths.policy,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		Test:         false,
		Overlay: OverlayIntent{
			Packs: []OverlayPackIntent{{ID: "blackmatrix7_OpenAI", Target: "AI"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "requires a matching proxy group") {
		t.Fatalf("error = %v, want missing proxy group error", err)
	}
}

func TestRenderUnknownProxyGroupNodeReturnsError(t *testing.T) {
	paths := writePlanFixture(t)

	_, err := Render(context.Background(), Options{
		Subscription: paths.subscription,
		Policy:       paths.policy,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		Test:         false,
		Overlay: OverlayIntent{
			Packs: []OverlayPackIntent{{ID: "blackmatrix7_OpenAI", Target: "AI"}},
			ProxyGroups: []OverlayProxyGroupIntent{
				{ID: "AI", Nodes: []string{"MISSING"}, Mode: "manual"},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown subscription node") {
		t.Fatalf("error = %v, want unknown subscription node error", err)
	}
}

func TestRenderDuplicateProxyGroupNodesAreDeduplicated(t *testing.T) {
	paths := writePlanFixture(t)

	result, err := Render(context.Background(), Options{
		Subscription: paths.subscription,
		Policy:       paths.policy,
		RulesCache:   paths.cacheDir,
		OutputDir:    paths.planDir,
		Test:         false,
		Overlay: OverlayIntent{
			Packs: []OverlayPackIntent{{ID: "blackmatrix7_OpenAI", Target: "AI"}},
			ProxyGroups: []OverlayProxyGroupIntent{
				{ID: "AI", Nodes: []string{"SG 01", "SG 01"}, Mode: "manual"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %+v, want none", result.Warnings)
	}
	if result.Overlay.ProxyGroups[0].NodeCount != 1 {
		t.Fatalf("node count = %d, want deduplicated SG node only", result.Overlay.ProxyGroups[0].NodeCount)
	}
}

type planFixturePaths struct {
	dir          string
	subscription string
	policy       string
	cacheDir     string
	planDir      string
}

func writePlanFixture(t *testing.T) planFixturePaths {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	paths := planFixturePaths{
		dir:          dir,
		subscription: filepath.Join(dir, "subscription.yaml"),
		policy:       filepath.Join(dir, "policy.yaml"),
		cacheDir:     filepath.Join(dir, ".runtime", "rules", "packs"),
		planDir:      filepath.Join(dir, ".runtime", "plans"),
	}
	writeFile(t, paths.subscription, `proxies:
  - name: "SG 01"
    type: ss
    server: sg.example.com
    port: 443
    password: secret
  - name: "JP Tokyo 01"
    type: trojan
    server: jp.example.com
    password: secret
  - name: "US 01"
    type: vmess
    server: us.example.com
    uuid: secret
`)
	writeFile(t, paths.policy, `rule_source:
  base_url: https://example.com/rules
  update_interval: 86400
groups:
  direct: DIRECT
  reject: REJECT
  proxy: PROXY
  auto: AUTO
  manual: MANUAL
  apple: Apple
provider_mapping:
  applications:
    path: applications.txt
    behavior: classical
    target: direct
  proxy:
    path: proxy.txt
    behavior: domain
    target: proxy
modes:
  default: whitelist
  whitelist:
    rules:
      - provider: applications
        target: direct
      - provider: proxy
        target: proxy
      - match: true
        target: proxy
  blacklist:
    rules:
      - match: true
        target: direct
`)
	writeFile(t, filepath.Join(dir, "localclash-packs.yaml"), `version: 1
proxy_groups: {}
enabled_packs: []
`)
	if err := os.MkdirAll(paths.cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(paths.cacheDir, "blackmatrix7.yaml"), `version: 1
source: blackmatrix7
adapter: blackmatrix7
renderable: true
packs:
  - id: Epic
    name: Epic
    target: DIRECT
    renderable: true
    components:
      - id: Epic
        behavior: classical
        format: yaml
        order_class: mixed
        url: https://example.com/Epic.yaml
        path: ./rule-packs/blackmatrix7/Epic.yaml
  - id: OpenAI
    name: OpenAI
    target: AI
    renderable: true
    components:
      - id: OpenAI
        behavior: classical
        format: yaml
        order_class: mixed
        url: https://example.com/OpenAI.yaml
        path: ./rule-packs/blackmatrix7/OpenAI.yaml
  - id: Steam
    name: Steam
    target: DIRECT
    renderable: true
    components:
      - id: Steam
        behavior: classical
        format: yaml
        order_class: mixed
        url: https://example.com/Steam.yaml
        path: ./rule-packs/blackmatrix7/Steam.yaml
`)
	writeFile(t, filepath.Join(paths.cacheDir, "sukkaw.yaml"), `version: 1
source: sukkaw
adapter: sukkaw
renderable: true
packs:
  - id: ai
    name: AI
    target: AI
    renderable: true
    components:
      - id: non_ip
        behavior: classical
        format: text
        order_class: non_ip
        url: https://ruleset.skk.moe/Clash/non_ip/ai.txt
        path: ./rule-packs/sukkaw/ai_non_ip.txt
`)
	return paths
}

func fixedPlanTime() time.Time {
	return time.Date(2026, 5, 16, 13, 0, 0, 0, time.UTC)
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func assertSummaryJSON(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result Result
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.PlanID == "" || result.Output == "" {
		t.Fatalf("summary result = %+v, want plan id and output", result)
	}
}

func readYAMLMap(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func proxyGroupNames(config map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, raw := range config["proxy-groups"].([]any) {
		group := raw.(map[string]any)
		out[group["name"].(string)] = true
	}
	return out
}

func assertMetadataHasNoSensitiveFields(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, banned := range []string{"sg.example.com", "jp.example.com", "us.example.com", "password", "server", "secret", "uuid"} {
		if strings.Contains(text, banned) {
			t.Fatalf("metadata leaked %q in %s", banned, text)
		}
	}
}
