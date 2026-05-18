package rules

import (
	"fmt"
	"os"
	"path/filepath"
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
	CacheDir   string
	RuntimeDir string
	ID         string
}

type PackRef struct {
	ID     string
	Source string
	Pack   string
	Name   string
}

type PackListResult struct {
	Total       int           `json:"total"`
	Packs       []PackSummary `json:"packs"`
	Guidance    []string      `json:"guidance,omitempty"`
	NextActions []string      `json:"next_actions,omitempty"`
}

type PackSummary struct {
	ID            string `json:"id"`
	Source        string `json:"source"`
	Name          string `json:"name"`
	Target        string `json:"target"`
	TargetMeaning string `json:"target_meaning,omitempty"`
	ProviderCount int    `json:"provider_count"`
	RuleCount     int    `json:"rule_count"`
}

type PackGetResult struct {
	Pack        PackDetail `json:"pack"`
	NextActions []string   `json:"next_actions,omitempty"`
}

type PackCatalog struct {
	Packs   []PackSummary         `json:"packs"`
	Details map[string]PackDetail `json:"details"`
}

type PackDetail struct {
	ID            string            `json:"id"`
	Source        string            `json:"source"`
	Name          string            `json:"name"`
	Target        string            `json:"target"`
	TargetMeaning string            `json:"target_meaning,omitempty"`
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
	URL      string `json:"-"`
	Path     string `json:"path,omitempty"`
}

type catalogEntry struct {
	Cache PackCache
	Pack  Pack
}

func ListPacks(opts PackListOptions) (PackListResult, error) {
	catalog, err := LoadPackCatalog(opts.CacheDir)
	if err != nil {
		return PackListResult{}, err
	}

	nameFilter := strings.ToLower(strings.TrimSpace(opts.Name))
	var packs []PackSummary
	for _, pack := range catalog.Packs {
		if opts.Source != "" && pack.Source != opts.Source {
			continue
		}
		if opts.Target != "" && pack.Target != opts.Target {
			continue
		}
		if nameFilter != "" && !strings.Contains(strings.ToLower(pack.Name), nameFilter) && !strings.Contains(strings.ToLower(pack.ID), nameFilter) {
			continue
		}
		packs = append(packs, pack)
	}

	if opts.Limit > 0 && len(packs) > opts.Limit {
		packs = packs[:opts.Limit]
	}
	return PackListResult{Total: len(packs), Packs: packs, Guidance: PackListGuidance(), NextActions: PackListNextActions()}, nil
}

func GetPack(opts PackGetOptions) (PackGetResult, error) {
	id := strings.TrimSpace(opts.ID)
	if id == "" {
		return PackGetResult{}, fmt.Errorf("pack id is required")
	}
	catalog, err := LoadPackCatalog(opts.CacheDir)
	if err != nil {
		return PackGetResult{}, err
	}
	if detail, ok := catalog.Details[id]; ok {
		return PackGetResult{Pack: AnnotatePackRuntime(detail, opts.RuntimeDir), NextActions: packRuleNextActions()}, nil
	}
	return PackGetResult{}, fmt.Errorf("pack %q not found in pack cache", id)
}

func packRuleNextActions() []string {
	return []string{
		"Use pack_rules_read with this pack id to inspect provider rule contents.",
		"Use pack_rules_prefetch with candidate pack ids before pack_rules_query when local provider-cache coverage is incomplete.",
	}
}

func PackListGuidance() []string {
	return []string{
		"packs_list lists available catalog packs, not currently active routing policy.",
		"The pack target field is the pack's default/recommended render target from the catalog. It is not evidence that the pack is currently configured.",
		"Use config_status to inspect active localclash.yaml intent and generated/mihomo.yaml overlay before claiming a pack is configured.",
	}
}

func PackListNextActions() []string {
	return []string{
		"Use packs_get or pack_rules_read on candidate pack ids before choosing packs.",
		"To change routing, call config_status first, then config_patch_create with the full desired retained config plus new pack targets.",
		"Apply only the exact patch_id returned by config_patch_create, then call config_status to verify.",
	}
}

func AnnotatePackRuntime(detail PackDetail, runtimeDir string) PackDetail {
	runtimeDir = strings.TrimSpace(runtimeDir)
	if runtimeDir == "" {
		runtimeDir = ".runtime/mihomo"
	}
	for i, provider := range detail.Providers {
		provider.Path = resolveProviderRuntimePath(runtimeDir, provider.Path)
		detail.Providers[i] = provider
	}
	return detail
}

func LoadPackCatalog(cacheDir string) (PackCatalog, error) {
	entries, err := loadCatalogEntries(cacheDir)
	if err != nil {
		return PackCatalog{}, err
	}
	catalog := PackCatalog{Details: map[string]PackDetail{}}
	for _, entry := range entries {
		summary := packSummary(entry)
		detail := packDetail(entry)
		catalog.Packs = append(catalog.Packs, summary)
		catalog.Details[summary.ID] = detail
	}
	return catalog, nil
}

func ResolvePackRef(cacheDir, id string) (PackRef, error) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return PackRef{}, fmt.Errorf("pack id is required")
	}
	entries, err := loadCatalogEntries(cacheDir)
	if err != nil {
		return PackRef{}, err
	}
	for _, entry := range entries {
		if catalogPackID(entry.Cache.Source, entry.Pack.ID) == trimmed {
			return packRef(entry), nil
		}
		for _, component := range entry.Pack.Components {
			if providerName(entry.Cache.Source, entry.Pack.ID, component.ID) == trimmed {
				return packRef(entry), nil
			}
		}
	}
	return PackRef{}, fmt.Errorf("pack %q not found in pack cache", trimmed)
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

	sources := make([]string, 0, len(caches))
	for source := range caches {
		sources = append(sources, source)
	}
	sort.Strings(sources)

	var entries []catalogEntry
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
		TargetMeaning: "catalog default/recommended target; not active configuration",
		ProviderCount: len(entry.Pack.Components),
		RuleCount:     len(entry.Pack.Components),
	}
}

func packRef(entry catalogEntry) PackRef {
	return PackRef{
		ID:     catalogPackID(entry.Cache.Source, entry.Pack.ID),
		Source: entry.Cache.Source,
		Pack:   entry.Pack.ID,
		Name:   packDisplayName(entry.Pack),
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
		TargetMeaning: "catalog default/recommended target; not active configuration",
		Renderable:    entry.Pack.Renderable,
		Reason:        entry.Pack.Reason,
		Providers:     providers,
		Rules:         rules,
		ProviderCount: len(providers),
		RuleCount:     len(rules),
	}
}

func resolveProviderRuntimePath(runtimeDir, providerPath string) string {
	providerPath = strings.TrimSpace(providerPath)
	if providerPath == "" {
		return ""
	}
	if filepath.IsAbs(providerPath) {
		return filepath.ToSlash(filepath.Clean(providerPath))
	}
	cleanRuntime := filepath.ToSlash(filepath.Clean(runtimeDir))
	cleanProvider := filepath.ToSlash(filepath.Clean(providerPath))
	if cleanProvider == cleanRuntime || strings.HasPrefix(cleanProvider, cleanRuntime+"/") {
		return cleanProvider
	}
	return filepath.ToSlash(filepath.Clean(filepath.Join(runtimeDir, providerPath)))
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
