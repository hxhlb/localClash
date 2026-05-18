package rules

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Source struct {
	ID         string `yaml:"id"`
	Adapter    string `yaml:"adapter"`
	URL        string `yaml:"url"`
	BaseURL    string `yaml:"base_url,omitempty"`
	RawBaseURL string `yaml:"raw_base_url,omitempty"`
}

type PackCache struct {
	Version    int    `yaml:"version"`
	Source     string `yaml:"source"`
	Adapter    string `yaml:"adapter"`
	Renderable bool   `yaml:"renderable"`
	Packs      []Pack `yaml:"packs"`
}

type Pack struct {
	ID         string      `yaml:"id"`
	Name       string      `yaml:"name,omitempty"`
	Target     string      `yaml:"target,omitempty"`
	Renderable bool        `yaml:"renderable"`
	Reason     string      `yaml:"reason,omitempty"`
	Components []Component `yaml:"components,omitempty"`
}

type Component struct {
	ID         string `yaml:"id"`
	Behavior   string `yaml:"behavior"`
	Format     string `yaml:"format"`
	OrderClass string `yaml:"order_class"`
	URL        string `yaml:"url"`
	Path       string `yaml:"path"`
}

type Selection struct {
	Version       int                    `yaml:"version"`
	ProxyGroups   map[string]ProxyGroup  `yaml:"proxy_groups,omitempty"`
	CustomRules   []CustomRule           `yaml:"custom_rules,omitempty"`
	RuleProviders []ExternalRuleProvider `yaml:"rule_providers,omitempty"`
	EnabledPack   []SelectedPack         `yaml:"enabled_packs"`
}

type ProxyGroup struct {
	Nodes  []string `yaml:"nodes"`
	Auto   bool     `yaml:"auto"`
	Manual bool     `yaml:"manual"`
	Smart  bool     `yaml:"smart"`
	Direct bool     `yaml:"direct"`
}

type SelectedPack struct {
	Source string `yaml:"source"`
	Pack   string `yaml:"pack"`
	Target string `yaml:"target"`
}

type CustomRule struct {
	ID     string           `yaml:"id" json:"id"`
	Target string           `yaml:"target" json:"target"`
	Reason string           `yaml:"reason,omitempty" json:"reason,omitempty"`
	Rules  []CustomRuleLine `yaml:"rules" json:"rules"`
}

type CustomRuleLine struct {
	Type      string `yaml:"type" json:"type"`
	Value     string `yaml:"value" json:"value"`
	NoResolve bool   `yaml:"no_resolve,omitempty" json:"no_resolve,omitempty"`
}

type ExternalRuleProvider struct {
	ID       string `yaml:"id" json:"id"`
	Target   string `yaml:"target" json:"target"`
	Reason   string `yaml:"reason,omitempty" json:"reason,omitempty"`
	Type     string `yaml:"type" json:"type"`
	Behavior string `yaml:"behavior" json:"behavior"`
	Format   string `yaml:"format" json:"format"`
	Path     string `yaml:"path" json:"path"`
	URL      string `yaml:"url,omitempty" json:"url,omitempty"`
	Interval int    `yaml:"interval,omitempty" json:"interval,omitempty"`
}

type Fragment struct {
	RuleProviders map[string]map[string]any `yaml:"rule-providers"`
	ProxyGroups   []map[string]any          `yaml:"proxy-groups,omitempty"`
	Rules         []string                  `yaml:"rules"`
}

type Options struct {
	SourcesDir    string
	CacheDir      string
	SelectionPath string
	Subscription  string
	OutputPath    string
}

func NormalizeOptions(opts Options) Options {
	if strings.TrimSpace(opts.SourcesDir) == "" {
		opts.SourcesDir = "rule-sources"
	}
	if strings.TrimSpace(opts.CacheDir) == "" {
		opts.CacheDir = ".runtime/rules/packs"
	}
	if strings.TrimSpace(opts.SelectionPath) == "" {
		opts.SelectionPath = "localclash-packs.yaml"
	}
	if strings.TrimSpace(opts.Subscription) == "" {
		opts.Subscription = "subscription.yaml"
	}
	return opts
}

