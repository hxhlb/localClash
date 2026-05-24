package localconfig

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"localclash/internal/rules"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Version        int                    `json:"version" yaml:"version"`
	PolicyTemplate string                 `json:"policy_template,omitempty" yaml:"policy_template,omitempty"`
	ProxyGroups    map[string]ProxyGroup  `json:"proxy_groups" yaml:"proxy_groups,omitempty"`
	PolicyGroups   map[string]PolicyGroup `json:"policy_groups,omitempty" yaml:"policy_groups,omitempty"`
	CustomRules    []CustomRule           `json:"custom_rules,omitempty" yaml:"custom_rules,omitempty"`
	RuleProviders  []ExternalRuleProvider `json:"rule_providers,omitempty" yaml:"rule_providers,omitempty"`
	Packs          []Pack                 `json:"packs" yaml:"packs,omitempty"`
}

type ProxyGroup struct {
	Mode          string   `json:"mode" yaml:"mode"`
	Match         *Match   `json:"match,omitempty" yaml:"match,omitempty"`
	Nodes         []string `json:"nodes,omitempty" yaml:"nodes,omitempty"`
	SelectedNodes []string `json:"selected_nodes,omitempty" yaml:"selected_nodes,omitempty"`
	Optional      bool     `json:"optional,omitempty" yaml:"optional,omitempty"`
	Reason        string   `json:"reason,omitempty" yaml:"reason,omitempty"`
	Boundary      string   `json:"boundary,omitempty" yaml:"boundary,omitempty"`
}

