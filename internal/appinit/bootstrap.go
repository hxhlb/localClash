package appinit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"localclash/internal/configrender"
	"localclash/internal/rules"
	"localclash/internal/runtimeprofile"
	"localclash/internal/subscriptions"
)

type Options struct {
	RuntimeRoot         string
	RuleSourcesDir      string
	RulesCacheDir       string
	GeneratedConfig     string
	SubscriptionConfig  string
	SubscriptionPath    string
	SubscriptionRuntime string
	MihomoRuntimeDir    string
	CorePath            string
	PolicyPath          string
	PacksSelectionPath  string
	RuntimeProfilePath  string
}

type RuntimeState struct {
	Paths        RuntimePaths      `json:"paths"`
	Core         CoreState         `json:"core"`
	Subscription SubscriptionState `json:"subscription"`
	Rules        RulesState        `json:"rules"`
	Config       ConfigState       `json:"config"`
	Warnings     []string          `json:"warnings"`
	Diagnostics  []Diagnostic      `json:"diagnostics"`
}

type RuntimePaths struct {
	RuntimeRoot         string `json:"runtime_root"`
	RuleSourcesDir      string `json:"rule_sources_dir"`
	RulesCacheDir       string `json:"rules_cache_dir"`
	PacksDir            string `json:"packs_dir"`
	GeneratedConfig     string `json:"generated_config"`
	SubscriptionConfig  string `json:"subscription_config"`
	SubscriptionPath    string `json:"subscription_path"`
	SubscriptionRuntime string `json:"subscription_runtime"`
	MihomoRuntimeDir    string `json:"mihomo_runtime_dir"`
	CorePath            string `json:"core_path"`
	PolicyPath          string `json:"policy_path"`
	PacksSelectionPath  string `json:"packs_selection_path,omitempty"`
	RuntimeProfilePath  string `json:"runtime_profile_path"`
}

type CoreState struct {
	Path           string `json:"path"`
	Exists         bool   `json:"exists"`
	Missing        bool   `json:"missing"`
	Version        string `json:"version,omitempty"`
	SmartSupported bool   `json:"smart_supported"`
}

type SubscriptionState struct {
	Configured bool   `json:"configured"`
	Path       string `json:"path"`
	Available  bool   `json:"available"`
	Sources    int    `json:"sources"`
	Diagnostic string `json:"diagnostic,omitempty"`
}

type RulesState struct {
	SourcesDiscovered int                         `json:"sources_discovered"`
	CacheDir          string                      `json:"cache_dir"`
	CatalogAvailable  bool                        `json:"catalog_available"`
	PacksGenerated    bool                        `json:"packs_generated"`
	Packs             []rules.PackSummary         `json:"packs"`
	Details           map[string]rules.PackDetail `json:"-"`
	Diagnostic        string                      `json:"diagnostic,omitempty"`
}

type ConfigState struct {
	Path       string `json:"path"`
	Rendered   bool   `json:"rendered"`
	Available  bool   `json:"available"`
	Diagnostic string `json:"diagnostic,omitempty"`
}

