package envinspect

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"localclash/internal/appinit"

	"gopkg.in/yaml.v3"
)

type Options struct {
	RootDir                string
	WorkDir                string
	Paths                  appinit.RuntimePaths
	OpenClashReferenceRoot string
	CommandTimeout         time.Duration
}

type Result struct {
	Host               HostInfo           `json:"host"`
	Observed           Observed           `json:"observed"`
	Capabilities       Capabilities       `json:"capabilities"`
	LocalClashState    LocalClashState    `json:"localclash_state"`
	OpenClashState     OpenClashState     `json:"openclash_state"`
	SetupReadiness     SetupReadiness     `json:"setup_readiness"`
	SafetyBoundaries   []string           `json:"safety_boundaries"`
	Redactions         []string           `json:"redactions"`
	ReferenceSnapshots ReferenceSnapshots `json:"reference_snapshots,omitempty"`
}

type HostInfo struct {
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	Hostname       string `json:"hostname,omitempty"`
	Kernel         string `json:"kernel,omitempty"`
	OpenWrtRelease string `json:"openwrt_release,omitempty"`
	ServiceManager string `json:"service_manager,omitempty"`
}

type Observed struct {
	Commands     map[string]CommandObservation `json:"commands"`
	Interfaces   []InterfaceObservation        `json:"interfaces"`
	Routes       []RouteObservation            `json:"routes"`
	DNSResolvers []DNSResolverObservation      `json:"dns_resolvers"`
	Services     []ServiceObservation          `json:"services"`
	Files        []FileObservation             `json:"files"`
}

type CommandObservation struct {
	Present bool   `json:"present"`
	Path    string `json:"path,omitempty"`
}

type InterfaceObservation struct {
	Name      string   `json:"name"`
	Flags     []string `json:"flags,omitempty"`
	Addresses []string `json:"addresses,omitempty"`
}

type RouteObservation struct {
	Destination string `json:"destination"`
	Gateway     string `json:"gateway,omitempty"`
	Interface   string `json:"interface,omitempty"`
	Source      string `json:"source"`
}

type DNSResolverObservation struct {
	Server    string `json:"server"`
	Scope     string `json:"scope,omitempty"`
	Interface string `json:"interface,omitempty"`
	Source    string `json:"source"`
}

type ServiceObservation struct {
	Name    string `json:"name"`
	Present bool   `json:"present"`
	Enabled *bool  `json:"enabled,omitempty"`
	Running *bool  `json:"running,omitempty"`
	Source  string `json:"source"`
}

type FileObservation struct {
	Path    string `json:"path"`
	Present bool   `json:"present"`
	Kind    string `json:"kind,omitempty"`
	Size    int64  `json:"size,omitempty"`
}

type Capabilities struct {
	ServiceManager Capability             `json:"service_manager"`
	Firewall       FirewallCapability     `json:"firewall"`
	DNS            DNSCapability          `json:"dns"`
	DHCP           DHCPCapability         `json:"dhcp"`
	ProxyRuntime   ProxyRuntimeCapability `json:"proxy_runtime"`
}

type Capability struct {
	Name       string   `json:"name,omitempty"`
	Available  bool     `json:"available"`
	Evidence   []string `json:"evidence,omitempty"`
	Confidence string   `json:"confidence,omitempty"`
}

type FirewallCapability struct {
	Backends   []Capability `json:"backends"`
	CanManage  bool         `json:"can_manage"`
	Evidence   []string     `json:"evidence,omitempty"`
	Confidence string       `json:"confidence,omitempty"`
}

type DNSCapability struct {
	Services   []ServiceObservation `json:"services"`
	CanObserve bool                 `json:"can_observe"`
	Evidence   []string             `json:"evidence,omitempty"`
	Confidence string               `json:"confidence,omitempty"`
}

type DHCPCapability struct {
	Services   []ServiceObservation `json:"services"`
	LANServers []DHCPServerEvidence `json:"lan_servers,omitempty"`
	Evidence   []string             `json:"evidence,omitempty"`
	Confidence string               `json:"confidence,omitempty"`
}

type DHCPServerEvidence struct {
	Interface string `json:"interface"`
	IPv4Mode  string `json:"ipv4_mode,omitempty"`
	IPv6Mode  string `json:"ipv6_mode,omitempty"`
	RAMode    string `json:"ra_mode,omitempty"`
	Source    string `json:"source"`
}

