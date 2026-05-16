package rules

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

type PackListOptions struct {
	CacheDir string
	Source   string
	Name     string
	Target   string
	Limit    int
}

type PackGetOptions struct {
	CacheDir string
	ID       string
}

type PackListResult struct {
	Total int           `json:"total"`
	Packs []PackSummary `json:"packs"`
}

type PackSummary struct {
	ID            string `json:"id"`
	Source        string `json:"source"`
	Name          string `json:"name"`
	Target        string `json:"target"`
	ProviderCount int    `json:"provider_count"`
	RuleCount     int    `json:"rule_count"`
}

type PackGetResult struct {
	Pack PackDetail `json:"pack"`
}

type PackDetail struct {
	ID            string            `json:"id"`
	Source        string            `json:"source"`
	Name          string            `json:"name"`
	Target        string            `json:"target"`
	Renderable    bool              `json:"renderable"`
	Reason        string            `json:"reason,omitempty"`
	Providers     []ProviderSummary `json:"providers"`
	Rules         []string          `json:"rules"`
	ProviderCount int               `json:"provider_count"`
	RuleCount     int               `json:"rule_count"`
}

type ProviderSummary struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Behavior string `json:"behavior"`
	Format   string `json:"format"`
	URL      string `json:"url"`
	Path     string `json:"path,omitempty"`
}

type catalogEntry struct {
	Cache PackCache
	Pack  Pack
}

func ListPacks(opts PackListOptions) (PackListResult, error) {
	entries, err := loadCatalogEntries(opts.CacheDir)
	if err != nil {
		return PackListResult{}, err
	}

	nameFilter := strings.ToLower(strings.TrimSpace(opts.Name))
	var packs []PackSummary
	for _, entry := range entries {
		if opts.Source != "" && entry.Cache.Source != opts.Source {
			continue
		}
		if opts.Target != "" && entry.Pack.Target != opts.Target {
			continue
		}
		name := packDisplayName(entry.Pack)
		if nameFilter != "" && !strings.Contains(strings.ToLower(name), nameFilter) && !strings.Contains(strings.ToLower(entry.Pack.ID), nameFilter) {
			continue
		}
		packs = append(packs, packSummary(entry))
	}

	if opts.Limit > 0 && len(packs) > opts.Limit {
		packs = packs[:opts.Limit]
	}
	return PackListResult{Total: len(packs), Packs: packs}, nil
}

func GetPack(opts PackGetOptions) (PackGetResult, error) {
	id := strings.TrimSpace(opts.ID)
	if id == "" {
		return PackGetResult{}, fmt.Errorf("pack id is required")
	}
	entries, err := loadCatalogEntries(opts.CacheDir)
	if err != nil {
		return PackGetResult{}, err
	}
	for _, entry := range entries {
		if catalogPackID(entry.Cache.Source, entry.Pack.ID) == id {
			return PackGetResult{Pack: packDetail(entry)}, nil
		}
	}
	return PackGetResult{}, fmt.Errorf("pack %q not found in pack cache", id)
}

func loadCatalogEntries(cacheDir string) ([]catalogEntry, error) {
	normalized := NormalizeOptions(Options{CacheDir: cacheDir})
	caches, err := LoadPackCaches(normalized.CacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("pack cache directory %q does not exist; run rules adapt first", normalized.CacheDir)
		}
		return nil, err
	}

	var entries []catalogEntry
	sources := make([]string, 0, len(caches))
	for source := range caches {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	for _, source := range sources {
		cache := caches[source]
		packs := append([]Pack(nil), cache.Packs...)
		sort.Slice(packs, func(i, j int) bool {
			left, right := packDisplayName(packs[i]), packDisplayName(packs[j])
			if left == right {
				return packs[i].ID < packs[j].ID
			}
			return left < right
		})
		for _, pack := range packs {
			entries = append(entries, catalogEntry{Cache: cache, Pack: pack})
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no packs found in %q; run rules adapt first", normalized.CacheDir)
	}
	return entries, nil
}

func packSummary(entry catalogEntry) PackSummary {
	return PackSummary{
		ID:            catalogPackID(entry.Cache.Source, entry.Pack.ID),
		Source:        entry.Cache.Source,
		Name:          packDisplayName(entry.Pack),
		Target:        entry.Pack.Target,
		ProviderCount: len(entry.Pack.Components),
		RuleCount:     len(entry.Pack.Components),
	}
}

func packDetail(entry catalogEntry) PackDetail {
	providers := make([]ProviderSummary, 0, len(entry.Pack.Components))
	rules := make([]string, 0, len(entry.Pack.Components))
	target := entry.Pack.Target
	if target == "" {
		target = "<target>"
	}
	for _, component := range entry.Pack.Components {
		name := providerName(entry.Cache.Source, entry.Pack.ID, component.ID)
		providers = append(providers, ProviderSummary{
			Name:     name,
			Type:     "http",
			Behavior: component.Behavior,
			Format:   component.Format,
			URL:      component.URL,
			Path:     component.Path,
		})
		rules = append(rules, fmt.Sprintf("RULE-SET,%s,%s", name, target))
	}
	return PackDetail{
		ID:            catalogPackID(entry.Cache.Source, entry.Pack.ID),
		Source:        entry.Cache.Source,
		Name:          packDisplayName(entry.Pack),
		Target:        entry.Pack.Target,
		Renderable:    entry.Pack.Renderable,
		Reason:        entry.Pack.Reason,
		Providers:     providers,
		Rules:         rules,
		ProviderCount: len(providers),
		RuleCount:     len(rules),
	}
}

func catalogPackID(source, packID string) string {
	return providerName(source, packID, "")
}

func PackCatalogID(source, packID string) string {
	return catalogPackID(source, packID)
}

func packDisplayName(pack Pack) string {
	if strings.TrimSpace(pack.Name) != "" {
		return pack.Name
	}
	return pack.ID
}
