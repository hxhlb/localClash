package configplan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"localclash/internal/configrender"
	"localclash/internal/localconfig"
	"localclash/internal/mihomotest"
	"localclash/internal/rules"
	"localclash/internal/runtimeprofile"
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
	OnStage             func(StageEvent) `json:"-"`
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
	TestExplicit        bool
	Now                 time.Time
	OnStage             func(StageEvent) `json:"-"`
}

type StageEvent struct {
	Stage      string         `json:"stage"`
	Event      string         `json:"event"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	Error      string         `json:"error,omitempty"`
	Fields     map[string]any `json:"fields,omitempty"`
}

type OverlayIntent struct {
	Packs         []OverlayPackIntent         `json:"packs,omitempty" yaml:"packs,omitempty"`
	CustomRules   []OverlayCustomRuleIntent   `json:"custom_rules,omitempty" yaml:"custom_rules,omitempty"`
	RuleProviders []OverlayRuleProviderIntent `json:"rule_providers,omitempty" yaml:"rule_providers,omitempty"`
	ProxyGroups   []OverlayProxyGroupIntent   `json:"proxy_groups,omitempty" yaml:"proxy_groups,omitempty"`
	PolicyGroups  []OverlayPolicyGroupIntent  `json:"policy_groups,omitempty" yaml:"policy_groups,omitempty"`
}

type OverlayPackIntent struct {
	ID     string `json:"id" yaml:"id"`
	Type   string `json:"type,omitempty" yaml:"type,omitempty"`
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

type OverlayPolicyGroupIntent struct {
	ID       string   `json:"id" yaml:"id"`
	Mode     string   `json:"mode" yaml:"mode"`
	Exits    []string `json:"exits" yaml:"exits"`
	Reason   string   `json:"reason,omitempty" yaml:"reason,omitempty"`
	Boundary string   `json:"boundary,omitempty" yaml:"boundary,omitempty"`
}

type OverlayCustomRuleIntent = localconfig.CustomRule
type OverlayRuleProviderIntent = localconfig.ExternalRuleProvider

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
	NextActions []string         `json:"next_actions,omitempty"`
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
	NextActions   []string            `json:"next_actions,omitempty"`
}

type BackupResult struct {
	Source string `json:"source"`
	Backup string `json:"backup"`
}

type MihomoTestResult struct {
	Enabled       bool   `json:"enabled"`
	Passed        bool   `json:"passed"`
	Output        string `json:"output"`
	Error         string `json:"error,omitempty"`
	TimedOut      bool   `json:"timed_out,omitempty"`
	DurationMS    int64  `json:"duration_ms,omitempty"`
	ExitCode      int    `json:"exit_code,omitempty"`
	Isolated      bool   `json:"isolated,omitempty"`
	WorkDir       string `json:"work_dir,omitempty"`
	SourceWorkDir string `json:"source_work_dir,omitempty"`
}

type OverlaySummary struct {
	Packs         []OverlayPackSummary         `json:"packs"`
	CustomRules   []OverlayCustomRuleSummary   `json:"custom_rules"`
	RuleProviders []OverlayRuleProviderSummary `json:"rule_providers"`
	ProxyGroups   []OverlayProxyGroupSummary   `json:"proxy_groups"`
	PolicyGroups  []OverlayPolicyGroupSummary  `json:"policy_groups"`
}

type OverlayPackSummary struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
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

type OverlayPolicyGroupSummary struct {
	ID        string   `json:"id"`
	Mode      string   `json:"mode"`
	Exits     []string `json:"exits"`
	ExitCount int      `json:"exit_count"`
	Reason    string   `json:"reason,omitempty"`
	Boundary  string   `json:"boundary,omitempty"`
}

type OverlayCustomRuleSummary struct {
	ID        string                       `json:"id"`
	Target    string                       `json:"target"`
	RuleCount int                          `json:"rule_count"`
	Reason    string                       `json:"reason,omitempty"`
	Rules     []localconfig.CustomRuleLine `json:"rules,omitempty"`
}

type OverlayRuleProviderSummary struct {
	ID       string `json:"id"`
	Target   string `json:"target"`
	Reason   string `json:"reason,omitempty"`
	Type     string `json:"type"`
	Behavior string `json:"behavior"`
	Format   string `json:"format"`
	Path     string `json:"path"`
	URL      string `json:"url,omitempty"`
	Interval int    `json:"interval,omitempty"`
}

type ChangesSummary struct {
	RuleProvidersAdded int `json:"rule_providers_added"`
	ProxyGroupsAdded   int `json:"proxy_groups_added"`
	PolicyGroupsAdded  int `json:"policy_groups_added"`
	RulesAdded         int `json:"rules_added"`
}

func Render(ctx context.Context, opts Options) (Result, error) {
	opts = normalizeOptions(opts)
	if len(opts.Overlay.Packs) == 0 && len(opts.Overlay.CustomRules) == 0 && len(opts.Overlay.RuleProviders) == 0 {
		return Result{}, fmt.Errorf("overlay.packs, overlay.custom_rules, or overlay.rule_providers is required")
	}
	stage := configPlanStageEmitter(opts.OnStage)

	finish := stage("resolve_candidate_config", nil)
	config, err := configWithOverlay(opts.ConfigPath, opts.Overlay)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	resolved, err := localconfig.Resolve(localconfig.ResolveOptions{
		Config:              config,
		SubscriptionPath:    opts.Subscription,
		SubscriptionConfig:  opts.SubscriptionConfig,
		SubscriptionRuntime: opts.SubscriptionRuntime,
		RulesCache:          opts.RulesCache,
		OnStage:             nestedLocalConfigStage(opts.OnStage, "resolve_candidate_config"),
	})
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	overlaySummary := requestedOverlaySummary(resolved, opts.Overlay, opts.RulesCache)
	warnings := resolved.Warnings
	finish(nil, map[string]any{
		"proxy_groups":  len(resolved.ProxyGroups),
		"policy_groups": len(resolved.PolicyGroups),
		"packs":         len(resolved.Packs),
		"custom_rules":  len(resolved.CustomRules),
	})

	finish = stage("allocate_patch", map[string]any{"patches_dir": opts.OutputDir, "patch_name": opts.PlanName})
	planID, err := allocatePlanID(opts.OutputDir, opts.PlanName, opts.Now)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	planDir := filepath.Join(opts.OutputDir, planID)
	outputPath := filepath.Join(planDir, "mihomo.yaml")
	summaryPath := filepath.Join(planDir, "summary.json")
	configPath := filepath.Join(planDir, "localclash.json")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, map[string]any{"patch_id": planID, "patch_dir": planDir})

	finish = stage("write_candidate_config", map[string]any{"config": configPath})
	if err := localconfig.Write(configPath, resolved.Config); err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, nil)

	finish = stage("write_candidate_selection", nil)
	selectionPath, cleanup, err := writeTempSelection(resolved.Selection)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	defer cleanup()
	finish(nil, map[string]any{"selection": selectionPath})

	finish = stage("render_candidate", map[string]any{"output": outputPath})
	if _, err := configrender.Render(configrender.Options{
		SourcePath:         opts.Subscription,
		PolicyPath:         opts.Policy,
		Mode:               renderMode(opts.Mode),
		OutputPath:         outputPath,
		PacksSelectionPath: selectionPath,
		RulesCacheDir:      opts.RulesCache,
		RuntimeProfilePath: opts.RuntimeProfilePath,
		Force:              true,
		OnStage:            nestedConfigRenderStage(opts.OnStage, "render_candidate"),
	}); err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, nil)

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
		Changes:    changesSummaryFromOverlay(opts.Overlay, overlaySummary),
		Warnings:   warnings,
		NextActions: []string{
			"Review this patch output and summary before applying.",
			"Apply only the exact patch_id returned here; do not invent or reuse a patch_id.",
			"After config_patch_apply, call config_status to verify durable intent and generated overlay.",
		},
	}
	if opts.Test {
		finish = stage("mihomo_test", map[string]any{"core": opts.CorePath, "source_work_dir": opts.WorkDir, "isolated": true})
		result.MihomoTest = runMihomoTest(ctx, opts, outputPath)
		finish(mihomoStageError(result.MihomoTest), map[string]any{
			"passed":      result.MihomoTest.Passed,
			"timed_out":   result.MihomoTest.TimedOut,
			"duration_ms": result.MihomoTest.DurationMS,
			"exit_code":   result.MihomoTest.ExitCode,
			"work_dir":    result.MihomoTest.WorkDir,
		})
		result.Valid = result.MihomoTest.Passed
		if !result.Valid {
			result.NextActions = mihomoTestFailureNextActions(result.MihomoTest)
		}
	}
	finish = stage("write_summary", map[string]any{"summary": summaryPath})
	if err := writeSummary(summaryPath, result); err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, map[string]any{"valid": result.Valid})
	return result, nil
}

func Apply(ctx context.Context, opts ApplyOptions) (ApplyResult, error) {
	stage := configPlanStageEmitter(opts.OnStage)
	opts = normalizeApplyLocatorOptions(opts)
	finish := stage("load_patch_summary", map[string]any{"patch_id": opts.PlanID, "patches_dir": opts.PlansDir, "summary": opts.SummaryPath})
	summaryPath, err := resolveSummaryPath(opts)
	if err != nil {
		finish(err, nil)
		return ApplyResult{}, err
	}
	plan, err := readSummary(summaryPath)
	if err != nil {
		finish(err, nil)
		return ApplyResult{}, err
	}
	if !plan.Valid {
		finish(fmt.Errorf("plan %q is not valid and cannot be applied", plan.PlanID), nil)
		return ApplyResult{}, fmt.Errorf("plan %q is not valid and cannot be applied", plan.PlanID)
	}
	finish(nil, map[string]any{"patch_id": plan.PlanID, "summary": summaryPath})
	if opts.PlanID == "" {
		opts.PlanID = plan.PlanID
	}
	opts = normalizeApplyOptions(applyPlanInputDefaults(opts, plan.Inputs))

	finish = stage("resolve_apply_config", nil)
	config, err := loadApplyConfig(opts, plan)
	if err != nil {
		finish(err, nil)
		return ApplyResult{}, err
	}
	config = preserveExistingPolicyTemplate(opts.ConfigPath, config)
	resolved, err := localconfig.Resolve(localconfig.ResolveOptions{
		Config:              config,
		SubscriptionPath:    opts.Subscription,
		SubscriptionConfig:  opts.SubscriptionConfig,
		SubscriptionRuntime: opts.SubscriptionRuntime,
		RulesCache:          opts.RulesCache,
		OnStage:             nestedLocalConfigStage(opts.OnStage, "resolve_apply_config"),
	})
	if err != nil {
		finish(err, nil)
		return ApplyResult{}, err
	}
	overlaySummary := overlaySummaryFromResolved(resolved)
	warnings := resolved.Warnings
	finish(nil, map[string]any{
		"proxy_groups":  len(resolved.ProxyGroups),
		"policy_groups": len(resolved.PolicyGroups),
		"packs":         len(resolved.Packs),
		"custom_rules":  len(resolved.CustomRules),
	})

	finish = stage("prepare_temp_render", nil)
	tempDir, err := os.MkdirTemp("", "localclash-plan-apply-*")
	if err != nil {
		finish(err, nil)
		return ApplyResult{}, err
	}
	defer os.RemoveAll(tempDir)
	tempSelection := filepath.Join(tempDir, "localclash-packs.gob")
	tempOutput := filepath.Join(tempDir, "mihomo.yaml")
	if err := localconfig.WriteSelection(tempSelection, resolved.Selection); err != nil {
		finish(err, nil)
		return ApplyResult{}, err
	}
	finish(nil, map[string]any{"temp_dir": tempDir})

	finish = stage("render_candidate", map[string]any{"output": tempOutput})
	renderResult, err := configrender.Render(configrender.Options{
		SourcePath:         opts.Subscription,
		PolicyPath:         opts.Policy,
		Mode:               renderMode(opts.Mode),
		OutputPath:         tempOutput,
		PacksSelectionPath: tempSelection,
		RulesCacheDir:      opts.RulesCache,
		RuntimeProfilePath: opts.RuntimeProfilePath,
		Force:              true,
		OnStage:            nestedConfigRenderStage(opts.OnStage, "render_candidate"),
	})
	if err != nil {
		finish(err, nil)
		return ApplyResult{}, err
	}
	finish(nil, map[string]any{"proxy_count": renderResult.ProxyCount, "rule_count": renderResult.RuleCount})

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
		NextActions: []string{
			"Call config_status to verify the applied durable intent and generated overlay.",
			"Call runtime_status to see whether Mihomo is already running; config changes do not restart runtime automatically.",
			"Ask for confirmation before run_runtime or stop_runtime.",
		},
	}
	result.Warnings = append(result.Warnings, warnings...)
	result.Render.OutputPath = opts.OutputPath
	if opts.Test {
		finish = stage("mihomo_test", map[string]any{"core": opts.CorePath, "source_work_dir": opts.WorkDir, "isolated": true})
		result.MihomoTest = runMihomoTest(ctx, Options{
			CorePath: opts.CorePath,
			WorkDir:  opts.WorkDir,
		}, tempOutput)
		finish(mihomoStageError(result.MihomoTest), map[string]any{
			"passed":      result.MihomoTest.Passed,
			"timed_out":   result.MihomoTest.TimedOut,
			"duration_ms": result.MihomoTest.DurationMS,
			"exit_code":   result.MihomoTest.ExitCode,
			"work_dir":    result.MihomoTest.WorkDir,
		})
		result.Valid = result.MihomoTest.Passed
		if !result.Valid {
			result.NextActions = mihomoTestFailureNextActions(result.MihomoTest)
			return result, nil
		}
	}

	finish = stage("backup_apply_targets", map[string]any{"backup_dir": opts.BackupDir})
	backups, err := backupApplyTargets(opts)
	if err != nil {
		finish(err, nil)
		return ApplyResult{}, err
	}
	result.Backups = backups
	finish(nil, map[string]any{"backup_count": len(backups)})

	finish = stage("write_active_config", map[string]any{"config": opts.ConfigPath, "selection": opts.SelectionPath, "output": opts.OutputPath})
	if err := localconfig.Write(opts.ConfigPath, resolved.Config); err != nil {
		finish(err, nil)
		return ApplyResult{}, err
	}
	if err := localconfig.WriteSelection(opts.SelectionPath, resolved.Selection); err != nil {
		finish(err, nil)
		return ApplyResult{}, err
	}
	if err := copyFile(tempOutput, opts.OutputPath); err != nil {
		finish(err, nil)
		return ApplyResult{}, err
	}
	result.Applied = true
	finish(nil, nil)
	return result, nil
}

func configPlanStageEmitter(callback func(StageEvent)) func(string, map[string]any) func(error, map[string]any) {
	return func(stage string, fields map[string]any) func(error, map[string]any) {
		if callback == nil {
			return func(error, map[string]any) {}
		}
		started := time.Now()
		callback(StageEvent{Stage: stage, Event: "started", Fields: fields})
		return func(err error, doneFields map[string]any) {
			event := StageEvent{
				Stage:      stage,
				Event:      "done",
				DurationMS: time.Since(started).Milliseconds(),
				Fields:     doneFields,
			}
			if err != nil {
				event.Event = "error"
				event.Error = err.Error()
			}
			callback(event)
		}
	}
}

func nestedConfigRenderStage(callback func(StageEvent), parent string) func(configrender.StageEvent) {
	if callback == nil {
		return nil
	}
	return func(event configrender.StageEvent) {
		callback(StageEvent{
			Stage:      parent + "." + event.Stage,
			Event:      event.Event,
			DurationMS: event.DurationMS,
			Error:      event.Error,
			Fields:     event.Fields,
		})
	}
}

func nestedLocalConfigStage(callback func(StageEvent), parent string) func(localconfig.StageEvent) {
	if callback == nil {
		return nil
	}
	return func(event localconfig.StageEvent) {
		callback(StageEvent{
			Stage:      parent + "." + event.Stage,
			Event:      event.Event,
			DurationMS: event.DurationMS,
			Error:      event.Error,
			Fields:     event.Fields,
		})
	}
}

func mihomoStageError(result MihomoTestResult) error {
	if result.Passed || result.Error == "" {
		return nil
	}
	return errors.New(result.Error)
}

func normalizeOptions(opts Options) Options {
	if opts.Subscription == "" {
		opts.Subscription = "subscription.gob"
	}
	if opts.Policy == "" {
		opts.Policy = "policies/loyalsoldier.json"
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
		opts.ConfigPath = "localclash.json"
	}
	if opts.SubscriptionConfig == "" {
		opts.SubscriptionConfig = "localclash-subscriptions.json"
	}
	if opts.SubscriptionRuntime == "" {
		opts.SubscriptionRuntime = filepath.Join(".runtime", "subscriptions")
	}
	if opts.CorePath == "" {
		opts.CorePath = activeRuntimeCorePath(opts.RuntimeProfilePath)
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
		opts.Subscription = "subscription.gob"
	}
	if opts.Policy == "" {
		opts.Policy = "policies/loyalsoldier.json"
	}
	if opts.RulesCache == "" {
		opts.RulesCache = ".runtime/rules/packs"
	}
	if opts.RuntimeProfilePath == "" {
		opts.RuntimeProfilePath = runtimeprofile.DefaultPath
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = "localclash.json"
	}
	if opts.SubscriptionConfig == "" {
		opts.SubscriptionConfig = "localclash-subscriptions.json"
	}
	if opts.SubscriptionRuntime == "" {
		opts.SubscriptionRuntime = filepath.Join(".runtime", "subscriptions")
	}
	if opts.SelectionPath == "" {
		opts.SelectionPath = "localclash-packs.gob"
	}
	if opts.OutputPath == "" {
		opts.OutputPath = "generated/mihomo.yaml"
	}
	if opts.CorePath == "" {
		opts.CorePath = activeRuntimeCorePath(opts.RuntimeProfilePath)
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

func activeRuntimeCorePath(runtimeProfilePath string) string {
	corePath, err := runtimeprofile.ActiveCorePath(runtimeProfilePath)
	if err == nil && strings.TrimSpace(corePath) != "" {
		return corePath
	}
	return runtimeprofile.MetaCorePath
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
		Version:       1,
		ProxyGroups:   map[string]localconfig.ProxyGroup{},
		PolicyGroups:  map[string]localconfig.PolicyGroup{},
		Packs:         make([]localconfig.Pack, 0, len(overlay.Packs)),
		CustomRules:   make([]localconfig.CustomRule, 0, len(overlay.CustomRules)),
		RuleProviders: make([]localconfig.ExternalRuleProvider, 0, len(overlay.RuleProviders)),
	}
	if len(overlay.PolicyGroups) > 0 {
		config.Version = 2
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
	for _, group := range overlay.PolicyGroups {
		config.PolicyGroups[group.ID] = localconfig.PolicyGroup{
			Mode:     group.Mode,
			Exits:    append([]string{}, group.Exits...),
			Reason:   group.Reason,
			Boundary: group.Boundary,
		}
	}
	for _, pack := range overlay.Packs {
		config.Packs = append(config.Packs, localconfig.Pack{ID: pack.ID, Type: pack.Type, Target: pack.Target, Reason: pack.Reason})
	}
	for _, custom := range overlay.CustomRules {
		config.CustomRules = append(config.CustomRules, custom)
	}
	for _, provider := range overlay.RuleProviders {
		config.RuleProviders = append(config.RuleProviders, provider)
	}
	return config
}

func configWithOverlay(path string, overlay OverlayIntent) (localconfig.Config, error) {
	base, err := localconfig.Load(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return localconfig.Config{}, err
		}
		base = localconfig.Config{
			Version:     1,
			ProxyGroups: map[string]localconfig.ProxyGroup{},
		}
	}
	return mergeOverlayConfig(base, configFromOverlay(overlay)), nil
}

func mergeOverlayConfig(base localconfig.Config, overlay localconfig.Config) localconfig.Config {
	if base.Version == 0 {
		base.Version = 1
	}
	if base.ProxyGroups == nil {
		base.ProxyGroups = map[string]localconfig.ProxyGroup{}
	}
	if base.PolicyGroups == nil {
		base.PolicyGroups = map[string]localconfig.PolicyGroup{}
	}
	for id, group := range overlay.ProxyGroups {
		base.ProxyGroups[id] = group
	}
	for id, group := range overlay.PolicyGroups {
		base.PolicyGroups[id] = group
	}
	base.Packs = mergePacks(base.Packs, overlay.Packs)
	base.CustomRules = mergeCustomRules(base.CustomRules, overlay.CustomRules)
	base.RuleProviders = mergeRuleProviders(base.RuleProviders, overlay.RuleProviders)
	if overlay.Version > base.Version {
		base.Version = overlay.Version
	}
	if len(base.PolicyGroups) > 0 && base.Version < 2 {
		base.Version = 2
	}
	return base
}

func mergePacks(base []localconfig.Pack, overlay []localconfig.Pack) []localconfig.Pack {
	merged := append([]localconfig.Pack{}, base...)
	index := map[string]int{}
	for i, item := range merged {
		index[strings.TrimSpace(item.ID)] = i
	}
	for _, item := range overlay {
		id := strings.TrimSpace(item.ID)
		if i, ok := index[id]; ok {
			merged[i] = item
			continue
		}
		index[id] = len(merged)
		merged = append(merged, item)
	}
	return merged
}

func mergeCustomRules(base []localconfig.CustomRule, overlay []localconfig.CustomRule) []localconfig.CustomRule {
	merged := append([]localconfig.CustomRule{}, base...)
	index := map[string]int{}
	for i, item := range merged {
		index[strings.TrimSpace(item.ID)] = i
	}
	for _, item := range overlay {
		id := strings.TrimSpace(item.ID)
		if i, ok := index[id]; ok {
			merged[i] = item
			continue
		}
		index[id] = len(merged)
		merged = append(merged, item)
	}
	return merged
}

func mergeRuleProviders(base []localconfig.ExternalRuleProvider, overlay []localconfig.ExternalRuleProvider) []localconfig.ExternalRuleProvider {
	merged := append([]localconfig.ExternalRuleProvider{}, base...)
	index := map[string]int{}
	for i, item := range merged {
		index[strings.TrimSpace(item.ID)] = i
	}
	for _, item := range overlay {
		id := strings.TrimSpace(item.ID)
		if i, ok := index[id]; ok {
			merged[i] = item
			continue
		}
		index[id] = len(merged)
		merged = append(merged, item)
	}
	return merged
}

func preserveExistingPolicyTemplate(path string, config localconfig.Config) localconfig.Config {
	if strings.TrimSpace(config.PolicyTemplate) != "" {
		return config
	}
	existing, err := localconfig.Load(path)
	if err != nil {
		return config
	}
	config.PolicyTemplate = existing.PolicyTemplate
	return config
}

func overlaySummaryFromResolved(resolved localconfig.Resolved) OverlaySummary {
	summary := OverlaySummary{
		Packs:         make([]OverlayPackSummary, 0, len(resolved.Packs)),
		CustomRules:   make([]OverlayCustomRuleSummary, 0, len(resolved.CustomRules)),
		RuleProviders: make([]OverlayRuleProviderSummary, 0, len(resolved.RuleProviders)),
		ProxyGroups:   make([]OverlayProxyGroupSummary, 0, len(resolved.ProxyGroups)),
		PolicyGroups:  make([]OverlayPolicyGroupSummary, 0, len(resolved.PolicyGroups)),
	}
	for _, pack := range resolved.Packs {
		summary.Packs = append(summary.Packs, OverlayPackSummary{ID: pack.ID, Type: pack.Type, Target: pack.Target, Reason: pack.Reason})
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
	for _, provider := range resolved.RuleProviders {
		summary.RuleProviders = append(summary.RuleProviders, OverlayRuleProviderSummary{
			ID:       provider.ID,
			Target:   provider.Target,
			Reason:   provider.Reason,
			Type:     provider.Type,
			Behavior: provider.Behavior,
			Format:   provider.Format,
			Path:     provider.Path,
			URL:      provider.URL,
			Interval: provider.Interval,
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
	for _, group := range resolved.PolicyGroups {
		summary.PolicyGroups = append(summary.PolicyGroups, OverlayPolicyGroupSummary{
			ID:        group.ID,
			Mode:      group.Mode,
			Exits:     append([]string{}, group.Exits...),
			ExitCount: group.ExitCount,
			Reason:    group.Reason,
			Boundary:  group.Boundary,
		})
	}
	return summary
}

func requestedOverlaySummary(resolved localconfig.Resolved, overlay OverlayIntent, rulesCache string) OverlaySummary {
	full := overlaySummaryFromResolved(resolved)
	summary := OverlaySummary{
		Packs:         make([]OverlayPackSummary, 0, len(overlay.Packs)),
		CustomRules:   make([]OverlayCustomRuleSummary, 0, len(overlay.CustomRules)),
		RuleProviders: make([]OverlayRuleProviderSummary, 0, len(overlay.RuleProviders)),
		ProxyGroups:   make([]OverlayProxyGroupSummary, 0, len(overlay.ProxyGroups)),
		PolicyGroups:  make([]OverlayPolicyGroupSummary, 0, len(overlay.PolicyGroups)),
	}
	packsByID := map[string]OverlayPackSummary{}
	for _, item := range full.Packs {
		packsByID[item.ID] = item
	}
	var packIndex *rules.PackIndex
	if len(overlay.Packs) > 0 {
		packIndex, _ = rules.LoadPackIndex(rules.PackIndexPath(rulesCache))
	}
	for _, item := range overlay.Packs {
		id := strings.TrimSpace(item.ID)
		if found, ok := packsByID[id]; ok {
			summary.Packs = append(summary.Packs, found)
			continue
		}
		if packIndex != nil {
			ref, err := packIndex.ResolvePackRef(id)
			if err == nil {
				summary.Packs = append(summary.Packs, OverlayPackSummary{ID: ref.ID, Type: ref.Type, Target: item.Target, Reason: item.Reason})
				continue
			}
		}
		summary.Packs = append(summary.Packs, OverlayPackSummary{ID: item.ID, Type: item.Type, Target: item.Target, Reason: item.Reason})
	}

	customRulesByID := map[string]OverlayCustomRuleSummary{}
	for _, item := range full.CustomRules {
		customRulesByID[item.ID] = item
	}
	for _, item := range overlay.CustomRules {
		id := strings.TrimSpace(item.ID)
		if found, ok := customRulesByID[id]; ok {
			summary.CustomRules = append(summary.CustomRules, found)
		}
	}

	ruleProvidersByID := map[string]OverlayRuleProviderSummary{}
	for _, item := range full.RuleProviders {
		ruleProvidersByID[item.ID] = item
	}
	for _, item := range overlay.RuleProviders {
		id := strings.TrimSpace(item.ID)
		if found, ok := ruleProvidersByID[id]; ok {
			summary.RuleProviders = append(summary.RuleProviders, found)
		}
	}

	proxyGroupsByID := map[string]OverlayProxyGroupSummary{}
	for _, item := range full.ProxyGroups {
		proxyGroupsByID[item.ID] = item
	}
	for _, item := range overlay.ProxyGroups {
		id := strings.TrimSpace(item.ID)
		if found, ok := proxyGroupsByID[id]; ok {
			summary.ProxyGroups = append(summary.ProxyGroups, found)
		}
	}

	policyGroupsByID := map[string]OverlayPolicyGroupSummary{}
	for _, item := range full.PolicyGroups {
		policyGroupsByID[item.ID] = item
	}
	for _, item := range overlay.PolicyGroups {
		id := strings.TrimSpace(item.ID)
		if found, ok := policyGroupsByID[id]; ok {
			summary.PolicyGroups = append(summary.PolicyGroups, found)
		}
	}
	return summary
}

func changesSummaryFromOverlay(overlay OverlayIntent, summary OverlaySummary) ChangesSummary {
	changes := ChangesSummary{
		ProxyGroupsAdded:   len(overlay.ProxyGroups),
		PolicyGroupsAdded:  len(overlay.PolicyGroups),
		RuleProvidersAdded: len(overlay.RuleProviders),
		RulesAdded:         len(summary.Packs) + len(summary.RuleProviders),
	}
	for _, pack := range summary.Packs {
		if pack.Type == rules.PackTypeRuleProvider {
			changes.RuleProvidersAdded++
		}
	}
	for _, custom := range summary.CustomRules {
		changes.RulesAdded += custom.RuleCount
	}
	return changes
}

func loadApplyConfig(opts ApplyOptions, plan Result) (localconfig.Config, error) {
	path := plan.ConfigPath
	if strings.TrimSpace(path) == "" && strings.TrimSpace(plan.SummaryPath) != "" {
		path = filepath.Join(filepath.Dir(plan.SummaryPath), "localclash.json")
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
	var packIndex *rules.PackIndex
	if len(opts.Overlay.Packs) > 0 {
		var err error
		packIndex, err = rules.LoadPackIndex(rules.PackIndexPath(opts.RulesCache))
		if err != nil {
			return rules.Selection{}, OverlaySummary{}, nil, err
		}
	}
	for _, pack := range opts.Overlay.Packs {
		ref, err := packIndex.ResolvePackRef(pack.ID)
		if err != nil {
			return rules.Selection{}, OverlaySummary{}, nil, err
		}
		if err := assertOverlayPackType(pack.ID, pack.Type, ref.Type); err != nil {
			return rules.Selection{}, OverlaySummary{}, nil, err
		}
		target := strings.TrimSpace(pack.Target)
		if target == "" {
			return rules.Selection{}, OverlaySummary{}, nil, fmt.Errorf("pack %q target is required", pack.ID)
		}
		selected = append(selected, rules.SelectedPack{Source: ref.Source, Pack: ref.Pack, Target: target})
		overlayPacks = append(overlayPacks, OverlayPackSummary{ID: ref.ID, Type: ref.Type, Target: target})
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
		if mode != "manual" && mode != "auto" && mode != "smart" && mode != "direct" {
			return rules.Selection{}, OverlaySummary{}, nil, fmt.Errorf("proxy group %q mode must be manual, auto, smart, or direct", id)
		}
		if mode == "direct" && (len(group.Nodes) > 0 || group.Match != nil) {
			return rules.Selection{}, OverlaySummary{}, nil, fmt.Errorf("proxy group %q direct mode cannot use match or nodes", id)
		}
		if mode != "direct" && len(group.Nodes) == 0 {
			return rules.Selection{}, OverlaySummary{}, nil, fmt.Errorf("proxy group %q nodes is required", id)
		}
		var nodes []string
		var err error
		if mode != "direct" {
			nodes, err = validateProxyGroupNodes(id, group.Nodes, proxyNames)
			if err != nil {
				return rules.Selection{}, OverlaySummary{}, nil, err
			}
		}
		pg := rules.ProxyGroup{Nodes: nodes}
		switch mode {
		case "manual":
			pg.Manual = true
		case "auto":
			pg.Auto = true
		case "smart":
			pg.Smart = true
		case "direct":
			pg.Direct = true
		}
		proxyGroups[id] = pg
		proxyGroupSummaries = append(proxyGroupSummaries, OverlayProxyGroupSummary{ID: id, Nodes: append([]string(nil), nodes...), Mode: mode, NodeCount: len(nodes)})
	}

	policyGroups, policyGroupSummaries, err := buildPolicyGroupsFromOverlay(opts.Overlay.PolicyGroups, proxyGroups)
	if err != nil {
		return rules.Selection{}, OverlaySummary{}, nil, err
	}
	for _, pack := range selected {
		if isBuiltInTarget(pack.Target) {
			continue
		}
		if _, ok := proxyGroups[pack.Target]; ok {
			continue
		}
		if _, ok := policyGroups[pack.Target]; !ok {
			return rules.Selection{}, OverlaySummary{}, nil, fmt.Errorf("pack target %q requires a matching proxy group or policy group", pack.Target)
		}
	}

	sort.Slice(proxyGroupSummaries, func(i, j int) bool { return proxyGroupSummaries[i].ID < proxyGroupSummaries[j].ID })
	sort.Slice(policyGroupSummaries, func(i, j int) bool { return policyGroupSummaries[i].ID < policyGroupSummaries[j].ID })

	return rules.Selection{
		Version:      1,
		ProxyGroups:  proxyGroups,
		PolicyGroups: policyGroups,
		EnabledPack:  selected,
	}, OverlaySummary{Packs: overlayPacks, ProxyGroups: proxyGroupSummaries, PolicyGroups: policyGroupSummaries}, nil, nil
}

func buildPolicyGroupsFromOverlay(groups []OverlayPolicyGroupIntent, proxyGroups map[string]rules.ProxyGroup) (map[string]rules.PolicyGroup, []OverlayPolicyGroupSummary, error) {
	policyGroups := map[string]rules.PolicyGroup{}
	summaries := make([]OverlayPolicyGroupSummary, 0, len(groups))
	for _, group := range groups {
		id := strings.TrimSpace(group.ID)
		if id == "" {
			return nil, nil, fmt.Errorf("policy group id is required")
		}
		if _, exists := proxyGroups[id]; exists {
			return nil, nil, fmt.Errorf("policy group %q conflicts with a proxy group id", id)
		}
		if _, exists := policyGroups[id]; exists {
			return nil, nil, fmt.Errorf("policy group %q is defined more than once", id)
		}
		mode := strings.ToLower(strings.TrimSpace(group.Mode))
		if mode != "manual" && mode != "auto" && mode != "smart" {
			return nil, nil, fmt.Errorf("policy group %q mode must be manual, auto, or smart", id)
		}
		exits, err := validatePolicyGroupExits(id, group.Exits, proxyGroups)
		if err != nil {
			return nil, nil, err
		}
		pg := rules.PolicyGroup{Exits: exits}
		switch mode {
		case "manual":
			pg.Manual = true
		case "auto":
			pg.Auto = true
		case "smart":
			pg.Smart = true
		}
		policyGroups[id] = pg
		summaries = append(summaries, OverlayPolicyGroupSummary{
			ID:        id,
			Mode:      mode,
			Exits:     append([]string{}, exits...),
			ExitCount: len(exits),
			Reason:    group.Reason,
			Boundary:  group.Boundary,
		})
	}
	return policyGroups, summaries, nil
}

func validatePolicyGroupExits(groupID string, rawExits []string, proxyGroups map[string]rules.ProxyGroup) ([]string, error) {
	if len(rawExits) == 0 {
		return nil, fmt.Errorf("policy group %q exits is required", groupID)
	}
	exits := make([]string, 0, len(rawExits))
	seen := map[string]bool{}
	for _, rawExit := range rawExits {
		exit := strings.TrimSpace(rawExit)
		if exit == "" {
			return nil, fmt.Errorf("policy group %q has an empty exit", groupID)
		}
		if builtIn := canonicalBuiltInTarget(exit); builtIn != "" {
			exit = builtIn
		} else if _, ok := proxyGroups[exit]; !ok {
			return nil, fmt.Errorf("policy group %q exit %q requires a built-in target or matching proxy group", groupID, exit)
		}
		if seen[exit] {
			continue
		}
		seen[exit] = true
		exits = append(exits, exit)
	}
	return exits, nil
}

func assertOverlayPackType(id, declared, actual string) error {
	declared = strings.TrimSpace(declared)
	if declared == "" {
		return nil
	}
	if actual == "" {
		return fmt.Errorf("pack %q has no catalog type; remove type or refresh pack catalog", id)
	}
	if declared != actual {
		return fmt.Errorf("pack %q is type %q, but request declared %q", id, actual, declared)
	}
	return nil
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
	file, err := os.CreateTemp("", "localclash-plan-selection-*.gob")
	if err != nil {
		return "", nil, err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", nil, err
	}
	if err := rules.WriteSelection(path, selection); err != nil {
		os.Remove(path)
		return "", nil, err
	}
	return path, func() { _ = os.Remove(path) }, nil
}

func writeSelection(path string, selection rules.Selection) error {
	return rules.WriteSelection(path, selection)
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
		Packs:         make([]OverlayPackIntent, 0, len(summary.Packs)),
		CustomRules:   make([]OverlayCustomRuleIntent, 0, len(summary.CustomRules)),
		RuleProviders: make([]OverlayRuleProviderIntent, 0, len(summary.RuleProviders)),
		ProxyGroups:   make([]OverlayProxyGroupIntent, 0, len(summary.ProxyGroups)),
		PolicyGroups:  make([]OverlayPolicyGroupIntent, 0, len(summary.PolicyGroups)),
	}
	for _, pack := range summary.Packs {
		intent.Packs = append(intent.Packs, OverlayPackIntent{ID: pack.ID, Type: pack.Type, Target: pack.Target, Reason: pack.Reason})
	}
	for _, custom := range summary.CustomRules {
		intent.CustomRules = append(intent.CustomRules, localconfig.CustomRule{
			ID:     custom.ID,
			Target: custom.Target,
			Reason: custom.Reason,
			Rules:  append([]localconfig.CustomRuleLine{}, custom.Rules...),
		})
	}
	for _, provider := range summary.RuleProviders {
		intent.RuleProviders = append(intent.RuleProviders, localconfig.ExternalRuleProvider{
			ID:       provider.ID,
			Target:   provider.Target,
			Reason:   provider.Reason,
			Type:     provider.Type,
			Behavior: provider.Behavior,
			Format:   provider.Format,
			Path:     provider.Path,
			URL:      provider.URL,
			Interval: provider.Interval,
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
	for _, group := range summary.PolicyGroups {
		intent.PolicyGroups = append(intent.PolicyGroups, OverlayPolicyGroupIntent{
			ID:       group.ID,
			Mode:     group.Mode,
			Exits:    append([]string{}, group.Exits...),
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
		{source: opts.ConfigPath, name: "localclash.json"},
		{source: opts.SelectionPath, name: "localclash-packs.gob"},
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

func runMihomoTest(ctx context.Context, opts Options, configPath string) MihomoTestResult {
	start := time.Now()
	workDir, cleanup, err := mihomotest.SnapshotRuntimeDir(opts.WorkDir, "localclash-mihomo-test-*")
	if err != nil {
		return MihomoTestResult{
			Enabled:       true,
			Passed:        false,
			Error:         "cannot create isolated mihomo test runtime dir: " + err.Error(),
			Output:        "cannot create isolated mihomo test runtime dir: " + err.Error(),
			DurationMS:    time.Since(start).Milliseconds(),
			Isolated:      true,
			SourceWorkDir: opts.WorkDir,
		}
	}
	defer cleanup()
	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, opts.CorePath, "-d", workDir, "-f", configPath, "-t")
	output, err := cmd.CombinedOutput()
	result := MihomoTestResult{
		Enabled:       true,
		Passed:        err == nil,
		Output:        compactOutput(output, err),
		DurationMS:    time.Since(start).Milliseconds(),
		Isolated:      true,
		WorkDir:       workDir,
		SourceWorkDir: opts.WorkDir,
	}
	if err == nil {
		return result
	}
	result.Error = err.Error()
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		result.TimedOut = true
		result.Error = "mihomo config test timed out after 90s: " + err.Error()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
	}
	return result
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
	if err != nil {
		lines = append(lines, "error: "+err.Error())
	}
	return strings.Join(lines, "\n")
}

func mihomoTestFailureNextActions(test MihomoTestResult) []string {
	actions := []string{
		"Do not apply this patch until the Mihomo config test failure is understood.",
		"Inspect mihomo_test.error, mihomo_test.timed_out, mihomo_test.duration_ms, and mihomo_test.output.",
	}
	if test.TimedOut {
		actions = append(actions, "The test timed out; retry on the router, inspect CPU/disk pressure, or reduce GEOSITE/rule loading cost before bypassing validation.")
	}
	actions = append(actions,
		"Only recreate with test=false after the user explicitly accepts bypassing Mihomo validation.",
		"After a validated patch applies, call config_status to verify durable intent and generated overlay.",
	)
	return actions
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
	return canonicalBuiltInTarget(target) != ""
}

func canonicalBuiltInTarget(target string) string {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "direct", "reject", "proxy", "manual":
		return strings.ToUpper(strings.TrimSpace(target))
	default:
		return ""
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
