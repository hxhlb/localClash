package configinspect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectBaseReturnsBaseSummary(t *testing.T) {
	path := writeInspectConfig(t, true, true)

	result, err := InspectBase(Options{ConfigPath: path, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Layer != "base" || result.Modifiable {
		t.Fatalf("layer/modifiable = %s/%v, want base/false", result.Layer, result.Modifiable)
	}
	if result.Summary.ProxiesCount != 2 {
		t.Fatalf("proxies count = %d, want 2", result.Summary.ProxiesCount)
	}
	if len(result.Summary.ProxyGroups) != 1 || result.Summary.ProxyGroups[0].Name != "PROXY" {
		t.Fatalf("proxy groups = %+v, want limited PROXY", result.Summary.ProxyGroups)
	}
	if len(result.Summary.RuleProviders) != 1 {
		t.Fatalf("rule providers = %+v, want limit 1", result.Summary.RuleProviders)
	}
	if result.Summary.RulesCount != 1 || len(result.Summary.RulesSample) != 1 {
		t.Fatalf("rules summary = count %d sample %+v, want count 1 sample 1", result.Summary.RulesCount, result.Summary.RulesSample)
	}
	if result.Summary.RulesSample[0] != "RULE-SET,applications,DIRECT" {
		t.Fatalf("rules sample = %+v, want base applications rule only", result.Summary.RulesSample)
	}
	assertInspectNoSensitiveLeak(t, result)
}

func TestInspectBaseMissingConfigReturnsClearError(t *testing.T) {
	_, err := InspectBase(Options{ConfigPath: filepath.Join(t.TempDir(), "missing.yaml")})
	if err == nil {
		t.Fatal("expected missing config error")
	}
	if !strings.Contains(err.Error(), "run config_render first") {
		t.Fatalf("error = %q, want config_render hint", err.Error())
	}
}

func TestInspectOverlayWithMetadataReturnsOverlay(t *testing.T) {
	path := writeInspectConfig(t, true, true)

	result, err := InspectOverlay(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if result.Layer != "overlay" || !result.Modifiable {
		t.Fatalf("layer/modifiable = %s/%v, want overlay/true", result.Layer, result.Modifiable)
	}
	if !result.MetadataPresent || !result.OverlayPresent {
		t.Fatalf("metadata/overlay present = %v/%v, want true/true", result.MetadataPresent, result.OverlayPresent)
	}
	if len(result.Packs) != 1 || result.Packs[0].ID != "blackmatrix7_OpenAI" {
		t.Fatalf("packs = %+v, want OpenAI pack", result.Packs)
	}
	if len(result.ProxyGroups) != 1 || result.ProxyGroups[0].ID != "AI" {
		t.Fatalf("proxy groups = %+v, want AI", result.ProxyGroups)
	}
	if len(result.RuleProviders) != 1 || result.RuleProviders[0].Name != "blackmatrix7_OpenAI" {
		t.Fatalf("providers = %+v, want blackmatrix7_OpenAI", result.RuleProviders)
	}
	if len(result.Rules) != 1 || result.Rules[0].Target != "AI" {
		t.Fatalf("rules = %+v, want AI target", result.Rules)
	}
	assertInspectNoSensitiveLeak(t, result)
}

func TestInspectOverlayWithoutMetadataDoesNotGuess(t *testing.T) {
	path := writeInspectConfig(t, false, false)

	result, err := InspectOverlay(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if result.MetadataPresent || result.OverlayPresent {
		t.Fatalf("metadata/overlay present = %v/%v, want false/false", result.MetadataPresent, result.OverlayPresent)
	}
	if len(result.Packs) != 0 || len(result.RuleProviders) != 0 || len(result.Rules) != 0 {
		t.Fatalf("overlay result = %+v, want empty arrays", result)
	}
}

func TestInspectOverlayWithEmptyMetadataReturnsNoOverlay(t *testing.T) {
	path := writeInspectConfig(t, true, false)

	result, err := InspectOverlay(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if !result.MetadataPresent || result.OverlayPresent {
		t.Fatalf("metadata/overlay present = %v/%v, want true/false", result.MetadataPresent, result.OverlayPresent)
	}
	if len(result.Packs) != 0 || len(result.ProxyGroups) != 0 || len(result.RuleProviders) != 0 || len(result.Rules) != 0 {
		t.Fatalf("overlay arrays = %+v, want empty", result)
	}
	assertInspectNoSensitiveLeak(t, result)
}

func writeInspectConfig(t *testing.T, metadata bool, overlay bool) string {
	t.Helper()
	content := `
mode: rule
external-controller: 127.0.0.1:9090
external-ui: ui/zashboard
proxies:
  - name: JP 01
    type: ss
    server: jp.example.com
    password: secret
  - name: SG 01
    type: trojan
    server: sg.example.com
    password: secret2
proxy-groups:
  - name: PROXY
    type: select
    proxies: [JP 01, SG 01]
  - name: AUTO
    type: url-test
    proxies: [JP 01, SG 01]
rule-providers:
  applications:
    type: http
    behavior: classical
    url: https://example.com/applications.yaml
  blackmatrix7_OpenAI:
    type: http
    behavior: classical
    url: https://example.com/OpenAI.yaml
rules:
  - RULE-SET,applications,DIRECT
  - RULE-SET,blackmatrix7_OpenAI,AI
`
	if metadata {
		content += `
x-localclash:
  version: 1
  base:
    modifiable: false
    description: localClash generated base config
  overlay:
    modifiable: true
    packs:
`
		if overlay {
			content += `      - id: blackmatrix7_OpenAI
        source: blackmatrix7
        target: AI
    proxy_groups:
      - id: AI
        mode: manual
        nodes: [SG 01, JP Tokyo 01, US 01]
    rule_providers:
      - name: blackmatrix7_OpenAI
        behavior: classical
        type: http
    rules:
      - type: RULE-SET
        provider: blackmatrix7_OpenAI
        target: AI
`
		} else {
			content += `    proxy_groups: []
    rule_providers: []
    rules: []
`
		}
		content += `    insertion: after local safety baseline, before base rules
`
	}
	path := filepath.Join(t.TempDir(), "mihomo.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertInspectNoSensitiveLeak(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, banned := range []string{"jp.example.com", "sg.example.com", "secret", "secret2", "password", "server"} {
		if strings.Contains(text, banned) {
			t.Fatalf("inspection leaked %q in %s", banned, text)
		}
	}
}
