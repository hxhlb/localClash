package doctor

import "testing"

func TestRuleTarget(t *testing.T) {
	tests := []struct {
		name         string
		rule         string
		wantTarget   string
		wantProvider string
		wantOK       bool
	}{
		{name: "match", rule: "MATCH,PROXY", wantTarget: "PROXY", wantOK: true},
		{name: "rule set", rule: "RULE-SET,private,DIRECT", wantTarget: "DIRECT", wantProvider: "private", wantOK: true},
		{name: "domain", rule: "DOMAIN-SUFFIX,local,DIRECT", wantTarget: "DIRECT", wantOK: true},
		{name: "cidr with no resolve", rule: "IP-CIDR,127.0.0.0/8,DIRECT,no-resolve", wantTarget: "DIRECT", wantOK: true},
		{name: "invalid", rule: "MATCH", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTarget, gotProvider, gotOK := ruleTarget(tt.rule)
			if gotOK != tt.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotTarget != tt.wantTarget {
				t.Fatalf("target = %q, want %q", gotTarget, tt.wantTarget)
			}
			if gotProvider != tt.wantProvider {
				t.Fatalf("provider = %q, want %q", gotProvider, tt.wantProvider)
			}
		})
	}
}

func TestCheckProxyGroupReferencesDetectsMissingReference(t *testing.T) {
	config := map[string]any{
		"proxies": []any{
			map[string]any{"name": "HK"},
		},
		"proxy-groups": []any{
			map[string]any{"name": "PROXY", "proxies": []any{"HK", "MISSING"}},
		},
	}

	check := checkProxyGroupReferences(config)
	if check.Status != statusFail {
		t.Fatalf("status = %s, want %s", check.Status, statusFail)
	}
	if check.Metrics["missing"] != 1 {
		t.Fatalf("missing = %d, want 1", check.Metrics["missing"])
	}
}

func TestCheckRuleTargetsDetectsMissingGroupAndProvider(t *testing.T) {
	config := map[string]any{
		"proxy-groups": []any{
			map[string]any{"name": "PROXY", "proxies": []any{"DIRECT"}},
		},
		"rule-providers": map[string]any{
			"private": map[string]any{},
		},
		"rules": []any{
			"RULE-SET,private,PROXY",
			"RULE-SET,missing-provider,PROXY",
			"DOMAIN,example.com,MISSING",
		},
	}

	check := checkRuleTargets(config)
	if check.Status != statusFail {
		t.Fatalf("status = %s, want %s", check.Status, statusFail)
	}
	if check.Metrics["missing_targets"] != 1 {
		t.Fatalf("missing_targets = %d, want 1", check.Metrics["missing_targets"])
	}
	if check.Metrics["missing_providers"] != 1 {
		t.Fatalf("missing_providers = %d, want 1", check.Metrics["missing_providers"])
	}
}

func TestAggregateStatus(t *testing.T) {
	if got := aggregateStatus([]Check{{Status: statusOK}, {Status: statusWarn}}); got != statusWarn {
		t.Fatalf("aggregateStatus warn = %s, want %s", got, statusWarn)
	}
	if got := aggregateStatus([]Check{{Status: statusOK}, {Status: statusFail}, {Status: statusWarn}}); got != statusFail {
		t.Fatalf("aggregateStatus fail = %s, want %s", got, statusFail)
	}
}
