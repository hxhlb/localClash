package localconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"localclash/internal/rules"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Version     int                   `json:"version" yaml:"version"`
	ProxyGroups map[string]ProxyGroup `json:"proxy_groups" yaml:"proxy_groups,omitempty"`
	CustomRules []CustomRule          `json:"custom_rules,omitempty" yaml:"custom_rules,omitempty"`
	Packs       []Pack                `json:"packs" yaml:"packs,omitempty"`
}

type ProxyGroup struct {
	Mode          string   `json:"mode" yaml:"mode"`
	Match         *Match   `json:"match,omitempty" yaml:"match,omitempty"`
	Nodes         []string `json:"nodes,omitempty" yaml:"nodes,omitempty"`
	SelectedNodes []string `json:"selected_nodes,omitempty" yaml:"selected_nodes,omitempty"`
	Reason        string   `json:"reason,omitempty" yaml:"reason,omitempty"`
	Boundary      string   `json:"boundary,omitempty" yaml:"boundary,omitempty"`
}

type Match struct {
	Type          string   `json:"type" yaml:"type"`
	Pattern       string   `json:"pattern" yaml:"pattern"`
	SourceIDs     []string `json:"source_ids,omitempty" yaml:"source_ids,omitempty"`
	Min           int      `json:"min,omitempty" yaml:"min,omitempty"`
	Max           int      `json:"max,omitempty" yaml:"max,omitempty"`
	CaseSensitive bool     `json:"case_sensitive,omitempty" yaml:"case_sensitive,omitempty"`
}

type Pack struct {
	ID     string `json:"id" yaml:"id"`
	Target string `json:"target" yaml:"target"`
	Reason string `json:"reason,omitempty" yaml:"reason,omitempty"`
}

type CustomRule struct {
	ID     string           `json:"id" yaml:"id"`
	Target string           `json:"target" yaml:"target"`
	Reason string           `json:"reason,omitempty" yaml:"reason,omitempty"`
	Rules  []CustomRuleLine `json:"rules" yaml:"rules"`
}

type CustomRuleLine struct {
	Type      string `json:"type" yaml:"type"`
	Value     string `json:"value" yaml:"value"`
	NoResolve bool   `json:"no_resolve,omitempty" yaml:"no_resolve,omitempty"`
}

type ResolveOptions struct {
	Config              Config
	SubscriptionPath    string
	SubscriptionConfig  string
	SubscriptionRuntime string
	RulesCache          string
}

type Resolved struct {
	Config      Config             `json:"config"`
	Selection   rules.Selection    `json:"selection"`
	ProxyGroups []ProxyGroupResult `json:"proxy_groups"`
	CustomRules []CustomRuleResult `json:"custom_rules"`
	Packs       []PackResult       `json:"packs"`
	Warnings    []string           `json:"warnings"`
}

type ProxyGroupResult struct {
	ID            string   `json:"id"`
	Mode          string   `json:"mode"`
	Match         *Match   `json:"match,omitempty"`
	SelectedNodes []string `json:"selected_nodes"`
	NodeCount     int      `json:"node_count"`
	Reason        string   `json:"reason,omitempty"`
	Boundary      string   `json:"boundary,omitempty"`
}

type PackResult struct {
	ID     string `json:"id"`
	Target string `json:"target"`
	Reason string `json:"reason,omitempty"`
}

type CustomRuleResult struct {
	ID        string           `json:"id"`
	Target    string           `json:"target"`
	RuleCount int              `json:"rule_count"`
	Reason    string           `json:"reason,omitempty"`
	Rules     []CustomRuleLine `json:"rules,omitempty"`
}

type SubscriptionNode struct {
	Name     string `json:"name"`
	Type     string `json:"type,omitempty"`
	SourceID string `json:"source_id,omitempty"`
}

type MissingNodesError struct {
	GroupID string
	Nodes   []string
}

func (err *MissingNodesError) Error() string {
	if len(err.Nodes) == 1 {
		return fmt.Sprintf("proxy group %q references unknown subscription node %q", err.GroupID, err.Nodes[0])
	}
	return fmt.Sprintf("proxy group %q references %d unknown subscription nodes", err.GroupID, len(err.Nodes))
}