func Adapt(ctx context.Context, opts Options) ([]PackCache, error) {
	opts = NormalizeOptions(opts)
	sources, err := LoadSources(opts.SourcesDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(opts.CacheDir, 0o755); err != nil {
		return nil, err
	}

	caches := make([]PackCache, 0, len(sources))
	for _, source := range sources {
		cache, err := adaptSource(ctx, source)
		if err != nil {
			return nil, fmt.Errorf("adapt %s: %w", source.ID, err)
		}
		if err := WritePackCache(opts.CacheDir, cache); err != nil {
			return nil, err
		}
		caches = append(caches, cache)
	}
	return caches, nil
}

func Render(opts Options) (Fragment, error) {
	opts = NormalizeOptions(opts)
	selection, err := LoadSelection(opts.SelectionPath)
	if err != nil {
		return Fragment{}, err
	}
	caches, err := LoadPackCaches(opts.CacheDir)
	if err != nil {
		return Fragment{}, err
	}
	proxyNames, err := LoadSubscriptionProxyNames(opts.Subscription)
	if err != nil {
		return Fragment{}, err
	}
	return RenderFragment(selection, caches, proxyNames)
}

func LoadSources(dir string) ([]Source, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var sources []Source
	for _, entry := range entries {
		if shouldSkipYAMLFile(entry.Name(), entry.IsDir()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var source Source
		if err := yaml.Unmarshal(data, &source); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if err := validateSource(source); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].ID < sources[j].ID })
	return sources, nil
}

func validateSource(source Source) error {
	if source.ID == "" {
		return errors.New("id is required")
	}
	if source.Adapter == "" {
		return errors.New("adapter is required")
	}
	if source.URL == "" {
		return errors.New("url is required")
	}
	switch source.Adapter {
	case "sukkaw":
		if source.BaseURL == "" {
			return errors.New("base_url is required for sukkaw")
		}
	case "blackmatrix7", "v2fly-dlc", "syncnext":
		if source.RawBaseURL == "" {
			return fmt.Errorf("raw_base_url is required for %s", source.Adapter)
		}
	default:
		return fmt.Errorf("unknown adapter %q", source.Adapter)
	}
	return nil
}

func WritePackCache(dir string, cache PackCache) error {
	data, err := yaml.Marshal(cache)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, cache.Source+".yaml"), data, 0o644)
}

func LoadPackCaches(dir string) (map[string]PackCache, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	caches := map[string]PackCache{}
	for _, entry := range entries {
		if shouldSkipYAMLFile(entry.Name(), entry.IsDir()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var cache PackCache
		if err := yaml.Unmarshal(data, &cache); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if cache.Source == "" {
			return nil, fmt.Errorf("%s: source is required", path)
		}
		caches[cache.Source] = cache
	}
	return caches, nil
}

func shouldSkipYAMLFile(name string, isDir bool) bool {
	if isDir || !strings.HasSuffix(name, ".yaml") {
		return true
	}
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "._")
}

func LoadSelection(path string) (Selection, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Selection{}, err
	}
	var selection Selection
	if err := yaml.Unmarshal(data, &selection); err != nil {
		return Selection{}, err
	}
	return selection, nil
}

func LoadSubscriptionProxyNames(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var source map[string]any
	if err := yaml.Unmarshal(data, &source); err != nil {
		return nil, err
	}
	raw, ok := source["proxies"].([]any)
	if !ok {
		return nil, fmt.Errorf("subscription %q has no proxies", path)
	}
	names := make([]string, 0, len(raw))
	for _, item := range raw {
		proxy, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("subscription %q contains an invalid proxy entry", path)
		}
		name, ok := proxy["name"].(string)
		if !ok || name == "" {
			return nil, fmt.Errorf("subscription %q contains a proxy without name", path)
		}
		names = append(names, name)
	}
	return names, nil
}

