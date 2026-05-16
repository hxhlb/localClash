package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"localclash/internal/configrender"

	"gopkg.in/yaml.v3"
)

type Options struct {
	CorePath         string
	SubscriptionPath string
	ConfigPath       string
	PolicyPath       string
	DashboardDir     string
	WorkDir          string
	JSON             bool
	Timeout          time.Duration
}

type Report struct {
	Status string  `json:"status"`
	Checks []Check `json:"checks"`
}

type Check struct {
	ID      string         `json:"id"`
	Title   string         `json:"title"`
	Status  string         `json:"status"`
	Path    string         `json:"path,omitempty"`
	Summary string         `json:"summary,omitempty"`
	Details []string       `json:"details,omitempty"`
	Metrics map[string]int `json:"metrics,omitempty"`
}

const (
	statusOK   = "ok"
	statusWarn = "warn"
	statusFail = "fail"
)

var builtInRuleTargets = map[string]bool{
	"DIRECT": true,
	"REJECT": true,
}

func Run(ctx context.Context, opts Options) (Report, error) {
	opts = normalizeOptions(opts)
	report := Report{}

	core := checkCore(ctx, opts)
	subscription := checkYAMLFile("subscription", "subscription.yaml", opts.SubscriptionPath)
	if subscription.Status == statusOK {
		checkSubscriptionProxyCount(&subscription)
	}
	config := checkYAMLFile("generated_config", "generated/mihomo.yaml", opts.ConfigPath)
	policy := checkYAMLFile("policy", "policy", opts.PolicyPath)
	if policy.Status == statusOK {
		checkPolicyMode(&policy)
	}

	report.add(core)
	report.add(subscription)
	report.add(config)
	report.add(policy)

	if config.Status == statusOK {
		configData, _ := readYAMLMap(opts.ConfigPath)
		report.add(checkLocalBaseline(configData))
		report.add(checkProxyGroupReferences(configData))
		report.add(checkRuleTargets(configData))
	}

	report.add(checkDashboard(opts.DashboardDir))
	report.add(checkMihomoTest(ctx, opts, core.Status, config.Status))
	report.Status = aggregateStatus(report.Checks)
	return report, nil
}

func normalizeOptions(opts Options) Options {
	opts.CorePath = strings.TrimSpace(opts.CorePath)
	opts.SubscriptionPath = strings.TrimSpace(opts.SubscriptionPath)
	opts.ConfigPath = strings.TrimSpace(opts.ConfigPath)
	opts.PolicyPath = strings.TrimSpace(opts.PolicyPath)
	opts.DashboardDir = strings.TrimSpace(opts.DashboardDir)
	if opts.CorePath == "" {
		opts.CorePath = "bin/mihomo"
	}
	if opts.SubscriptionPath == "" {
		opts.SubscriptionPath = "subscription.yaml"
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = "generated/mihomo.yaml"
	}
	if opts.PolicyPath == "" {
		opts.PolicyPath = "policies/loyalsoldier.yaml"
	}
	if opts.DashboardDir == "" {
		opts.DashboardDir = ".runtime/mihomo/ui/zashboard"
	}
	if opts.WorkDir == "" {
		opts.WorkDir = ".runtime/mihomo"
	}
	if opts.Timeout == 0 {
		opts.Timeout = 90 * time.Second
	}
	return opts
}

func (report *Report) add(check Check) {
	report.Checks = append(report.Checks, check)
}

func aggregateStatus(checks []Check) string {
	status := statusOK
	for _, check := range checks {
		switch check.Status {
		case statusFail:
			return statusFail
		case statusWarn:
			status = statusWarn
		}
	}
	return status
}

func checkCore(ctx context.Context, opts Options) Check {
	check := Check{ID: "core", Title: "core", Path: opts.CorePath}
	info, err := os.Stat(opts.CorePath)
	if err != nil {
		check.Status = statusFail
		check.Summary = fmt.Sprintf("core not found: %v", err)
		return check
	}
	if info.IsDir() {
		check.Status = statusFail
		check.Summary = "core path is a directory"
		return check
	}
	if info.Mode()&0o111 == 0 {
		check.Status = statusFail
		check.Summary = "core exists but is not executable"
		return check
	}

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, opts.CorePath, "-v").CombinedOutput()
	if err != nil {
		check.Status = statusFail
		check.Summary = "core exists but version command failed"
		check.Details = []string{compactOutput(output, err)}
		return check
	}
	check.Status = statusOK
	check.Summary = firstLine(output)
	return check
}

