package rules

import (
	"encoding/gob"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	PackIndexSchemaVersion = 1
	PackIndexFilename      = "index.gob"
)

type PackIndex struct {
	SchemaVersion int
	GeneratedAt   time.Time
	Caches        map[string]PackCache
	Catalog       PackCatalog
	Refs          map[string]PackRef
}

func BuildPackIndex(caches map[string]PackCache) (PackIndex, error) {
	entries, err := catalogEntriesFromCaches(caches)
	if err != nil {
		return PackIndex{}, err
	}
	index := PackIndex{
		SchemaVersion: PackIndexSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Caches:        copyPackCaches(caches),
		Catalog:       PackCatalog{Details: map[string]PackDetail{}},
		Refs:          map[string]PackRef{},
	}
	for _, entry := range entries {
		summary := packSummary(entry)
		detail := packDetail(entry)
		ref := packRef(entry)
		index.Catalog.Packs = append(index.Catalog.Packs, summary)
		index.Catalog.Details[summary.ID] = detail
		index.addPackRef(summary.ID, ref)
		for _, component := range entry.Pack.Components {
			index.addPackRef(providerName(entry.Cache.Source, entry.Pack.ID, component.ID), ref)
		}
	}
	return index, nil
}

func WritePackIndex(path string, caches map[string]PackCache) error {
	index, err := BuildPackIndex(caches)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, PackIndexFilename+".*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	encodeErr := gob.NewEncoder(temp).Encode(index)
	closeErr := temp.Close()
	if encodeErr != nil {
		_ = os.Remove(tempPath)
		return encodeErr
	}
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return closeErr
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func LoadPackIndex(path string) (*PackIndex, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("pack index not found: run localclash rules adapt")
		}
		return nil, err
	}
	defer file.Close()
	var index PackIndex
	if err := gob.NewDecoder(file).Decode(&index); err != nil {
		return nil, err
	}
	if index.SchemaVersion != PackIndexSchemaVersion {
		return nil, fmt.Errorf("pack index schema version mismatch: expected %d, got %d; run localclash rules adapt", PackIndexSchemaVersion, index.SchemaVersion)
	}
	if len(index.Caches) == 0 || len(index.Catalog.Packs) == 0 {
		return nil, fmt.Errorf("pack index is empty: run localclash rules adapt")
	}
	if index.Catalog.Details == nil {
		return nil, fmt.Errorf("pack index is missing details: run localclash rules adapt")
	}
	if index.Refs == nil {
		return nil, fmt.Errorf("pack index is missing refs: run localclash rules adapt")
	}
	return &index, nil
}

func (index *PackIndex) ResolvePackRef(id string) (PackRef, error) {
	if index == nil {
		return PackRef{}, fmt.Errorf("pack index not found: run localclash rules adapt")
	}
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return PackRef{}, fmt.Errorf("pack id is required")
	}
	lookupID := trimmed
	attr := ""
	if base, suffix, ok := strings.Cut(trimmed, "@"); ok {
		lookupID = base
		attr = suffix
		if err := validateGeoSiteAttr(attr); err != nil {
			return PackRef{}, err
		}
	}
	ref, ok := index.Refs[lookupKey(lookupID)]
	if !ok {
		return PackRef{}, fmt.Errorf("pack %q not found in pack cache", trimmed)
	}
	if attr == "" {
		return ref, nil
	}
	if ref.Type != PackTypeGeoSite {
		return PackRef{}, fmt.Errorf("pack %q does not support geosite attribute %q", lookupID, attr)
	}
	ref.ID = trimmed
	ref.Pack = ref.Pack + "@" + attr
	ref.Name = ref.Pack
	ref.RenderRuleTemplate = fmt.Sprintf("GEOSITE,%s,<target>", ref.Pack)
	return ref, nil
}

func (index *PackIndex) addPackRef(id string, ref PackRef) {
	if index.Refs == nil {
		index.Refs = map[string]PackRef{}
	}
	index.Refs[lookupKey(id)] = ref
}

func lookupKey(id string) string {
	return normalizePackLookupID(strings.TrimSpace(id))
}

func PackIndexPath(dir string) string {
	dir = NormalizeOptions(Options{CacheDir: dir}).CacheDir
	return filepath.Join(dir, PackIndexFilename)
}

func DumpPackIndex(path, format string) ([]byte, error) {
	index, err := LoadPackIndex(path)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "json":
		return json.MarshalIndent(index, "", "  ")
	case "yaml":
		return yaml.Marshal(index)
	default:
		return nil, fmt.Errorf("unsupported index dump format %q: use json or yaml", format)
	}
}

func copyPackCaches(caches map[string]PackCache) map[string]PackCache {
	out := make(map[string]PackCache, len(caches))
	for source, cache := range caches {
		packs := make([]Pack, len(cache.Packs))
		copy(packs, cache.Packs)
		cache.Packs = packs
		out[source] = cache
	}
	return out
}

func removeLegacyPackCacheYAML(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if shouldSkipYAMLFile(entry.Name(), entry.IsDir()) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func packCachesBySource(caches []PackCache) map[string]PackCache {
	out := make(map[string]PackCache, len(caches))
	for _, cache := range caches {
		out[cache.Source] = cache
	}
	return out
}

func sortedPackCacheSources(caches map[string]PackCache) []string {
	sources := make([]string, 0, len(caches))
	for source := range caches {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	return sources
}
