package localconfig

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
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
	writeTestFile(t, filepath.Join(rulesCache, "blackmatrix7.yaml"), `version: 1
source: blackmatrix7
adapter: blackmatrix7
renderable: true
packs:
  - id: Steam
    name: Steam
    target: DIRECT
    renderable: true
`)

	resolved, err := Resolve(ResolveOptions{
		Config: Config{
			ProxyGroups: map[string]ProxyGroup{
				"SteamHK": {
					Mode:  "manual",
					Match: &Match{Type: "name_regex", Pattern: "HK", SourceIDs: []string{"main"}, Min: 1},
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
	writeTestFile(t, filepath.Join(rulesCache, "blackmatrix7.yaml"), `version: 1
source: blackmatrix7
adapter: blackmatrix7
renderable: true
packs:
  - id: Steam
    name: Steam
    target: DIRECT
    renderable: true
`)

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
	writeTestFile(t, filepath.Join(rulesCache, "v2fly-dlc.yaml"), `version: 1
source: v2fly-dlc
adapter: v2fly-dlc
renderable: true
packs:
  - id: steam
    renderable: true
    components:
      - id: domain
        behavior: v2fly-dlc
        format: text
        order_class: domain
        url: https://example.com/steam
        path: ./rule-packs/v2fly-dlc/steam.txt
`)

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

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