func checkYAMLFile(id, title, path string) Check {
	check := Check{ID: id, Title: title, Path: path}
	info, err := os.Stat(path)
	if err != nil {
		check.Status = statusFail
		check.Summary = fmt.Sprintf("file not found: %v", err)
		return check
	}
	if info.IsDir() {
		check.Status = statusFail
		check.Summary = "path is a directory"
		return check
	}
	if _, err := readYAMLMap(path); err != nil {
		check.Status = statusFail
		check.Summary = fmt.Sprintf("YAML parse failed: %v", err)
		return check
	}
	check.Status = statusOK
	check.Summary = "exists and parses"
	return check
}

func checkSubscriptionProxyCount(check *Check) {
	data, err := readYAMLMap(check.Path)
	if err != nil {
		return
	}
	proxies, ok := data["proxies"].([]any)
	if !ok || len(proxies) == 0 {
		check.Status = statusFail
		check.Summary = "subscription has no usable proxies"
		return
	}
	check.Summary = fmt.Sprintf("exists and parses; %d proxies", len(proxies))
	check.Metrics = map[string]int{"proxies": len(proxies)}
}

func checkPolicyMode(check *Check) {
	data, err := readYAMLMap(check.Path)
	if err != nil {
		return
	}
	modes, ok := data["modes"].(map[string]any)
	if !ok {
		check.Status = statusFail
		check.Summary = "policy has no modes"
		return
	}
	mode, ok := modes["default"].(string)
	if !ok || mode == "" {
		check.Status = statusFail
		check.Summary = "policy has no default mode"
		return
	}
	if mode != "whitelist" && mode != "blacklist" {
		check.Status = statusFail
		check.Summary = fmt.Sprintf("policy default mode %q is not whitelist or blacklist", mode)
		return
	}
	check.Summary = fmt.Sprintf("default mode: %s", mode)
	check.Metrics = map[string]int{"modes": len(modes) - 1}
}

func checkLocalBaseline(config map[string]any) Check {
	check := Check{ID: "local_safety_baseline", Title: "local safety baseline"}
	rules := stringList(config["rules"])
	missing := missingStrings(configrender.LocalBaselineRuleLines(), rules)
	if len(missing) > 0 {
		check.Status = statusFail
		check.Summary = fmt.Sprintf("%d local baseline rules are missing", len(missing))
		check.Details = limitDetails(missing, 8)
		check.Metrics = map[string]int{"missing": len(missing)}
		return check
	}
	check.Status = statusOK
	check.Summary = fmt.Sprintf("%d local baseline rules injected", len(configrender.LocalBaselineRuleLines()))
	check.Metrics = map[string]int{"rules": len(configrender.LocalBaselineRuleLines())}
	return check
}

