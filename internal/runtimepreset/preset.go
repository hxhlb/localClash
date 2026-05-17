package runtimepreset

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	DefaultPath = "mihomo-preset.yaml"
	Normal      = "normal"
	Router      = "router"
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
	Version int               `yaml:"version" json:"version"`
	Active  string            `yaml:"active" json:"active"`
	Presets map[string]Preset `yaml:"presets" json:"presets"`
}

type Preset struct {
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
	Mihomo      map[string]any `yaml:"mihomo" json:"mihomo"`
	Deploy      map[string]any `yaml:"deploy,omitempty" json:"deploy,omitempty"`
}

type Status struct {
	Path             string         `json:"path"`
	Exists           bool           `json:"exists"`
	Active           string         `json:"active"`
	AvailablePresets []string       `json:"available_presets"`
	Summary          map[string]any `json:"summary"`
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
	preset, ok := file.Presets[file.Active]
	if !ok {
		return Status{}, fmt.Errorf("active runtime preset %q is not defined", file.Active)
	}
	names := make([]string, 0, len(file.Presets))
	for name := range file.Presets {
		names = append(names, name)
	}
	sort.Strings(names)
	return Status{
		Path:             path,
		Exists:           exists,
		Active:           file.Active,
		AvailablePresets: names,
		Summary:          summarize(preset),
	}, nil
}

func Configure(path, active string) (Status, error) {
	path = normalizePath(path)
	active = strings.TrimSpace(active)
	if active != Normal && active != Router {
		return Status{}, fmt.Errorf("runtime preset must be %q or %q, got %q", Normal, Router, active)
	}
	file, _, err := Load(path)
	if err != nil {
		return Status{}, err
	}
	if _, ok := file.Presets[active]; !ok {
		return Status{}, fmt.Errorf("runtime preset %q is not defined in %s", active, path)
	}
	file.Active = active
	if err := write(path, file); err != nil {
		return Status{}, err
	}
	return StatusFor(path)
}

func ActivePreset(path string) (string, Preset, bool, error) {
	file, exists, err := Load(path)
	if err != nil {
		return "", Preset{}, exists, err
	}
	preset, ok := file.Presets[file.Active]
	if !ok {
		return "", Preset{}, exists, fmt.Errorf("active runtime preset %q is not defined", file.Active)
	}
	return file.Active, preset, exists, nil
}

func ApplyToConfig(config map[string]any, preset Preset) {
	mergeMihomo(config, preset.Mihomo)
}

func DefaultFile() File {
	return File{
		Version: 1,
		Active:  Normal,
		Presets: map[string]Preset{
			Normal: {
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
			Router: {
				Description: "Router transparent proxy based on Ronnie's OpenClash redir-host-mix defaults.",
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
					"external-ui":         "/usr/share/openclash/ui",
					"ipv6":                true,
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
					"profile": map[string]any{"store-selected": true},
				},
				Deploy: map[string]any{
					"lan-interface":      "br-lan",
					"wan-interface":      "pppoe-wan",
					"router-self-proxy":  true,
					"dnsmasq-redirect":   true,
					"ipv6-mode":          3,
					"openclash-conflict": "block_apply",
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
	return file
}

func validate(file File) error {
	if file.Version != 1 {
		return fmt.Errorf("unsupported runtime preset version %d", file.Version)
	}
	if file.Active == "" {
		return errors.New("runtime preset active is required")
	}
	if len(file.Presets) == 0 {
		return errors.New("runtime preset file has no presets")
	}
	for name, preset := range file.Presets {
		if name != Normal && name != Router {
			return fmt.Errorf("unsupported runtime preset %q; only %q and %q are supported in v0", name, Normal, Router)
		}
		if len(preset.Mihomo) == 0 {
			return fmt.Errorf("runtime preset %q has no mihomo config", name)
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

func summarize(preset Preset) map[string]any {
	m := preset.Mihomo
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
