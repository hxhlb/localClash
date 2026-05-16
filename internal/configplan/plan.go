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
	Packs          []OverlayPackIntent          `json:"packs" yaml:"packs"`
	VirtualTargets []OverlayVirtualTargetIntent `json:"virtual_targets" yaml:"virtual_targets"`
}

type OverlayPackIntent struct {
	ID     string `json:"id" yaml:"id"`
	Target string `json:"target" yaml:"target"`
}

type OverlayVirtualTargetIntent struct {
	ID         string   `json:"id" yaml:"id"`
	NodeLabels []string `json:"node_labels" yaml:"node_labels"`
	Mode       string   `json:"mode" yaml:"mode"`
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
	Packs          []OverlayPackSummary          `json:"packs"`
	VirtualTargets []OverlayVirtualTargetSummary `json:"virtual_targets"`
}

type OverlayPackSummary struct {
	ID     string `json:"id"`
	Target string `json:"target"`
}

type OverlayVirtualTargetSummary struct {
	ID         string   `json:"id"`
	NodeLabels []string `json:"node_labels"`
	Mode       string   `json:"mode"`
	NodeCount  int      `json:"node_count"`
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

	baseSelection, err := loadNodeLabelSelection()
	if err != nil {
		return Result{}, err
	}
	proxyNames, err := rules.LoadSubscriptionProxyNames(opts.Subscription)
	if err != nil {
		return Result{}, err
	}

	selection, overlaySummary, warnings, err := buildSelection(opts, baseSelection, proxyNames)
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
			ProxyGroupsAdded:   len(overlayInspection.VirtualTargets),
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

func buildSelection(opts Options, base rules.Selection, proxyNames []string) (rules.Selection, OverlaySummary, []string, error) {
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

	virtuals := map[string]rules.VirtualTarget{}
	virtualSummaries := make([]OverlayVirtualTargetSummary, 0, len(opts.Overlay.VirtualTargets))
	for _, target := range opts.Overlay.VirtualTargets {
		id := strings.TrimSpace(target.ID)
		if id == "" {
			return rules.Selection{}, OverlaySummary{}, nil, fmt.Errorf("virtual target id is required")
		}
		if _, exists := virtuals[id]; exists {
			return rules.Selection{}, OverlaySummary{}, nil, fmt.Errorf("virtual target %q is defined more than once", id)
		}
		mode := strings.ToLower(strings.TrimSpace(target.Mode))
		if mode != "manual" && mode != "auto" {
			return rules.Selection{}, OverlaySummary{}, nil, fmt.Errorf("virtual target %q mode must be manual or auto", id)
		}
		if len(target.NodeLabels) == 0 {
			return rules.Selection{}, OverlaySummary{}, nil, fmt.Errorf("virtual target %q node_labels is required", id)
		}
		labels := make([]string, 0, len(target.NodeLabels))
		for _, rawLabel := range target.NodeLabels {
			label := strings.TrimSpace(rawLabel)
			if label == "" {
				return rules.Selection{}, OverlaySummary{}, nil, fmt.Errorf("virtual target %q has an empty node label", id)
			}
			if _, ok := base.NodeLabels[label]; !ok {
				return rules.Selection{}, OverlaySummary{}, nil, fmt.Errorf("virtual target %q references unknown node label %q", id, label)
			}
			labels = append(labels, label)
		}
		vt := rules.VirtualTarget{Candidates: rules.VirtualTargetCandidates{Labels: labels}}
		if mode == "manual" {
			vt.Manual = true
		} else {
			vt.Auto = true
		}
		virtuals[id] = vt
		virtualSummaries = append(virtualSummaries, OverlayVirtualTargetSummary{ID: id, NodeLabels: append([]string(nil), labels...), Mode: mode})
	}

	for _, pack := range selected {
		if isBuiltInTarget(pack.Target) {
			continue
		}
		if _, ok := virtuals[pack.Target]; !ok {
			return rules.Selection{}, OverlaySummary{}, nil, fmt.Errorf("pack target %q requires a matching virtual target", pack.Target)
		}
	}

	warnings, counts, err := classifyVirtualTargetCandidates(base.NodeLabels, virtuals, proxyNames)
	if err != nil {
		return rules.Selection{}, OverlaySummary{}, nil, err
	}
	for i := range virtualSummaries {
		virtualSummaries[i].NodeCount = counts[virtualSummaries[i].ID]
	}
	sort.Slice(virtualSummaries, func(i, j int) bool { return virtualSummaries[i].ID < virtualSummaries[j].ID })

	return rules.Selection{
		Version:        1,
		NodeLabels:     base.NodeLabels,
		VirtualTargets: virtuals,
		EnabledPack:    selected,
	}, OverlaySummary{Packs: overlayPacks, VirtualTargets: virtualSummaries}, warnings, nil
}

func classifyVirtualTargetCandidates(labels map[string]rules.NodeLabel, virtuals map[string]rules.VirtualTarget, proxyNames []string) ([]string, map[string]int, error) {
	classified, err := rules.ClassifyProxyNames(proxyNames, labels)
	if err != nil {
		return nil, nil, err
	}
	var warnings []string
	counts := map[string]int{}
	for targetID, target := range virtuals {
		seen := map[string]bool{}
		for _, label := range target.Candidates.Labels {
			candidates := classified[label]
			if len(candidates) == 0 {
				warnings = append(warnings, fmt.Sprintf("node label %q has no candidate proxies", label))
			}
			for _, candidate := range candidates {
				seen[candidate] = true
			}
		}
		counts[targetID] = len(seen)
	}
	sort.Strings(warnings)
	return warnings, counts, nil
}

func loadNodeLabelSelection() (rules.Selection, error) {
	for _, path := range []string{"localclash-packs.yaml", "localclash-packs.yaml.example"} {
		selection, err := rules.LoadSelection(path)
		if err == nil {
			if selection.NodeLabels == nil {
				selection.NodeLabels = map[string]rules.NodeLabel{}
			}
			return selection, nil
		}
		if !os.IsNotExist(err) {
			return rules.Selection{}, err
		}
	}
	return rules.Selection{NodeLabels: map[string]rules.NodeLabel{}}, nil
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