type ProxyRuntimeCapability struct {
	MihomoCorePresent bool     `json:"mihomo_core_present"`
	OpenClashPresent  bool     `json:"openclash_present"`
	CanRunBackground  bool     `json:"can_run_background"`
	Evidence          []string `json:"evidence,omitempty"`
	Confidence        string   `json:"confidence,omitempty"`
}

type LocalClashState struct {
	Present                    bool   `json:"present"`
	WorkDir                    string `json:"work_dir,omitempty"`
	SubscriptionPresent        bool   `json:"subscription_present"`
	SubscriptionSourcesPresent bool   `json:"subscription_sources_present"`
	GeneratedConfigPresent     bool   `json:"generated_config_present"`
	RuntimeDirPresent          bool   `json:"runtime_dir_present"`
	RulesPackCachePresent      bool   `json:"rules_pack_cache_present"`
	CorePresent                bool   `json:"core_present"`
}

type OpenClashState struct {
	Present          bool              `json:"present"`
	UCIConfigPresent bool              `json:"uci_config_present"`
	ConfigPath       string            `json:"active_config,omitempty"`
	Features         map[string]string `json:"features,omitempty"`
	SectionCounts    map[string]int    `json:"section_counts,omitempty"`
	ActiveProfile    *ProfileSummary   `json:"active_profile,omitempty"`
}

type ProfileSummary struct {
	Path                string              `json:"path"`
	ProxiesCount        int                 `json:"proxies_count"`
	ProxyGroupsCount    int                 `json:"proxy_groups_count"`
	ProxyProvidersCount int                 `json:"proxy_providers_count"`
	RuleProvidersCount  int                 `json:"rule_providers_count"`
	RulesCount          int                 `json:"rules_count"`
	Mode                string              `json:"mode,omitempty"`
	AllowLAN            *bool               `json:"allow_lan,omitempty"`
	ProxyGroupsSample   []ProxyGroupSummary `json:"proxy_groups_sample,omitempty"`
}

type ProxyGroupSummary struct {
	Name         string `json:"name"`
	Type         string `json:"type,omitempty"`
	ProxiesCount int    `json:"proxies_count"`
	UseCount     int    `json:"use_count"`
}

type SetupReadiness struct {
	Level       string   `json:"level"`
	Missing     []string `json:"missing"`
	NextActions []string `json:"next_actions"`
}

type ReferenceSnapshots struct {
	Root      string   `json:"root"`
	Snapshots []string `json:"snapshots"`
}

func Inspect(ctx context.Context, opts Options) (Result, error) {
	opts = normalizeOptions(opts)
	result := Result{
		Host: HostInfo{
			OS:   runtime.GOOS,
			Arch: runtime.GOARCH,
		},
		Observed: Observed{
			Commands: map[string]CommandObservation{},
		},
		SafetyBoundaries: []string{
			"Do not infer that this host is a router from OS, interface names, or address ranges alone.",
			"Do not change DNS, DHCP, firewall, or OpenClash state without an explicit apply plan and user confirmation.",
			"Do not expose subscription URLs, proxy server addresses, passwords, UUIDs, private keys, or WAN credentials.",
			"Do not verify or claim proxy egress region from node names or environment metadata.",
		},
		Redactions: []string{
			"subscription_urls",
			"proxy_credentials",
			"proxy_server_addresses",
			"wan_credentials",
			"private_keys",
			"tokens",
		},
	}
	result.Host.Hostname, _ = os.Hostname()
	result.Host.Kernel = strings.TrimSpace(commandOutput(ctx, opts, "uname", "-r"))
	result.Host.OpenWrtRelease = readOpenWrtRelease(opts)
	result.Host.ServiceManager = detectServiceManager(opts)
	result.Observed.Commands = observeCommands([]string{"launchctl", "systemctl", "procd", "ubus", "uci", "fw4", "nft", "iptables", "ip6tables", "pfctl", "dnsmasq", "odhcpd", "bootpd", "mDNSResponder", "ip", "netstat", "route", "scutil"})
	result.Observed.Interfaces = observeInterfaces()
	result.Observed.Routes = observeRoutes(ctx, opts)
	result.Observed.DNSResolvers = observeDNSResolvers(ctx, opts)
	result.Observed.Services = observeServices(ctx, opts)
	result.LocalClashState = inspectLocalClash(opts)
	result.OpenClashState = inspectOpenClash(opts)
	result.Observed.Files = observeFiles(opts, result.OpenClashState.ConfigPath)
	result.Capabilities = buildCapabilities(result, opts)
	result.SetupReadiness = buildSetupReadiness(result)
	result.ReferenceSnapshots = inspectReferenceSnapshots(opts.OpenClashReferenceRoot)
	return result, nil
}

