package localconfig

import (
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

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