type PolicyGroup struct {
	Mode     string   `json:"mode" yaml:"mode"`
	Exits    []string `json:"exits" yaml:"exits"`
	Reason   string   `json:"reason,omitempty" yaml:"reason,omitempty"`
	Boundary string   `json:"boundary,omitempty" yaml:"boundary,omitempty"`
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
	Type   string `json:"type,omitempty" yaml:"type,omitempty"`
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

type ExternalRuleProvider struct {
	ID       string `json:"id" yaml:"id"`
	Target   string `json:"target" yaml:"target"`
	Reason   string `json:"reason,omitempty" yaml:"reason,omitempty"`
	Type     string `json:"type,omitempty" yaml:"type,omitempty"`
	Behavior string `json:"behavior,omitempty" yaml:"behavior,omitempty"`
	Format   string `json:"format,omitempty" yaml:"format,omitempty"`
	Path     string `json:"path,omitempty" yaml:"path,omitempty"`
	URL      string `json:"url,omitempty" yaml:"url,omitempty"`
	Interval int    `json:"interval,omitempty" yaml:"interval,omitempty"`
}

type ResolveOptions struct {
	Config              Config
	SubscriptionPath    string
	SubscriptionConfig  string
	SubscriptionRuntime string
	RulesCache          string
}

type Resolved struct {
	Config        Config               `json:"config"`
	Selection     rules.Selection      `json:"selection"`
	ProxyGroups   []ProxyGroupResult   `json:"proxy_groups"`
	PolicyGroups  []PolicyGroupResult  `json:"policy_groups"`
	CustomRules   []CustomRuleResult   `json:"custom_rules"`
	RuleProviders []RuleProviderResult `json:"rule_providers"`
	Packs         []PackResult         `json:"packs"`
	Warnings      []string             `json:"warnings"`
}

type ProxyGroupResult struct {
	ID            string   `json:"id"`
	Mode          string   `json:"mode"`
	Match         *Match   `json:"match,omitempty"`
	SelectedNodes []string `json:"selected_nodes"`
	NodeCount     int      `json:"node_count"`
	Optional      bool     `json:"optional,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	Boundary      string   `json:"boundary,omitempty"`
}

type PolicyGroupResult struct {
	ID        string   `json:"id"`
	Mode      string   `json:"mode"`
	Exits     []string `json:"exits"`
	ExitCount int      `json:"exit_count"`
	Reason    string   `json:"reason,omitempty"`
	Boundary  string   `json:"boundary,omitempty"`
}

type PackResult struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
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

type RuleProviderResult struct {
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
		Version:      1,
		ProxyGroups:  map[string]rules.ProxyGroup{},
		PolicyGroups: map[string]rules.PolicyGroup{},
	}
	resolvedConfig := opts.Config
	if resolvedConfig.ProxyGroups == nil {
		resolvedConfig.ProxyGroups = map[string]ProxyGroup{}
	}
	if resolvedConfig.PolicyGroups == nil {
		resolvedConfig.PolicyGroups = map[string]PolicyGroup{}
	}
	groupIDs := make([]string, 0, len(resolvedConfig.ProxyGroups))
	for id := range resolvedConfig.ProxyGroups {
		groupIDs = append(groupIDs, id)
	}
	sort.Strings(groupIDs)
	var groupResults []ProxyGroupResult
	for _, id := range groupIDs {
		group := resolvedConfig.ProxyGroups[id]
		mode := strings.ToLower(strings.TrimSpace(group.Mode))
		var selected []string
		var err error
		if mode == "direct" {
			if group.Match != nil || len(group.Nodes) > 0 {
				return Resolved{}, fmt.Errorf("proxy group %q direct mode cannot use match or nodes", id)
			}
		} else {
			selected, err = resolveProxyGroup(id, group, nodes)
			if err != nil {
				return Resolved{}, err
			}
		}
		group.Mode = mode
		group.SelectedNodes = selected
		resolvedConfig.ProxyGroups[id] = group
		ruleGroup := rules.ProxyGroup{Nodes: selected, Optional: group.Optional}
		switch mode {
		case "manual":
			ruleGroup.Manual = true
		case "auto":
			ruleGroup.Auto = true
		case "smart":
			ruleGroup.Smart = true
		case "direct":
			ruleGroup.Direct = true
		default:
			return Resolved{}, fmt.Errorf("proxy group %q mode must be manual, auto, smart, or direct", id)
		}
		selection.ProxyGroups[id] = ruleGroup
		groupResults = append(groupResults, ProxyGroupResult{
			ID:            id,
			Mode:          mode,
			Match:         group.Match,
			SelectedNodes: append([]string{}, selected...),
			NodeCount:     len(selected),
			Optional:      group.Optional,
			Reason:        group.Reason,
			Boundary:      group.Boundary,
		})
	}
	policyIDs := make([]string, 0, len(resolvedConfig.PolicyGroups))
	for id := range resolvedConfig.PolicyGroups {
		if _, exists := resolvedConfig.ProxyGroups[id]; exists {
			return Resolved{}, fmt.Errorf("policy group %q conflicts with a proxy group id", id)
		}
		policyIDs = append(policyIDs, id)
	}
	sort.Strings(policyIDs)
	var policyResults []PolicyGroupResult
	for _, id := range policyIDs {
		group := resolvedConfig.PolicyGroups[id]
		mode := strings.ToLower(strings.TrimSpace(group.Mode))
		ruleGroup := rules.PolicyGroup{Exits: normalizePolicyGroupExits(group.Exits)}
		switch mode {
		case "manual":
			ruleGroup.Manual = true
		case "auto":
			ruleGroup.Auto = true
		case "smart":
			ruleGroup.Smart = true
		default:
			return Resolved{}, fmt.Errorf("policy group %q mode must be manual, auto, or smart", id)
		}
		if err := validatePolicyGroupExits(id, ruleGroup.Exits, selection.ProxyGroups); err != nil {
			return Resolved{}, err
		}
		group.Mode = mode
		group.Exits = append([]string{}, ruleGroup.Exits...)
		resolvedConfig.PolicyGroups[id] = group
		selection.PolicyGroups[id] = ruleGroup
		policyResults = append(policyResults, PolicyGroupResult{
			ID:        id,
			Mode:      mode,
			Exits:     append([]string{}, ruleGroup.Exits...),
			ExitCount: len(ruleGroup.Exits),
			Reason:    group.Reason,
			Boundary:  group.Boundary,
		})
	}
	resolvedPacks := make([]Pack, 0, len(resolvedConfig.Packs))
	packResults := make([]PackResult, 0, len(resolvedConfig.Packs))
	for _, pack := range resolvedConfig.Packs {
		ref, err := rules.ResolvePackRef(opts.RulesCache, pack.ID)
		if err != nil {
			return Resolved{}, err
		}
		if err := assertPackType(pack.ID, pack.Type, ref.Type); err != nil {
			return Resolved{}, err
		}
		target := strings.TrimSpace(pack.Target)
		if target == "" {
			return Resolved{}, fmt.Errorf("pack %q target is required", pack.ID)
		}
		if !isKnownTarget(target, selection.ProxyGroups, selection.PolicyGroups) {
			return Resolved{}, fmt.Errorf("pack target %q requires a matching proxy group or policy group", target)
		}
		selection.EnabledPack = append(selection.EnabledPack, rules.SelectedPack{Source: ref.Source, Pack: ref.Pack, Target: target})
		resolvedPacks = append(resolvedPacks, Pack{ID: ref.ID, Type: ref.Type, Target: target, Reason: pack.Reason})
		packResults = append(packResults, PackResult{ID: ref.ID, Type: ref.Type, Target: target, Reason: pack.Reason})
	}
	resolvedConfig.Packs = resolvedPacks
	resolvedCustomRules, customRuleResults, err := resolveCustomRules(resolvedConfig.CustomRules, selection.ProxyGroups, selection.PolicyGroups)
	if err != nil {
		return Resolved{}, err
	}
	resolvedConfig.CustomRules = resolvedCustomRules
	selection.CustomRules = customRulesForSelection(resolvedCustomRules)
	resolvedRuleProviders, ruleProviderResults, err := resolveRuleProviders(resolvedConfig.RuleProviders, selection.ProxyGroups, selection.PolicyGroups)
	if err != nil {
		return Resolved{}, err
	}
	resolvedConfig.RuleProviders = resolvedRuleProviders
	selection.RuleProviders = ruleProvidersForSelection(resolvedRuleProviders)
	return Resolved{Config: resolvedConfig, Selection: selection, ProxyGroups: groupResults, PolicyGroups: policyResults, CustomRules: customRuleResults, RuleProviders: ruleProviderResults, Packs: packResults}, nil
}

func assertPackType(id, declared, actual string) error {
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
		return resolveMatch(id, *group.Match, nodes, group.Optional)
	}
	return resolveExactNodes(id, group.Nodes, nodes)
}

func resolveMatch(id string, match Match, nodes []SubscriptionNode, optional bool) ([]string, error) {
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
	if min < 0 {
		return nil, fmt.Errorf("proxy group %q match.min must be >= 0", id)
	}
	if min == 0 && !optional {
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

func normalizePolicyGroupExits(rawExits []string) []string {
	exits := make([]string, 0, len(rawExits))
	seen := map[string]bool{}
	for _, raw := range rawExits {
		exit := canonicalBuiltInTarget(raw)
		if exit == "" {
			exit = strings.TrimSpace(raw)
		}
		if seen[exit] {
			continue
		}
		seen[exit] = true
		exits = append(exits, exit)
	}
	return exits
}

func validatePolicyGroupExits(id string, exits []string, proxyGroups map[string]rules.ProxyGroup) error {
	if len(exits) == 0 {
		return fmt.Errorf("policy group %q exits is required", id)
	}
	for _, exit := range exits {
		if strings.TrimSpace(exit) == "" {
			return fmt.Errorf("policy group %q contains an empty exit", id)
		}
		if isBuiltInTarget(exit) {
			continue
		}
		if _, ok := proxyGroups[exit]; !ok {
			return fmt.Errorf("policy group %q exit %q requires a built-in target or matching proxy group", id, exit)
		}
	}
	return nil
}

func resolveCustomRules(customRules []CustomRule, proxyGroups map[string]rules.ProxyGroup, policyGroups map[string]rules.PolicyGroup) ([]CustomRule, []CustomRuleResult, error) {
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
		if !isKnownTarget(target, proxyGroups, policyGroups) {
			return nil, nil, fmt.Errorf("custom rule target %q requires a matching proxy group or policy group", target)
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

func resolveRuleProviders(providers []ExternalRuleProvider, proxyGroups map[string]rules.ProxyGroup, policyGroups map[string]rules.PolicyGroup) ([]ExternalRuleProvider, []RuleProviderResult, error) {
	resolved := make([]ExternalRuleProvider, 0, len(providers))
	results := make([]RuleProviderResult, 0, len(providers))
	ids := map[string]bool{}
	for _, provider := range providers {
		normalized, err := NormalizeRuleProvider(provider)
		if err != nil {
			return nil, nil, err
		}
		if ids[normalized.ID] {
			return nil, nil, fmt.Errorf("rule provider %q is defined more than once", normalized.ID)
		}
		ids[normalized.ID] = true
		if !isKnownTarget(normalized.Target, proxyGroups, policyGroups) {
			return nil, nil, fmt.Errorf("rule provider target %q requires a matching proxy group or policy group", normalized.Target)
		}
		resolved = append(resolved, normalized)
		results = append(results, ruleProviderResult(normalized))
	}
	return resolved, results, nil
}

func NormalizeRuleProvider(provider ExternalRuleProvider) (ExternalRuleProvider, error) {
	provider.ID = strings.TrimSpace(provider.ID)
	if provider.ID == "" {
		return ExternalRuleProvider{}, fmt.Errorf("rule provider id is required")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]+$`).MatchString(provider.ID) {
		return ExternalRuleProvider{}, fmt.Errorf("rule provider %q contains unsupported characters", provider.ID)
	}
	provider.Target = strings.TrimSpace(provider.Target)
	if provider.Target == "" {
		return ExternalRuleProvider{}, fmt.Errorf("rule provider %q target is required", provider.ID)
	}
	provider.Type = strings.ToLower(strings.TrimSpace(provider.Type))
	if provider.Type == "" {
		provider.Type = "http"
	}
	provider.Behavior = strings.ToLower(strings.TrimSpace(provider.Behavior))
	if provider.Behavior == "" {
		provider.Behavior = "classical"
	}
	provider.Format = strings.ToLower(strings.TrimSpace(provider.Format))
	if provider.Format == "" {
		provider.Format = "yaml"
	}
	provider.Path = strings.TrimSpace(provider.Path)
	if provider.Path == "" {
		provider.Path = "./rule_provider/" + provider.ID + ".yaml"
	}
	provider.URL = strings.TrimSpace(provider.URL)
	switch provider.Type {
	case "http":
		if provider.URL == "" {
			return ExternalRuleProvider{}, fmt.Errorf("rule provider %q url is required for http type", provider.ID)
		}
		parsed, err := url.Parse(provider.URL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return ExternalRuleProvider{}, fmt.Errorf("rule provider %q url is invalid", provider.ID)
		}
		if provider.Interval == 0 {
			provider.Interval = 86400
		}
	case "file":
		if provider.URL != "" {
			return ExternalRuleProvider{}, fmt.Errorf("rule provider %q url is only supported for http type", provider.ID)
		}
	default:
		return ExternalRuleProvider{}, fmt.Errorf("rule provider %q type %q is unsupported", provider.ID, provider.Type)
	}
	switch provider.Behavior {
	case "classical", "domain", "ipcidr":
	default:
		return ExternalRuleProvider{}, fmt.Errorf("rule provider %q behavior %q is unsupported", provider.ID, provider.Behavior)
	}
	switch provider.Format {
	case "yaml", "text", "mrs":
	default:
		return ExternalRuleProvider{}, fmt.Errorf("rule provider %q format %q is unsupported", provider.ID, provider.Format)
	}
	if provider.Interval < 0 {
		return ExternalRuleProvider{}, fmt.Errorf("rule provider %q interval must be non-negative", provider.ID)
	}
	return provider, nil
}