type Diagnostic struct {
	Step    string `json:"step"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

func Bootstrap(ctx context.Context, opts Options) RuntimeState {
	opts = normalizeOptions(opts)
	state := RuntimeState{
		Paths: RuntimePaths{
			RuntimeRoot:         opts.RuntimeRoot,
			RuleSourcesDir:      opts.RuleSourcesDir,
			RulesCacheDir:       opts.RulesCacheDir,
			PacksDir:            opts.RulesCacheDir,
			GeneratedConfig:     opts.GeneratedConfig,
			SubscriptionConfig:  opts.SubscriptionConfig,
			SubscriptionPath:    opts.SubscriptionPath,
			SubscriptionRuntime: opts.SubscriptionRuntime,
			MihomoRuntimeDir:    opts.MihomoRuntimeDir,
			CorePath:            opts.CorePath,
			PolicyPath:          opts.PolicyPath,
			PacksSelectionPath:  opts.PacksSelectionPath,
			RuntimeProfilePath:  opts.RuntimeProfilePath,
		},
		Rules:  RulesState{CacheDir: opts.RulesCacheDir, Details: map[string]rules.PackDetail{}},
		Config: ConfigState{Path: opts.GeneratedConfig},
	}
	ensureDirs(&state, opts)
	inspectCore(ctx, &state, opts)
	inspectSubscription(&state, opts)
	ensureRulesCatalog(ctx, &state, opts)
	ensureGeneratedConfig(&state, opts)
	return state
}

func normalizeOptions(opts Options) Options {
	if strings.TrimSpace(opts.RuntimeRoot) == "" {
		opts.RuntimeRoot = ".runtime"
	}
	if strings.TrimSpace(opts.RuleSourcesDir) == "" {
		opts.RuleSourcesDir = "rule-sources"
	}
	if strings.TrimSpace(opts.RulesCacheDir) == "" {
		opts.RulesCacheDir = filepath.Join(opts.RuntimeRoot, "rules", "packs")
	}
	if strings.TrimSpace(opts.GeneratedConfig) == "" {
		opts.GeneratedConfig = "generated/mihomo.yaml"
	}
	if strings.TrimSpace(opts.SubscriptionConfig) == "" {
		opts.SubscriptionConfig = "localclash-subscriptions.yaml"
	}
	if strings.TrimSpace(opts.SubscriptionPath) == "" {
		opts.SubscriptionPath = "subscription.yaml"
	}
	if strings.TrimSpace(opts.SubscriptionRuntime) == "" {
		opts.SubscriptionRuntime = filepath.Join(opts.RuntimeRoot, "subscriptions")
	}
	if strings.TrimSpace(opts.MihomoRuntimeDir) == "" {
		opts.MihomoRuntimeDir = filepath.Join(opts.RuntimeRoot, "mihomo")
	}
	if strings.TrimSpace(opts.PolicyPath) == "" {
		opts.PolicyPath = "policies/loyalsoldier.yaml"
	}
	if strings.TrimSpace(opts.PacksSelectionPath) == "" && fileExists("localclash-packs.yaml") {
		opts.PacksSelectionPath = "localclash-packs.yaml"
	}
	if strings.TrimSpace(opts.RuntimeProfilePath) == "" {
		opts.RuntimeProfilePath = runtimeprofile.DefaultPath
	}
	if strings.TrimSpace(opts.CorePath) == "" {
		if corePath, err := runtimeprofile.ActiveCorePath(opts.RuntimeProfilePath); err == nil && strings.TrimSpace(corePath) != "" {
			opts.CorePath = corePath
		} else {
			opts.CorePath = runtimeprofile.MetaCorePath
		}
	}
	return opts
}

func ensureDirs(state *RuntimeState, opts Options) {
	for _, dir := range []string{opts.RuntimeRoot, opts.RulesCacheDir, opts.SubscriptionRuntime, opts.MihomoRuntimeDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			state.addDiagnostic("runtime_dirs", "warning", err.Error())
		}
	}
}

func inspectCore(ctx context.Context, state *RuntimeState, opts Options) {
	state.Core.Path = opts.CorePath
	info, err := os.Stat(opts.CorePath)
	if err != nil || info.IsDir() {
		state.Core.Missing = true
		state.addDiagnostic("core", "warning", "mihomo core is missing; run core download before starting runtime")
		return
	}
	state.Core.Exists = true
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(runCtx, opts.CorePath, "-v").CombinedOutput()
	if err == nil {
		state.Core.Version = strings.TrimSpace(string(output))
		state.Core.SmartSupported = strings.Contains(strings.ToLower(state.Core.Version), "smart")
	}
}

func inspectSubscription(state *RuntimeState, opts Options) {
	status, err := subscriptions.Status(subscriptions.StatusOptions{
		ConfigPath: opts.SubscriptionConfig,
		MergedPath: opts.SubscriptionPath,
		RuntimeDir: opts.SubscriptionRuntime,
	})
	if err != nil {
		state.Subscription.Diagnostic = err.Error()
		state.addDiagnostic("subscription", "warning", err.Error())
		return
	}
	state.Subscription.Configured = status.Configured
	state.Subscription.Path = opts.SubscriptionPath
	state.Subscription.Available = status.Merged.Exists && status.Merged.ProxiesCount > 0
	state.Subscription.Sources = len(status.Sources)
	if !state.Subscription.Available {
		if status.Message != "" {
			state.Subscription.Diagnostic = status.Message
		} else {
			state.Subscription.Diagnostic = "effective subscription.yaml is unavailable; run subscriptions_refresh"
		}
		state.addDiagnostic("subscription", "warning", state.Subscription.Diagnostic)
	}
}

func ensureRulesCatalog(ctx context.Context, state *RuntimeState, opts Options) {
	sources, err := rules.LoadSources(opts.RuleSourcesDir)
	if err != nil {
		state.Rules.Diagnostic = err.Error()
		state.addDiagnostic("rules_sources", "warning", err.Error())
	} else {
		state.Rules.SourcesDiscovered = len(sources)
	}
	catalog, err := rules.LoadPackCatalog(opts.RulesCacheDir)
	if err != nil {
		if _, adaptErr := rules.Adapt(ctx, rules.Options{SourcesDir: opts.RuleSourcesDir, CacheDir: opts.RulesCacheDir}); adaptErr != nil {
			state.Rules.Diagnostic = adaptErr.Error()
			state.addDiagnostic("packs_catalog", "warning", adaptErr.Error())
			return
		}
		state.Rules.PacksGenerated = true
		catalog, err = rules.LoadPackCatalog(opts.RulesCacheDir)
	}
	if err != nil {
		state.Rules.Diagnostic = err.Error()
		state.addDiagnostic("packs_catalog", "warning", err.Error())
		return
	}
	state.Rules.CatalogAvailable = true
	state.Rules.Packs = catalog.Packs
	state.Rules.Details = catalog.Details
}

func ensureGeneratedConfig(state *RuntimeState, opts Options) {
	if !state.Subscription.Available {
		state.Config.Available = fileExists(opts.GeneratedConfig)
		state.Config.Diagnostic = "config render skipped because effective subscription is unavailable"
		state.addDiagnostic("config_render", "warning", state.Config.Diagnostic)
		return
	}
	renderOpts := configrender.Options{
		SourcePath:         opts.SubscriptionPath,
		PolicyPath:         opts.PolicyPath,
		OutputPath:         opts.GeneratedConfig,
		RulesCacheDir:      opts.RulesCacheDir,
		RuntimeProfilePath: opts.RuntimeProfilePath,
		Force:              true,
	}
	if opts.PacksSelectionPath != "" && fileExists(opts.PacksSelectionPath) {
		renderOpts.PacksSelectionPath = opts.PacksSelectionPath
	}
	if _, err := configrender.Render(renderOpts); err != nil {
		state.Config.Available = fileExists(opts.GeneratedConfig)
		state.Config.Diagnostic = err.Error()
		state.addDiagnostic("config_render", "warning", err.Error())
		return
	}
	state.Config.Rendered = true
	state.Config.Available = true
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (state *RuntimeState) addDiagnostic(step, level, message string) {
	state.Diagnostics = append(state.Diagnostics, Diagnostic{Step: step, Level: level, Message: message})
	if level == "warning" {
		state.Warnings = append(state.Warnings, message)
	}
}
