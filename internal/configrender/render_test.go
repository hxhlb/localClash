package configrender

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"localclash/internal/configmeta"
	"localclash/internal/runtimeprofile"
)

func TestBuildRulesWhitelistFallback(t *testing.T) {
	pol := policy{
		Groups: map[string]string{
			"direct": "DIRECT",
			"proxy":  "PROXY",
			"reject": "REJECT",
		},
		ProviderMapping: map[string]providerDefinition{
			"applications": {Path: "applications.txt", Behavior: "classical"},
		},
	}
	mode := policyMode{Rules: []ruleSpec{
		{Provider: "applications", Target: "direct"},
		{Match: true, Target: "proxy"},
	}}

	rules, err := buildRules(pol, mode)
	if err != nil {
		t.Fatal(err)
	}
	if got := rules[len(rules)-1]; got != "MATCH,PROXY" {
		t.Fatalf("fallback rule = %q, want MATCH,PROXY", got)
	}
}

func TestBuildRulesSupportsDomainSuffix(t *testing.T) {
	pol := policy{
		Groups: map[string]string{"direct": "DIRECT"},
	}
	mode := policyMode{Rules: []ruleSpec{
		{DomainSuffix: "local", Target: "direct"},
	}}

	rules, err := buildRules(pol, mode)
	if err != nil {
		t.Fatal(err)
	}
	if got := rules[0]; got != "DOMAIN-SUFFIX,local,DIRECT" {
		t.Fatalf("rule = %q, want DOMAIN-SUFFIX,local,DIRECT", got)
	}
}

func TestParseOverlayRuleLineSupportsGeoSite(t *testing.T) {
	rule, ok := parseOverlayRuleLine("GEOSITE,google,Global")
	if !ok {
		t.Fatal("expected GEOSITE overlay rule to parse")
	}
	if rule.Type != "GEOSITE" || rule.Value != "google" || rule.Target != "Global" || rule.Provider != "" {
		t.Fatalf("rule = %+v, want GEOSITE google Global", rule)
	}
}

