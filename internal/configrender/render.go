package configrender

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Options struct {
	SourcePath string
	PolicyPath string
	Mode       string
	OutputPath string
	Force      bool
}

type Result struct {
	OutputPath string
	Mode       string
	ProxyCount int
	RuleCount  int
}

type policy struct {
	RuleSource struct {
		BaseURL        string `yaml:"base_url"`
		PrimaryBaseURL string `yaml:"primary_base_url"`
		UpdateInterval int    `yaml:"update_interval"`
	} `yaml:"rule_source"`
	Groups          map[string]string             `yaml:"groups"`
	ProviderMapping map[string]providerDefinition `yaml:"provider_mapping"`
	Modes           policyModes                   `yaml:"modes"`
}

type policyModes struct {
	Default   string     `yaml:"default"`
	Whitelist policyMode `yaml:"whitelist"`
	Blacklist policyMode `yaml:"blacklist"`
}

type policyMode struct {
	Fallback string     `yaml:"fallback"`
	Rules    []ruleSpec `yaml:"rules"`
}

type providerDefinition struct {
	Path     string `yaml:"path"`
	Behavior string `yaml:"behavior"`
	Target   string `yaml:"target"`
}

type ruleSpec struct {
	Provider     string `yaml:"provider,omitempty"`
	Domain       string `yaml:"domain,omitempty"`
	DomainSuffix string `yaml:"domain_suffix,omitempty"`
	IPCIDR       string `yaml:"ip_cidr,omitempty"`
	IPCIDR6      string `yaml:"ip_cidr6,omitempty"`
	GeoIP        string `yaml:"geoip,omitempty"`
	Match        bool   `yaml:"match,omitempty"`
	Target       string `yaml:"target"`
	NoResolve    bool   `yaml:"no_resolve,omitempty"`
}

var localBaselineRules = []ruleSpec{
	{Domain: "localhost", Target: "direct"},
	{DomainSuffix: "localhost", Target: "direct"},
	{DomainSuffix: "local", Target: "direct"},
	{DomainSuffix: "lan", Target: "direct"},
	{DomainSuffix: "home.arpa", Target: "direct"},
	{IPCIDR: "127.0.0.0/8", Target: "direct", NoResolve: true},
	{IPCIDR: "10.0.0.0/8", Target: "direct", NoResolve: true},
	{IPCIDR: "172.16.0.0/12", Target: "direct", NoResolve: true},
	{IPCIDR: "192.168.0.0/16", Target: "direct", NoResolve: true},
	{IPCIDR: "169.254.0.0/16", Target: "direct", NoResolve: true},
	{IPCIDR6: "::1/128", Target: "direct", NoResolve: true},
	{IPCIDR6: "fc00::/7", Target: "direct", NoResolve: true},
	{IPCIDR6: "fe80::/10", Target: "direct", NoResolve: true},
}

func Render(opts Options) (Result, error) {
	opts = normalizeOptions(opts)
	if err := ensureOutput(opts.OutputPath, opts.Force); err != nil {
		return Result{}, err
	}

	source, err := readYAMLMap(opts.SourcePath)
	if err != nil {
		return Result{}, err
	}
	pol, err := readPolicy(opts.PolicyPath)
	if err != nil {
		return Result{}, err
	}
	modeName, mode, err := selectMode(pol, opts.Mode)
	if err != nil {
		return Result{}, err
	}

	proxies, err := readProxies(source)
	if err != nil {
		return Result{}, err
	}
	proxyNames, err := proxyNames(proxies)
	if err != nil {
		return Result{}, err
	}

	rendered, err := buildRuntimeConfig(source, pol, mode, proxyNames, proxies)
	if err != nil {
		return Result{}, err
	}

	if err := os.MkdirAll(filepath.Dir(opts.OutputPath), 0o755); err != nil {
		return Result{}, err
	}
	data, err := yaml.Marshal(rendered)
	if err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(opts.OutputPath, data, 0o644); err != nil {
		return Result{}, err
	}

	return Result{
		OutputPath: opts.OutputPath,
		Mode:       modeName,
		ProxyCount: len(proxyNames),
		RuleCount:  len(mode.Rules),
	}, nil
}

