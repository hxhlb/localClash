package configplan

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"localclash/internal/configinspect"
	"localclash/internal/configrender"
	"localclash/internal/localconfig"
	"localclash/internal/rules"
	"localclash/internal/runtimeprofile"

	"gopkg.in/yaml.v3"
)

type Options struct {
	PlanName            string
	Subscription        string
	Policy              string
	Mode                string
	RulesCache          string
	RuntimeProfilePath  string
	OutputDir           string
	ConfigPath          string
	SubscriptionConfig  string
	SubscriptionRuntime string
	Test                bool
	Overlay             OverlayIntent
	CorePath            string
	WorkDir             string
	Now                 time.Time
}

type ApplyOptions struct {
	PlanID              string
	PlansDir            string
	SummaryPath         string
	Subscription        string
	Policy              string
	Mode                string
	RulesCache          string
	RuntimeProfilePath  string
	ConfigPath          string
	SubscriptionConfig  string
	SubscriptionRuntime string
	SelectionPath       string
	OutputPath          string
	CorePath            string
	WorkDir             string
	BackupDir           string
	Test                bool
	Now                 time.Time
}

type OverlayIntent struct {
	Packs       []OverlayPackIntent       `json:"packs,omitempty" yaml:"packs,omitempty"`
	CustomRules []OverlayCustomRuleIntent `json:"custom_rules,omitempty" yaml:"custom_rules,omitempty"`
	ProxyGroups []OverlayProxyGroupIntent `json:"proxy_groups,omitempty" yaml:"proxy_groups,omitempty"`
}

type OverlayPackIntent struct {
	ID     string `json:"id" yaml:"id"`
	Target string `json:"target" yaml:"target"`
	Reason string `json:"reason,omitempty" yaml:"reason,omitempty"`
}

type OverlayProxyGroupIntent struct {
	ID       string             `json:"id" yaml:"id"`
	Nodes    []string           `json:"nodes,omitempty" yaml:"nodes,omitempty"`
	Match    *localconfig.Match `json:"match,omitempty" yaml:"match,omitempty"`
	Mode     string             `json:"mode" yaml:"mode"`
	Reason   string             `json:"reason,omitempty" yaml:"reason,omitempty"`
	Boundary string             `json:"boundary,omitempty" yaml:"boundary,omitempty"`
}

type OverlayCustomRuleIntent = localconfig.CustomRule

type Result struct {
	PlanID      string           `json:"patch_id"`
	Output      string           `json:"output"`
	SummaryPath string           `json:"summary_path"`
	ConfigPath  string           `json:"config_path"`
	Inputs      PlanInputs       `json:"inputs"`
	Valid       bool             `json:"valid"`
	MihomoTest  MihomoTestResult `json:"mihomo_test"`
	Overlay     OverlaySummary   `json:"overlay"`
	Changes     ChangesSummary   `json:"changes"`
	Warnings    []string         `json:"warnings"`
}

type PlanInputs struct {
	Subscription        string `json:"subscription"`
	Policy              string `json:"policy"`
	Mode                string `json:"mode,omitempty"`
	RulesCache          string `json:"rules_cache"`
	RuntimeProfilePath  string `json:"runtime_profile"`
	SubscriptionConfig  string `json:"subscription_config,omitempty"`
	SubscriptionRuntime string `json:"subscription_runtime,omitempty"`
}

type ApplyResult struct {
	Applied       bool                `json:"applied"`
	PlanID        string              `json:"patch_id"`
	SummaryPath   string              `json:"summary_path"`
	ConfigPath    string              `json:"config_path"`
	SelectionPath string              `json:"selection_path"`
	OutputPath    string              `json:"output_path"`
	Valid         bool                `json:"valid"`
	MihomoTest    MihomoTestResult    `json:"mihomo_test"`
	Overlay       OverlaySummary      `json:"overlay"`
	Render        configrender.Result `json:"render"`
	Backups       []BackupResult      `json:"backups,omitempty"`
	Warnings      []string            `json:"warnings"`
}

type BackupResult struct {
	Source string `json:"source"`
	Backup string `json:"backup"`
}

