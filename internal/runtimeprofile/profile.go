package runtimeprofile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	DefaultPath = "localclash-runtime.yaml"
	ModeNormal  = "normal"
	ModeRouter  = "router"
	CoreMeta    = "meta"
	CoreSmart   = "smart"
)

var (
	MetaCorePath  = filepath.Join("bin", runtime.GOOS+"-"+runtime.GOARCH, "mihomo-meta")
	SmartCorePath = filepath.Join("bin", "linux-"+runtime.GOARCH, "mihomo-smart")
)

var dynamicConfigKeys = map[string]bool{
	"proxies":         true,
	"proxy-groups":    true,
	"proxy-providers": true,
	"rule-providers":  true,
	"rules":           true,
	"x-localclash":    true,
}

type File struct {
	Version  int                `yaml:"version" json:"version"`
	Mode     string             `yaml:"mode" json:"mode"`
	Core     string             `yaml:"core" json:"core"`
	Profiles map[string]Profile `yaml:"profiles" json:"profiles"`
	Cores    map[string]Core    `yaml:"cores" json:"cores"`
	Smart    SmartOptions       `yaml:"smart,omitempty" json:"smart,omitempty"`
}

type Profile struct {
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
	Mihomo      map[string]any `yaml:"mihomo" json:"mihomo"`
	Deploy      map[string]any `yaml:"deploy,omitempty" json:"deploy,omitempty"`
}

type Core struct {
	Path string `yaml:"path" json:"path"`
}

type SmartOptions struct {
	UseLightGBM    bool    `yaml:"uselightgbm" json:"uselightgbm"`
	PreferASN      bool    `yaml:"prefer-asn" json:"prefer_asn"`
	CollectData    bool    `yaml:"collectdata,omitempty" json:"collectdata,omitempty"`
	SampleRate     float64 `yaml:"sample-rate,omitempty" json:"sample_rate,omitempty"`
	PolicyPriority string  `yaml:"policy-priority,omitempty" json:"policy_priority,omitempty"`
}

type Status struct {
	Path                   string         `json:"path"`
	Exists                 bool           `json:"exists"`
	Mode                   string         `json:"mode"`
	Core                   string         `json:"core"`
	CorePath               string         `json:"core_path"`
	AvailableModes         []string       `json:"available_modes"`
	AvailableCores         []string       `json:"available_cores"`
	Summary                map[string]any `json:"summary"`
	SmartGroupDefault      SmartOptions   `json:"smart_group_default"`
	RouterTakeoverRequired bool           `json:"router_takeover_required,omitempty"`
	NextActions            []string       `json:"next_actions,omitempty"`
}

func Load(path string) (File, bool, error) {
	path = normalizePath(path)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultFile(), false, nil
	}
	if err != nil {
		return File{}, false, err
	}
	var file File
	if err := yaml.Unmarshal(data, &file); err != nil {
		return File{}, true, err
	}
	file = normalizeFile(file)
	if err := validate(file); err != nil {
		return File{}, true, err
	}
	return file, true, nil
}

func StatusFor(path string) (Status, error) {
	path = normalizePath(path)
	file, exists, err := Load(path)
	if err != nil {
		return Status{}, err
	}
	profile, ok := file.Profiles[file.Mode]
	if !ok {
		return Status{}, fmt.Errorf("runtime mode %q is not defined", file.Mode)
	}
	modes := make([]string, 0, len(file.Profiles))
	for name := range file.Profiles {
		modes = append(modes, name)
	}
	sort.Strings(modes)
	cores := make([]string, 0, len(file.Cores))
	for name := range file.Cores {
		cores = append(cores, name)
	}
	sort.Strings(cores)
	status := Status{
		Path:              path,
		Exists:            exists,
		Mode:              file.Mode,
		Core:              file.Core,
		CorePath:          ActiveCorePathFromFile(file),
		AvailableModes:    modes,
		AvailableCores:    cores,
		Summary:           summarize(profile),
		SmartGroupDefault: file.Smart,
	}
	if file.Mode == ModeRouter {
		status.RouterTakeoverRequired = true
		status.NextActions = []string{
			"call config_render when generated/mihomo.yaml is missing or stale",
			"call run_runtime after user confirmation to start Mihomo",
			"call router_takeover_apply after user confirmation to capture router traffic",
		}
	} else {
		status.NextActions = []string{
			"call config_render when generated/mihomo.yaml is missing or stale",
			"call run_runtime after user confirmation to start Mihomo",
		}
	}
	return status, nil
}

