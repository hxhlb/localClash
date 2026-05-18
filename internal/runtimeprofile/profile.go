package runtimeprofile

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed profiles/*.default.yaml
var defaultProfileFS embed.FS

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
	Path        string         `yaml:"path,omitempty" json:"path,omitempty"`
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
	Mihomo      map[string]any `yaml:"mihomo" json:"mihomo"`
	Deploy      map[string]any `yaml:"deploy,omitempty" json:"deploy,omitempty"`
}

type diskFile struct {
	Version  int                   `yaml:"version" json:"version"`
	Mode     string                `yaml:"mode" json:"mode"`
	Core     string                `yaml:"core" json:"core"`
	Profiles map[string]profileRef `yaml:"profiles" json:"profiles"`
	Cores    map[string]Core       `yaml:"cores" json:"cores"`
	Smart    SmartOptions          `yaml:"smart,omitempty" json:"smart,omitempty"`
}

type profileRef struct {
	Path string `yaml:"path" json:"path"`
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
	if err := ensureProfileFiles(path); err != nil {
		return File{}, false, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		file := DefaultFile()
		if err := write(path, file); err != nil {
			return File{}, false, err
		}
		return file, true, nil
	}
	if err != nil {
		return File{}, false, err
	}
	file, err := parseFile(path, data)
	if err != nil {
		return File{}, true, err
	}
	return file, true, nil
}

func parseFile(path string, data []byte) (File, error) {
	var disk diskFile
	if err := yaml.Unmarshal(data, &disk); err != nil {
		return File{}, err
	}
	file, err := materializeDiskFile(path, normalizeDiskFile(disk))
	if err != nil {
		return File{}, err
	}
	if err := validate(file); err != nil {
		return File{}, err
	}
	return file, nil
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
	file := File{
		Version: 1,
		Mode:    ModeNormal,
		Core:    CoreMeta,
		Cores: map[string]Core{
			CoreMeta:  {Path: MetaCorePath},
			CoreSmart: {Path: SmartCorePath},
		},
		Smart:    SmartOptions{UseLightGBM: true, PreferASN: true},
		Profiles: map[string]Profile{},
	}
	for _, mode := range []string{ModeNormal, ModeRouter} {
		profile, err := readEmbeddedDefaultProfile(mode)
		if err != nil {
			panic(fmt.Sprintf("invalid embedded %s default profile: %v", mode, err))
		}
		profile.Path = userProfileRelPath(mode)
		file.Profiles[mode] = profile
	}
	return file
}

func normalizePath(path string) string {
	if strings.TrimSpace(path) == "" {
		return DefaultPath
	}
	return path
}

func normalizeDiskFile(file diskFile) diskFile {
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
	for name, core := range file.Cores {
		core.Path = expandRuntimePlaceholders(core.Path)
		file.Cores[name] = core
	}
	if len(file.Profiles) == 0 {
		file.Profiles = map[string]profileRef{}
		for _, mode := range []string{ModeNormal, ModeRouter} {
			file.Profiles[mode] = profileRef{Path: userProfileRelPath(mode)}
		}
	}
	return file
}

func materializeDiskFile(path string, disk diskFile) (File, error) {
	file := File{
		Version:  disk.Version,
		Mode:     disk.Mode,
		Core:     disk.Core,
		Cores:    disk.Cores,
		Smart:    disk.Smart,
		Profiles: map[string]Profile{},
	}
	baseDir := filepath.Dir(path)
	for mode, ref := range disk.Profiles {
		profilePath := strings.TrimSpace(ref.Path)
		if profilePath == "" {
			return File{}, fmt.Errorf("runtime profile %q has no path", mode)
		}
		profile, err := readProfileFile(resolveRuntimePath(baseDir, profilePath))
		if err != nil {
			return File{}, fmt.Errorf("load runtime profile %q from %s: %w", mode, profilePath, err)
		}
		profile.Path = profilePath
		file.Profiles[mode] = profile
	}
	return file, nil
}

func expandRuntimePlaceholders(value string) string {
	return strings.NewReplacer(
		"${LOCALCLASH_HOST_OS}", runtime.GOOS,
		"${LOCALCLASH_HOST_ARCH}", runtime.GOARCH,
		"${LOCALCLASH_HOST_PLATFORM}", runtime.GOOS+"-"+runtime.GOARCH,
	).Replace(value)
}

func ensureProfileFiles(runtimePath string) error {
	baseDir := filepath.Dir(normalizePath(runtimePath))
	for _, mode := range []string{ModeNormal, ModeRouter} {
		defaultPath := resolveRuntimePath(baseDir, defaultProfileRelPath(mode))
		if err := writeDefaultProfileFile(defaultPath, defaultProfileBytes(mode)); err != nil {
			return err
		}
		userPath := resolveRuntimePath(baseDir, userProfileRelPath(mode))
		if _, err := os.Stat(userPath); errors.Is(err, os.ErrNotExist) {
			data, err := os.ReadFile(defaultPath)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(userPath, data, 0o644); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	return nil
}

func writeDefaultProfileFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	current, err := os.ReadFile(path)
	if err == nil && string(current) == string(data) {
		return nil
	}
	return os.WriteFile(path, data, 0o644)
}

func defaultProfileBytes(mode string) []byte {
	data, err := defaultProfileFS.ReadFile(filepath.ToSlash(defaultProfileRelPath(mode)))
	if err != nil {
		panic(fmt.Sprintf("missing embedded %s default profile: %v", mode, err))
	}
	return []byte(expandRuntimePlaceholders(string(data)))
}

func readEmbeddedDefaultProfile(mode string) (Profile, error) {
	var profile Profile
	if err := yaml.Unmarshal(defaultProfileBytes(mode), &profile); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func readProfileFile(path string) (Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, err
	}
	var profile Profile
	if err := yaml.Unmarshal(data, &profile); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func defaultProfileRelPath(mode string) string {
	return filepath.Join("profiles", mode+".default.yaml")
}

func userProfileRelPath(mode string) string {
	return filepath.Join("profiles", mode+".yaml")
}

func resolveRuntimePath(baseDir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if strings.TrimSpace(baseDir) == "" || baseDir == "." {
		return path
	}
	return filepath.Join(baseDir, path)
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
	disk := diskFile{
		Version:  file.Version,
		Mode:     file.Mode,
		Core:     file.Core,
		Cores:    file.Cores,
		Smart:    file.Smart,
		Profiles: map[string]profileRef{},
	}
	for name, profile := range file.Profiles {
		profilePath := strings.TrimSpace(profile.Path)
		if profilePath == "" {
			profilePath = userProfileRelPath(name)
		}
		disk.Profiles[name] = profileRef{Path: profilePath}
	}
	data, err := yaml.Marshal(disk)
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