func normalizeOptions(opts Options) Options {
	if strings.TrimSpace(opts.RootDir) == "" {
		opts.RootDir = "/"
	}
	if strings.TrimSpace(opts.WorkDir) == "" {
		if wd, err := os.Getwd(); err == nil {
			opts.WorkDir = wd
		}
	}
	if opts.CommandTimeout <= 0 {
		opts.CommandTimeout = 2 * time.Second
	}
	if opts.Paths.RuntimeRoot == "" {
		opts.Paths.RuntimeRoot = ".runtime"
	}
	if opts.Paths.SubscriptionPath == "" {
		opts.Paths.SubscriptionPath = "subscription.yaml"
	}
	if opts.Paths.SubscriptionConfig == "" {
		opts.Paths.SubscriptionConfig = "localclash-subscriptions.yaml"
	}
	if opts.Paths.GeneratedConfig == "" {
		opts.Paths.GeneratedConfig = "generated/mihomo.yaml"
	}
	if opts.Paths.MihomoRuntimeDir == "" {
		opts.Paths.MihomoRuntimeDir = filepath.Join(opts.Paths.RuntimeRoot, "mihomo")
	}
	if opts.Paths.RulesCacheDir == "" {
		opts.Paths.RulesCacheDir = filepath.Join(opts.Paths.RuntimeRoot, "rules", "packs")
	}
	if opts.Paths.CorePath == "" {
		opts.Paths.CorePath = "bin/mihomo"
	}
	return opts
}

func observeCommands(names []string) map[string]CommandObservation {
	out := map[string]CommandObservation{}
	for _, name := range names {
		path, err := exec.LookPath(name)
		out[name] = CommandObservation{Present: err == nil, Path: path}
	}
	return out
}

func observeInterfaces() []InterfaceObservation {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := make([]InterfaceObservation, 0, len(ifaces))
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		item := InterfaceObservation{Name: iface.Name, Flags: interfaceFlags(iface.Flags)}
		for _, addr := range addrs {
			item.Addresses = append(item.Addresses, addr.String())
		}
		sort.Strings(item.Addresses)
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func interfaceFlags(flags net.Flags) []string {
	var out []string
	if flags&net.FlagUp != 0 {
		out = append(out, "up")
	}
	if flags&net.FlagBroadcast != 0 {
		out = append(out, "broadcast")
	}
	if flags&net.FlagLoopback != 0 {
		out = append(out, "loopback")
	}
	if flags&net.FlagPointToPoint != 0 {
		out = append(out, "point_to_point")
	}
	if flags&net.FlagMulticast != 0 {
		out = append(out, "multicast")
	}
	return out
}

func observeRoutes(ctx context.Context, opts Options) []RouteObservation {
	if runtime.GOOS == "darwin" {
		return parseDarwinRoutes(commandOutput(ctx, opts, "netstat", "-rn", "-f", "inet"))
	}
	return parseLinuxRoutes(commandOutput(ctx, opts, "ip", "-4", "route", "show"))
}

func parseLinuxRoutes(text string) []RouteObservation {
	var out []RouteObservation
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		route := RouteObservation{Destination: fields[0], Source: "ip route"}
		for i := 1; i < len(fields)-1; i++ {
			switch fields[i] {
			case "via":
				route.Gateway = fields[i+1]
			case "dev":
				route.Interface = fields[i+1]
			}
		}
		out = append(out, route)
	}
	return out
}

func parseDarwinRoutes(text string) []RouteObservation {
	var out []RouteObservation
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 || fields[0] != "default" {
			continue
		}
		out = append(out, RouteObservation{
			Destination: "default",
			Gateway:     fields[1],
			Interface:   fields[len(fields)-1],
			Source:      "netstat",
		})
	}
	return out
}

func observeDNSResolvers(ctx context.Context, opts Options) []DNSResolverObservation {
	if runtime.GOOS == "darwin" {
		return parseDarwinDNS(commandOutput(ctx, opts, "scutil", "--dns"))
	}
	return parseResolvConf(rootPath(opts.RootDir, "/etc/resolv.conf"))
}