func normalizeOptions(opts Options) Options {
	if opts.SourcePath == "" {
		opts.SourcePath = "subscription.yaml"
	}
	if opts.PolicyPath == "" {
		opts.PolicyPath = "policies/loyalsoldier.yaml"
	}
	if opts.OutputPath == "" {
		opts.OutputPath = "generated/mihomo.yaml"
	}
	return opts
}

func ensureOutput(path string, force bool) error {
	if strings.TrimSpace(path) == "" || path == "." || path == string(filepath.Separator) {
		return fmt.Errorf("output path %q is not a file path", path)
	}
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("output path %q is a directory", path)
		}
		if !force {
			return fmt.Errorf("output path %q already exists; pass --force to overwrite", path)
		}
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
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
	return out, nil
}

func readPolicy(path string) (policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return policy{}, err
	}
	var pol policy
	if err := yaml.Unmarshal(data, &pol); err != nil {
		return policy{}, err
	}
	if len(pol.Groups) == 0 {
		return policy{}, errors.New("policy has no groups")
	}
	if len(pol.ProviderMapping) == 0 {
		return policy{}, errors.New("policy has no provider_mapping")
	}
	return pol, nil
}

func selectMode(pol policy, requested string) (string, policyMode, error) {
	name := requested
	if name == "" {
		name = pol.Modes.Default
	}
	switch name {
	case "whitelist":
		return name, pol.Modes.Whitelist, nil
	case "blacklist":
		return name, pol.Modes.Blacklist, nil
	default:
		return "", policyMode{}, fmt.Errorf("unknown policy mode %q", name)
	}
}

func readProxies(source map[string]any) ([]any, error) {
	raw, ok := source["proxies"]
	if !ok {
		return nil, errors.New("source subscription has no proxies")
	}
	proxies, ok := raw.([]any)
	if !ok || len(proxies) == 0 {
		return nil, errors.New("source subscription proxies is empty or invalid")
	}
	return proxies, nil
}