func Configure(path, mode, core string) (Status, error) {
	path = normalizePath(path)
	mode = strings.TrimSpace(mode)
	core = strings.TrimSpace(core)
	file, _, err := Load(path)
	if err != nil {
		return Status{}, err
	}
	if mode != "" {
		if _, ok := file.Profiles[mode]; !ok {
			return Status{}, fmt.Errorf("runtime mode %q is not defined in %s", mode, path)
		}
		file.Mode = mode
	}
	if core != "" {
		if _, ok := file.Cores[core]; !ok {
			return Status{}, fmt.Errorf("runtime core %q is not defined in %s", core, path)
		}
		file.Core = core
	}
	if mode == "" && core == "" {
		return Status{}, errors.New("runtime profile configure requires mode or core")
	}
	if err := write(path, file); err != nil {
		return Status{}, err
	}
	return StatusFor(path)
}

func ActiveProfile(path string) (File, Profile, bool, error) {
	file, exists, err := Load(path)
	if err != nil {
		return File{}, Profile{}, exists, err
	}
	profile, ok := file.Profiles[file.Mode]
	if !ok {
		return File{}, Profile{}, exists, fmt.Errorf("runtime mode %q is not defined", file.Mode)
	}
	return file, profile, exists, nil
}

func ActiveCorePath(path string) (string, error) {
	file, _, err := Load(path)
	if err != nil {
		return "", err
	}
	return ActiveCorePathFromFile(file), nil
}

func ActiveCorePathFromFile(file File) string {
	if core, ok := file.Cores[file.Core]; ok && strings.TrimSpace(core.Path) != "" {
		return core.Path
	}
	if file.Core == CoreSmart {
		return SmartCorePath
	}
	return MetaCorePath
}

func ApplyToConfig(config map[string]any, profile Profile) {
	mergeMihomo(config, profile.Mihomo)
}