type MihomoTestResult struct {
	Enabled bool   `json:"enabled"`
	Passed  bool   `json:"passed"`
	Output  string `json:"output"`
}

type OverlaySummary struct {
	Packs       []OverlayPackSummary       `json:"packs"`
	CustomRules []OverlayCustomRuleSummary `json:"custom_rules"`
	ProxyGroups []OverlayProxyGroupSummary `json:"proxy_groups"`
}

type OverlayPackSummary struct {
	ID     string `json:"id"`
	Target string `json:"target"`
	Reason string `json:"reason,omitempty"`
}

type OverlayProxyGroupSummary struct {
	ID            string             `json:"id"`
	Nodes         []string           `json:"nodes"`
	SelectedNodes []string           `json:"selected_nodes,omitempty"`
	Match         *localconfig.Match `json:"match,omitempty"`
	Mode          string             `json:"mode"`
	NodeCount     int                `json:"node_count"`
	Reason        string             `json:"reason,omitempty"`
	Boundary      string             `json:"boundary,omitempty"`
}

type OverlayCustomRuleSummary struct {
	ID        string                       `json:"id"`
	Target    string                       `json:"target"`
	RuleCount int                          `json:"rule_count"`
	Reason    string                       `json:"reason,omitempty"`
	Rules     []localconfig.CustomRuleLine `json:"rules,omitempty"`
}

type ChangesSummary struct {
	RuleProvidersAdded int `json:"rule_providers_added"`
	ProxyGroupsAdded   int `json:"proxy_groups_added"`
	RulesAdded         int `json:"rules_added"`
}

func Render(ctx context.Context, opts Options) (Result, error) {
	opts = normalizeOptions(opts)
	if len(opts.Overlay.Packs) == 0 && len(opts.Overlay.CustomRules) == 0 {
		return Result{}, fmt.Errorf("overlay.packs or overlay.custom_rules is required")
	}

	config := configFromOverlay(opts.Overlay)
	resolved, err := localconfig.Resolve(localconfig.ResolveOptions{
		Config:              config,
		SubscriptionPath:    opts.Subscription,
		SubscriptionConfig:  opts.SubscriptionConfig,
		SubscriptionRuntime: opts.SubscriptionRuntime,
		RulesCache:          opts.RulesCache,
	})
	if err != nil {
		return Result{}, err
	}
	overlaySummary := overlaySummaryFromResolved(resolved)
	warnings := resolved.Warnings

	planID, err := allocatePlanID(opts.OutputDir, opts.PlanName, opts.Now)
	if err != nil {
		return Result{}, err
	}
	planDir := filepath.Join(opts.OutputDir, planID)
	outputPath := filepath.Join(planDir, "mihomo.yaml")
	summaryPath := filepath.Join(planDir, "summary.json")
	configPath := filepath.Join(planDir, "localclash.yaml")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		return Result{}, err
	}
	if err := localconfig.Write(configPath, resolved.Config); err != nil {
		return Result{}, err
	}

	selectionPath, cleanup, err := writeTempSelection(resolved.Selection)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()

	if _, err := configrender.Render(configrender.Options{
		SourcePath:         opts.Subscription,
		PolicyPath:         opts.Policy,
		Mode:               renderMode(opts.Mode),
		OutputPath:         outputPath,
		PacksSelectionPath: selectionPath,
		RulesCacheDir:      opts.RulesCache,
		RuntimeProfilePath: opts.RuntimeProfilePath,
		Force:              true,
	}); err != nil {
		return Result{}, err
	}

	overlayInspection, err := configinspect.InspectOverlay(configinspect.Options{ConfigPath: outputPath})
	if err != nil {
		return Result{}, err
	}
	result := Result{
		PlanID:      planID,
		Output:      outputPath,
		SummaryPath: summaryPath,
		ConfigPath:  configPath,
		Inputs: PlanInputs{
			Subscription:        opts.Subscription,
			Policy:              opts.Policy,
			Mode:                opts.Mode,
			RulesCache:          opts.RulesCache,
			RuntimeProfilePath:  opts.RuntimeProfilePath,
			SubscriptionConfig:  opts.SubscriptionConfig,
			SubscriptionRuntime: opts.SubscriptionRuntime,
		},
		Valid:      true,
		MihomoTest: MihomoTestResult{Enabled: opts.Test},
		Overlay:    overlaySummary,
		Changes: ChangesSummary{
			RuleProvidersAdded: len(overlayInspection.RuleProviders),
			ProxyGroupsAdded:   len(overlayInspection.ProxyGroups),
			RulesAdded:         len(overlayInspection.Rules),
		},
		Warnings: warnings,
	}
	if opts.Test {
		result.MihomoTest.Passed, result.MihomoTest.Output = runMihomoTest(ctx, opts, outputPath)
		result.Valid = result.MihomoTest.Passed
	}
	if err := writeSummary(summaryPath, result); err != nil {
		return Result{}, err
	}
	return result, nil
}