func proxyNames(proxies []any) ([]string, error) {
	names := make([]string, 0, len(proxies))
	seen := map[string]bool{}
	for _, raw := range proxies {
		proxy, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("source proxy entry is not a map")
		}
		name, ok := proxy["name"].(string)
		if !ok || name == "" {
			return nil, errors.New("source proxy entry has no name")
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names, nil
}

func buildRuntimeConfig(source map[string]any, pol policy, mode policyMode, proxyNames []string, proxies []any) (map[string]any, error) {
	config := map[string]any{
		"mixed-port":          7890,
		"allow-lan":           false,
		"mode":                "rule",
		"log-level":           "info",
		"external-controller": "127.0.0.1:9090",
		"unified-delay":       true,
		"proxies":             proxies,
	}
	if hosts, ok := source["hosts"]; ok {
		config["hosts"] = hosts
	}
	if dns, ok := source["dns"]; ok {
		config["dns"] = withLocalDNSPolicy(dns)
	}

	providerNames := providersUsed(mode)
	config["rule-providers"] = buildRuleProviders(pol, providerNames)
	config["proxy-groups"] = buildProxyGroups(pol.Groups, proxyNames)

	rules, err := buildRules(pol, withLocalBaseline(mode))
	if err != nil {
		return nil, err
	}
	config["rules"] = rules
	return config, nil
}

func withLocalBaseline(mode policyMode) policyMode {
	rules := make([]ruleSpec, 0, len(localBaselineRules)+len(mode.Rules))
	rules = append(rules, localBaselineRules...)
	rules = append(rules, mode.Rules...)
	mode.Rules = rules
	return mode
}

func withLocalDNSPolicy(raw any) any {
	dns, ok := raw.(map[string]any)
	if !ok {
		return raw
	}
	dns = cloneMap(dns)
	dns["use-system-hosts"] = true

	policy, _ := dns["nameserver-policy"].(map[string]any)
	policy = cloneMap(policy)
	for _, domain := range []string{"+.local", "+.lan", "+.home.arpa", "localhost", "+.localhost"} {
		policy[domain] = "system"
	}
	dns["nameserver-policy"] = policy

	filter := stringSlice(dns["fake-ip-filter"])
	for _, domain := range []string{"*.local", "+.local", "*.lan", "+.lan", "*.home.arpa", "+.home.arpa", "localhost", "+.localhost"} {
		filter = appendUnique(filter, domain)
	}
	dns["fake-ip-filter"] = filter

	return dns
}

func cloneMap(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func stringSlice(raw any) []string {
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if s, ok := value.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func providersUsed(mode policyMode) []string {
	seen := map[string]bool{}
	var names []string
	for _, rule := range mode.Rules {
		if rule.Provider == "" || seen[rule.Provider] {
			continue
		}
		seen[rule.Provider] = true
		names = append(names, rule.Provider)
	}
	sort.Strings(names)
	return names
}

func buildRuleProviders(pol policy, names []string) map[string]any {
	out := map[string]any{}
	baseURL := strings.TrimRight(pol.RuleSource.BaseURL, "/")
	interval := pol.RuleSource.UpdateInterval
	if interval == 0 {
		interval = 86400
	}
	for _, name := range names {
		def := pol.ProviderMapping[name]
		out[name] = map[string]any{
			"type":     "http",
			"behavior": def.Behavior,
			"url":      baseURL + "/" + def.Path,
			"path":     "./ruleset/" + name + ".yaml",
			"interval": interval,
		}
	}
	return out
}

func buildProxyGroups(groups map[string]string, proxyNames []string) []map[string]any {
	direct := groups["direct"]
	proxy := groups["proxy"]
	auto := groups["auto"]
	manual := groups["manual"]
	apple := groups["apple"]

	manualChoices := append([]string{}, proxyNames...)
	autoChoices := append([]string{}, proxyNames...)
	proxyChoices := []string{auto, manual, direct}
	return []map[string]any{
		{
			"name":    proxy,
			"type":    "select",
			"proxies": proxyChoices,
		},
		{
			"name":     auto,
			"type":     "url-test",
			"proxies":  autoChoices,
			"url":      "http://www.gstatic.com/generate_204",
			"interval": 300,
		},
		{
			"name":    manual,
			"type":    "select",
			"proxies": manualChoices,
		},
		{
			"name":    apple,
			"type":    "select",
			"proxies": []string{direct, proxy},
		},
	}
}

func buildRules(pol policy, mode policyMode) ([]string, error) {
	rules := make([]string, 0, len(mode.Rules))
	for _, rule := range mode.Rules {
		target := pol.Groups[rule.Target]
		if target == "" {
			return nil, fmt.Errorf("unknown target group %q", rule.Target)
		}
		switch {
		case rule.Provider != "":
			if _, ok := pol.ProviderMapping[rule.Provider]; !ok {
				return nil, fmt.Errorf("unknown provider %q", rule.Provider)
			}
			line := fmt.Sprintf("RULE-SET,%s,%s", rule.Provider, target)
			if rule.NoResolve {
				line += ",no-resolve"
			}
			rules = append(rules, line)
		case rule.Domain != "":
			rules = append(rules, fmt.Sprintf("DOMAIN,%s,%s", rule.Domain, target))
		case rule.DomainSuffix != "":
			rules = append(rules, fmt.Sprintf("DOMAIN-SUFFIX,%s,%s", rule.DomainSuffix, target))
		case rule.IPCIDR != "":
			line := fmt.Sprintf("IP-CIDR,%s,%s", rule.IPCIDR, target)
			if rule.NoResolve {
				line += ",no-resolve"
			}
			rules = append(rules, line)
		case rule.IPCIDR6 != "":
			line := fmt.Sprintf("IP-CIDR6,%s,%s", rule.IPCIDR6, target)
			if rule.NoResolve {
				line += ",no-resolve"
			}
			rules = append(rules, line)
		case rule.GeoIP != "":
			rules = append(rules, fmt.Sprintf("GEOIP,%s,%s", rule.GeoIP, target))
		case rule.Match:
			rules = append(rules, fmt.Sprintf("MATCH,%s", target))
		default:
			return nil, errors.New("empty rule in policy mode")
		}
	}
	return rules, nil
}