func DefaultFile() File {
	return File{
		Version: 1,
		Mode:    ModeNormal,
		Core:    CoreMeta,
		Cores: map[string]Core{
			CoreMeta:  {Path: MetaCorePath},
			CoreSmart: {Path: SmartCorePath},
		},
		Smart: SmartOptions{UseLightGBM: true, PreferASN: true},
		Profiles: map[string]Profile{
			ModeNormal: {
				Description: "Local standalone Mihomo proxy, matching localClash's original generated config.",
				Mihomo: map[string]any{
					"mixed-port":          7890,
					"allow-lan":           false,
					"mode":                "rule",
					"log-level":           "info",
					"external-controller": "127.0.0.1:9090",
					"external-ui":         "ui/zashboard",
					"unified-delay":       true,
				},
			},
			ModeRouter: {
				Description: "Router transparent proxy based on Ronnie's redir-host-mix defaults.",
				Mihomo: map[string]any{
					"mixed-port":          7893,
					"redir-port":          7892,
					"tproxy-port":         7895,
					"port":                7890,
					"socks-port":          7891,
					"allow-lan":           true,
					"bind-address":        "*",
					"mode":                "rule",
					"log-level":           "warning",
					"external-controller": "0.0.0.0:9090",
					"external-ui":         "ui/zashboard",
					"ipv6":                true,
					"interface-name":      "pppoe-wan",
					"geodata-mode":        true,
					"geodata-loader":      "standard",
					"tcp-concurrent":      true,
					"unified-delay":       true,
					"find-process-mode":   "off",
					"keep-alive-interval": 15,
					"keep-alive-idle":     600,
					"geox-url": map[string]any{
						"mmdb":    "https://testingcf.jsdelivr.net/gh/alecthw/mmdb_china_ip_list@release/Country.mmdb",
						"geoip":   "https://testingcf.jsdelivr.net/gh/Loyalsoldier/v2ray-rules-dat@release/geoip.dat",
						"geosite": "https://testingcf.jsdelivr.net/gh/Loyalsoldier/v2ray-rules-dat@release/geosite.dat",
						"asn":     "https://testingcf.jsdelivr.net/gh/xishang0128/geoip@release/GeoLite2-ASN.mmdb",
					},
					"dns": map[string]any{
						"enable":                  true,
						"ipv6":                    true,
						"enhanced-mode":           "redir-host",
						"listen":                  "0.0.0.0:7874",
						"respect-rules":           true,
						"fake-ip-filter-mode":     "blacklist",
						"nameserver":              []string{"tcp://127.0.0.1:5335"},
						"proxy-server-nameserver": []string{"tcp://127.0.0.1:5335"},
						"direct-nameserver":       []string{"tcp://127.0.0.1:5335"},
						"default-nameserver":      []string{"tcp://127.0.0.1:5335"},
					},
					"tun": map[string]any{
						"enable":                   true,
						"stack":                    "mixed",
						"device":                   "utun",
						"dns-hijack":               []string{"127.0.0.1:53"},
						"endpoint-independent-nat": true,
						"auto-route":               false,
						"auto-detect-interface":    false,
						"auto-redirect":            false,
						"strict-route":             false,
						"disable-icmp-forwarding":  false,
					},
					"sniffer": map[string]any{
						"enable":               true,
						"override-destination": true,
						"sniff": map[string]any{
							"QUIC": map[string]any{
								"ports": []int{443},
							},
							"TLS": map[string]any{
								"ports": []int{443, 8443},
							},
							"HTTP": map[string]any{
								"ports":                []any{80, "8080-8880"},
								"override-destination": true,
							},
						},
						"force-domain":      []string{"+.netflix.com", "+.nflxvideo.net", "+.amazonaws.com", "+.media.dssott.com"},
						"skip-domain":       []string{"Mijia Cloud", "dlg.io.mi.com", "+.oray.com", "+.sunlogin.net", "+.push.apple.com"},
						"force-dns-mapping": true,
						"parse-pure-ip":     true,
					},
					"profile": map[string]any{"store-selected": true},
					"ntp": map[string]any{
						"enable":          true,
						"server":          "time.apple.com",
						"port":            123,
						"interval":        30,
						"write-to-system": true,
					},
				},
				Deploy: map[string]any{
					"lan-interface":     "br-lan",
					"wan-interface":     "pppoe-wan",
					"router-self-proxy": true,
					"dnsmasq-redirect":  true,
					"ipv6-mode":         3,
				},
			},
		},
	}
}

func normalizePath(path string) string {
	if strings.TrimSpace(path) == "" {
		return DefaultPath
	}
	return path
}

func normalizeFile(file File) File {
	if file.Version == 0 {
		file.Version = 1
	}
	if file.Mode == "" {
		file.Mode = ModeNormal
	}
	if file.Core == "" {
		file.Core = CoreMeta
	}
	if file.Cores == nil {
		file.Cores = map[string]Core{}
	}
	if _, ok := file.Cores[CoreMeta]; !ok {
		file.Cores[CoreMeta] = Core{Path: MetaCorePath}
	}
	if _, ok := file.Cores[CoreSmart]; !ok {
		file.Cores[CoreSmart] = Core{Path: SmartCorePath}
	}
	if !file.Smart.UseLightGBM && !file.Smart.PreferASN && !file.Smart.CollectData && file.Smart.SampleRate == 0 && file.Smart.PolicyPriority == "" {
		file.Smart = SmartOptions{UseLightGBM: true, PreferASN: true}
	}
	defaults := DefaultFile()
	if file.Profiles == nil {
		file.Profiles = cloneProfiles(defaults.Profiles)
	} else {
		for name, defaultProfile := range defaults.Profiles {
			profile, ok := file.Profiles[name]
			if !ok {
				file.Profiles[name] = cloneProfile(defaultProfile)
				continue
			}
			if strings.TrimSpace(profile.Description) == "" {
				profile.Description = defaultProfile.Description
			}
			profile.Mihomo = mergeMissingMap(profile.Mihomo, defaultProfile.Mihomo)
			profile.Deploy = mergeMissingMap(profile.Deploy, defaultProfile.Deploy)
			file.Profiles[name] = profile
		}
	}
	return file
}