func Apply(ctx context.Context, opts ApplyOptions) (ApplyResult, error) {
	opts = normalizeApplyLocatorOptions(opts)
	summaryPath, err := resolveSummaryPath(opts)
	if err != nil {
		return ApplyResult{}, err
	}
	plan, err := readSummary(summaryPath)
	if err != nil {
		return ApplyResult{}, err
	}
	if !plan.Valid {
		return ApplyResult{}, fmt.Errorf("plan %q is not valid and cannot be applied", plan.PlanID)
	}
	if opts.PlanID == "" {
		opts.PlanID = plan.PlanID
	}
	opts = normalizeApplyOptions(applyPlanInputDefaults(opts, plan.Inputs))

	config, err := loadApplyConfig(opts, plan)
	if err != nil {
		return ApplyResult{}, err
	}
	resolved, err := localconfig.Resolve(localconfig.ResolveOptions{
		Config:              config,
		SubscriptionPath:    opts.Subscription,
		SubscriptionConfig:  opts.SubscriptionConfig,
		SubscriptionRuntime: opts.SubscriptionRuntime,
		RulesCache:          opts.RulesCache,
	})
	if err != nil {
		return ApplyResult{}, err
	}
	overlaySummary := overlaySummaryFromResolved(resolved)
	warnings := resolved.Warnings

	tempDir, err := os.MkdirTemp("", "localclash-plan-apply-*")
	if err != nil {
		return ApplyResult{}, err
	}
	defer os.RemoveAll(tempDir)
	tempSelection := filepath.Join(tempDir, "localclash-packs.yaml")
	tempOutput := filepath.Join(tempDir, "mihomo.yaml")
	if err := localconfig.WriteSelection(tempSelection, resolved.Selection); err != nil {
		return ApplyResult{}, err
	}
	renderResult, err := configrender.Render(configrender.Options{
		SourcePath:         opts.Subscription,
		PolicyPath:         opts.Policy,
		Mode:               renderMode(opts.Mode),
		OutputPath:         tempOutput,
		PacksSelectionPath: tempSelection,
		RulesCacheDir:      opts.RulesCache,
		RuntimeProfilePath: opts.RuntimeProfilePath,
		Force:              true,
	})
	if err != nil {
		return ApplyResult{}, err
	}

	result := ApplyResult{
		PlanID:        plan.PlanID,
		SummaryPath:   summaryPath,
		ConfigPath:    opts.ConfigPath,
		SelectionPath: opts.SelectionPath,
		OutputPath:    opts.OutputPath,
		Valid:         true,
		MihomoTest:    MihomoTestResult{Enabled: opts.Test},
		Overlay:       overlaySummary,
		Render:        renderResult,
		Warnings:      append([]string{}, plan.Warnings...),
	}
	result.Warnings = append(result.Warnings, warnings...)
	result.Render.OutputPath = opts.OutputPath
	if opts.Test {
		result.MihomoTest.Passed, result.MihomoTest.Output = runMihomoTest(ctx, Options{
			CorePath: opts.CorePath,
			WorkDir:  opts.WorkDir,
		}, tempOutput)
		result.Valid = result.MihomoTest.Passed
		if !result.Valid {
			return result, nil
		}
	}

	backups, err := backupApplyTargets(opts)
	if err != nil {
		return ApplyResult{}, err
	}
	result.Backups = backups
	if err := localconfig.Write(opts.ConfigPath, resolved.Config); err != nil {
		return ApplyResult{}, err
	}
	if err := localconfig.WriteSelection(opts.SelectionPath, resolved.Selection); err != nil {
		return ApplyResult{}, err
	}
	if err := copyFile(tempOutput, opts.OutputPath); err != nil {
		return ApplyResult{}, err
	}
	result.Applied = true
	return result, nil
}

