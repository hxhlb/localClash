package rules

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRenderFragmentUsesSelectionAndPackCache(t *testing.T) {
	selection := Selection{EnabledPack: []SelectedPack{
		{Source: "blackmatrix7", Pack: "OpenAI", Target: "proxy"},
	}}
	caches := map[string]PackCache{
		"blackmatrix7": {
			Source:  "blackmatrix7",
			Adapter: "blackmatrix7",
			Packs: []Pack{
				{
					ID:         "OpenAI",
					Renderable: true,
					Components: []Component{
						{
							ID:         "OpenAI",
							Behavior:   "classical",
							Format:     "yaml",
							OrderClass: "mixed",
							URL:        "https://example.com/OpenAI.yaml",
							Path:       "./rule-packs/blackmatrix7/OpenAI.yaml",
						},
					},
				},
			},
		},
	}

	fragment, err := RenderFragment(selection, caches)
	if err != nil {
		t.Fatal(err)
	}
	if len(fragment.RuleProviders) != 1 {
		t.Fatalf("providers = %d, want 1", len(fragment.RuleProviders))
	}
	if got := fragment.Rules[0]; got != "RULE-SET,blackmatrix7_OpenAI,PROXY" {
		t.Fatalf("rule = %q, want RULE-SET,blackmatrix7_OpenAI,PROXY", got)
	}
}

func TestRenderFragmentRejectsNonRenderablePack(t *testing.T) {
	selection := Selection{EnabledPack: []SelectedPack{
		{Source: "v2fly-dlc", Pack: "apple", Target: "proxy"},
	}}
	caches := map[string]PackCache{
		"v2fly-dlc": {
			Source: "v2fly-dlc",
			Packs: []Pack{
				{ID: "apple", Renderable: false, Reason: "requires conversion"},
			},
		},
	}

	if _, err := RenderFragment(selection, caches); err == nil {
		t.Fatal("expected non-renderable pack error")
	}
}

func TestSelectionWithVirtualTargetParses(t *testing.T) {
	raw := []byte(`
version: 1
node_labels:
  JP:
    match:
      - "🇯🇵"
      - "日本"
      - "\\bJP\\b"
  SG:
    match:
      - "🇸🇬"
      - "新加坡"
      - "\\bSG\\b"
  US:
    match:
      - "🇺🇸"
      - "美国"
      - "\\bUS\\b"
virtual_targets:
  AI:
    candidates:
      labels: [JP, SG, US]
    manual: true
    direct: false
enabled_packs:
  - source: sukkaw
    pack: ai
    target: AI
`)
	var selection Selection
	if err := yaml.Unmarshal(raw, &selection); err != nil {
		t.Fatal(err)
	}
	if len(selection.NodeLabels) != 3 {
		t.Fatalf("node labels = %d, want 3", len(selection.NodeLabels))
	}
	if !selection.VirtualTargets["AI"].Manual || selection.VirtualTargets["AI"].Auto {
		t.Fatalf("AI virtual target = %+v, want manual only", selection.VirtualTargets["AI"])
	}
}

func TestClassifyProxyNamesByLabelRegex(t *testing.T) {
	labels := map[string]NodeLabel{
		"JP": {Match: []string{"🇯🇵", "日本", "\\bJP\\b", "Japan"}},
		"SG": {Match: []string{"🇸🇬", "新加坡", "\\bSG\\b", "Singapore"}},
		"US": {Match: []string{"🇺🇸", "美国", "\\bUS\\b", "USA"}},
	}
	classified, err := ClassifyProxyNames([]string{
		"🇯🇵 Tokyo 01",
		"新加坡 SG 02",
		"US Los Angeles",
		"Hong Kong 01",
	}, labels)
	if err != nil {
		t.Fatal(err)
	}
	if got := classified["JP"][0]; got != "🇯🇵 Tokyo 01" {
		t.Fatalf("JP = %q, want Tokyo", got)
	}
	if got := classified["SG"][0]; got != "新加坡 SG 02" {
		t.Fatalf("SG = %q, want Singapore", got)
	}
	if got := classified["US"][0]; got != "US Los Angeles" {
		t.Fatalf("US = %q, want US Los Angeles", got)
	}
}

func TestClassifyProxyNamesReportsRegexCompileError(t *testing.T) {
	_, err := ClassifyProxyNames([]string{"JP 01"}, map[string]NodeLabel{
		"JP": {Match: []string{"("}},
	})
	if err == nil {
		t.Fatal("expected regex compile error")
	}
	if got := err.Error(); !strings.Contains(got, `node label "JP" pattern "(" is invalid`) {
		t.Fatalf("error = %q, want clear label and pattern context", got)
	}
}

