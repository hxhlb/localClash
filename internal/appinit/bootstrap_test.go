package appinit

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"localclash/internal/rules"

	"gopkg.in/yaml.v3"
)

func TestBootstrapBuildsRuntimeStateFromLocalArtifacts(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "bin", "mihomo")
	writeAppinitFile(t, core, "#!/bin/sh\nif [ \"$1\" = \"-v\" ]; then echo Mihomo test; exit 0; fi\nexit 0\n", 0o755)
	subscription := filepath.Join(dir, "subscription.gob")
	writeAppinitFile(t, subscription, `proxies:
  - name: SG 01
    type: ss
    server: sg.example.com
    password: secret
`, 0o644)
	cacheDir := filepath.Join(dir, ".runtime", "rules", "packs")
	writeAppinitPackIndex(t, cacheDir)

	state := Bootstrap(context.Background(), Options{
		RuntimeRoot:        filepath.Join(dir, ".runtime"),
		RuleSourcesDir:     filepath.Join(dir, "rule-sources"),
		RulesCacheDir:      cacheDir,
		GeneratedConfig:    filepath.Join(dir, "generated", "mihomo.yaml"),
		SubscriptionPath:   subscription,
		MihomoRuntimeDir:   filepath.Join(dir, ".runtime", "mihomo"),
		CorePath:           core,
		RuntimeProfilePath: filepath.Join(dir, "localclash-runtime.json"),
	})

	if !state.Core.Exists || !strings.Contains(state.Core.Version, "Mihomo test") {
		t.Fatalf("core = %+v, want version", state.Core)
	}
	if !state.Subscription.Available {
		t.Fatalf("subscription = %+v, want available", state.Subscription)
	}
	if !state.Rules.CatalogAvailable || len(state.Rules.Packs) != 1 {
		t.Fatalf("rules = %+v, want one pack catalog", state.Rules)
	}
	if state.Config.Rendered || state.Config.Available {
		t.Fatalf("config = %+v, bootstrap should not render generated config", state.Config)
	}
	if _, err := os.Stat(state.Paths.GeneratedConfig); !os.IsNotExist(err) {
		t.Fatalf("generated config should not be created by bootstrap, err=%v", err)
	}
}

func TestBootstrapRecordsDiagnosticsWithoutFailingProcess(t *testing.T) {
	dir := t.TempDir()
	state := Bootstrap(context.Background(), Options{
		RuntimeRoot:        filepath.Join(dir, ".runtime"),
		RuleSourcesDir:     filepath.Join(dir, "missing-rule-sources"),
		RulesCacheDir:      filepath.Join(dir, ".runtime", "rules", "packs"),
		GeneratedConfig:    filepath.Join(dir, "generated", "mihomo.yaml"),
		SubscriptionPath:   filepath.Join(dir, "subscription.gob"),
		MihomoRuntimeDir:   filepath.Join(dir, ".runtime", "mihomo"),
		CorePath:           filepath.Join(dir, "bin", "mihomo"),
		RuntimeProfilePath: filepath.Join(dir, "localclash-runtime.json"),
	})

	if state.Core.Exists {
		t.Fatal("core should be missing")
	}
	if state.Subscription.Available {
		t.Fatal("subscription should be unavailable")
	}
	if state.Rules.CatalogAvailable {
		t.Fatal("rules catalog should be unavailable")
	}
	if len(state.Diagnostics) == 0 {
		t.Fatal("expected bootstrap diagnostics")
	}
	if _, err := os.Stat(state.Paths.RulesCacheDir); err != nil {
		t.Fatalf("rules cache dir should be created: %v", err)
	}
}