func ruleProviderResult(provider ExternalRuleProvider) RuleProviderResult {
	return RuleProviderResult{
		ID:       provider.ID,
		Target:   provider.Target,
		Reason:   provider.Reason,
		Type:     provider.Type,
		Behavior: provider.Behavior,
		Format:   provider.Format,
		Path:     provider.Path,
		URL:      provider.URL,
		Interval: provider.Interval,
	}
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

func ruleProvidersForSelection(providers []ExternalRuleProvider) []rules.ExternalRuleProvider {
	out := make([]rules.ExternalRuleProvider, 0, len(providers))
	for _, provider := range providers {
		out = append(out, rules.ExternalRuleProvider{
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
	return canonicalBuiltInTarget(target) != ""
}

func canonicalBuiltInTarget(target string) string {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "direct", "reject", "proxy", "manual", "auto":
		return strings.ToUpper(strings.TrimSpace(target))
	default:
		return ""
	}
}

func isKnownTarget(target string, proxyGroups map[string]rules.ProxyGroup, policyGroups map[string]rules.PolicyGroup) bool {
	if isBuiltInTarget(target) {
		return true
	}
	trimmed := strings.TrimSpace(target)
	if _, ok := proxyGroups[trimmed]; ok {
		return true
	}
	if _, ok := policyGroups[trimmed]; ok {
		return true
	}
	return false
}
