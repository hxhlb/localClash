package configrender

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"localclash/internal/configmeta"
	"localclash/internal/rules"
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

func TestRenderDefaultOverlayUsesBaseManualAutoSelectors(t *testing.T) {
	paths := writeRenderFixture(t)
	writeFile(t, paths.policy, `rule_source:
  base_url: https://example.com/rules
  update_interval: 86400
groups:
  direct: DIRECT
  reject: REJECT
  proxy: PROXY
  auto: ⚡ 自动选择
  manual: 🎯 手动选择
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
        target: direct
`)
	writeFile(t, paths.selection, `version: 1
proxy_groups:
  "🌐 全球直连":
    direct: true
  "🇭🇰 香港节点":
    nodes: ["🇯🇵日本01 | JP"]
    auto: true
    optional: true
policy_groups:
  "🎮 Steam":
    exits: [AUTO, MANUAL, "🌐 全球直连", "🇭🇰 香港节点"]
    manual: true
enabled_packs:
  - source: blackmatrix7
    pack: OpenAI
    target: "🎮 Steam"
`)

	result, err := Render(Options{
		SourcePath:         paths.subscription,
		PolicyPath:         paths.policy,
		OutputPath:         filepath.Join(paths.dir, "default-overlay.yaml"),
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
	for _, want := range []string{"⚡ 自动选择", "🎯 手动选择", "🎮 Steam", "🇭🇰 香港节点"} {
		if !groups[want] {
			t.Fatalf("missing proxy-group %q in %+v", want, groups)
		}
	}
	for _, unwanted := range []string{"AUTO", "MANUAL", "PROXY"} {
		if groups[unwanted] {
			t.Fatalf("proxy-group %q should not be emitted for default overlay", unwanted)
		}
	}
	steam := proxyGroupFromConfig(t, config, "🎮 Steam")
	wantSteam := []string{"⚡ 自动选择", "🎯 手动选择", "🌐 全球直连", "🇭🇰 香港节点"}
	if got := steam["proxies"].([]any); len(got) != len(wantSteam) {
		t.Fatalf("Steam proxies = %+v, want %+v", got, wantSteam)
	} else {
		for i, want := range wantSteam {
			if got[i] != want {
				t.Fatalf("Steam proxies = %+v, want %+v", got, wantSteam)
			}
		}
	}
	manual := proxyGroupFromConfig(t, config, "🎯 手动选择")
	wantManualPrefix := []string{"⚡ 自动选择", "🇭🇰 香港节点", "🇯🇵日本01 | JP"}
	if got := manual["proxies"].([]any); len(got) < len(wantManualPrefix) {
		t.Fatalf("manual proxies = %+v, want prefix %+v", got, wantManualPrefix)
	} else {
		for i, want := range wantManualPrefix {
			if got[i] != want {
				t.Fatalf("manual proxies = %+v, want prefix %+v", got, wantManualPrefix)
			}
		}
	}
	order := proxyGroupOrderFromConfig(config)
	if len(order) < 2 || order[0] != "🎯 手动选择" || order[1] != "⚡ 自动选择" {
		t.Fatalf("proxy group order = %+v, want manual and auto selectors first", order)
	}
	if indexOf(order, "🇭🇰 香港节点") < indexOf(order, "🎯 手动选择") {
		t.Fatalf("region group order = %+v, want region groups after non-region groups", order)
	}
}

func TestRenderOmitsLegacyAppleGroupWhenPolicyDoesNotDefineIt(t *testing.T) {
	paths := writeRenderFixture(t)
	writeFile(t, paths.policy, `rule_source:
  base_url: https://example.com/rules
  update_interval: 86400
groups:
  direct: DIRECT
  reject: REJECT
  proxy: PROXY
  auto: AUTO
  manual: MANUAL
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
`)

	result, err := Render(Options{
		SourcePath:         paths.subscription,
		PolicyPath:         paths.policy,
		OutputPath:         filepath.Join(paths.dir, "without-legacy-apple.yaml"),
		RuntimeProfilePath: filepath.Join(paths.dir, "localclash-runtime.yaml"),
		Force:              true,
	})
	if err != nil {
		t.Fatal(err)
	}
	config := readTestYAML(t, result.OutputPath)
	if proxyGroupNamesFromConfig(config)["Apple"] {
		t.Fatal("legacy Apple proxy-group should not be generated without groups.apple")
	}
	if _, ok := config["rule-providers"].(map[string]any)["apple"]; ok {
		t.Fatal("legacy apple rule-provider should not be generated without apple provider")
	}
	for _, rule := range testStringSlice(config["rules"]) {
		if strings.Contains(rule, "RULE-SET,apple,") {
			t.Fatalf("legacy apple rule should not be generated: %s", rule)
		}
	}
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
	writeRenderPackIndex(t, paths.cacheDir)
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

func writeRenderPackIndex(t *testing.T, cacheDir string) {
	t.Helper()
	if err := rules.WritePackIndex(rules.PackIndexPath(cacheDir), map[string]rules.PackCache{
		"sukkaw": {
			Version:    1,
			Source:     "sukkaw",
			Adapter:    "sukkaw",
			Renderable: true,
			Packs: []rules.Pack{{
				ID:         "ai",
				Renderable: true,
				Components: []rules.Component{{
					ID:         "non_ip",
					Behavior:   "classical",
					Format:     "text",
					OrderClass: "non_ip",
					URL:        "https://ruleset.skk.moe/Clash/non_ip/ai.txt",
					Path:       "./rule-packs/sukkaw/ai_non_ip.txt",
				}},
			}},
		},
		"blackmatrix7": {
			Version:    1,
			Source:     "blackmatrix7",
			Adapter:    "blackmatrix7",
			Renderable: true,
			Packs: []rules.Pack{{
				ID:         "OpenAI",
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

func proxyGroupOrderFromConfig(config map[string]any) []string {
	var out []string
	for _, raw := range config["proxy-groups"].([]any) {
		group := raw.(map[string]any)
		out = append(out, group["name"].(string))
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