func normalizeOptions(opts Options) Options {
	if opts.Subscription == "" {
		opts.Subscription = "subscription.yaml"
	}
	if opts.Policy == "" {
		opts.Policy = "policies/loyalsoldier.yaml"
	}
	if opts.RulesCache == "" {
		opts.RulesCache = ".runtime/rules/packs"
	}
	if opts.RuntimeProfilePath == "" {
		opts.RuntimeProfilePath = runtimeprofile.DefaultPath
	}
	if opts.OutputDir == "" {
		opts.OutputDir = filepath.Join(".runtime", "patches")
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = "localclash.yaml"
	}
	if opts.SubscriptionConfig == "" {
		opts.SubscriptionConfig = "localclash-subscriptions.yaml"
	}
	if opts.SubscriptionRuntime == "" {
		opts.SubscriptionRuntime = filepath.Join(".runtime", "subscriptions")
	}
	if opts.CorePath == "" {
		opts.CorePath = runtimeprofile.MetaCorePath
	}
	if opts.WorkDir == "" {
		opts.WorkDir = ".runtime/mihomo"
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	return opts
}

func normalizeApplyOptions(opts ApplyOptions) ApplyOptions {
	if opts.PlansDir == "" {
		opts.PlansDir = filepath.Join(".runtime", "patches")
	}
	if opts.Subscription == "" {
		opts.Subscription = "subscription.yaml"
	}
	if opts.Policy == "" {
		opts.Policy = "policies/loyalsoldier.yaml"
	}
	if opts.RulesCache == "" {
		opts.RulesCache = ".runtime/rules/packs"
	}
	if opts.RuntimeProfilePath == "" {
		opts.RuntimeProfilePath = runtimeprofile.DefaultPath
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = "localclash.yaml"
	}
	if opts.SubscriptionConfig == "" {
		opts.SubscriptionConfig = "localclash-subscriptions.yaml"
	}
	if opts.SubscriptionRuntime == "" {
		opts.SubscriptionRuntime = filepath.Join(".runtime", "subscriptions")
	}
	if opts.SelectionPath == "" {
		opts.SelectionPath = "localclash-packs.yaml"
	}
	if opts.OutputPath == "" {
		opts.OutputPath = "generated/mihomo.yaml"
	}
	if opts.CorePath == "" {
		opts.CorePath = runtimeprofile.MetaCorePath
	}
	if opts.WorkDir == "" {
		opts.WorkDir = ".runtime/mihomo"
	}
	if opts.BackupDir == "" {
		opts.BackupDir = filepath.Join(".runtime", "backups", "config-patch-apply")
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	return opts
}

func normalizeApplyLocatorOptions(opts ApplyOptions) ApplyOptions {
	if opts.PlansDir == "" {
		opts.PlansDir = filepath.Join(".runtime", "patches")
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	return opts
}

func applyPlanInputDefaults(opts ApplyOptions, inputs PlanInputs) ApplyOptions {
	if opts.Subscription == "" {
		opts.Subscription = inputs.Subscription
	}
	if opts.Policy == "" {
		opts.Policy = inputs.Policy
	}
	if opts.Mode == "" {
		opts.Mode = inputs.Mode
	}
	if opts.RulesCache == "" {
		opts.RulesCache = inputs.RulesCache
	}
	if opts.RuntimeProfilePath == "" {
		opts.RuntimeProfilePath = inputs.RuntimeProfilePath
	}
	if opts.SubscriptionConfig == "" {
		opts.SubscriptionConfig = inputs.SubscriptionConfig
	}
	if opts.SubscriptionRuntime == "" {
		opts.SubscriptionRuntime = inputs.SubscriptionRuntime
	}
	return opts
}

func configFromOverlay(overlay OverlayIntent) localconfig.Config {
	config := localconfig.Config{
		Version:     1,
		ProxyGroups: map[string]localconfig.ProxyGroup{},
		Packs:       make([]localconfig.Pack, 0, len(overlay.Packs)),
		CustomRules: make([]localconfig.CustomRule, 0, len(overlay.CustomRules)),
	}
	for _, group := range overlay.ProxyGroups {
		config.ProxyGroups[group.ID] = localconfig.ProxyGroup{
			Mode:     group.Mode,
			Match:    group.Match,
			Nodes:    append([]string{}, group.Nodes...),
			Reason:   group.Reason,
			Boundary: group.Boundary,
		}
	}
	for _, pack := range overlay.Packs {
		config.Packs = append(config.Packs, localconfig.Pack{ID: pack.ID, Target: pack.Target, Reason: pack.Reason})
	}
	for _, custom := range overlay.CustomRules {
		config.CustomRules = append(config.CustomRules, custom)
	}
	return config
}

func overlaySummaryFromResolved(resolved localconfig.Resolved) OverlaySummary {
	summary := OverlaySummary{
		Packs:       make([]OverlayPackSummary, 0, len(resolved.Packs)),
		CustomRules: make([]OverlayCustomRuleSummary, 0, len(resolved.CustomRules)),
		ProxyGroups: make([]OverlayProxyGroupSummary, 0, len(resolved.ProxyGroups)),
	}
	for _, pack := range resolved.Packs {
		summary.Packs = append(summary.Packs, OverlayPackSummary{ID: pack.ID, Target: pack.Target, Reason: pack.Reason})
	}
	for _, custom := range resolved.CustomRules {
		summary.CustomRules = append(summary.CustomRules, OverlayCustomRuleSummary{
			ID:        custom.ID,
			Target:    custom.Target,
			RuleCount: custom.RuleCount,
			Reason:    custom.Reason,
			Rules:     append([]localconfig.CustomRuleLine{}, custom.Rules...),
		})
	}
	for _, group := range resolved.ProxyGroups {
		nodes := append([]string{}, group.SelectedNodes...)
		summary.ProxyGroups = append(summary.ProxyGroups, OverlayProxyGroupSummary{
			ID:            group.ID,
			Nodes:         nodes,
			SelectedNodes: nodes,
			Match:         group.Match,
			Mode:          group.Mode,
			NodeCount:     group.NodeCount,
			Reason:        group.Reason,
			Boundary:      group.Boundary,
		})
	}
	return summary
}

func loadApplyConfig(opts ApplyOptions, plan Result) (localconfig.Config, error) {
	path := plan.ConfigPath
	if strings.TrimSpace(path) == "" && strings.TrimSpace(plan.SummaryPath) != "" {
		path = filepath.Join(filepath.Dir(plan.SummaryPath), "localclash.yaml")
	}
	if strings.TrimSpace(path) != "" {
		if config, err := localconfig.Load(path); err == nil {
			return config, nil
		} else if !os.IsNotExist(err) {
			return localconfig.Config{}, err
		}
	}
	return configFromOverlay(intentFromSummary(plan.Overlay)), nil
}

func buildSelection(opts Options, proxyNames []string) (rules.Selection, OverlaySummary, []string, error) {
	selected := make([]rules.SelectedPack, 0, len(opts.Overlay.Packs))
	overlayPacks := make([]OverlayPackSummary, 0, len(opts.Overlay.Packs))
	for _, pack := range opts.Overlay.Packs {
		ref, err := rules.ResolvePackRef(opts.RulesCache, pack.ID)
		if err != nil {
			return rules.Selection{}, OverlaySummary{}, nil, err
		}
		target := strings.TrimSpace(pack.Target)
		if target == "" {
			return rules.Selection{}, OverlaySummary{}, nil, fmt.Errorf("pack %q target is required", pack.ID)
		}
		selected = append(selected, rules.SelectedPack{Source: ref.Source, Pack: ref.Pack, Target: target})
		overlayPacks = append(overlayPacks, OverlayPackSummary{ID: ref.ID, Target: target})
	}

	proxyGroups := map[string]rules.ProxyGroup{}
	proxyGroupSummaries := make([]OverlayProxyGroupSummary, 0, len(opts.Overlay.ProxyGroups))
	for _, group := range opts.Overlay.ProxyGroups {
		id := strings.TrimSpace(group.ID)
		if id == "" {
			return rules.Selection{}, OverlaySummary{}, nil, fmt.Errorf("proxy group id is required")
		}
		if _, exists := proxyGroups[id]; exists {
			return rules.Selection{}, OverlaySummary{}, nil, fmt.Errorf("proxy group %q is defined more than once", id)
		}
		mode := strings.ToLower(strings.TrimSpace(group.Mode))
		if mode != "manual" && mode != "auto" && mode != "smart" {
			return rules.Selection{}, OverlaySummary{}, nil, fmt.Errorf("proxy group %q mode must be manual, auto, or smart", id)
		}
		if len(group.Nodes) == 0 {
			return rules.Selection{}, OverlaySummary{}, nil, fmt.Errorf("proxy group %q nodes is required", id)
		}
		nodes, err := validateProxyGroupNodes(id, group.Nodes, proxyNames)
		if err != nil {
			return rules.Selection{}, OverlaySummary{}, nil, err
		}
		pg := rules.ProxyGroup{Nodes: nodes}
		switch mode {
		case "manual":
			pg.Manual = true
		case "auto":
			pg.Auto = true
		case "smart":
			pg.Smart = true
		}
		proxyGroups[id] = pg
		proxyGroupSummaries = append(proxyGroupSummaries, OverlayProxyGroupSummary{ID: id, Nodes: append([]string(nil), nodes...), Mode: mode, NodeCount: len(nodes)})
	}

	for _, pack := range selected {
		if isBuiltInTarget(pack.Target) {
			continue
		}
		if _, ok := proxyGroups[pack.Target]; !ok {
			return rules.Selection{}, OverlaySummary{}, nil, fmt.Errorf("pack target %q requires a matching proxy group", pack.Target)
		}
	}

	sort.Slice(proxyGroupSummaries, func(i, j int) bool { return proxyGroupSummaries[i].ID < proxyGroupSummaries[j].ID })

	return rules.Selection{
		Version:     1,
		ProxyGroups: proxyGroups,
		EnabledPack: selected,
	}, OverlaySummary{Packs: overlayPacks, ProxyGroups: proxyGroupSummaries}, nil, nil
}

func validateProxyGroupNodes(groupID string, rawNodes []string, proxyNames []string) ([]string, error) {
	available := map[string]bool{}
	for _, name := range proxyNames {
		available[name] = true
	}
	nodes := make([]string, 0, len(rawNodes))
	seen := map[string]bool{}
	for _, rawNode := range rawNodes {
		node := strings.TrimSpace(rawNode)
		if node == "" {
			return nil, fmt.Errorf("proxy group %q has an empty node name", groupID)
		}
		if !available[node] {
			return nil, fmt.Errorf("proxy group %q references unknown subscription node %q", groupID, node)
		}
		if seen[node] {
			continue
		}
		seen[node] = true
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func writeTempSelection(selection rules.Selection) (string, func(), error) {
	file, err := os.CreateTemp("", "localclash-plan-selection-*.yaml")
	if err != nil {
		return "", nil, err
	}
	path := file.Name()
	data, err := yaml.Marshal(selection)
	if err != nil {
		file.Close()
		os.Remove(path)
		return "", nil, err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		os.Remove(path)
		return "", nil, err
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", nil, err
	}
	return path, func() { _ = os.Remove(path) }, nil
}

func writeSelection(path string, selection rules.Selection) error {
	data, err := yaml.Marshal(selection)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func writeSummary(path string, result Result) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func readSummary(path string) (Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}
	var result Result
	if err := json.Unmarshal(data, &result); err != nil {
		return Result{}, err
	}
	return result, nil
}

func resolveSummaryPath(opts ApplyOptions) (string, error) {
	if strings.TrimSpace(opts.SummaryPath) != "" {
		return opts.SummaryPath, nil
	}
	id := strings.TrimSpace(opts.PlanID)
	if id == "" {
		return "", fmt.Errorf("patch_id is required")
	}
	if filepath.Base(id) != id || id == "." || id == ".." {
		return "", fmt.Errorf("patch_id %q must be a single patch directory name", id)
	}
	return filepath.Join(opts.PlansDir, id, "summary.json"), nil
}

func intentFromSummary(summary OverlaySummary) OverlayIntent {
	intent := OverlayIntent{
		Packs:       make([]OverlayPackIntent, 0, len(summary.Packs)),
		CustomRules: make([]OverlayCustomRuleIntent, 0, len(summary.CustomRules)),
		ProxyGroups: make([]OverlayProxyGroupIntent, 0, len(summary.ProxyGroups)),
	}
	for _, pack := range summary.Packs {
		intent.Packs = append(intent.Packs, OverlayPackIntent{ID: pack.ID, Target: pack.Target, Reason: pack.Reason})
	}
	for _, custom := range summary.CustomRules {
		intent.CustomRules = append(intent.CustomRules, localconfig.CustomRule{
			ID:     custom.ID,
			Target: custom.Target,
			Reason: custom.Reason,
			Rules:  append([]localconfig.CustomRuleLine{}, custom.Rules...),
		})
	}
	for _, group := range summary.ProxyGroups {
		intent.ProxyGroups = append(intent.ProxyGroups, OverlayProxyGroupIntent{
			ID:       group.ID,
			Nodes:    append([]string{}, group.Nodes...),
			Match:    group.Match,
			Mode:     group.Mode,
			Reason:   group.Reason,
			Boundary: group.Boundary,
		})
	}
	return intent
}

func backupApplyTargets(opts ApplyOptions) ([]BackupResult, error) {
	backupRoot := filepath.Join(opts.BackupDir, buildPlanID(opts.PlanID, opts.Now))
	targets := []struct {
		source string
		name   string
	}{
		{source: opts.ConfigPath, name: "localclash.yaml"},
		{source: opts.SelectionPath, name: "localclash-packs.yaml"},
		{source: opts.OutputPath, name: "mihomo.yaml"},
	}
	var backups []BackupResult
	for _, target := range targets {
		if !fileExists(target.source) {
			continue
		}
		backup := filepath.Join(backupRoot, target.name)
		if err := copyFile(target.source, backup); err != nil {
			return nil, err
		}
		backups = append(backups, BackupResult{Source: target.source, Backup: backup})
	}
	return backups, nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

func runMihomoTest(ctx context.Context, opts Options, configPath string) (bool, string) {
	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, opts.CorePath, "-d", opts.WorkDir, "-f", configPath, "-t")
	output, err := cmd.CombinedOutput()
	return err == nil, compactOutput(output, err)
}

func compactOutput(output []byte, err error) string {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		if err != nil {
			return err.Error()
		}
		return ""
	}
	const maxLines = 8
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n")
}

func buildPlanID(name string, now time.Time) string {
	slug := slugify(name)
	if slug == "" {
		slug = "plan"
	}
	return slug + "-" + now.Format("20060102-150405")
}

func allocatePlanID(outputDir, name string, now time.Time) (string, error) {
	base := buildPlanID(name, now)
	id := base
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(outputDir, id)); err != nil {
			if os.IsNotExist(err) {
				return id, nil
			}
			return "", err
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	re := regexp.MustCompile(`[^a-z0-9]+`)
	value = re.ReplaceAllString(value, "-")
	return strings.Trim(value, "-")
}

func renderMode(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), "rule") {
		return ""
	}
	return mode
}

func isBuiltInTarget(target string) bool {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "direct", "reject", "proxy", "manual":
		return true
	default:
		return false
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