func TestRenderFragmentMaterializesVirtualTarget(t *testing.T) {
	selection := Selection{
		NodeLabels: map[string]NodeLabel{
			"JP": {Match: []string{"🇯🇵", "\\bJP\\b"}},
			"SG": {Match: []string{"🇸🇬", "\\bSG\\b"}},
			"US": {Match: []string{"🇺🇸", "\\bUS\\b"}},
		},
		VirtualTargets: map[string]VirtualTarget{
			"AI": {
				Candidates: VirtualTargetCandidates{Labels: []string{"JP", "SG", "US"}},
				Manual:     true,
				Direct:     false,
			},
		},
		EnabledPack: []SelectedPack{
			{Source: "sukkaw", Pack: "ai", Target: "AI"},
			{Source: "blackmatrix7", Pack: "OpenAI", Target: "AI"},
		},
	}
	fragment, err := RenderFragment(selection, testPackCaches(), []string{"🇯🇵 Tokyo", "SG Singapore", "🇺🇸 US"})
	if err != nil {
		t.Fatal(err)
	}
	if got := fragment.Rules[0]; got != "RULE-SET,sukkaw_ai_non_ip,AI" {
		t.Fatalf("first rule = %q, want target AI", got)
	}
	if got := fragment.Rules[1]; got != "RULE-SET,blackmatrix7_OpenAI,AI" {
		t.Fatalf("second rule = %q, want target AI", got)
	}
	groupNames := proxyGroupNames(fragment.ProxyGroups)
	if !groupNames["AI"] {
		t.Fatalf("missing proxy group AI in %+v", groupNames)
	}
	for _, unwanted := range []string{"AI_AUTO", "AI_MANUAL", "JP", "SG", "US"} {
		if groupNames[unwanted] {
			t.Fatalf("%q should not be materialized as a proxy group", unwanted)
		}
	}
}

func TestRenderFragmentRejectsConflictingVirtualTargetModes(t *testing.T) {
	selection := Selection{
		NodeLabels: map[string]NodeLabel{
			"JP": {Match: []string{"🇯🇵"}},
		},
		VirtualTargets: map[string]VirtualTarget{
			"AI": {
				Candidates: VirtualTargetCandidates{Labels: []string{"JP"}},
				Auto:       true,
				Manual:     true,
			},
		},
		EnabledPack: []SelectedPack{
			{Source: "sukkaw", Pack: "ai", Target: "AI"},
		},
	}
	if _, err := RenderFragment(selection, testPackCaches(), []string{"🇯🇵 Tokyo"}); err == nil {
		t.Fatal("expected conflicting virtual target mode error")
	}
}

func TestRenderFragmentRejectsUnknownTarget(t *testing.T) {
	selection := Selection{EnabledPack: []SelectedPack{
		{Source: "sukkaw", Pack: "ai", Target: "MISSING"},
	}}
	if _, err := RenderFragment(selection, testPackCaches()); err == nil {
		t.Fatal("expected unknown target error")
	}
}

func TestRenderFragmentRejectsEmptyVirtualTargetCandidates(t *testing.T) {
	selection := Selection{
		NodeLabels: map[string]NodeLabel{
			"JP": {Match: []string{"🇯🇵"}},
		},
		VirtualTargets: map[string]VirtualTarget{
			"AI": {
				Candidates: VirtualTargetCandidates{Labels: []string{"JP"}},
				Manual:     true,
			},
		},
		EnabledPack: []SelectedPack{
			{Source: "sukkaw", Pack: "ai", Target: "AI"},
		},
	}
	if _, err := RenderFragment(selection, testPackCaches(), []string{"HK 01"}); err == nil {
		t.Fatal("expected empty virtual target candidate error")
	}
}

func TestValidateSourceRequiresAdapterFields(t *testing.T) {
	if err := validateSource(Source{ID: "sukkaw", Adapter: "sukkaw", URL: "https://github.com/SukkaW/Surge"}); err == nil {
		t.Fatal("expected missing base_url error")
	}
	if err := validateSource(Source{ID: "blackmatrix7", Adapter: "blackmatrix7", URL: "https://github.com/blackmatrix7/ios_rule_script/tree/master/rule/Clash"}); err == nil {
		t.Fatal("expected missing raw_base_url error")
	}
}

func TestGitHubAPIContentsURL(t *testing.T) {
	got, err := githubAPIContentsURL("https://github.com/blackmatrix7/ios_rule_script/tree/master/rule/Clash")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://api.github.com/repos/blackmatrix7/ios_rule_script/contents/rule/Clash?ref=master"
	if got != want {
		t.Fatalf("api url = %q, want %q", got, want)
	}
}

func TestSortComponentsUsesRulesetOrder(t *testing.T) {
	components := []Component{
		{ID: "ip", OrderClass: "ip"},
		{ID: "domainset", OrderClass: "domainset"},
		{ID: "non_ip", OrderClass: "non_ip"},
	}
	sortComponents(components)
	if got := components[0].OrderClass; got != "domainset" {
		t.Fatalf("first = %q, want domainset", got)
	}
	if got := components[1].OrderClass; got != "non_ip" {
		t.Fatalf("second = %q, want non_ip", got)
	}
	if got := components[2].OrderClass; got != "ip" {
		t.Fatalf("third = %q, want ip", got)
	}
}

func testPackCaches() map[string]PackCache {
	return map[string]PackCache{
		"sukkaw": {
			Source: "sukkaw",
			Packs: []Pack{
				{
					ID:         "ai",
					Renderable: true,
					Components: []Component{
						{
							ID:         "non_ip",
							Behavior:   "classical",
							Format:     "text",
							OrderClass: "non_ip",
							URL:        "https://example.com/ai.txt",
							Path:       "./rule-packs/sukkaw/ai_non_ip.txt",
						},
					},
				},
			},
		},
		"blackmatrix7": {
			Source: "blackmatrix7",
			Packs: []Pack{
				{
					ID:         "OpenAI",
					Renderable: true,
					Components: []Component{
						{
							ID:         "OpenAI",
							Behavior:   "classical",
							Format:     "yaml",
							OrderClass: "mixed",
							URL:        "https://example.com/OpenAI.yaml",
							Path:       "./rule-packs/blackmatrix7/OpenAI.yaml",
						},
					},
				},
			},
		},
	}
}

func proxyGroupNames(groups []map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, group := range groups {
		if name, ok := group["name"].(string); ok {
			out[name] = true
		}
	}
	return out
}