func WriteFragment(path string, fragment Fragment) error {
	data, err := yaml.Marshal(fragment)
	if err != nil {
		return err
	}
	if path == "" || path == "-" {
		_, err = os.Stdout.Write(data)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func RenderFragment(selection Selection, caches map[string]PackCache, proxyNames ...[]string) (Fragment, error) {
	fragment := Fragment{
		RuleProviders: map[string]map[string]any{},
	}
	targets, err := prepareTargets(selection, firstProxyNames(proxyNames))
	if err != nil {
		return Fragment{}, err
	}
	usedProxyGroups := map[string]bool{}
	for _, custom := range selection.CustomRules {
		target, proxyGroup, err := renderTarget(custom.Target, targets)
		if err != nil {
			return Fragment{}, err
		}
		if proxyGroup {
			usedProxyGroups[target] = true
		}
		lines, err := renderCustomRuleLines(custom, target)
		if err != nil {
			return Fragment{}, err
		}
		fragment.Rules = append(fragment.Rules, lines...)
	}
	for _, provider := range selection.RuleProviders {
		target, proxyGroup, err := renderTarget(provider.Target, targets)
		if err != nil {
			return Fragment{}, err
		}
		if proxyGroup {
			usedProxyGroups[target] = true
		}
		rendered, err := renderExternalRuleProvider(provider)
		if err != nil {
			return Fragment{}, err
		}
		fragment.RuleProviders[provider.ID] = rendered
		fragment.Rules = append(fragment.Rules, fmt.Sprintf("RULE-SET,%s,%s", provider.ID, target))
	}
	for _, enabled := range selection.EnabledPack {
		cache, ok := caches[enabled.Source]
		if !ok {
			return Fragment{}, fmt.Errorf("source %q has no pack cache; run rules adapt first", enabled.Source)
		}
		pack, ok := findPack(cache.Packs, enabled.Pack)
		if !ok {
			return Fragment{}, fmt.Errorf("pack %q not found in source %q", enabled.Pack, enabled.Source)
		}
		if !pack.Renderable {
			return Fragment{}, fmt.Errorf("pack %q from source %q is not renderable: %s", enabled.Pack, enabled.Source, pack.Reason)
		}
		target, proxyGroup, err := renderTarget(enabled.Target, targets)
		if err != nil {
			return Fragment{}, err
		}
		if proxyGroup {
			usedProxyGroups[target] = true
		}
		for _, component := range pack.Components {
			providerName := providerName(enabled.Source, pack.ID, component.ID)
			fragment.RuleProviders[providerName] = map[string]any{
				"type":     "http",
				"behavior": component.Behavior,
				"format":   component.Format,
				"url":      component.URL,
				"path":     component.Path,
			}
			fragment.Rules = append(fragment.Rules, fmt.Sprintf("RULE-SET,%s,%s", providerName, target))
		}
	}
	proxyGroups, err := materializeProxyGroups(usedProxyGroups, targets)
	if err != nil {
		return Fragment{}, err
	}
	fragment.ProxyGroups = proxyGroups
	return fragment, nil
}

func renderExternalRuleProvider(provider ExternalRuleProvider) (map[string]any, error) {
	id := strings.TrimSpace(provider.ID)
	if id == "" {
		return nil, fmt.Errorf("rule provider id is required")
	}
	out := map[string]any{
		"type":     strings.TrimSpace(provider.Type),
		"behavior": strings.TrimSpace(provider.Behavior),
		"format":   strings.TrimSpace(provider.Format),
		"path":     strings.TrimSpace(provider.Path),
	}
	if out["type"] == "" || out["behavior"] == "" || out["format"] == "" || out["path"] == "" {
		return nil, fmt.Errorf("rule provider %q requires type, behavior, format, and path", id)
	}
	if strings.TrimSpace(provider.URL) != "" {
		out["url"] = strings.TrimSpace(provider.URL)
	}
	if provider.Interval > 0 {
		out["interval"] = provider.Interval
	}
	return out, nil
}

func renderCustomRuleLines(custom CustomRule, target string) ([]string, error) {
	id := strings.TrimSpace(custom.ID)
	if id == "" {
		return nil, fmt.Errorf("custom rule id is required")
	}
	if strings.TrimSpace(target) == "" {
		return nil, fmt.Errorf("custom rule %q target is required", id)
	}
	if len(custom.Rules) == 0 {
		return nil, fmt.Errorf("custom rule %q rules is required", id)
	}
	lines := make([]string, 0, len(custom.Rules))
	for _, rule := range custom.Rules {
		line, err := renderCustomRuleLine(id, rule, target)
		if err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	return lines, nil
}

func renderCustomRuleLine(id string, rule CustomRuleLine, target string) (string, error) {
	value := strings.TrimSpace(rule.Value)
	if value == "" {
		return "", fmt.Errorf("custom rule %q contains an empty value", id)
	}
	var kind string
	switch strings.ToLower(strings.TrimSpace(rule.Type)) {
	case "domain":
		kind = "DOMAIN"
	case "domain_suffix":
		kind = "DOMAIN-SUFFIX"
	case "ip_cidr":
		kind = "IP-CIDR"
	case "ip_cidr6":
		kind = "IP-CIDR6"
	default:
		return "", fmt.Errorf("custom rule %q type %q is unsupported", id, rule.Type)
	}
	line := fmt.Sprintf("%s,%s,%s", kind, value, target)
	if rule.NoResolve {
		line += ",no-resolve"
	}
	return line, nil
}

func firstProxyNames(values [][]string) []string {
	if len(values) == 0 {
		return nil
	}
	return values[0]
}

func findPack(packs []Pack, id string) (Pack, bool) {
	for _, pack := range packs {
		if pack.ID == id {
			return pack, true
		}
	}
	return Pack{}, false
}

type preparedTargets struct {
	proxyGroups map[string]ProxyGroup
}

func prepareTargets(selection Selection, proxyNames []string) (preparedTargets, error) {
	available := map[string]bool{}
	for _, name := range proxyNames {
		available[name] = true
	}
	for groupName, group := range selection.ProxyGroups {
		if len(group.Nodes) == 0 && !group.Direct {
			return preparedTargets{}, fmt.Errorf("proxy group %q has no nodes", groupName)
		}
		enabledModes := 0
		for _, enabled := range []bool{group.Auto, group.Manual, group.Smart} {
			if enabled {
				enabledModes++
			}
		}
		if enabledModes > 1 {
			return preparedTargets{}, fmt.Errorf("proxy group %q can enable only one of auto, manual, or smart", groupName)
		}
		if enabledModes == 0 && !group.Direct {
			return preparedTargets{}, fmt.Errorf("proxy group %q has no enabled choices", groupName)
		}
		if group.Direct && (group.Auto || group.Smart) {
			return preparedTargets{}, fmt.Errorf("proxy group %q cannot combine direct with auto or smart mode", groupName)
		}
		seen := map[string]bool{}
		for _, node := range group.Nodes {
			node = strings.TrimSpace(node)
			if node == "" {
				return preparedTargets{}, fmt.Errorf("proxy group %q has an empty node name", groupName)
			}
			if seen[node] {
				continue
			}
			seen[node] = true
			if !available[node] {
				return preparedTargets{}, fmt.Errorf("proxy group %q references unknown subscription node %q", groupName, node)
			}
		}
	}
	return preparedTargets{proxyGroups: selection.ProxyGroups}, nil
}

func renderTarget(target string, targets preparedTargets) (string, bool, error) {
	trimmed := strings.TrimSpace(target)
	switch strings.ToLower(trimmed) {
	case "proxy":
		return "PROXY", false, nil
	case "direct":
		return "DIRECT", false, nil
	case "reject":
		return "REJECT", false, nil
	case "manual":
		return "MANUAL", false, nil
	default:
		if _, ok := targets.proxyGroups[trimmed]; ok {
			return trimmed, true, nil
		}
		return "", false, fmt.Errorf("unknown pack target %q", target)
	}
}

func materializeProxyGroups(used map[string]bool, targets preparedTargets) ([]map[string]any, error) {
	names := make([]string, 0, len(used))
	for name := range used {
		names = append(names, name)
	}
	sort.Strings(names)
	var groups []map[string]any
	for _, name := range names {
		group := targets.proxyGroups[name]
		candidates := candidateProxies(group)
		if len(candidates) == 0 && !group.Direct {
			return nil, fmt.Errorf("proxy group %q has no candidate proxies", name)
		}
		if group.Auto {
			groups = append(groups, map[string]any{
				"name":     name,
				"type":     "url-test",
				"proxies":  candidates,
				"url":      "http://www.gstatic.com/generate_204",
				"interval": 300,
			})
			continue
		}
		if group.Smart {
			groups = append(groups, map[string]any{
				"name":        name,
				"type":        "smart",
				"proxies":     candidates,
				"uselightgbm": true,
				"prefer-asn":  true,
			})
			continue
		}
		choices := append([]string{}, candidates...)
		if group.Direct {
			choices = append(choices, "DIRECT")
		}
		groups = append(groups, map[string]any{
			"name":    name,
			"type":    "select",
			"proxies": choices,
		})
	}
	return groups, nil
}

func candidateProxies(group ProxyGroup) []string {
	var candidates []string
	seen := map[string]bool{}
	for _, proxy := range group.Nodes {
		proxy = strings.TrimSpace(proxy)
		if proxy == "" || seen[proxy] {
			continue
		}
		seen[proxy] = true
		candidates = append(candidates, proxy)
	}
	return candidates
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

var unsafeProviderChars = regexp.MustCompile(`[^A-Za-z0-9_]+`)

func providerName(source, pack, component string) string {
	raw := source + "_" + pack
	if component != "" && component != pack {
		raw += "_" + component
	}
	raw = strings.ReplaceAll(raw, "-", "_")
	raw = unsafeProviderChars.ReplaceAllString(raw, "_")
	return strings.Trim(raw, "_")
}

func githubAPIContentsURL(htmlURL string) (string, error) {
	parsed, err := url.Parse(htmlURL)
	if err != nil {
		return "", err
	}
	if parsed.Host != "github.com" {
		return "", fmt.Errorf("expected github.com url, got %q", htmlURL)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 5 || parts[2] != "tree" {
		return "", fmt.Errorf("expected GitHub tree url, got %q", htmlURL)
	}
	owner, repo, ref := parts[0], parts[1], parts[3]
	repoPath := strings.Join(parts[4:], "/")
	return fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s", owner, repo, repoPath, ref), nil
}

func trimConf(name string) (string, bool) {
	if !strings.HasSuffix(name, ".conf") {
		return "", false
	}
	return strings.TrimSuffix(name, ".conf"), true
}
