package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestCheckConfigFileMissingPathIncludesResolvedPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join(dir, "policies"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "subscription.yaml"), []byte("proxies: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	check := checkConfigFile("policy", "policy", filepath.Join("policies", "loyalsoldier.json"))
	if check.Status != statusFail {
		t.Fatalf("status = %s, want %s", check.Status, statusFail)
	}
	details := strings.Join(check.Details, "\n")
	for _, want := range []string{
		"working directory: " + dir,
		"resolved path: " + filepath.Join(dir, "policies", "loyalsoldier.json"),
	} {
		if !strings.Contains(details, want) {
			t.Fatalf("details = %q, want %q", details, want)
		}
	}
}

func TestRunAlwaysIncludesWorkingDirectoryTree(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join(dir, "policies"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "subscription.yaml"), []byte("proxies: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Run(context.Background(), Options{
		CorePath:         filepath.Join(dir, "missing-core"),
		SubscriptionPath: "subscription.yaml",
		ConfigPath:       filepath.Join("generated", "mihomo.yaml"),
		PolicyPath:       filepath.Join("policies", "loyalsoldier.yaml"),
		DashboardDir:     filepath.Join(dir, "missing-dashboard"),
		WorkDir:          filepath.Join(dir, ".runtime", "mihomo"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var workingDir *Check
	for i := range report.Checks {
		if report.Checks[i].ID == "working_directory" {
			workingDir = &report.Checks[i]
			break
		}
	}
	if workingDir == nil {
		t.Fatal("doctor report missing working_directory check")
	}
	if workingDir.Status != statusOK || workingDir.Path != dir {
		t.Fatalf("working_directory = %+v, want ok at temp dir", *workingDir)
	}
	details := strings.Join(workingDir.Details, "\n")
	for _, want := range []string{"policies/", "subscription.yaml"} {
		if !strings.Contains(details, want) {
			t.Fatalf("working directory details = %q, want %q", details, want)
		}
	}
}

func TestRunIncludesMihomoConfigValidationCheck(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "mihomo")
	if err := os.WriteFile(core, []byte(`#!/bin/sh
for arg in "$@"; do
  if [ "$arg" = "-v" ]; then
    echo "Mihomo Meta test"
    exit 0
  fi
  if [ "$arg" = "-t" ]; then
    echo "configuration file test is successful"
    exit 0
  fi
done
exit 0
`), 0o755); err != nil {
		t.Fatal(err)
	}
	subscription := filepath.Join(dir, "subscription.yaml")
	if err := os.WriteFile(subscription, []byte("proxies:\n  - name: SG\n    type: ss\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "mihomo.yaml")
	if err := os.WriteFile(config, []byte("proxies: []\nproxy-groups: []\nrules: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	policy := filepath.Join(dir, "policy.yaml")
	if err := os.WriteFile(policy, []byte("modes:\n  default: whitelist\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	report, err := Run(context.Background(), Options{
		CorePath:         core,
		SubscriptionPath: subscription,
		ConfigPath:       config,
		PolicyPath:       policy,
		DashboardDir:     filepath.Join(dir, "missing-dashboard"),
		WorkDir:          workDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	var mihomoTest *Check
	for i := range report.Checks {
		if report.Checks[i].ID == "mihomo_test" {
			mihomoTest = &report.Checks[i]
			break
		}
	}
	if mihomoTest == nil {
		t.Fatal("doctor report missing mihomo_test check")
	}
	if mihomoTest.Status != statusOK || mihomoTest.Summary != "mihomo config test passed" {
		t.Fatalf("mihomo_test = %+v, want ok config validation", *mihomoTest)
	}
}