func checkProxyGroupReferences(config map[string]any) Check {
	check := Check{ID: "proxy_group_references", Title: "proxy-groups references"}
	proxies := proxyNames(config["proxies"])
	groups, groupRefs := proxyGroups(config["proxy-groups"])
	if len(groups) == 0 {
		check.Status = statusFail
		check.Summary = "generated config has no proxy-groups"
		return check
	}

	allowed := map[string]bool{}
	for name := range builtInRuleTargets {
		allowed[name] = true
	}
	for name := range proxies {
		allowed[name] = true
	}
	for name := range groups {
		allowed[name] = true
	}

	var missing []string
	for group, refs := range groupRefs {
		for _, ref := range refs {
			if !allowed[ref] {
				missing = append(missing, fmt.Sprintf("%s -> %s", group, ref))
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		check.Status = statusFail
		check.Summary = fmt.Sprintf("%d proxy-group references are missing", len(missing))
		check.Details = limitDetails(missing, 12)
		check.Metrics = map[string]int{"missing": len(missing), "groups": len(groups), "proxies": len(proxies)}
		return check
	}
	check.Status = statusOK
	check.Summary = fmt.Sprintf("%d groups reference existing proxies/groups", len(groups))
	check.Metrics = map[string]int{"groups": len(groups), "proxies": len(proxies)}
	return check
}

func checkRuleTargets(config map[string]any) Check {
	check := Check{ID: "rule_targets", Title: "rules targets"}
	groupNames, _ := proxyGroups(config["proxy-groups"])
	providers := mapKeys(config["rule-providers"])
	allowed := map[string]bool{}
	for name := range builtInRuleTargets {
		allowed[name] = true
	}
	for name := range groupNames {
		allowed[name] = true
	}

	var missingTargets []string
	var missingProviders []string
	var unparsable []string
	for _, rule := range stringList(config["rules"]) {
		target, provider, ok := ruleTarget(rule)
		if !ok {
			unparsable = append(unparsable, rule)
			continue
		}
		if target != "" && !allowed[target] {
			missingTargets = append(missingTargets, rule)
		}
		if provider != "" && !providers[provider] {
			missingProviders = append(missingProviders, rule)
		}
	}
	sort.Strings(missingTargets)
	sort.Strings(missingProviders)
	sort.Strings(unparsable)

	if len(missingTargets)+len(missingProviders)+len(unparsable) > 0 {
		check.Status = statusFail
		check.Summary = fmt.Sprintf("%d rule issues found", len(missingTargets)+len(missingProviders)+len(unparsable))
		check.Details = appendIssueDetails(nil, "missing target", missingTargets)
		check.Details = appendIssueDetails(check.Details, "missing provider", missingProviders)
		check.Details = appendIssueDetails(check.Details, "unparsable rule", unparsable)
		check.Details = limitDetails(check.Details, 16)
		check.Metrics = map[string]int{
			"missing_targets":   len(missingTargets),
			"missing_providers": len(missingProviders),
			"unparsable":        len(unparsable),
		}
		return check
	}
	check.Status = statusOK
	check.Summary = "all rule targets and rule-providers resolve"
	return check
}

func checkDashboard(path string) Check {
	check := Check{ID: "zashboard", Title: "zashboard", Path: path}
	info, err := os.Stat(filepath.Join(path, "index.html"))
	if err != nil {
		check.Status = statusWarn
		check.Summary = fmt.Sprintf("zashboard index.html not found: %v", err)
		return check
	}
	if info.IsDir() {
		check.Status = statusFail
		check.Summary = "zashboard index.html is a directory"
		return check
	}
	check.Status = statusOK
	check.Summary = "downloaded"
	return check
}

func checkMihomoTest(ctx context.Context, opts Options, coreStatus, configStatus string) Check {
	check := Check{ID: "mihomo_test", Title: "mihomo -t", Path: opts.WorkDir}
	if coreStatus != statusOK || configStatus != statusOK {
		check.Status = statusFail
		check.Summary = "skipped because core or generated config failed earlier checks"
		return check
	}

	info, err := os.Stat(opts.WorkDir)
	if err != nil {
		check.Status = statusFail
		check.Summary = fmt.Sprintf("workdir snapshot source is not available: %v", err)
		return check
	}
	if !info.IsDir() {
		check.Status = statusFail
		check.Summary = "workdir snapshot source is not a directory"
		return check
	}
	workDir, cleanup, err := runtimeSnapshot(opts.WorkDir)
	if err != nil {
		check.Status = statusFail
		check.Summary = fmt.Sprintf("cannot create runtime snapshot: %v", err)
		return check
	}
	defer cleanup()

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, opts.CorePath, "-d", workDir, "-f", opts.ConfigPath, "-t")
	output, err := cmd.CombinedOutput()
	if err != nil {
		check.Status = statusFail
		check.Summary = "mihomo config test failed"
		check.Details = []string{compactOutput(output, err)}
		return check
	}
	check.Status = statusOK
	check.Summary = "mihomo config test passed"
	line := lastNonEmptyLine(output)
	if line != "" {
		check.Details = []string{line}
	}
	return check
}

func runtimeSnapshot(sourceDir string) (string, func(), error) {
	targetDir, err := os.MkdirTemp("", "localclash-doctor-mihomo-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(targetDir) }
	if err := copyRuntimeCache(sourceDir, targetDir); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return targetDir, cleanup, nil
}

func copyRuntimeCache(sourceDir, targetDir string) error {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "logs" || name == "ui" || name == "mihomo.log" {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		sourcePath := filepath.Join(sourceDir, name)
		targetPath := filepath.Join(targetDir, name)
		if entry.IsDir() {
			if err := copyDir(sourcePath, targetPath); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(sourcePath, targetPath); err != nil {
			return err
		}
	}
	return nil
}

func copyDir(sourceDir, targetDir string) error {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		sourcePath := filepath.Join(sourceDir, entry.Name())
		targetPath := filepath.Join(targetDir, entry.Name())
		if entry.IsDir() {
			if err := copyDir(sourcePath, targetPath); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(sourcePath, targetPath); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(sourcePath, targetPath string) error {
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(targetPath, data, 0o644)
}

func PrintText(report Report) {
	fmt.Println("localclash doctor")
	fmt.Printf("Status: %s\n\n", strings.ToUpper(report.Status))
	for _, check := range report.Checks {
		fmt.Printf("[%s] %s", strings.ToUpper(check.Status), check.Title)
		if check.Path != "" {
			fmt.Printf(" (%s)", check.Path)
		}
		if check.Summary != "" {
			fmt.Printf(": %s", check.Summary)
		}
		fmt.Println()
		for _, detail := range check.Details {
			fmt.Printf("     %s\n", detail)
		}
	}
}

func PrintJSON(report Report) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func readYAMLMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, errors.New("YAML document is empty")
	}
	return out, nil
}

func stringList(raw any) []string {
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func proxyNames(raw any) map[string]bool {
	out := map[string]bool{}
	values, ok := raw.([]any)
	if !ok {
		return out
	}
	for _, value := range values {
		item, ok := value.(map[string]any)
		if !ok {
			continue
		}
		name, ok := item["name"].(string)
		if ok && name != "" {
			out[name] = true
		}
	}
	return out
}

func proxyGroups(raw any) (map[string]bool, map[string][]string) {
	names := map[string]bool{}
	refs := map[string][]string{}
	values, ok := raw.([]any)
	if !ok {
		return names, refs
	}
	for _, value := range values {
		item, ok := value.(map[string]any)
		if !ok {
			continue
		}
		name, ok := item["name"].(string)
		if !ok || name == "" {
			continue
		}
		names[name] = true
		refs[name] = stringList(item["proxies"])
	}
	return names, refs
}

func mapKeys(raw any) map[string]bool {
	out := map[string]bool{}
	values, ok := raw.(map[string]any)
	if !ok {
		return out
	}
	for key := range values {
		out[key] = true
	}
	return out
}

func ruleTarget(rule string) (target string, provider string, ok bool) {
	parts := splitRule(rule)
	if len(parts) == 0 {
		return "", "", false
	}
	switch strings.ToUpper(parts[0]) {
	case "MATCH":
		if len(parts) < 2 {
			return "", "", false
		}
		return parts[1], "", true
	case "RULE-SET":
		if len(parts) < 3 {
			return "", "", false
		}
		return parts[2], parts[1], true
	default:
		if len(parts) < 3 {
			return "", "", false
		}
		return parts[2], "", true
	}
}

func splitRule(rule string) []string {
	raw := strings.Split(rule, ",")
	parts := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func missingStrings(required, actual []string) []string {
	present := map[string]bool{}
	for _, value := range actual {
		present[value] = true
	}
	var missing []string
	for _, value := range required {
		if !present[value] {
			missing = append(missing, value)
		}
	}
	return missing
}

func appendIssueDetails(details []string, label string, values []string) []string {
	for _, value := range values {
		details = append(details, label+": "+value)
	}
	return details
}

func limitDetails(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	out := append([]string{}, values[:limit]...)
	out = append(out, fmt.Sprintf("... %d more", len(values)-limit))
	return out
}

func compactOutput(output []byte, err error) string {
	lines := nonEmptyLines(output)
	if len(lines) == 0 {
		return err.Error()
	}
	const maxLines = 6
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, " | ")
}

func firstLine(output []byte) string {
	lines := nonEmptyLines(output)
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}

func lastNonEmptyLine(output []byte) string {
	lines := nonEmptyLines(output)
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

func nonEmptyLines(output []byte) []string {
	output = bytes.ReplaceAll(output, []byte("\r\n"), []byte("\n"))
	raw := strings.Split(string(output), "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
