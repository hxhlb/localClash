package localconfig

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"localclash/internal/rules"
)

func TestResolveNameRegexUsesSourceArtifactsAndMergeNames(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "localclash-subscriptions.yaml")
	runtimeDir := filepath.Join(dir, ".runtime", "subscriptions")
	writeTestFile(t, configPath, `sources:
  - id: main
  - id: backup
`)
	writeTestFile(t, filepath.Join(runtimeDir, "main.yaml"), `proxies:
  - name: HK 01
    type: ss
  - name: SG 01
    type: ss
`)
	writeTestFile(t, filepath.Join(runtimeDir, "backup.yaml"), `proxies:
  - name: HK 01
    type: ss
`)
	rulesCache := filepath.Join(dir, "rules")
	writeTestPackCache(t, rulesCache, "blackmatrix7", "blackmatrix7", testRulePack("Steam", "DIRECT"))

	resolved, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"SteamHK": {
					Mode:  "manual",
					Match: &Match{Type: "name_regex", Pattern: "HK", SourceIDs: []string{"main"}, Min: 1},
				},
				"MainSG": {
					Mode:  "manual",
					Match: &Match{Type: "name_regex", Pattern: "SG", SourceIDs: []string{"main"}, Min: 1},
				},
			},
			Packs: []Pack{{ID: "blackmatrix7_Steam", Target: "SteamHK"}},
		},
		SubscriptionPath:    filepath.Join(dir, "subscription.yaml"),
		SubscriptionConfig:  configPath,
		SubscriptionRuntime: runtimeDir,
		RulesCache:          rulesCache,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	got := resolved.Config.ProxyGroups["SteamHK"].SelectedNodes
	want := []string{"[main] HK 01"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected nodes = %#v, want %#v", got, want)
	}
	got = resolved.Config.ProxyGroups["MainSG"].SelectedNodes
	want = []string{"[main] SG 01"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected nodes = %#v, want %#v", got, want)
	}
}

func TestResolveNameRegexEnforcesMin(t *testing.T) {
	dir := t.TempDir()
	subscriptionPath := filepath.Join(dir, "subscription.yaml")
	writeTestFile(t, subscriptionPath, `proxies:
  - name: SG 01
    type: ss
`)

	_, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"SteamHK": {Mode: "manual", Match: &Match{Type: "name_regex", Pattern: "HK", Min: 1}},
			},
		},
		SubscriptionPath: subscriptionPath,
	})
	if err == nil {
		t.Fatal("Resolve succeeded, want min-match error")
	}
}

func TestResolveExactNodesSupportsExplicitHumanChoice(t *testing.T) {
	dir := t.TempDir()
	subscriptionPath := filepath.Join(dir, "subscription.yaml")
	writeTestFile(t, subscriptionPath, `proxies:
  - name: HK 01
    type: ss
  - name: HK Dedicated
    type: ss
`)

	resolved, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"SteamHK": {Mode: "manual", Nodes: []string{"HK Dedicated"}},
			},
		},
		SubscriptionPath: subscriptionPath,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	got := resolved.Config.ProxyGroups["SteamHK"].SelectedNodes
	want := []string{"HK Dedicated"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected nodes = %#v, want %#v", got, want)
	}
}

func TestResolveProxyGroupSupportsSmartMode(t *testing.T) {
	dir := t.TempDir()
	subscriptionPath := filepath.Join(dir, "subscription.yaml")
	writeTestFile(t, subscriptionPath, `proxies:
  - name: SG 01
    type: ss
`)

	resolved, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"AI": {Mode: "smart", Nodes: []string{"SG 01"}},
			},
		},
		SubscriptionPath: subscriptionPath,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if !resolved.Selection.ProxyGroups["AI"].Smart {
		t.Fatalf("selection proxy group = %+v, want smart", resolved.Selection.ProxyGroups["AI"])
	}
}