func TestProxyNamesDeduplicates(t *testing.T) {
	names, err := proxyNames([]any{
		map[string]any{"name": "HK"},
		map[string]any{"name": "HK"},
		map[string]any{"name": "JP"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Fatalf("len(names) = %d, want 2", len(names))
	}
}

func TestWithLocalDNSPolicy(t *testing.T) {
	dns := map[string]any{
		"fake-ip-filter": []any{"*.lan"},
	}

	got := withLocalDNSPolicy(dns).(map[string]any)
	policy := got["nameserver-policy"].(map[string]any)
	if policy["+.local"] != "system" {
		t.Fatalf("nameserver-policy +.local = %v, want system", policy["+.local"])
	}
	if policy["+.lan"] != "system" {
		t.Fatalf("nameserver-policy +.lan = %v, want system", policy["+.lan"])
	}
	if policy["+.home.arpa"] != "system" {
		t.Fatalf("nameserver-policy +.home.arpa = %v, want system", policy["+.home.arpa"])
	}
	if got["use-system-hosts"] != true {
		t.Fatal("use-system-hosts should be enabled")
	}
	filters := got["fake-ip-filter"].([]string)
	if !containsString(filters, "+.home.arpa") {
		t.Fatalf("fake-ip-filter = %v, want +.home.arpa", filters)
	}
}

func TestWithLocalBaselinePrependsRules(t *testing.T) {
	mode := policyMode{Rules: []ruleSpec{{Match: true, Target: "proxy"}}}

	got := withLocalBaseline(mode)
	if got.Rules[0].Domain != "localhost" {
		t.Fatalf("first baseline rule = %+v, want localhost", got.Rules[0])
	}
	if got.Rules[len(got.Rules)-1].Match != true {
		t.Fatalf("last rule = %+v, want original match rule", got.Rules[len(got.Rules)-1])
	}
}

func TestRenderWithoutPacksSelectionPreservesBaseConfig(t *testing.T) {
	paths := writeRenderFixture(t)

	result, err := Render(Options{
		SourcePath:         paths.subscription,
		PolicyPath:         paths.policy,
		OutputPath:         filepath.Join(paths.dir, "base.yaml"),
		RuntimeProfilePath: filepath.Join(paths.dir, "localclash-runtime.yaml"),
		Force:              true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RuleCount != len(LocalBaselineRuleLines())+3 {
		t.Fatalf("RuleCount = %d, want baseline + 3", result.RuleCount)
	}
	config := readTestYAML(t, result.OutputPath)
	if _, ok := config["rule-providers"].(map[string]any)["sukkaw_ai_non_ip"]; ok {
		t.Fatal("base config should not include pack rule-provider")
	}
	if proxyGroupNamesFromConfig(config)["AI"] {
		t.Fatal("base config should not include AI proxy-group")
	}
	metadata := config[configmeta.Key].(map[string]any)
	overlay := metadata["overlay"].(map[string]any)
	if overlay["modifiable"] != true {
		t.Fatalf("overlay modifiable = %v, want true", overlay["modifiable"])
	}
	if len(overlay["packs"].([]any)) != 0 {
		t.Fatalf("overlay packs = %v, want empty", overlay["packs"])
	}
	assertNoSensitiveConfigMetadata(t, metadata)
}

func TestRenderWithPacksSelectionIncludesProxyGroupFragment(t *testing.T) {
	paths := writeRenderFixture(t)

	result, err := Render(Options{
		SourcePath:         paths.subscription,
		PolicyPath:         paths.policy,
		OutputPath:         filepath.Join(paths.dir, "with-packs.yaml"),
		PacksSelectionPath: paths.selection,
		RulesCacheDir:      paths.cacheDir,
		RuntimeProfilePath: filepath.Join(paths.dir, "localclash-runtime.yaml"),
		Force:              true,
	})
	if err != nil {
		t.Fatal(err)
	}
	config := readTestYAML(t, result.OutputPath)

	groups := proxyGroupNamesFromConfig(config)
	if !groups["AI"] {
		t.Fatal("missing proxy-group AI")
	}
	for _, unwanted := range []string{"AI_AUTO", "AI_MANUAL", "JP", "SG", "US"} {
		if groups[unwanted] {
			t.Fatalf("%q should not be materialized", unwanted)
		}
	}

	providers := config["rule-providers"].(map[string]any)
	for _, want := range []string{"sukkaw_ai_non_ip", "blackmatrix7_OpenAI"} {
		if _, ok := providers[want]; !ok {
			t.Fatalf("missing rule-provider %q", want)
		}
	}

	rules := testStringSlice(config["rules"])
	packIndex := indexOf(rules, "RULE-SET,sukkaw_ai_non_ip,AI")
	baseIndex := indexOf(rules, "RULE-SET,applications,DIRECT")
	if packIndex < 0 {
		t.Fatal("missing sukkaw AI rule")
	}
	if baseIndex < 0 {
		t.Fatal("missing base applications rule")
	}
	if packIndex > baseIndex {
		t.Fatalf("pack rule index %d should be before base rule index %d", packIndex, baseIndex)
	}

	metadata := config["x-localclash"].(map[string]any)
	overlay := metadata["overlay"].(map[string]any)
	packs := overlay["packs"].([]any)
	if len(packs) != 2 {
		t.Fatalf("overlay packs = %d, want 2", len(packs))
	}
	proxyGroups := overlay["proxy_groups"].([]any)
	if len(proxyGroups) != 1 {
		t.Fatalf("overlay proxy groups = %d, want 1", len(proxyGroups))
	}
	if got := proxyGroups[0].(map[string]any)["mode"]; got != "manual" {
		t.Fatalf("proxy group mode = %v, want manual", got)
	}
	assertNoSensitiveConfigMetadata(t, metadata)
}

func TestRenderWithPacksSelectionRejectsMissingProxyGroupNode(t *testing.T) {
	paths := writeRenderFixture(t)
	writeFile(t, paths.selection, `version: 1
proxy_groups:
  AI:
    nodes: ["🇯🇵日本01 | JP"]
    manual: true
enabled_packs:
  - source: sukkaw
    pack: ai
    target: AI
`)

	_, err := Render(Options{
		SourcePath:         paths.subscriptionNoJP,
		PolicyPath:         paths.policy,
		OutputPath:         filepath.Join(paths.dir, "empty-candidates.yaml"),
		PacksSelectionPath: paths.selection,
		RulesCacheDir:      paths.cacheDir,
		RuntimeProfilePath: filepath.Join(paths.dir, "localclash-runtime.yaml"),
		Force:              true,
	})
	if err == nil {
		t.Fatal("expected empty candidate error")
	}
}

func TestRenderAppliesActiveRuntimeProfile(t *testing.T) {
	paths := writeRenderFixture(t)
	profilePath := filepath.Join(paths.dir, "localclash-runtime.yaml")
	if _, err := runtimeprofile.Configure(profilePath, runtimeprofile.ModeRouter, ""); err != nil {
		t.Fatal(err)
	}

	result, err := Render(Options{
		SourcePath:         paths.subscription,
		PolicyPath:         paths.policy,
		OutputPath:         filepath.Join(paths.dir, "router.yaml"),
		RuntimeProfilePath: profilePath,
		RulesCacheDir:      paths.cacheDir,
		Force:              true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RuntimeMode != runtimeprofile.ModeRouter {
		t.Fatalf("runtime mode = %q, want %q", result.RuntimeMode, runtimeprofile.ModeRouter)
	}
	config := readTestYAML(t, result.OutputPath)
	if config["mixed-port"] != 7893 {
		t.Fatalf("mixed-port = %v, want router preset port", config["mixed-port"])
	}
	if config["allow-lan"] != true {
		t.Fatalf("allow-lan = %v, want true", config["allow-lan"])
	}
	if _, ok := config["proxies"].([]any); !ok {
		t.Fatalf("proxies should remain generated dynamic config, got %T", config["proxies"])
	}
	dns := config["dns"].(map[string]any)
	if dns["listen"] != "0.0.0.0:7874" {
		t.Fatalf("dns.listen = %v, want router DNS listen", dns["listen"])
	}
}

func TestRenderSmartCoreMaterializesAutoGroupsAsSmart(t *testing.T) {
	paths := writeRenderFixture(t)
	profilePath := filepath.Join(paths.dir, "localclash-runtime.yaml")
	if _, err := runtimeprofile.Configure(profilePath, "", runtimeprofile.CoreSmart); err != nil {
		t.Fatal(err)
	}
	writeFile(t, paths.selection, `version: 1
proxy_groups:
  AI:
    nodes: ["🇯🇵日本01 | JP"]
    auto: true
enabled_packs:
  - source: sukkaw
    pack: ai
    target: AI
`)

	result, err := Render(Options{
		SourcePath:         paths.subscription,
		PolicyPath:         paths.policy,
		OutputPath:         filepath.Join(paths.dir, "smart.yaml"),
		PacksSelectionPath: paths.selection,
		RulesCacheDir:      paths.cacheDir,
		RuntimeProfilePath: profilePath,
		Force:              true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Core != runtimeprofile.CoreSmart {
		t.Fatalf("core = %q, want smart", result.Core)
	}
	config := readTestYAML(t, result.OutputPath)
	for _, name := range []string{"AUTO", "AI"} {
		group := proxyGroupFromConfig(t, config, name)
		if group["type"] != "smart" {
			t.Fatalf("%s type = %v, want smart", name, group["type"])
		}
		if group["url"] != nil || group["interval"] != nil {
			t.Fatalf("%s kept url-test fields: %+v", name, group)
		}
		if group["uselightgbm"] != true || group["prefer-asn"] != true {
			t.Fatalf("%s smart options = %+v, want defaults", name, group)
		}
	}
	metadata := config["x-localclash"].(map[string]any)
	overlay := metadata["overlay"].(map[string]any)
	proxyGroups := overlay["proxy_groups"].([]any)
	if got := proxyGroups[0].(map[string]any)["mode"]; got != "auto" {
		t.Fatalf("metadata proxy group mode = %v, want original auto intent", got)
	}
}

func TestMergeRuleProvidersRejectsConflict(t *testing.T) {
	base := map[string]any{"applications": map[string]any{}}
	err := mergeRuleProviders(base, map[string]map[string]any{"applications": {}})
	if err == nil {
		t.Fatal("expected provider conflict error")
	}
}

func TestMergeProxyGroupsRejectsConflict(t *testing.T) {
	_, err := mergeProxyGroups([]map[string]any{{"name": "PROXY"}}, []map[string]any{{"name": "PROXY"}})
	if err == nil {
		t.Fatal("expected proxy-group conflict error")
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type renderFixturePaths struct {
	dir              string
	subscription     string
	subscriptionNoJP string
	policy           string
	cacheDir         string
	selection        string
}

func writeRenderFixture(t *testing.T) renderFixturePaths {
	t.Helper()
	dir := t.TempDir()
	paths := renderFixturePaths{
		dir:              dir,
		subscription:     filepath.Join(dir, "subscription.yaml"),
		subscriptionNoJP: filepath.Join(dir, "subscription-no-jp.yaml"),
		policy:           filepath.Join(dir, "policy.yaml"),
		cacheDir:         filepath.Join(dir, "packs"),
		selection:        filepath.Join(dir, "packs.yaml"),
	}
	writeFile(t, paths.subscription, `proxies:
  - name: "🇯🇵日本01 | JP"
    type: ss
    server: example.com
    port: 443
    cipher: none
    password: test
`)
	writeFile(t, paths.subscriptionNoJP, `proxies:
  - name: "HK 01"
    type: ss
    server: example.com
    port: 443
    cipher: none
    password: test
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
	if err := os.MkdirAll(paths.cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(paths.cacheDir, "sukkaw.yaml"), `version: 1
source: sukkaw
adapter: sukkaw
renderable: true
packs:
  - id: ai
    renderable: true
    components:
      - id: non_ip
        behavior: classical
        format: text
        order_class: non_ip
        url: https://ruleset.skk.moe/Clash/non_ip/ai.txt
        path: ./rule-packs/sukkaw/ai_non_ip.txt
`)
	writeFile(t, filepath.Join(paths.cacheDir, "blackmatrix7.yaml"), `version: 1
source: blackmatrix7
adapter: blackmatrix7
renderable: true
packs:
  - id: OpenAI
    renderable: true
    components:
      - id: OpenAI
        behavior: classical
        format: yaml
        order_class: mixed
        url: https://example.com/OpenAI.yaml
        path: ./rule-packs/blackmatrix7/OpenAI.yaml
`)
	writeFile(t, paths.selection, `version: 1
proxy_groups:
  AI:
    nodes: ["🇯🇵日本01 | JP"]
    manual: true
    direct: false
enabled_packs:
  - source: sukkaw
    pack: ai
    target: AI
  - source: blackmatrix7
    pack: OpenAI
    target: AI
`)
	return paths
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestYAML(t *testing.T, path string) map[string]any {
	t.Helper()
	out, err := readYAMLMap(path)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func proxyGroupNamesFromConfig(config map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, raw := range config["proxy-groups"].([]any) {
		group := raw.(map[string]any)
		out[group["name"].(string)] = true
	}
	return out
}

func proxyGroupFromConfig(t *testing.T, config map[string]any, name string) map[string]any {
	t.Helper()
	for _, raw := range config["proxy-groups"].([]any) {
		group := raw.(map[string]any)
		if group["name"] == name {
			return group
		}
	}
	t.Fatalf("missing proxy-group %q", name)
	return nil
}

func testStringSlice(raw any) []string {
	values := raw.([]any)
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value.(string))
	}
	return out
}

func indexOf(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}
	return -1
}

func assertNoSensitiveConfigMetadata(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, banned := range []string{"example.com", "password", "server", "cipher", "test"} {
		if strings.Contains(text, banned) {
			t.Fatalf("metadata leaked %q in %s", banned, text)
		}
	}
}
