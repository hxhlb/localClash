package appinit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"localclash/internal/configrender"
	"localclash/internal/localconfig"
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

var defaultWorkDirCandidates = []string{"/root/localclash"}

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
	baseDir := defaultBaseDir(opts)
	if strings.TrimSpace(opts.RuntimeRoot) == "" {
		opts.RuntimeRoot = defaultPath(baseDir, ".runtime")
	}
	if strings.TrimSpace(opts.RuleSourcesDir) == "" {
		opts.RuleSourcesDir = defaultPath(baseDir, "rule-sources")
	}
	if strings.TrimSpace(opts.RulesCacheDir) == "" {
		opts.RulesCacheDir = filepath.Join(opts.RuntimeRoot, "rules", "packs")
	}
	if strings.TrimSpace(opts.GeneratedConfig) == "" {
		opts.GeneratedConfig = defaultPath(baseDir, "generated/mihomo.yaml")
	}
	if strings.TrimSpace(opts.SubscriptionConfig) == "" {
		opts.SubscriptionConfig = defaultPath(baseDir, "localclash-subscriptions.yaml")
	}
	if strings.TrimSpace(opts.SubscriptionPath) == "" {
		opts.SubscriptionPath = defaultPath(baseDir, "subscription.yaml")
	}
	if strings.TrimSpace(opts.SubscriptionRuntime) == "" {
		opts.SubscriptionRuntime = filepath.Join(opts.RuntimeRoot, "subscriptions")
	}
	if strings.TrimSpace(opts.MihomoRuntimeDir) == "" {
		opts.MihomoRuntimeDir = filepath.Join(opts.RuntimeRoot, "mihomo")
	}
	if strings.TrimSpace(opts.PolicyPath) == "" {
		opts.PolicyPath = defaultPath(baseDir, "policies/loyalsoldier.yaml")
	}
	if strings.TrimSpace(opts.PacksSelectionPath) == "" && fileExists(defaultPath(baseDir, "localclash-packs.yaml")) {
		opts.PacksSelectionPath = defaultPath(baseDir, "localclash-packs.yaml")
	}
	if strings.TrimSpace(opts.RuntimeProfilePath) == "" {
		opts.RuntimeProfilePath = defaultPath(baseDir, runtimeprofile.DefaultPath)
	}
	if strings.TrimSpace(opts.CorePath) == "" {
		if corePath, err := runtimeprofile.ActiveCorePath(opts.RuntimeProfilePath); err == nil && strings.TrimSpace(corePath) != "" {
			opts.CorePath = defaultPath(baseDir, corePath)
		} else {
			opts.CorePath = defaultPath(baseDir, runtimeprofile.MetaCorePath)
		}
	}
	return opts
}

func defaultBaseDir(opts Options) string {
	if hasExplicitPath(opts) {
		return ""
	}
	if dir := strings.TrimSpace(os.Getenv("LOCALCLASH_WORKDIR")); dir != "" {
		return dir
	}
	if looksLikeWorkDir(".") {
		return ""
	}
	for _, candidate := range defaultWorkDirCandidates {
		if looksLikeWorkDir(candidate) {
			return candidate
		}
	}
	return ""
}

func hasExplicitPath(opts Options) bool {
	return strings.TrimSpace(opts.RuntimeRoot) != "" ||
		strings.TrimSpace(opts.RuleSourcesDir) != "" ||
		strings.TrimSpace(opts.RulesCacheDir) != "" ||
		strings.TrimSpace(opts.GeneratedConfig) != "" ||
		strings.TrimSpace(opts.SubscriptionConfig) != "" ||
		strings.TrimSpace(opts.SubscriptionPath) != "" ||
		strings.TrimSpace(opts.SubscriptionRuntime) != "" ||
		strings.TrimSpace(opts.MihomoRuntimeDir) != "" ||
		strings.TrimSpace(opts.CorePath) != "" ||
		strings.TrimSpace(opts.PolicyPath) != "" ||
		strings.TrimSpace(opts.PacksSelectionPath) != "" ||
		strings.TrimSpace(opts.RuntimeProfilePath) != ""
}

func looksLikeWorkDir(dir string) bool {
	for _, marker := range []string{
		"localclash-runtime.yaml",
		"localclash.yaml",
		"subscription.yaml",
		filepath.Join("generated", "mihomo.yaml"),
		filepath.Join(".runtime", "mihomo", "mihomo.pid"),
		"policy-templates",
		"rule-sources",
	} {
		if fileExists(filepath.Join(dir, marker)) {
			return true
		}
	}
	return false
}

func defaultPath(baseDir, path string) string {
	if baseDir == "" || path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(baseDir, path)
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
	configPath := defaultWorkDirPath(opts.RuntimeRoot, "localclash.yaml")
	if fileExists(configPath) {
		config, err := localconfig.Load(configPath)
		if err != nil {
			state.Config.Available = fileExists(opts.GeneratedConfig)
			state.Config.Diagnostic = err.Error()
			state.addDiagnostic("config_render", "warning", err.Error())
			return
		}
		resolved, err := localconfig.Resolve(localconfig.ResolveOptions{
			Config:              config,
			SubscriptionPath:    opts.SubscriptionPath,
			SubscriptionConfig:  opts.SubscriptionConfig,
			SubscriptionRuntime: opts.SubscriptionRuntime,
			RulesCache:          opts.RulesCacheDir,
		})
		if err != nil {
			state.Config.Available = fileExists(opts.GeneratedConfig)
			state.Config.Diagnostic = err.Error()
			state.addDiagnostic("config_render", "warning", err.Error())
			return
		}
		selectionPath := opts.PacksSelectionPath
		if strings.TrimSpace(selectionPath) == "" {
			selectionPath = defaultWorkDirPath(opts.RuntimeRoot, "localclash-packs.yaml")
		}
		if err := localconfig.WriteSelection(selectionPath, resolved.Selection); err != nil {
			state.Config.Available = fileExists(opts.GeneratedConfig)
			state.Config.Diagnostic = err.Error()
			state.addDiagnostic("config_render", "warning", err.Error())
			return
		}
		renderOpts.PacksSelectionPath = selectionPath
	} else if opts.PacksSelectionPath != "" && fileExists(opts.PacksSelectionPath) {
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

func defaultWorkDirPath(runtimeRoot, name string) string {
	if filepath.Base(runtimeRoot) == ".runtime" {
		return filepath.Join(filepath.Dir(runtimeRoot), name)
	}
	return name
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