func parseDarwinDNS(text string) []DNSResolverObservation {
	var out []DNSResolverObservation
	var current DNSResolverObservation
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "resolver #") {
			current = DNSResolverObservation{Source: "scutil"}
			continue
		}
		if strings.HasPrefix(line, "nameserver[") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				item := current
				item.Server = strings.TrimSpace(parts[1])
				if item.Server != "" {
					out = append(out, item)
				}
			}
			continue
		}
		if strings.HasPrefix(line, "domain") || strings.HasPrefix(line, "search domain") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				current.Scope = strings.TrimSpace(parts[1])
			}
			continue
		}
		if strings.HasPrefix(line, "if_index") {
			parts := strings.Split(line, "(")
			if len(parts) > 1 {
				current.Interface = strings.TrimSuffix(parts[1], ")")
			}
		}
	}
	return uniqueDNSResolvers(out)
}

func parseResolvConf(path string) []DNSResolverObservation {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []DNSResolverObservation
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[0] == "nameserver" {
			out = append(out, DNSResolverObservation{Server: fields[1], Source: path})
		}
	}
	return uniqueDNSResolvers(out)
}

func uniqueDNSResolvers(values []DNSResolverObservation) []DNSResolverObservation {
	seen := map[string]bool{}
	var out []DNSResolverObservation
	for _, value := range values {
		key := value.Server + "|" + value.Scope + "|" + value.Interface
		if value.Server == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func observeServices(ctx context.Context, opts Options) []ServiceObservation {
	names := []string{"network", "firewall", "dnsmasq", "odhcpd", "openclash"}
	var out []ServiceObservation
	for _, name := range names {
		path := rootPath(opts.RootDir, "/etc/init.d/"+name)
		item := ServiceObservation{Name: name, Present: fileExists(path), Source: "/etc/init.d"}
		if item.Present && opts.RootDir == "/" {
			enabled := commandSuccess(ctx, opts, path, "enabled")
			running := commandSuccess(ctx, opts, path, "running")
			item.Enabled = &enabled
			item.Running = &running
		}
		out = append(out, item)
	}
	return out
}

func inspectLocalClash(opts Options) LocalClashState {
	state := LocalClashState{
		WorkDir:                    opts.WorkDir,
		SubscriptionPresent:        fileExists(workPath(opts.WorkDir, opts.Paths.SubscriptionPath)),
		SubscriptionSourcesPresent: fileExists(workPath(opts.WorkDir, opts.Paths.SubscriptionConfig)),
		GeneratedConfigPresent:     fileExists(workPath(opts.WorkDir, opts.Paths.GeneratedConfig)),
		RuntimeDirPresent:          dirExists(workPath(opts.WorkDir, opts.Paths.MihomoRuntimeDir)),
		RulesPackCachePresent:      dirExists(workPath(opts.WorkDir, opts.Paths.RulesCacheDir)),
		CorePresent:                fileExists(workPath(opts.WorkDir, opts.Paths.CorePath)),
	}
	state.Present = state.SubscriptionPresent || state.SubscriptionSourcesPresent || state.GeneratedConfigPresent || state.RuntimeDirPresent || state.CorePresent
	return state
}

func inspectOpenClash(opts Options) OpenClashState {
	uciPath := rootPath(opts.RootDir, "/etc/config/openclash")
	state := OpenClashState{
		Present:          pathExists(rootPath(opts.RootDir, "/etc/openclash")) || fileExists(uciPath),
		UCIConfigPresent: fileExists(uciPath),
		Features:         map[string]string{},
		SectionCounts:    map[string]int{},
	}
	if !state.UCIConfigPresent {
		return state
	}
	config, err := parseUCIFile(uciPath)
	if err != nil {
		return state
	}
	for _, section := range config.Sections {
		state.SectionCounts[section.Type]++
	}
	if openclash := firstSection(config, "openclash"); openclash != nil {
		for _, key := range safeOpenClashKeys() {
			if value, ok := openclash.Options[key]; ok {
				state.Features[key] = value
			}
		}
		state.ConfigPath = openclash.Options["config_path"]
	}
	if state.ConfigPath != "" {
		if summary, err := inspectProfile(rootPath(opts.RootDir, state.ConfigPath)); err == nil {
			state.ActiveProfile = &summary
		}
	}
	return state
}

func safeOpenClashKeys() []string {
	return []string{
		"enable", "auto_update", "enable_custom_dns", "enable_custom_clash_rules",
		"operation_mode", "redirect_dns", "dashboard_type", "config_path",
		"enable_geoip_dat", "enable_meta_sniffer", "enable_tcp_concurrent",
		"en_mode", "proxy_mode", "enable_redirect_dns", "router_self_proxy",
		"other_rule_auto_update", "geo_auto_update", "geoip_auto_update",
		"geosite_auto_update", "chnr_auto_update", "auto_restart",
		"dashboard_forward_ssl", "ipv6_enable", "ipv6_dns", "ipv6_mode",
		"enable_respect_rules", "enable_unified_delay",
		"enable_meta_sniffer_pure_ip", "geoasn_auto_update", "smart_enable",
		"lan_ac_mode", "smart_strategy", "smart_prefer_asn", "smart_enable_lgbm",
	}
}

func inspectProfile(path string) (ProfileSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ProfileSummary{}, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return ProfileSummary{}, err
	}
	summary := ProfileSummary{
		Path:                path,
		ProxiesCount:        len(anySlice(doc["proxies"])),
		ProxyGroupsCount:    len(anySlice(doc["proxy-groups"])),
		ProxyProvidersCount: len(anyMap(doc["proxy-providers"])),
		RuleProvidersCount:  len(anyMap(doc["rule-providers"])),
		RulesCount:          len(anySlice(doc["rules"])),
		Mode:                stringValue(doc["mode"]),
	}
	if allowLAN, ok := boolValue(doc["allow-lan"]); ok {
		summary.AllowLAN = &allowLAN
	}
	for _, raw := range anySlice(doc["proxy-groups"]) {
		group, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		summary.ProxyGroupsSample = append(summary.ProxyGroupsSample, ProxyGroupSummary{
			Name:         stringValue(group["name"]),
			Type:         stringValue(group["type"]),
			ProxiesCount: len(anySlice(group["proxies"])),
			UseCount:     len(anySlice(group["use"])),
		})
		if len(summary.ProxyGroupsSample) >= 20 {
			break
		}
	}
	return summary, nil
}

func observeFiles(opts Options, openClashConfigPath string) []FileObservation {
	paths := []string{
		workPath(opts.WorkDir, opts.Paths.SubscriptionPath),
		workPath(opts.WorkDir, opts.Paths.SubscriptionConfig),
		workPath(opts.WorkDir, opts.Paths.GeneratedConfig),
		workPath(opts.WorkDir, opts.Paths.CorePath),
		workPath(opts.WorkDir, opts.Paths.MihomoRuntimeDir),
		workPath(opts.WorkDir, opts.Paths.RulesCacheDir),
		rootPath(opts.RootDir, "/etc/config/openclash"),
		rootPath(opts.RootDir, "/etc/openclash"),
		rootPath(opts.RootDir, "/etc/openclash/core/clash_meta"),
	}
	if openClashConfigPath != "" {
		paths = append(paths, rootPath(opts.RootDir, openClashConfigPath))
	}
	seen := map[string]bool{}
	var out []FileObservation
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, fileObservation(path))
	}
	return out
}

