package configrender

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"localclash/internal/configmeta"
	rulespkg "localclash/internal/rules"
	"localclash/internal/runtimeprofile"

	"gopkg.in/yaml.v3"
)

type StageEvent struct {
	Stage      string         `json:"stage"`
	Event      string         `json:"event"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	Error      string         `json:"error,omitempty"`
	Fields     map[string]any `json:"fields,omitempty"`
}

type Options struct {
	SourcePath         string
	Source             map[string]any `json:"-"`
	PolicyPath         string
	Mode               string
	OutputPath         string
	PacksSelectionPath string
	Selection          *rulespkg.Selection `json:"-"`
	RulesCacheDir      string
	RuntimeProfilePath string
	Force              bool
	OnStage            func(StageEvent) `json:"-"`
}

type Result struct {
	OutputPath  string
	Mode        string
	RuntimeMode string
	Core        string
	ProxyCount  int
	RuleCount   int
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

func LocalBaselineRuleLines() []string {
	pol := policy{Groups: map[string]string{"direct": "DIRECT"}}
	rules, err := buildRules(pol, policyMode{Rules: localBaselineRules})
	if err != nil {
		return nil
	}
	return append([]string{}, rules...)
}

func Render(opts Options) (Result, error) {
	opts = normalizeOptions(opts)
	stage := configRenderStageEmitter(opts.OnStage)

	finish := stage("ensure_output", map[string]any{"output": opts.OutputPath, "force": opts.Force})
	if err := ensureOutput(opts.OutputPath, opts.Force); err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, nil)

	finish = stage("read_subscription", map[string]any{"path": opts.SourcePath})
	source := opts.Source
	var err error
	if source == nil {
		source, err = readYAMLMap(opts.SourcePath)
		if err != nil {
			finish(err, nil)
			return Result{}, err
		}
	}
	finish(nil, map[string]any{"source": renderInputSource(source, opts.Source)})

	finish = stage("read_policy", map[string]any{"path": opts.PolicyPath})
	pol, err := readPolicy(opts.PolicyPath)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, nil)

	finish = stage("select_policy_mode", map[string]any{"mode": opts.Mode})
	modeName, mode, err := selectMode(pol, opts.Mode)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, map[string]any{"selected_mode": modeName})

	finish = stage("read_runtime_profile", map[string]any{"path": opts.RuntimeProfilePath})
	runtimeFile, profile, _, err := runtimeprofile.ActiveProfile(opts.RuntimeProfilePath)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, map[string]any{"runtime_mode": runtimeFile.Mode, "core": runtimeFile.Core})

	finish = stage("read_proxies", nil)
	proxies, err := readProxies(source)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	proxyNames, err := proxyNames(proxies)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, map[string]any{"proxy_count": len(proxyNames)})

	var fragment *rulespkg.Fragment
	var selection *rulespkg.Selection
	if opts.Selection != nil || opts.PacksSelectionPath != "" {
		finish = stage("render_pack_selection", map[string]any{"selection": opts.PacksSelectionPath, "rules_cache": opts.RulesCacheDir})
		if opts.Selection != nil {
			selection = opts.Selection
		} else {
			loadedSelection, err := rulespkg.LoadSelection(opts.PacksSelectionPath)
			if err != nil {
				finish(err, nil)
				return Result{}, err
			}
			selection = &loadedSelection
		}
		renderedFragment, renderStats, err := rulespkg.RenderSelectionWithStats(*selection, opts.RulesCacheDir, proxyNames)
		if err != nil {
			finish(err, nil)
			return Result{}, err
		}
		fragment = &renderedFragment
		finish(nil, mergeRenderFields(map[string]any{
			"selection_source":    renderSelectionSource(opts.Selection),
			"rule_provider_count": len(renderedFragment.RuleProviders),
			"rule_count":          len(renderedFragment.Rules),
		}, renderStats.Fields()))
	}

	finish = stage("build_runtime_config", nil)
	rendered, err := buildRuntimeConfig(source, pol, mode, proxyNames, proxies, fragment)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, nil)

	if runtimeFile.Core == runtimeprofile.CoreSmart {
		finish = stage("apply_smart_core_groups", nil)
		applySmartCoreProxyGroups(rendered, runtimeFile.Smart)
		finish(nil, nil)
	}
	finish = stage("apply_runtime_profile", map[string]any{"runtime_mode": runtimeFile.Mode, "core": runtimeFile.Core})
	runtimeprofile.ApplyToConfig(rendered, profile)
	finish(nil, nil)
	rendered[configmeta.Key] = buildLocalClashMetadata(selection, fragment)

	finish = stage("write_output", map[string]any{"output": opts.OutputPath})
	if err := os.MkdirAll(filepath.Dir(opts.OutputPath), 0o755); err != nil {
		finish(err, nil)
		return Result{}, err
	}
	data, err := yaml.Marshal(rendered)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	if err := os.WriteFile(opts.OutputPath, data, 0o644); err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, map[string]any{"bytes": len(data)})

	return Result{
		OutputPath:  opts.OutputPath,
		Mode:        modeName,
		RuntimeMode: runtimeFile.Mode,
		Core:        runtimeFile.Core,
		ProxyCount:  len(proxyNames),
		RuleCount:   len(rendered["rules"].([]string)),
	}, nil
}

func configRenderStageEmitter(callback func(StageEvent)) func(string, map[string]any) func(error, map[string]any) {
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

func renderInputSource(source, provided map[string]any) string {
	if provided != nil {
		return "provided"
	}
	return "disk"
}

func renderSelectionSource(selection *rulespkg.Selection) string {
	if selection != nil {
		return "provided"
	}
	return "disk"
}

func mergeRenderFields(base map[string]any, extra map[string]any) map[string]any {
	for key, value := range extra {
		base[key] = value
	}
	return base
}

func buildLocalClashMetadata(selection *rulespkg.Selection, fragment *rulespkg.Fragment) configmeta.Metadata {
	metadata := configmeta.Metadata{
		Version: 1,
		Base: configmeta.BaseMetadata{
			Modifiable:  false,
			Description: "localClash generated base config",
		},
		Overlay: configmeta.OverlayMetadata{
			Modifiable:    true,
			Packs:         []configmeta.OverlayPack{},
			ProxyGroups:   []configmeta.OverlayProxyGroup{},
			PolicyGroups:  []configmeta.OverlayPolicyGroup{},
			RuleProviders: []configmeta.OverlayRuleProvider{},
			Rules:         []configmeta.OverlayRule{},
			Insertion:     "after local safety baseline, before base rules",
		},
	}
	if selection != nil {
		usedProxyGroups := map[string]bool{}
		usedPolicyGroups := map[string]bool{}
		markTarget := func(target string) {
			if _, ok := selection.PolicyGroups[target]; ok {
				usedPolicyGroups[target] = true
				for _, exit := range selection.PolicyGroups[target].Exits {
					group, ok := selection.ProxyGroups[exit]
					if ok && !(group.Optional && len(group.Nodes) == 0 && !group.Direct) {
						usedProxyGroups[exit] = true
					}
				}
				return
			}
			if _, ok := selection.ProxyGroups[target]; ok {
				usedProxyGroups[target] = true
			}
		}
		for _, enabled := range selection.EnabledPack {
			metadata.Overlay.Packs = append(metadata.Overlay.Packs, configmeta.OverlayPack{
				ID:     rulespkg.PackCatalogID(enabled.Source, enabled.Pack),
				Source: enabled.Source,
				Type:   packTypeFromFragment(fragment, enabled),
				Target: enabled.Target,
			})
			markTarget(enabled.Target)
		}
		for _, custom := range selection.CustomRules {
			markTarget(custom.Target)
		}
		for _, provider := range selection.RuleProviders {
			markTarget(provider.Target)
		}
		proxyGroupIDs := make([]string, 0, len(usedProxyGroups))
		for id := range usedProxyGroups {
			proxyGroupIDs = append(proxyGroupIDs, id)
		}
		sort.Strings(proxyGroupIDs)
		for _, id := range proxyGroupIDs {
			group := selection.ProxyGroups[id]
			metadata.Overlay.ProxyGroups = append(metadata.Overlay.ProxyGroups, configmeta.OverlayProxyGroup{
				ID:    id,
				Mode:  proxyGroupMode(group),
				Nodes: append([]string(nil), group.Nodes...),
			})
		}
		policyGroupIDs := make([]string, 0, len(usedPolicyGroups))
		for id := range usedPolicyGroups {
			policyGroupIDs = append(policyGroupIDs, id)
		}
		sort.Strings(policyGroupIDs)
		for _, id := range policyGroupIDs {
			group := selection.PolicyGroups[id]
			metadata.Overlay.PolicyGroups = append(metadata.Overlay.PolicyGroups, configmeta.OverlayPolicyGroup{
				ID:    id,
				Mode:  policyGroupMode(group),
				Exits: append([]string(nil), group.Exits...),
			})
		}
	}
	if fragment != nil {
		providerNames := make([]string, 0, len(fragment.RuleProviders))
		for name := range fragment.RuleProviders {
			providerNames = append(providerNames, name)
		}
		sort.Strings(providerNames)
		for _, name := range providerNames {
			provider := fragment.RuleProviders[name]
			metadata.Overlay.RuleProviders = append(metadata.Overlay.RuleProviders, configmeta.OverlayRuleProvider{
				Name:     name,
				Type:     stringValue(provider["type"]),
				Behavior: stringValue(provider["behavior"]),
			})
		}
		for _, line := range fragment.Rules {
			rule, ok := parseOverlayRuleLine(line)
			if ok {
				metadata.Overlay.Rules = append(metadata.Overlay.Rules, rule)
			}
		}
	}
	return metadata
}

func packTypeFromFragment(fragment *rulespkg.Fragment, enabled rulespkg.SelectedPack) string {
	if fragment == nil {
		return ""
	}
	for _, rule := range fragment.Rules {
		parts := strings.Split(rule, ",")
		if len(parts) >= 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "GEOSITE") && strings.TrimSpace(parts[1]) == enabled.Pack {
			return rulespkg.PackTypeGeoSite
		}
	}
	return rulespkg.PackTypeRuleProvider
}

func proxyGroupMode(group rulespkg.ProxyGroup) string {
	switch {
	case group.Auto:
		return "auto"
	case group.Smart:
		return "smart"
	case group.Manual:
		return "manual"
	case group.Direct:
		return "direct"
	default:
		return ""
	}
}

func policyGroupMode(group rulespkg.PolicyGroup) string {
	switch {
	case group.Auto:
		return "auto"
	case group.Smart:
		return "smart"
	case group.Manual:
		return "manual"
	default:
		return ""
	}
}

func parseOverlayRuleLine(line string) (configmeta.OverlayRule, bool) {
	parts := strings.Split(line, ",")
	if len(parts) < 3 {
		return configmeta.OverlayRule{}, false
	}
	switch parts[0] {
	case "RULE-SET":
		return configmeta.OverlayRule{Type: parts[0], Provider: parts[1], Target: parts[2]}, true
	case "DOMAIN", "DOMAIN-SUFFIX", "GEOSITE", "IP-CIDR", "IP-CIDR6":
		return configmeta.OverlayRule{Type: parts[0], Value: parts[1], Target: parts[2]}, true
	default:
		return configmeta.OverlayRule{}, false
	}
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
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
	if opts.RulesCacheDir == "" {
		opts.RulesCacheDir = ".runtime/rules/packs"
	}
	if opts.RuntimeProfilePath == "" {
		opts.RuntimeProfilePath = runtimeprofile.DefaultPath
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

func buildRuntimeConfig(source map[string]any, pol policy, mode policyMode, proxyNames []string, proxies []any, fragment *rulespkg.Fragment) (map[string]any, error) {
	config := map[string]any{
		"mixed-port":          7890,
		"allow-lan":           false,
		"mode":                "rule",
		"log-level":           "info",
		"external-controller": "127.0.0.1:9090",
		"external-ui":         "ui/zashboard",
		"unified-delay":       true,
		"proxies":             proxies,
	}
	if hosts, ok := source["hosts"]; ok {
		config["hosts"] = hosts
	}
	if dns, ok := source["dns"]; ok {
		config["dns"] = withLocalDNSPolicy(dns)
	}
	if fragment != nil {
		rewritten := rewriteFragmentBuiltInTargets(*fragment, pol.Groups)
		fragment = &rewritten
	}

	providerNames := providersUsed(mode)
	ruleProviders := buildRuleProviders(pol, providerNames)
	if fragment != nil {
		if err := mergeRuleProviders(ruleProviders, fragment.RuleProviders); err != nil {
			return nil, err
		}
	}
	config["rule-providers"] = ruleProviders

	rules, err := buildOrderedRules(pol, mode, fragment)
	if err != nil {
		return nil, err
	}
	proxyGroups := buildProxyGroups(pol.Groups, proxyNames, baseProxyGroupsUsed(pol.Groups, rules, fragment), baseManualChoices(fragment))
	if fragment != nil {
		proxyGroups, err = mergeProxyGroups(proxyGroups, fragment.ProxyGroups)
		if err != nil {
			return nil, err
		}
	}
	sortProxyGroupsByDisplay(proxyGroups, baseManualChoiceSet(fragment))
	config["proxy-groups"] = proxyGroups

	config["rules"] = rules
	return config, nil
}

func rewriteFragmentBuiltInTargets(fragment rulespkg.Fragment, groups map[string]string) rulespkg.Fragment {
	rewrite := func(value string) string {
		switch strings.ToUpper(strings.TrimSpace(value)) {
		case "PROXY":
			if groups["proxy"] != "" {
				return groups["proxy"]
			}
		case "AUTO":
			if groups["auto"] != "" {
				return groups["auto"]
			}
		case "MANUAL":
			if groups["manual"] != "" {
				return groups["manual"]
			}
		case "DIRECT":
			if groups["direct"] != "" {
				return groups["direct"]
			}
		case "REJECT":
			if groups["reject"] != "" {
				return groups["reject"]
			}
		}
		return value
	}

	out := fragment
	out.Rules = append([]string{}, fragment.Rules...)
	for i, rule := range out.Rules {
		parts := strings.Split(rule, ",")
		if len(parts) >= 3 {
			parts[2] = rewrite(parts[2])
			out.Rules[i] = strings.Join(parts, ",")
		}
	}
	out.ProxyGroups = make([]map[string]any, 0, len(fragment.ProxyGroups))
	for _, group := range fragment.ProxyGroups {
		cloned := cloneMap(group)
		if proxies, ok := stringListFromAny(cloned["proxies"]); ok {
			for i, choice := range proxies {
				proxies[i] = rewrite(choice)
			}
			cloned["proxies"] = proxies
		}
		out.ProxyGroups = append(out.ProxyGroups, cloned)
	}
	out.BaseManualChoices = append([]string{}, fragment.BaseManualChoices...)
	return out
}

func baseProxyGroupsUsed(groups map[string]string, rules []string, fragment *rulespkg.Fragment) map[string]bool {
	if fragment == nil {
		return nil
	}
	used := map[string]bool{}
	mark := func(value string) {
		value = strings.TrimSpace(value)
		for _, name := range []string{groups["proxy"], groups["auto"], groups["manual"]} {
			if name != "" && value == name {
				used[name] = true
			}
		}
	}
	for _, rule := range rules {
		if target, ok := ruleTarget(rule); ok {
			mark(target)
		}
	}
	for _, group := range fragment.ProxyGroups {
		if proxies, ok := stringListFromAny(group["proxies"]); ok {
			for _, choice := range proxies {
				mark(choice)
			}
		}
	}
	if used[groups["proxy"]] {
		used[groups["auto"]] = true
		used[groups["manual"]] = true
	}
	if used[groups["manual"]] {
		used[groups["auto"]] = true
	}
	return used
}

func ruleTarget(rule string) (string, bool) {
	parts := strings.Split(rule, ",")
	if len(parts) < 3 {
		return "", false
	}
	return strings.TrimSpace(parts[2]), true
}

func baseManualChoices(fragment *rulespkg.Fragment) []string {
	if fragment == nil {
		return nil
	}
	return append([]string{}, fragment.BaseManualChoices...)
}

func baseManualChoiceSet(fragment *rulespkg.Fragment) map[string]bool {
	out := map[string]bool{}
	if fragment == nil {
		return out
	}
	for _, choice := range fragment.BaseManualChoices {
		out[choice] = true
	}
	return out
}

func sortProxyGroupsByDisplay(groups []map[string]any, regionGroups map[string]bool) {
	sort.SliceStable(groups, func(i, j int) bool {
		leftName, _ := groups[i]["name"].(string)
		rightName, _ := groups[j]["name"].(string)
		leftPinned := proxyGroupPinnedRank(leftName)
		rightPinned := proxyGroupPinnedRank(rightName)
		if leftPinned != rightPinned {
			return leftPinned < rightPinned
		}
		leftRegion := regionGroups[leftName]
		rightRegion := regionGroups[rightName]
		if leftRegion != rightRegion {
			return !leftRegion
		}
		leftKey := proxyGroupDisplaySortKey(leftName)
		rightKey := proxyGroupDisplaySortKey(rightName)
		if leftKey == rightKey {
			return leftName < rightName
		}
		return leftKey < rightKey
	})
}

func proxyGroupPinnedRank(name string) int {
	switch proxyGroupDisplaySortKey(name) {
	case "手动选择":
		return 0
	case "自动选择":
		return 1
	default:
		return 2
	}
}

func proxyGroupDisplaySortKey(name string) string {
	trimmed := strings.TrimSpace(name)
	trimmed = strings.TrimLeftFunc(trimmed, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	return strings.TrimSpace(trimmed)
}

func stringListFromAny(raw any) ([]string, bool) {
	switch values := raw.(type) {
	case []string:
		return append([]string{}, values...), true
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil, false
			}
			out = append(out, text)
		}
		return out, true
	default:
		return nil, false
	}
}

func applySmartCoreProxyGroups(config map[string]any, opts runtimeprofile.SmartOptions) {
	groups, ok := config["proxy-groups"].([]map[string]any)
	if !ok {
		return
	}
	for _, group := range groups {
		groupType := strings.ToLower(strings.TrimSpace(stringValue(group["type"])))
		if groupType == "url-test" {
			group["type"] = "smart"
			delete(group, "url")
			delete(group, "interval")
			delete(group, "tolerance")
			groupType = "smart"
		}
		if groupType == "smart" {
			applySmartGroupOptions(group, opts)
		}
	}
}

func applySmartGroupOptions(group map[string]any, opts runtimeprofile.SmartOptions) {
	if opts.UseLightGBM {
		setDefaultAny(group, "uselightgbm", true)
	}
	if opts.PreferASN {
		setDefaultAny(group, "prefer-asn", true)
	}
	if opts.CollectData {
		setDefaultAny(group, "collectdata", true)
	}
	if opts.SampleRate != 0 {
		setDefaultAny(group, "sample-rate", opts.SampleRate)
	}
	if strings.TrimSpace(opts.PolicyPriority) != "" {
		setDefaultAny(group, "policy-priority", opts.PolicyPriority)
	}
}

func setDefaultAny(values map[string]any, key string, value any) {
	if _, exists := values[key]; !exists {
		values[key] = value
	}
}

func buildOrderedRules(pol policy, mode policyMode, fragment *rulespkg.Fragment) ([]string, error) {
	baseline, err := buildRules(pol, policyMode{Rules: localBaselineRules})
	if err != nil {
		return nil, err
	}
	base, err := buildRules(pol, mode)
	if err != nil {
		return nil, err
	}
	rules := make([]string, 0, len(baseline)+len(base))
	rules = append(rules, baseline...)
	if fragment != nil {
		rules = append(rules, fragment.Rules...)
	}
	rules = append(rules, base...)
	return rules, nil
}

func mergeRuleProviders(base map[string]any, extra map[string]map[string]any) error {
	for name, provider := range extra {
		if _, exists := base[name]; exists {
			return fmt.Errorf("rule-provider %q already exists", name)
		}
		base[name] = provider
	}
	return nil
}

func mergeProxyGroups(base []map[string]any, extra []map[string]any) ([]map[string]any, error) {
	seen := map[string]bool{}
	for _, group := range base {
		name, ok := group["name"].(string)
		if !ok || name == "" {
			return nil, fmt.Errorf("proxy-group without name: %v", group)
		}
		seen[name] = true
	}
	merged := append([]map[string]any{}, base...)
	for _, group := range extra {
		name, ok := group["name"].(string)
		if !ok || name == "" {
			return nil, fmt.Errorf("proxy-group without name: %v", group)
		}
		if seen[name] {
			return nil, fmt.Errorf("proxy-group %q already exists", name)
		}
		seen[name] = true
		merged = append(merged, group)
	}
	return merged, nil
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

func buildProxyGroups(groups map[string]string, proxyNames []string, used map[string]bool, manualExtraChoices []string) []map[string]any {
	direct := groups["direct"]
	proxy := groups["proxy"]
	auto := groups["auto"]
	manual := groups["manual"]

	manualChoices := appendUnique(nil, auto)
	for _, choice := range manualExtraChoices {
		manualChoices = appendUnique(manualChoices, choice)
	}
	for _, proxyName := range proxyNames {
		manualChoices = appendUnique(manualChoices, proxyName)
	}
	autoChoices := append([]string{}, proxyNames...)
	proxyChoices := []string{auto, manual, direct}
	var out []map[string]any
	if shouldBuildBaseGroup(proxy, used) {
		out = append(out, map[string]any{
			"name":    proxy,
			"type":    "select",
			"proxies": proxyChoices,
		})
	}
	if shouldBuildBaseGroup(auto, used) {
		out = append(out, map[string]any{
			"name":     auto,
			"type":     "url-test",
			"proxies":  autoChoices,
			"url":      "http://www.gstatic.com/generate_204",
			"interval": 300,
		})
	}
	if shouldBuildBaseGroup(manual, used) {
		out = append(out, map[string]any{
			"name":    manual,
			"type":    "select",
			"proxies": manualChoices,
		})
	}
	if apple := groups["apple"]; shouldBuildBaseGroup(apple, used) {
		out = append(out, map[string]any{
			"name":    apple,
			"type":    "select",
			"proxies": []string{direct, proxy},
		})
	}
	return out
}

func shouldBuildBaseGroup(name string, used map[string]bool) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	return used == nil || used[name]
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