func validate(file File) error {
	if file.Version != 1 {
		return fmt.Errorf("unsupported runtime profile version %d", file.Version)
	}
	if file.Mode == "" {
		return errors.New("runtime profile mode is required")
	}
	if file.Core == "" {
		return errors.New("runtime profile core is required")
	}
	if len(file.Profiles) == 0 {
		return errors.New("runtime profile file has no profiles")
	}
	if _, ok := file.Profiles[file.Mode]; !ok {
		return fmt.Errorf("runtime mode %q is not defined", file.Mode)
	}
	if _, ok := file.Cores[file.Core]; !ok {
		return fmt.Errorf("runtime core %q is not defined", file.Core)
	}
	for name, profile := range file.Profiles {
		if name != ModeNormal && name != ModeRouter {
			return fmt.Errorf("unsupported runtime mode %q; only %q and %q are supported in v0", name, ModeNormal, ModeRouter)
		}
		if len(profile.Mihomo) == 0 {
			return fmt.Errorf("runtime profile %q has no mihomo config", name)
		}
	}
	return nil
}

func write(path string, file File) error {
	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	data, err := yaml.Marshal(file)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func mergeMihomo(dst map[string]any, src map[string]any) {
	for key, value := range src {
		if dynamicConfigKeys[key] {
			continue
		}
		srcMap, srcIsMap := value.(map[string]any)
		dstMap, dstIsMap := dst[key].(map[string]any)
		if srcIsMap && dstIsMap {
			mergeMihomo(dstMap, srcMap)
			continue
		}
		dst[key] = cloneValue(value)
	}
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, item := range typed {
			out[key] = cloneValue(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneValue(item)
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}

func cloneProfiles(profiles map[string]Profile) map[string]Profile {
	out := map[string]Profile{}
	for name, profile := range profiles {
		out[name] = cloneProfile(profile)
	}
	return out
}

func cloneProfile(profile Profile) Profile {
	return Profile{
		Description: profile.Description,
		Mihomo:      mergeMissingMap(nil, profile.Mihomo),
		Deploy:      mergeMissingMap(nil, profile.Deploy),
	}
}

func mergeMissingMap(dst, defaults map[string]any) map[string]any {
	if len(defaults) == 0 && dst == nil {
		return nil
	}
	if dst == nil {
		dst = map[string]any{}
	}
	for key, value := range defaults {
		if existing, ok := dst[key]; ok {
			existingMap, existingIsMap := existing.(map[string]any)
			defaultMap, defaultIsMap := value.(map[string]any)
			if existingIsMap && defaultIsMap {
				dst[key] = mergeMissingMap(existingMap, defaultMap)
			}
			continue
		}
		dst[key] = cloneValue(value)
	}
	return dst
}

func summarize(profile Profile) map[string]any {
	m := profile.Mihomo
	summary := map[string]any{}
	for _, key := range []string{"mixed-port", "redir-port", "tproxy-port", "port", "socks-port", "allow-lan", "bind-address", "external-controller", "ipv6"} {
		if value, ok := m[key]; ok {
			summary[key] = value
		}
	}
	if dns, ok := m["dns"].(map[string]any); ok {
		summary["dns"] = map[string]any{
			"enable":        dns["enable"],
			"listen":        dns["listen"],
			"enhanced-mode": dns["enhanced-mode"],
			"respect-rules": dns["respect-rules"],
		}
	}
	if tun, ok := m["tun"].(map[string]any); ok {
		summary["tun"] = map[string]any{
			"enable":        tun["enable"],
			"stack":         tun["stack"],
			"device":        tun["device"],
			"auto-route":    tun["auto-route"],
			"auto-redirect": tun["auto-redirect"],
		}
	}
	return summary
}