func fileObservation(path string) FileObservation {
	item := FileObservation{Path: path}
	info, err := os.Stat(path)
	if err != nil {
		return item
	}
	item.Present = true
	item.Size = info.Size()
	if info.IsDir() {
		item.Kind = "directory"
		item.Size = 0
	} else {
		item.Kind = "file"
	}
	return item
}

func buildCapabilities(result Result, opts Options) Capabilities {
	caps := Capabilities{
		ServiceManager: Capability{
			Name:       result.Host.ServiceManager,
			Available:  result.Host.ServiceManager != "",
			Confidence: "high",
		},
	}
	if caps.ServiceManager.Available {
		caps.ServiceManager.Evidence = []string{"service manager command or OS service model detected"}
	}
	for _, name := range []string{"fw4", "nft", "iptables", "ip6tables", "pfctl"} {
		if command, ok := result.Observed.Commands[name]; ok && command.Present {
			caps.Firewall.Backends = append(caps.Firewall.Backends, Capability{Name: name, Available: true, Evidence: []string{"command present"}, Confidence: "medium"})
		}
	}
	caps.Firewall.CanManage = len(caps.Firewall.Backends) > 0
	if caps.Firewall.CanManage {
		caps.Firewall.Confidence = "medium"
		caps.Firewall.Evidence = []string{"firewall command present"}
	}
	for _, service := range result.Observed.Services {
		if service.Name == "dnsmasq" || service.Name == "odhcpd" || service.Name == "openclash" {
			if service.Present {
				if service.Name == "dnsmasq" {
					caps.DNS.Services = append(caps.DNS.Services, service)
				}
				if service.Name == "dnsmasq" || service.Name == "odhcpd" {
					caps.DHCP.Services = append(caps.DHCP.Services, service)
				}
			}
		}
	}
	if command, ok := result.Observed.Commands["mDNSResponder"]; ok && command.Present {
		caps.DNS.Services = append(caps.DNS.Services, ServiceObservation{Name: "mDNSResponder", Present: true, Source: "command"})
	}
	if command, ok := result.Observed.Commands["bootpd"]; ok && command.Present {
		caps.DHCP.Services = append(caps.DHCP.Services, ServiceObservation{Name: "bootpd", Present: true, Source: "command"})
	}
	caps.DNS.CanObserve = len(caps.DNS.Services) > 0
	if caps.DNS.CanObserve {
		caps.DNS.Confidence = "medium"
		caps.DNS.Evidence = []string{"DNS-related service present"}
	}
	caps.DHCP.LANServers = inspectDHCPServers(opts)
	if len(caps.DHCP.LANServers) > 0 {
		caps.DHCP.Confidence = "high"
		caps.DHCP.Evidence = []string{"DHCP LAN server config present"}
	} else if len(caps.DHCP.Services) > 0 {
		caps.DHCP.Confidence = "low"
		caps.DHCP.Evidence = []string{"DHCP-capable service present"}
	}
	caps.ProxyRuntime = ProxyRuntimeCapability{
		MihomoCorePresent: result.LocalClashState.CorePresent || filePresent(result.Observed.Files, rootPath(opts.RootDir, "/etc/openclash/core/clash_meta")),
		OpenClashPresent:  result.OpenClashState.Present,
		CanRunBackground:  result.Host.ServiceManager != "",
		Confidence:        "medium",
	}
	if caps.ProxyRuntime.MihomoCorePresent {
		caps.ProxyRuntime.Evidence = append(caps.ProxyRuntime.Evidence, "mihomo/openclash core file present")
	}
	if caps.ProxyRuntime.OpenClashPresent {
		caps.ProxyRuntime.Evidence = append(caps.ProxyRuntime.Evidence, "OpenClash files present")
	}
	return caps
}