func TestResolveProxyGroupSupportsDirectMode(t *testing.T) {
	dir := t.TempDir()
	subscriptionPath := filepath.Join(dir, "subscription.yaml")
	writeTestFile(t, subscriptionPath, `proxies:
  - name: SG 01
    type: ss
`)

	resolved, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"全球直连": {Mode: "direct"},
			},
		},
		SubscriptionPath: subscriptionPath,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if !resolved.Selection.ProxyGroups["全球直连"].Direct {
		t.Fatalf("selection proxy group = %+v, want direct", resolved.Selection.ProxyGroups["全球直连"])
	}
	if got := resolved.ProxyGroups[0].NodeCount; got != 0 {
		t.Fatalf("direct group node count = %d, want 0", got)
	}
}

func TestResolvePolicyGroupTargetsProxyGroupExits(t *testing.T) {
	dir := t.TempDir()
	subscriptionPath := filepath.Join(dir, "subscription.yaml")
	writeTestFile(t, subscriptionPath, `proxies:
  - name: HK 01
    type: ss
  - name: JP Tokyo 01
    type: ss
`)
	rulesCache := filepath.Join(dir, "rules")
	writeTestPackCache(t, rulesCache, "blackmatrix7", "blackmatrix7", testRulePack("Steam", "DIRECT"))

	resolved, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"HK": {Mode: "manual", Nodes: []string{"HK 01"}},
				"JP": {Mode: "auto", Nodes: []string{"JP Tokyo 01"}},
			},
			PolicyGroups: map[string]PolicyGroup{
				"Steam": {Mode: "manual", Exits: []string{"HK", "JP", "direct"}},
			},
			Packs: []Pack{{ID: "blackmatrix7_Steam", Target: "Steam"}},
		},
		SubscriptionPath: subscriptionPath,
		RulesCache:       rulesCache,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	exits := resolved.Selection.PolicyGroups["Steam"].Exits
	want := []string{"HK", "JP", "DIRECT"}
	if !reflect.DeepEqual(exits, want) {
		t.Fatalf("policy exits = %#v, want %#v", exits, want)
	}
	if len(resolved.PolicyGroups) != 1 || resolved.PolicyGroups[0].ExitCount != 3 {
		t.Fatalf("resolved policy groups = %+v, want one group with three exits", resolved.PolicyGroups)
	}
}

func TestResolveOptionalProxyGroupCanBeEmpty(t *testing.T) {
	dir := t.TempDir()
	subscriptionPath := filepath.Join(dir, "subscription.yaml")
	writeTestFile(t, subscriptionPath, `proxies:
  - name: HK 01
    type: ss
`)
	rulesCache := filepath.Join(dir, "rules")
	writeTestPackCache(t, rulesCache, "v2fly-dlc", "v2fly-dlc", rules.Pack{
		ID:         "steam",
		Renderable: true,
		Components: []rules.Component{{
			ID:         "domain",
			Behavior:   "v2fly-dlc",
			Format:     "text",
			OrderClass: "domain",
			URL:        "https://example.com/steam",
			Path:       "./rule-packs/v2fly-dlc/steam.txt",
		}},
	})

	resolved, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"香港节点": {Mode: "auto", Match: &Match{Type: "name_regex", Pattern: "HK"}},
				"韩国节点": {Mode: "auto", Match: &Match{Type: "name_regex", Pattern: "KR"}, Optional: true},
			},
			PolicyGroups: map[string]PolicyGroup{
				"Steam": {Mode: "manual", Exits: []string{"香港节点", "韩国节点", "direct"}},
			},
			Packs: []Pack{{ID: "v2fly_dlc_steam", Target: "Steam"}},
		},
		SubscriptionPath: subscriptionPath,
		RulesCache:       rulesCache,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got := resolved.Selection.ProxyGroups["韩国节点"].Nodes; len(got) != 0 {
		t.Fatalf("optional group nodes = %#v, want empty", got)
	}
	if !resolved.Selection.ProxyGroups["韩国节点"].Optional {
		t.Fatalf("optional flag was not preserved: %+v", resolved.Selection.ProxyGroups["韩国节点"])
	}
}

