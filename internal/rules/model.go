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
	Version     int            `yaml:"version"`
	EnabledPack []SelectedPack `yaml:"enabled_packs"`
}

type SelectedPack struct {
	Source string `yaml:"source"`
	Pack   string `yaml:"pack"`
	Target string `yaml:"target"`
}

type Fragment struct {
	RuleProviders map[string]map[string]any `yaml:"rule-providers"`
	Rules         []string                  `yaml:"rules"`
}

type Options struct {
	SourcesDir    string
	CacheDir      string
	SelectionPath string
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
	return RenderFragment(selection, caches)
}

func LoadSources(dir string) ([]Source, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var sources []Source
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
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
	case "blackmatrix7", "v2fly-dlc":
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
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
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

func RenderFragment(selection Selection, caches map[string]PackCache) (Fragment, error) {
	fragment := Fragment{
		RuleProviders: map[string]map[string]any{},
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
		target, err := renderTarget(enabled.Target)
		if err != nil {
			return Fragment{}, err
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
	return fragment, nil
}

func findPack(packs []Pack, id string) (Pack, bool) {
	for _, pack := range packs {
		if pack.ID == id {
			return pack, true
		}
	}
	return Pack{}, false
}

func renderTarget(target string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "proxy":
		return "PROXY", nil
	case "direct":
		return "DIRECT", nil
	case "reject":
		return "REJECT", nil
	case "manual":
		return "MANUAL", nil
	default:
		return "", fmt.Errorf("unknown pack target %q", target)
	}
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
