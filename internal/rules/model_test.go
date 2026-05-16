package rules

import "testing"

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
