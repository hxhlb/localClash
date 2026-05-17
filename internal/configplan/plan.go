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
	"localclash/internal/rules"

	"gopkg.in/yaml.v3"
)

type Options struct {
	PlanName     string
	Subscription string
	Policy       string
	Mode         string
	RulesCache   string
	OutputDir    string
	Test         bool
	Overlay      OverlayIntent
	CorePath     string
	WorkDir      string
	Now          time.Time
}

type OverlayIntent struct {
	Packs       []OverlayPackIntent       `json:"packs" yaml:"packs"`
	ProxyGroups []OverlayProxyGroupIntent `json:"proxy_groups" yaml:"proxy_groups"`
}

type OverlayPackIntent struct {
	ID     string `json:"id" yaml:"id"`
	Target string `json:"target" yaml:"target"`
}

type OverlayProxyGroupIntent struct {
	ID    string   `json:"id" yaml:"id"`
	Nodes []string `json:"nodes" yaml:"nodes"`
	Mode  string   `json:"mode" yaml:"mode"`
}

type Result struct {
	PlanID      string           `json:"plan_id"`
	Output      string           `json:"output"`
	SummaryPath string           `json:"summary_path"`
	Valid       bool             `json:"valid"`
	MihomoTest  MihomoTestResult `json:"mihomo_test"`
	Overlay     OverlaySummary   `json:"overlay"`
	Changes     ChangesSummary   `json:"changes"`
	Warnings    []string         `json:"warnings"`
}

type MihomoTestResult struct {
	Enabled bool   `json:"enabled"`
	Passed  bool   `json:"passed"`
	Output  string `json:"output"`
}

type OverlaySummary struct {
	Packs       []OverlayPackSummary       `json:"packs"`
	ProxyGroups []OverlayProxyGroupSummary `json:"proxy_groups"`
}

type OverlayPackSummary struct {
	ID     string `json:"id"`
	Target string `json:"target"`
}

type OverlayProxyGroupSummary struct {
	ID        string   `json:"id"`
	Nodes     []string `json:"nodes"`
	Mode      string   `json:"mode"`
	NodeCount int      `json:"node_count"`
}

type ChangesSummary struct {
	RuleProvidersAdded int `json:"rule_providers_added"`
	ProxyGroupsAdded   int `json:"proxy_groups_added"`
	RulesAdded         int `json:"rules_added"`
}

func Render(ctx context.Context, opts Options) (Result, error) {
	opts = normalizeOptions(opts)
	if len(opts.Overlay.Packs) == 0 {
		return Result{}, fmt.Errorf("overlay.packs is required")
	}

	proxyNames, err := rules.LoadSubscriptionProxyNames(opts.Subscription)
	if err != nil {
		return Result{}, err
	}

	selection, overlaySummary, warnings, err := buildSelection(opts, proxyNames)
	if err != nil {
		return Result{}, err
	}

	planID, err := allocatePlanID(opts.OutputDir, opts.PlanName, opts.Now)
	if err != nil {
		return Result{}, err
	}
	planDir := filepath.Join(opts.OutputDir, planID)
	outputPath := filepath.Join(planDir, "mihomo.yaml")
	summaryPath := filepath.Join(planDir, "summary.json")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		return Result{}, err
	}

	selectionPath, cleanup, err := writeTempSelection(selection)
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
		Valid:       true,
		MihomoTest:  MihomoTestResult{Enabled: opts.Test},
		Overlay:     overlaySummary,
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
	if opts.OutputDir == "" {
		opts.OutputDir = ".runtime/plans"
	}
	if opts.CorePath == "" {
		opts.CorePath = "bin/mihomo"
	}
	if opts.WorkDir == "" {
		opts.WorkDir = ".runtime/mihomo"
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	return opts
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
		if mode != "manual" && mode != "auto" {
			return rules.Selection{}, OverlaySummary{}, nil, fmt.Errorf("proxy group %q mode must be manual or auto", id)
		}
		if len(group.Nodes) == 0 {
			return rules.Selection{}, OverlaySummary{}, nil, fmt.Errorf("proxy group %q nodes is required", id)
		}
		nodes, err := validateProxyGroupNodes(id, group.Nodes, proxyNames)
		if err != nil {
			return rules.Selection{}, OverlaySummary{}, nil, err
		}
		pg := rules.ProxyGroup{Nodes: nodes}
		if mode == "manual" {
			pg.Manual = true
		} else {
			pg.Auto = true
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

func writeSummary(path string, result Result) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
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