type subscriptionSources struct {
	Sources []struct {
		ID string `yaml:"id"`
	} `yaml:"sources"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func Write(path string, config Config) error {
	if config.Version == 0 {
		config.Version = 1
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func WriteSelection(path string, selection rules.Selection) error {
	data, err := yaml.Marshal(selection)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func Resolve(opts ResolveOptions) (Resolved, error) {
	opts = normalizeResolveOptions(opts)
	if opts.Config.Version == 0 {
		opts.Config.Version = 1
	}
	nodes, err := LoadSubscriptionNodes(SubscriptionNodeOptions{
		SubscriptionPath:    opts.SubscriptionPath,
		SubscriptionConfig:  opts.SubscriptionConfig,
		SubscriptionRuntime: opts.SubscriptionRuntime,
	})
	if err != nil {
		return Resolved{}, err
	}
	selection := rules.Selection{
		Version:     1,
		ProxyGroups: map[string]rules.ProxyGroup{},
	}
	resolvedConfig := opts.Config
	if resolvedConfig.ProxyGroups == nil {
		resolvedConfig.ProxyGroups = map[string]ProxyGroup{}
	}
	groupIDs := make([]string, 0, len(resolvedConfig.ProxyGroups))
	for id := range resolvedConfig.ProxyGroups {
		groupIDs = append(groupIDs, id)
	}
	sort.Strings(groupIDs)
	var groupResults []ProxyGroupResult
	for _, id := range groupIDs {
		group := resolvedConfig.ProxyGroups[id]
		selected, err := resolveProxyGroup(id, group, nodes)
		if err != nil {
			return Resolved{}, err
		}
		group.SelectedNodes = selected
		resolvedConfig.ProxyGroups[id] = group
		ruleGroup := rules.ProxyGroup{Nodes: selected}
		switch strings.ToLower(strings.TrimSpace(group.Mode)) {
		case "manual":
			ruleGroup.Manual = true
		case "auto":
			ruleGroup.Auto = true
		default:
			return Resolved{}, fmt.Errorf("proxy group %q mode must be manual or auto", id)
		}
		selection.ProxyGroups[id] = ruleGroup
		groupResults = append(groupResults, ProxyGroupResult{
			ID:            id,
			Mode:          strings.ToLower(strings.TrimSpace(group.Mode)),
			Match:         group.Match,
			SelectedNodes: append([]string{}, selected...),
			NodeCount:     len(selected),
			Reason:        group.Reason,
			Boundary:      group.Boundary,
		})
	}
	resolvedPacks := make([]Pack, 0, len(resolvedConfig.Packs))
	packResults := make([]PackResult, 0, len(resolvedConfig.Packs))
	for _, pack := range resolvedConfig.Packs {
		ref, err := rules.ResolvePackRef(opts.RulesCache, pack.ID)
		if err != nil {
			return Resolved{}, err
		}
		target := strings.TrimSpace(pack.Target)
		if target == "" {
			return Resolved{}, fmt.Errorf("pack %q target is required", pack.ID)
		}
		if !isBuiltInTarget(target) {
			if _, ok := selection.ProxyGroups[target]; !ok {
				return Resolved{}, fmt.Errorf("pack target %q requires a matching proxy group", target)
			}
		}
		selection.EnabledPack = append(selection.EnabledPack, rules.SelectedPack{Source: ref.Source, Pack: ref.Pack, Target: target})
		resolvedPacks = append(resolvedPacks, Pack{ID: ref.ID, Target: target, Reason: pack.Reason})
		packResults = append(packResults, PackResult{ID: ref.ID, Target: target, Reason: pack.Reason})
	}
	resolvedConfig.Packs = resolvedPacks
	resolvedCustomRules, customRuleResults, err := resolveCustomRules(resolvedConfig.CustomRules, selection.ProxyGroups)
	if err != nil {
		return Resolved{}, err
	}
	resolvedConfig.CustomRules = resolvedCustomRules
	selection.CustomRules = customRulesForSelection(resolvedCustomRules)
	return Resolved{Config: resolvedConfig, Selection: selection, ProxyGroups: groupResults, CustomRules: customRuleResults, Packs: packResults}, nil
}

type SubscriptionNodeOptions struct {
	SubscriptionPath    string
	SubscriptionConfig  string
	SubscriptionRuntime string
}

func LoadSubscriptionNodes(opts SubscriptionNodeOptions) ([]SubscriptionNode, error) {
	opts = normalizeSubscriptionNodeOptions(opts)
	sourceNodes, err := loadSourceSubscriptionNodes(opts)
	if err == nil && len(sourceNodes) > 0 {
		return sourceNodes, nil
	}
	return loadMergedSubscriptionNodes(opts.SubscriptionPath)
}

func loadMergedSubscriptionNodes(path string) ([]SubscriptionNode, error) {
	doc, err := readYAMLMap(path)
	if err != nil {
		return nil, err
	}
	var nodes []SubscriptionNode
	for _, proxy := range anyMapSlice(doc["proxies"]) {
		name := stringValue(proxy["name"])
		if name == "" {
			return nil, fmt.Errorf("subscription %q contains a proxy without name", path)
		}
		nodes = append(nodes, SubscriptionNode{Name: name, Type: stringValue(proxy["type"])})
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("subscription %q has no proxies", path)
	}
	return nodes, nil
}

func loadSourceSubscriptionNodes(opts SubscriptionNodeOptions) ([]SubscriptionNode, error) {
	data, err := os.ReadFile(opts.SubscriptionConfig)
	if err != nil {
		return nil, err
	}
	var sources subscriptionSources
	if err := yaml.Unmarshal(data, &sources); err != nil {
		return nil, err
	}
	nameCounts := map[string]int{}
	sourceDocs := map[string][]map[string]any{}
	for _, source := range sources.Sources {
		if source.ID == "" {
			continue
		}
		doc, err := readYAMLMap(filepath.Join(opts.SubscriptionRuntime, source.ID+".yaml"))
		if err != nil {
			return nil, err
		}
		proxies := anyMapSlice(doc["proxies"])
		sourceDocs[source.ID] = proxies
		for _, proxy := range proxies {
			nameCounts[stringValue(proxy["name"])]++
		}
	}
	usedNames := map[string]bool{}
	var nodes []SubscriptionNode
	for _, source := range sources.Sources {
		for _, proxy := range sourceDocs[source.ID] {
			name := stringValue(proxy["name"])
			if nameCounts[name] > 1 {
				name = "[" + source.ID + "] " + name
			}
			name = uniqueName(name, usedNames)
			usedNames[name] = true
			nodes = append(nodes, SubscriptionNode{Name: name, Type: stringValue(proxy["type"]), SourceID: source.ID})
		}
	}
	return nodes, nil
}

func resolveProxyGroup(id string, group ProxyGroup, nodes []SubscriptionNode) ([]string, error) {
	if group.Match != nil {
		if len(group.Nodes) > 0 {
			return nil, fmt.Errorf("proxy group %q must use either match or nodes, not both", id)
		}
		return resolveMatch(id, *group.Match, nodes)
	}
	return resolveExactNodes(id, group.Nodes, nodes)
}

func resolveMatch(id string, match Match, nodes []SubscriptionNode) ([]string, error) {
	if strings.TrimSpace(match.Type) == "" {
		match.Type = "name_regex"
	}
	if match.Type != "name_regex" {
		return nil, fmt.Errorf("proxy group %q match type %q is unsupported", id, match.Type)
	}
	pattern := strings.TrimSpace(match.Pattern)
	if pattern == "" {
		return nil, fmt.Errorf("proxy group %q match.pattern is required", id)
	}
	if !match.CaseSensitive {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("proxy group %q match.pattern is invalid: %w", id, err)
	}
	sourceIDs := map[string]bool{}
	for _, sourceID := range match.SourceIDs {
		sourceID = strings.TrimSpace(sourceID)
		if sourceID != "" {
			sourceIDs[sourceID] = true
		}
	}
	var selected []string
	seen := map[string]bool{}
	for _, node := range nodes {
		if len(sourceIDs) > 0 && !sourceIDs[node.SourceID] {
			continue
		}
		if !re.MatchString(node.Name) || seen[node.Name] {
			continue
		}
		seen[node.Name] = true
		selected = append(selected, node.Name)
		if match.Max > 0 && len(selected) >= match.Max {
			break
		}
	}
	min := match.Min
	if min <= 0 {
		min = 1
	}
	if len(selected) < min {
		return nil, fmt.Errorf("proxy group %q match selected %d nodes, below min %d", id, len(selected), min)
	}
	return selected, nil
}

func resolveExactNodes(id string, rawNodes []string, nodes []SubscriptionNode) ([]string, error) {
	available := map[string]bool{}
	for _, node := range nodes {
		available[node.Name] = true
	}
	seen := map[string]bool{}
	var selected []string
	var missing []string
	for _, raw := range rawNodes {
		node := strings.TrimSpace(raw)
		if node == "" {
			return nil, fmt.Errorf("proxy group %q has an empty node name", id)
		}
		if !available[node] {
			missing = append(missing, node)
			continue
		}
		if seen[node] {
			continue
		}
		seen[node] = true
		selected = append(selected, node)
	}
	if len(missing) > 0 {
		return nil, &MissingNodesError{GroupID: id, Nodes: missing}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("proxy group %q has no nodes or match selector", id)
	}
	return selected, nil
}

func resolveCustomRules(customRules []CustomRule, proxyGroups map[string]rules.ProxyGroup) ([]CustomRule, []CustomRuleResult, error) {
	resolved := make([]CustomRule, 0, len(customRules))
	results := make([]CustomRuleResult, 0, len(customRules))
	ids := map[string]bool{}
	for _, custom := range customRules {
		id := strings.TrimSpace(custom.ID)
		if id == "" {
			return nil, nil, fmt.Errorf("custom rule id is required")
		}
		if ids[id] {
			return nil, nil, fmt.Errorf("custom rule %q is defined more than once", id)
		}
		ids[id] = true
		target := strings.TrimSpace(custom.Target)
		if target == "" {
			return nil, nil, fmt.Errorf("custom rule %q target is required", id)
		}
		if !isBuiltInTarget(target) {
			if _, ok := proxyGroups[target]; !ok {
				return nil, nil, fmt.Errorf("custom rule target %q requires a matching proxy group", target)
			}
		}
		if len(custom.Rules) == 0 {
			return nil, nil, fmt.Errorf("custom rule %q rules is required", id)
		}
		lines := make([]CustomRuleLine, 0, len(custom.Rules))
		for _, rule := range custom.Rules {
			line, err := normalizeCustomRuleLine(id, rule)
			if err != nil {
				return nil, nil, err
			}
			lines = append(lines, line)
		}
		custom.ID = id
		custom.Target = target
		custom.Rules = lines
		resolved = append(resolved, custom)
		results = append(results, CustomRuleResult{
			ID:        id,
			Target:    target,
			RuleCount: len(lines),
			Reason:    custom.Reason,
			Rules:     append([]CustomRuleLine{}, lines...),
		})
	}
	return resolved, results, nil
}

func normalizeCustomRuleLine(id string, rule CustomRuleLine) (CustomRuleLine, error) {
	rule.Type = strings.ToLower(strings.TrimSpace(rule.Type))
	rule.Value = strings.TrimSpace(rule.Value)
	if rule.Value == "" {
		return CustomRuleLine{}, fmt.Errorf("custom rule %q contains an empty value", id)
	}
	switch rule.Type {
	case "domain", "domain_suffix", "ip_cidr", "ip_cidr6":
	default:
		return CustomRuleLine{}, fmt.Errorf("custom rule %q type %q is unsupported", id, rule.Type)
	}
	return rule, nil
}

func customRulesForSelection(customRules []CustomRule) []rules.CustomRule {
	out := make([]rules.CustomRule, 0, len(customRules))
	for _, custom := range customRules {
		lines := make([]rules.CustomRuleLine, 0, len(custom.Rules))
		for _, line := range custom.Rules {
			lines = append(lines, rules.CustomRuleLine{
				Type:      line.Type,
				Value:     line.Value,
				NoResolve: line.NoResolve,
			})
		}
		out = append(out, rules.CustomRule{
			ID:     custom.ID,
			Target: custom.Target,
			Reason: custom.Reason,
			Rules:  lines,
		})
	}
	return out
}

func normalizeResolveOptions(opts ResolveOptions) ResolveOptions {
	if strings.TrimSpace(opts.SubscriptionPath) == "" {
		opts.SubscriptionPath = "subscription.yaml"
	}
	if strings.TrimSpace(opts.SubscriptionConfig) == "" {
		opts.SubscriptionConfig = "localclash-subscriptions.yaml"
	}
	if strings.TrimSpace(opts.SubscriptionRuntime) == "" {
		opts.SubscriptionRuntime = filepath.Join(".runtime", "subscriptions")
	}
	if strings.TrimSpace(opts.RulesCache) == "" {
		opts.RulesCache = filepath.Join(".runtime", "rules", "packs")
	}
	return opts
}

func normalizeSubscriptionNodeOptions(opts SubscriptionNodeOptions) SubscriptionNodeOptions {
	if strings.TrimSpace(opts.SubscriptionPath) == "" {
		opts.SubscriptionPath = "subscription.yaml"
	}
	if strings.TrimSpace(opts.SubscriptionConfig) == "" {
		opts.SubscriptionConfig = "localclash-subscriptions.yaml"
	}
	if strings.TrimSpace(opts.SubscriptionRuntime) == "" {
		opts.SubscriptionRuntime = filepath.Join(".runtime", "subscriptions")
	}
	return opts
}

func readYAMLMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func anyMapSlice(value any) []map[string]any {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func uniqueName(name string, used map[string]bool) string {
	if !used[name] {
		return name
	}
	base := name
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s (%d)", base, i)
		if !used[candidate] {
			return candidate
		}
	}
}

func isBuiltInTarget(target string) bool {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "direct", "reject", "proxy", "manual":
		return true
	default:
		return false
	}
}
