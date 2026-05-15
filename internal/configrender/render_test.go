package configrender

import "testing"

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

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