func buildSetupReadiness(result Result) SetupReadiness {
	if result.LocalClashState.GeneratedConfigPresent && result.LocalClashState.SubscriptionPresent && result.LocalClashState.CorePresent {
		return SetupReadiness{Level: "configured", NextActions: []string{"inspect config", "render plan", "test config", "run runtime with confirmation"}}
	}
	if result.OpenClashState.Present && !result.LocalClashState.Present {
		return SetupReadiness{
			Level:       "openclash_configured_localclash_absent",
			Missing:     []string{"localclash_config", "localclash_runtime"},
			NextActions: []string{"treat OpenClash as existing deployment reference", "design import or migration path before applying changes"},
		}
	}
	var missing []string
	if !result.LocalClashState.SubscriptionPresent {
		missing = append(missing, "subscription")
	}
	if !result.LocalClashState.CorePresent {
		missing = append(missing, "mihomo_core")
	}
	if !result.LocalClashState.GeneratedConfigPresent {
		missing = append(missing, "generated_config")
	}
	level := "fresh"
	if result.LocalClashState.Present {
		level = "partial"
	}
	return SetupReadiness{Level: level, Missing: missing, NextActions: []string{"configure subscription sources", "refresh subscriptions", "download or verify mihomo core", "render generated config"}}
}

func inspectReferenceSnapshots(root string) ReferenceSnapshots {
	if strings.TrimSpace(root) == "" || !dirExists(root) {
		return ReferenceSnapshots{}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return ReferenceSnapshots{}
	}
	var snapshots []string
	for _, entry := range entries {
		if entry.IsDir() {
			snapshots = append(snapshots, filepath.Join(root, entry.Name()))
		}
	}
	sort.Strings(snapshots)
	return ReferenceSnapshots{Root: root, Snapshots: snapshots}
}

