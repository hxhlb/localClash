package appinit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapBuildsRuntimeStateFromLocalArtifacts(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "bin", "mihomo")
	writeAppinitFile(t, core, "#!/bin/sh\nif [ \"$1\" = \"-v\" ]; then echo Mihomo test; exit 0; fi\nexit 0\n", 0o755)
	subscription := filepath.Join(dir, "subscription.yaml")
	writeAppinitFile(t, subscription, `proxies:
  - name: SG 01
    type: ss
    server: sg.example.com
    password: secret
`, 0o644)
	policy := filepath.Join(dir, "policy.yaml")
	writeAppinitFile(t, policy, `rule_source:
  base_url: https://example.com/rules
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
modes:
  default: whitelist
  whitelist:
    rules:
      - provider: applications
        target: direct
      - match: true
        target: proxy
  blacklist:
    rules:
      - match: true
        target: direct
`, 0o644)
	cacheDir := filepath.Join(dir, ".runtime", "rules", "packs")
	writeAppinitFile(t, filepath.Join(cacheDir, "blackmatrix7.yaml"), `version: 1
source: blackmatrix7
adapter: blackmatrix7
renderable: true
packs:
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
`, 0o644)

	state := Bootstrap(context.Background(), Options{
		RuntimeRoot:        filepath.Join(dir, ".runtime"),
		RuleSourcesDir:     filepath.Join(dir, "rule-sources"),
		RulesCacheDir:      cacheDir,
		GeneratedConfig:    filepath.Join(dir, "generated", "mihomo.yaml"),
		SubscriptionPath:   subscription,
		MihomoRuntimeDir:   filepath.Join(dir, ".runtime", "mihomo"),
		CorePath:           core,
		PolicyPath:         policy,
		RuntimeProfilePath: filepath.Join(dir, "localclash-runtime.yaml"),
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
	if !state.Config.Rendered || !state.Config.Available {
		t.Fatalf("config = %+v, want rendered", state.Config)
	}
	if _, err := os.Stat(state.Paths.GeneratedConfig); err != nil {
		t.Fatalf("generated config missing: %v", err)
	}
}

func TestBootstrapRecordsDiagnosticsWithoutFailingProcess(t *testing.T) {
	dir := t.TempDir()
	state := Bootstrap(context.Background(), Options{
		RuntimeRoot:        filepath.Join(dir, ".runtime"),
		RuleSourcesDir:     filepath.Join(dir, "missing-rule-sources"),
		RulesCacheDir:      filepath.Join(dir, ".runtime", "rules", "packs"),
		GeneratedConfig:    filepath.Join(dir, "generated", "mihomo.yaml"),
		SubscriptionPath:   filepath.Join(dir, "subscription.yaml"),
		MihomoRuntimeDir:   filepath.Join(dir, ".runtime", "mihomo"),
		CorePath:           filepath.Join(dir, "bin", "mihomo"),
		PolicyPath:         filepath.Join(dir, "policy.yaml"),
		RuntimeProfilePath: filepath.Join(dir, "localclash-runtime.yaml"),
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

func TestBootstrapCanSkipGeneratedConfigRender(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "bin", "mihomo")
	writeAppinitFile(t, core, "#!/bin/sh\nif [ \"$1\" = \"-v\" ]; then echo Mihomo test; exit 0; fi\nexit 0\n", 0o755)
	subscription := filepath.Join(dir, "subscription.yaml")
	writeAppinitFile(t, subscription, `proxies:
  - name: SG 01
    type: ss
    server: sg.example.com
    password: secret
`, 0o644)
	policy := filepath.Join(dir, "policy.yaml")
	writeAppinitFile(t, policy, `rule_source:
  base_url: https://example.com/rules
groups:
  direct: DIRECT
  reject: REJECT
  proxy: PROXY
  auto: AUTO
  manual: MANUAL
modes:
  default: whitelist
  whitelist:
    rules:
      - match: true
        target: proxy
`, 0o644)
	generated := filepath.Join(dir, "generated", "mihomo.yaml")

	state := Bootstrap(context.Background(), Options{
		RuntimeRoot:         filepath.Join(dir, ".runtime"),
		RuleSourcesDir:      filepath.Join(dir, "missing-rule-sources"),
		RulesCacheDir:       filepath.Join(dir, ".runtime", "rules", "packs"),
		GeneratedConfig:     generated,
		SubscriptionPath:    subscription,
		MihomoRuntimeDir:    filepath.Join(dir, ".runtime", "mihomo"),
		CorePath:            core,
		PolicyPath:          policy,
		RuntimeProfilePath:  filepath.Join(dir, "localclash-runtime.yaml"),
		SkipGeneratedConfig: true,
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
	if got := defaultWorkDirPath(state.Paths.RuntimeRoot, "localclash.yaml"); got != filepath.Join(routerDir, "localclash.yaml") {
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
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}