func TestResolveExactNodesReportsMissingNodes(t *testing.T) {
	dir := t.TempDir()
	subscriptionPath := filepath.Join(dir, "subscription.yaml")
	writeTestFile(t, subscriptionPath, `proxies:
  - name: HK 01
    type: ss
`)

	_, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"SteamHK": {Mode: "manual", Nodes: []string{"HK Dedicated", "HK Backup"}},
			},
		},
		SubscriptionPath: subscriptionPath,
	})
	var missing *MissingNodesError
	if !errors.As(err, &missing) {
		t.Fatalf("error = %v, want MissingNodesError", err)
	}
	want := []string{"HK Dedicated", "HK Backup"}
	if missing.GroupID != "SteamHK" || !reflect.DeepEqual(missing.Nodes, want) {
		t.Fatalf("missing = %+v, want group SteamHK nodes %#v", missing, want)
	}
}

func TestResolveProxyGroupRejectsAmbiguousMatchAndNodes(t *testing.T) {
	dir := t.TempDir()
	subscriptionPath := filepath.Join(dir, "subscription.yaml")
	writeTestFile(t, subscriptionPath, `proxies:
  - name: HK 01
    type: ss
`)

	_, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"SteamHK": {
					Mode:  "manual",
					Match: &Match{Type: "name_regex", Pattern: "HK"},
					Nodes: []string{"HK 01"},
				},
			},
		},
		SubscriptionPath: subscriptionPath,
	})
	if err == nil {
		t.Fatal("Resolve succeeded, want ambiguous match/nodes error")
	}
}

func TestResolveEmitsStageTimings(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "localclash-subscriptions.yaml")
	runtimeDir := filepath.Join(dir, ".runtime", "subscriptions")
	writeTestFile(t, configPath, `sources:
  - id: main
`)
	writeTestFile(t, filepath.Join(runtimeDir, "main.yaml"), `proxies:
  - name: HK 01
    type: ss
`)
	rulesCache := filepath.Join(dir, "rules")
	writeTestPackCache(t, rulesCache, "blackmatrix7", "blackmatrix7", testRulePack("Steam", "DIRECT"))

	var events []StageEvent
	_, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"香港节点": {Mode: "manual", Match: &Match{Type: "name_regex", Pattern: "HK"}},
			},
			PolicyGroups: map[string]PolicyGroup{
				"Steam": {Mode: "manual", Exits: []string{"香港节点", "direct"}},
			},
			Packs: []Pack{{ID: "blackmatrix7_Steam", Target: "Steam"}},
		},
		SubscriptionPath:    filepath.Join(dir, "subscription.yaml"),
		SubscriptionConfig:  configPath,
		SubscriptionRuntime: runtimeDir,
		RulesCache:          rulesCache,
		OnStage: func(event StageEvent) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	assertStageEvent(t, events, "load_subscription_nodes", "done")
	assertStageEvent(t, events, "load_subscription_nodes.read_subscription_source_artifact", "done")
	assertStageEvent(t, events, "resolve_proxy_group", "done")
	assertStageEvent(t, events, "resolve_policy_group", "done")
	assertStageEvent(t, events, "resolve_pack", "done")
	assertStageEvent(t, events, "resolve_rule_providers", "done")
}

func assertStageEvent(t *testing.T, events []StageEvent, stage, event string) {
	t.Helper()
	for _, got := range events {
		if got.Stage == stage && got.Event == event {
			return
		}
	}
	names := make([]string, 0, len(events))
	for _, got := range events {
		names = append(names, got.Stage+":"+got.Event)
	}
	t.Fatalf("missing stage event %s:%s in %s", stage, event, strings.Join(names, ", "))
}

func testRulePack(id, target string) rules.Pack {
	return rules.Pack{
		ID:         id,
		Name:       id,
		Target:     target,
		Renderable: true,
		Components: []rules.Component{{
			ID:         id,
			Behavior:   "classical",
			Format:     "yaml",
			OrderClass: "mixed",
			URL:        "https://example.com/" + id + ".yaml",
			Path:       "./rule-packs/test/" + id + ".yaml",
		}},
	}
}

func writeTestPackCache(t *testing.T, dir, source, adapter string, packs ...rules.Pack) {
	t.Helper()
	if err := rules.WritePackIndex(rules.PackIndexPath(dir), map[string]rules.PackCache{
		source: {
			Version:    1,
			Source:     source,
			Adapter:    adapter,
			Renderable: true,
			Packs:      packs,
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