func TestBootstrapDoesNotRenderGeneratedConfigOnStartup(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "bin", "mihomo")
	writeAppinitFile(t, core, "#!/bin/sh\nif [ \"$1\" = \"-v\" ]; then echo Mihomo test; exit 0; fi\nexit 0\n", 0o755)
	subscription := filepath.Join(dir, "subscription.gob")
	writeAppinitFile(t, subscription, `proxies:
  - name: SG 01
    type: ss
    server: sg.example.com
    password: secret
`, 0o644)
	generated := filepath.Join(dir, "generated", "mihomo.yaml")

	state := Bootstrap(context.Background(), Options{
		RuntimeRoot:        filepath.Join(dir, ".runtime"),
		RuleSourcesDir:     filepath.Join(dir, "missing-rule-sources"),
		RulesCacheDir:      filepath.Join(dir, ".runtime", "rules", "packs"),
		GeneratedConfig:    generated,
		SubscriptionPath:   subscription,
		MihomoRuntimeDir:   filepath.Join(dir, ".runtime", "mihomo"),
		CorePath:           core,
		RuntimeProfilePath: filepath.Join(dir, "localclash-runtime.json"),
	})

	if state.Config.Rendered {
		t.Fatalf("config = %+v, want render skipped", state.Config)
	}
	if state.Config.Available {
		t.Fatalf("config = %+v, generated config should not be available", state.Config)
	}
	if _, err := os.Stat(generated); !os.IsNotExist(err) {
		t.Fatalf("generated config should not be created, err=%v", err)
	}
}

func TestBootstrapDefaultsToDetectedRouterWorkDir(t *testing.T) {
	wrongDir := t.TempDir()
	routerDir := t.TempDir()
	t.Chdir(wrongDir)
	writeAppinitFile(t, filepath.Join(routerDir, "generated", "mihomo.yaml"), "mixed-port: 7890\n", 0o644)
	oldCandidates := defaultWorkDirCandidates
	defaultWorkDirCandidates = []string{routerDir}
	t.Cleanup(func() {
		defaultWorkDirCandidates = oldCandidates
	})

	state := Bootstrap(context.Background(), Options{})

	if state.Paths.MihomoRuntimeDir != filepath.Join(routerDir, ".runtime", "mihomo") {
		t.Fatalf("mihomo runtime dir = %q, want detected router workdir", state.Paths.MihomoRuntimeDir)
	}
	if state.Paths.GeneratedConfig != filepath.Join(routerDir, "generated", "mihomo.yaml") {
		t.Fatalf("generated config = %q, want detected router workdir", state.Paths.GeneratedConfig)
	}
	if got := defaultWorkDirPath(state.Paths.RuntimeRoot, "localclash.json"); got != filepath.Join(routerDir, "localclash.json") {
		t.Fatalf("localclash config path = %q, want detected router workdir", got)
	}
	if _, err := os.Stat(filepath.Join(wrongDir, ".runtime")); !os.IsNotExist(err) {
		t.Fatalf("bootstrap should not create runtime dir in wrong cwd, err=%v", err)
	}
}

func writeAppinitFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var data []byte
	var err error
	switch filepath.Ext(path) {
	case ".json":
		var doc any
		if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
			t.Fatal(err)
		}
		data, err = json.MarshalIndent(doc, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
	case ".gob":
		gob.Register(map[string]any{})
		gob.Register([]any{})
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			t.Fatal(err)
		}
		encodeErr := gob.NewEncoder(file).Encode(struct {
			Version int
			Data    map[string]any
			Raw     []byte
		}{Version: 1, Data: doc, Raw: []byte(content)})
		closeErr := file.Close()
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		return
	default:
		data = []byte(content)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func writeAppinitPackIndex(t *testing.T, cacheDir string) {
	t.Helper()
	if err := rules.WritePackIndex(rules.PackIndexPath(cacheDir), map[string]rules.PackCache{
		"blackmatrix7": {
			Version:    1,
			Source:     "blackmatrix7",
			Adapter:    "blackmatrix7",
			Renderable: true,
			Packs: []rules.Pack{{
				ID:         "OpenAI",
				Name:       "OpenAI",
				Target:     "AI",
				Renderable: true,
				Components: []rules.Component{{
					ID:         "OpenAI",
					Behavior:   "classical",
					Format:     "yaml",
					OrderClass: "mixed",
					URL:        "https://example.com/OpenAI.yaml",
					Path:       "./rule-packs/blackmatrix7/OpenAI.yaml",
				}},
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}
}