type uciConfig struct {
	Sections []uciSection
}

type uciSection struct {
	Type    string
	Name    string
	Options map[string]string
}

func parseUCIFile(path string) (uciConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return uciConfig{}, err
	}
	var config uciConfig
	var current *uciSection
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := shellLikeFields(line)
		if len(fields) >= 2 && fields[0] == "config" {
			config.Sections = append(config.Sections, uciSection{Type: fields[1], Options: map[string]string{}})
			current = &config.Sections[len(config.Sections)-1]
			if len(fields) >= 3 {
				current.Name = fields[2]
			}
			continue
		}
		if current == nil || len(fields) < 3 {
			continue
		}
		if fields[0] == "option" {
			current.Options[fields[1]] = fields[2]
		}
	}
	return config, scanner.Err()
}

func shellLikeFields(line string) []string {
	var fields []string
	var current strings.Builder
	var quote rune
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t':
			if current.Len() > 0 {
				fields = append(fields, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		fields = append(fields, current.String())
	}
	return fields
}

func firstSection(config uciConfig, sectionType string) *uciSection {
	for i := range config.Sections {
		if config.Sections[i].Type == sectionType {
			return &config.Sections[i]
		}
	}
	return nil
}

func inspectDHCPServers(opts Options) []DHCPServerEvidence {
	config, err := parseUCIFile(rootPath(opts.RootDir, "/etc/config/dhcp"))
	if err != nil {
		return nil
	}
	var out []DHCPServerEvidence
	for _, section := range config.Sections {
		if section.Type != "dhcp" {
			continue
		}
		if section.Options["ignore"] == "1" {
			continue
		}
		ipv4 := section.Options["dhcpv4"]
		ipv6 := section.Options["dhcpv6"]
		ra := section.Options["ra"]
		if ipv4 == "" && ipv6 == "" && ra == "" {
			continue
		}
		out = append(out, DHCPServerEvidence{
			Interface: section.Options["interface"],
			IPv4Mode:  ipv4,
			IPv6Mode:  ipv6,
			RAMode:    ra,
			Source:    "/etc/config/dhcp",
		})
	}
	return out
}

func readOpenWrtRelease(opts Options) string {
	data, err := os.ReadFile(rootPath(opts.RootDir, "/etc/openwrt_release"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "DISTRIB_DESCRIPTION=") {
			return strings.Trim(strings.TrimPrefix(line, "DISTRIB_DESCRIPTION="), "'\"")
		}
	}
	return strings.TrimSpace(string(data))
}

func detectServiceManager(opts Options) string {
	switch {
	case fileExists(rootPath(opts.RootDir, "/sbin/procd")) || runtime.GOOS == "linux" && fileExists(rootPath(opts.RootDir, "/etc/openwrt_release")):
		return "procd"
	case commandExists("systemctl"):
		return "systemd"
	case runtime.GOOS == "darwin":
		return "launchd"
	default:
		return ""
	}
}

func commandOutput(ctx context.Context, opts Options, name string, args ...string) string {
	if !commandExists(name) {
		return ""
	}
	runCtx, cancel := context.WithTimeout(ctx, opts.CommandTimeout)
	defer cancel()
	output, err := exec.CommandContext(runCtx, name, args...).Output()
	if err != nil {
		return ""
	}
	return string(output)
}

func commandSuccess(ctx context.Context, opts Options, name string, args ...string) bool {
	runCtx, cancel := context.WithTimeout(ctx, opts.CommandTimeout)
	defer cancel()
	return exec.CommandContext(runCtx, name, args...).Run() == nil
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func rootPath(root, path string) string {
	if strings.TrimSpace(path) == "" {
		return path
	}
	if strings.TrimSpace(root) == "" || root == "/" {
		return path
	}
	return filepath.Join(root, strings.TrimPrefix(path, "/"))
}

func workPath(workDir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(workDir, path)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func filePresent(files []FileObservation, path string) bool {
	for _, file := range files {
		if file.Path == path && file.Present {
			return true
		}
	}
	return false
}

func anySlice(value any) []any {
	if values, ok := value.([]any); ok {
		return values
	}
	return nil
}

func anyMap(value any) map[string]any {
	if values, ok := value.(map[string]any); ok {
		return values
	}
	return nil
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func boolValue(value any) (bool, bool) {
	if v, ok := value.(bool); ok {
		return v, true
	}
	return false, false
}
