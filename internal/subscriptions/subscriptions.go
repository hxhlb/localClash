package subscriptions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const DefaultUserAgent = "clash-verge/v1.5.1"
const sourceIDPrefix = "S-"
const sourceIDHashLength = 8
const maxSourceDisplayIndex = 99
const refreshFetchConcurrency = 4
const (
	sourceTypeRemoteSubscription = "remote_subscription"
	sourceTypeInlineProxyURIs    = "inline_proxy_uris"

	subscriptionFormatMihomoYAML    = "mihomo_yaml"
	subscriptionFormatProxyURILines = "proxy_uri_lines"
)

type Source struct {
	ID          string   `json:"id" yaml:"id"`
	DisplayName string   `json:"display_name" yaml:"display_name"`
	Type        string   `json:"type,omitempty" yaml:"type,omitempty"`
	URI         string   `json:"uri,omitempty" yaml:"uri,omitempty"`
	URIs        []string `json:"uris,omitempty" yaml:"uris,omitempty"`
	URL         string   `json:"url,omitempty" yaml:"url,omitempty"` // legacy config key
}

type Config struct {
	Version int      `json:"version"`
	Sources []Source `json:"sources"`
}

type StatusOptions struct {
	ConfigPath string
	MergedPath string
	RuntimeDir string
}

type ConfigureOptions struct {
	ConfigPath string
	Sources    []Source
	URIs       []string
	URLs       []string
	Replace    *bool
}

type RefreshOptions struct {
	ConfigPath string
	IDs        []string
	RuntimeDir string
	MergedPath string
	Force      bool
	UserAgent  string
	OnStage    func(StageEvent) `json:"-"`
}

type StageEvent struct {
	Stage      string         `json:"stage"`
	Event      string         `json:"event"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	Error      string         `json:"error,omitempty"`
	Fields     map[string]any `json:"fields,omitempty"`
}

type StatusResult struct {
	Configured bool            `json:"configured"`
	Config     string          `json:"config"`
	Sources    []SourceStatus  `json:"sources"`
	Merged     ArtifactSummary `json:"merged"`
	Message    string          `json:"message,omitempty"`
}

type SourceStatus struct {
	ID               string `json:"id"`
	DisplayName      string `json:"display_name"`
	Type             string `json:"type"`
	URI              string `json:"uri,omitempty"`
	URL              string `json:"url,omitempty"`
	URIHash          string `json:"uri_hash,omitempty"`
	Artifact         string `json:"artifact"`
	Exists           bool   `json:"exists"`
	ProxiesCount     int    `json:"proxies_count,omitempty"`
	ProxyGroupsCount int    `json:"proxy_groups_count,omitempty"`
	RulesCount       int    `json:"rules_count,omitempty"`
	UpdatedAt        string `json:"updated_at,omitempty"`
}

type ArtifactSummary struct {
	Path                string `json:"path"`
	Exists              bool   `json:"exists"`
	ProxiesCount        int    `json:"proxies_count,omitempty"`
	ProxyGroupsCount    int    `json:"proxy_groups_count,omitempty"`
	RulesCount          int    `json:"rules_count,omitempty"`
	RenamedProxiesCount int    `json:"renamed_proxies_count,omitempty"`
	UpdatedAt           string `json:"updated_at,omitempty"`
}

type ConfigureResult struct {
	Config     string             `json:"config"`
	Configured bool               `json:"configured"`
	Sources    []ConfiguredSource `json:"sources"`
	Message    string             `json:"message"`
}

type ConfiguredSource struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Type        string `json:"type"`
	URI         string `json:"uri,omitempty"`
	URL         string `json:"url,omitempty"`
	URIHash     string `json:"uri_hash,omitempty"`
}

type GetResult struct {
	Config     string             `json:"config"`
	Configured bool               `json:"configured"`
	Sources    []ConfiguredSource `json:"sources"`
	URIs       []string           `json:"uris"`
	URLs       []string           `json:"urls"`
	Count      int                `json:"count"`
	Message    string             `json:"message,omitempty"`
}

type RefreshResult struct {
	Refreshed bool                   `json:"refreshed"`
	Sources   []RefreshSourceSummary `json:"sources"`
	Merged    ArtifactSummary        `json:"merged"`
	Warnings  []string               `json:"warnings"`
	Artifacts []RefreshArtifact      `json:"-"`
	MergedDoc map[string]any         `json:"-"`
}

type RefreshSourceSummary struct {
	ID               string `json:"id"`
	DisplayName      string `json:"display_name"`
	Artifact         string `json:"artifact"`
	ProxiesCount     int    `json:"proxies_count,omitempty"`
	ProxyGroupsCount int    `json:"proxy_groups_count,omitempty"`
	RulesCount       int    `json:"rules_count,omitempty"`
	Type             string `json:"type"`
	Format           string `json:"format,omitempty"`
	Status           string `json:"status"`
}

type RefreshArtifact struct {
	SourceID    string
	DisplayName string
	Proxies     []map[string]any
}

type subscriptionDoc struct {
	Data   map[string]any
	Raw    []byte
	Format string
}

type subscriptionArtifact struct {
	Version int
	Data    map[string]any
	Raw     []byte
	Format  string
}

var sourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var sourceDisplayNamePattern = regexp.MustCompile(`^[0-9]{2}$`)

func init() {
	gob.Register(map[string]any{})
	gob.Register([]any{})
	gob.Register([]map[string]any{})
}

func Status(opts StatusOptions) (StatusResult, error) {
	opts = normalizeStatusOptions(opts)
	result := StatusResult{
		Config: opts.ConfigPath,
		Merged: ArtifactSummary{
			Path: opts.MergedPath,
		},
	}
	config, err := readConfig(opts.ConfigPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			result.Message = "subscription sources are not configured; ask the user for one or more subscription URIs, then run subscriptions_configure"
			result.Merged = summarizeArtifact(opts.MergedPath)
			return result, nil
		}
		return StatusResult{}, err
	}
	result.Configured = true
	for _, source := range config.Sources {
		artifact := artifactPath(opts.RuntimeDir, source.ID)
		summary := summarizeArtifact(artifact)
		result.Sources = append(result.Sources, SourceStatus{
			ID:               source.ID,
			DisplayName:      sourceDisplayName(source),
			Type:             sourceType(source),
			URI:              MaskURI(sourcePrimaryURI(source)),
			URL:              legacyURLForResult(source),
			URIHash:          sourceURIHash(source),
			Artifact:         artifact,
			Exists:           summary.Exists,
			ProxiesCount:     summary.ProxiesCount,
			ProxyGroupsCount: summary.ProxyGroupsCount,
			RulesCount:       summary.RulesCount,
			UpdatedAt:        summary.UpdatedAt,
		})
	}
	result.Merged = summarizeArtifact(opts.MergedPath)
	return result, nil
}

func Get(opts StatusOptions) (GetResult, error) {
	opts = normalizeStatusOptions(opts)
	result := GetResult{
		Config: opts.ConfigPath,
	}
	config, err := readConfig(opts.ConfigPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			result.Message = "subscription sources are not configured; ask the user for one or more subscription URIs, then run subscriptions_configure"
			return result, nil
		}
		return GetResult{}, err
	}
	result.Configured = true
	for _, source := range config.Sources {
		uri := sourcePrimaryURI(source)
		result.Sources = append(result.Sources, ConfiguredSource{
			ID:          source.ID,
			DisplayName: sourceDisplayName(source),
			Type:        sourceType(source),
			URI:         uri,
			URL:         legacyURLForGet(source),
			URIHash:     sourceURIHash(source),
		})
		if uri != "" {
			result.URIs = append(result.URIs, uri)
		}
		if sourceType(source) == sourceTypeRemoteSubscription {
			result.URLs = append(result.URLs, uri)
		}
	}
	result.Count = len(result.Sources)
	return result, nil
}

func Configure(opts ConfigureOptions) (ConfigureResult, error) {
	opts = normalizeConfigureOptions(opts)
	if opts.Replace != nil && !*opts.Replace {
		return ConfigureResult{}, fmt.Errorf("replace=false is not supported in this version")
	}
	sources, err := configureSources(opts)
	if err != nil {
		return ConfigureResult{}, err
	}
	config := Config{Version: 1, Sources: sources}
	if err := writeConfig(opts.ConfigPath, config); err != nil {
		return ConfigureResult{}, err
	}
	result := ConfigureResult{
		Config:     opts.ConfigPath,
		Configured: true,
		Message:    "Subscription sources configured. Run subscriptions_refresh to update local artifacts.",
	}
	for _, source := range sources {
		result.Sources = append(result.Sources, ConfiguredSource{
			ID:          source.ID,
			DisplayName: source.DisplayName,
			Type:        sourceType(source),
			URI:         MaskURI(sourcePrimaryURI(source)),
			URL:         legacyMaskedURL(source),
			URIHash:     sourceURIHash(source),
		})
	}
	return result, nil
}

func SourcesFromURLs(rawURLs []string) ([]Source, error) {
	return SourcesFromURIs(rawURLs)
}

func configureSources(opts ConfigureOptions) ([]Source, error) {
	if len(opts.Sources) > 0 {
		return normalizeSources(opts.Sources)
	}
	rawURIs := opts.URIs
	if len(rawURIs) == 0 {
		rawURIs = opts.URLs
	}
	if len(rawURIs) == 0 {
		return nil, fmt.Errorf("uris is required")
	}
	return SourcesFromURIs(rawURIs)
}

func SourcesFromURIs(rawURIs []string) ([]Source, error) {
	inputs := make([]string, 0, len(rawURIs))
	for i, raw := range rawURIs {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return nil, fmt.Errorf("uris[%d] must not be empty", i)
		}
		inputs = append(inputs, trimmed)
	}
	return normalizeSourcesFromURIs(inputs)
}

func Refresh(ctx context.Context, opts RefreshOptions) (RefreshResult, error) {
	opts = normalizeRefreshOptions(opts)
	stage := subscriptionStageEmitter(opts.OnStage)

	finish := stage("read_config", map[string]any{"config": opts.ConfigPath})
	config, err := readConfig(opts.ConfigPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			finish(err, nil)
			return RefreshResult{}, fmt.Errorf("subscription sources are not configured; run subscriptions_configure first")
		}
		finish(err, nil)
		return RefreshResult{}, err
	}
	if len(config.Sources) == 0 {
		finish(fmt.Errorf("subscription sources are empty"), nil)
		return RefreshResult{}, fmt.Errorf("subscription sources are empty; run subscriptions_configure first")
	}
	finish(nil, map[string]any{"source_count": len(config.Sources)})

	finish = stage("validate_sources", nil)
	if err := validateSources(config.Sources); err != nil {
		finish(err, nil)
		return RefreshResult{}, err
	}
	finish(nil, nil)

	finish = stage("select_sources", map[string]any{"ids": opts.IDs})
	selected, err := selectedSourceIDs(config.Sources, opts.IDs)
	if err != nil {
		finish(err, nil)
		return RefreshResult{}, err
	}
	selectedCount := 0
	for _, ok := range selected {
		if ok {
			selectedCount++
		}
	}
	finish(nil, map[string]any{"selected_count": selectedCount})

	finish = stage("ensure_runtime_dir", map[string]any{"runtime_dir": opts.RuntimeDir})
	if err := os.MkdirAll(opts.RuntimeDir, 0o755); err != nil {
		finish(err, nil)
		return RefreshResult{}, err
	}
	finish(nil, nil)

	refreshed, docs, err := refreshSelectedSources(ctx, config.Sources, selected, opts, stage)
	if err != nil {
		return RefreshResult{}, err
	}

	finish = stage("read_artifacts", nil)
	result := RefreshResult{Refreshed: true, Warnings: []string{}}
	diskReads := 0
	for _, source := range config.Sources {
		path := artifactPath(opts.RuntimeDir, source.ID)
		doc, ok := docs[source.ID]
		if !ok {
			var err error
			doc, err = readSubscription(path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					if selected[source.ID] {
						finish(err, map[string]any{"source_id": source.ID})
						return RefreshResult{}, fmt.Errorf("source %q artifact was not written", source.ID)
					}
					result.Warnings = append(result.Warnings, fmt.Sprintf("source %q has no local artifact; run subscriptions_refresh for that source", source.ID))
					continue
				}
				finish(err, map[string]any{"source_id": source.ID})
				return RefreshResult{}, err
			}
			docs[source.ID] = doc
			diskReads++
		}
		summary := summarizeMap(doc.Data)
		status := "existing"
		if refreshed[source.ID] {
			status = "ok"
		}
		result.Artifacts = append(result.Artifacts, RefreshArtifact{
			SourceID:    source.ID,
			DisplayName: sourceDisplayName(source),
			Proxies:     proxyMaps(doc.Data),
		})
		result.Sources = append(result.Sources, RefreshSourceSummary{
			ID:               source.ID,
			DisplayName:      sourceDisplayName(source),
			Artifact:         path,
			ProxiesCount:     summary.ProxiesCount,
			ProxyGroupsCount: summary.ProxyGroupsCount,
			RulesCount:       summary.RulesCount,
			Type:             sourceType(source),
			Format:           doc.Format,
			Status:           status,
		})
	}
	if len(docs) == 0 {
		finish(fmt.Errorf("no subscription artifacts are available to merge"), nil)
		return RefreshResult{}, fmt.Errorf("no subscription artifacts are available to merge")
	}
	finish(nil, map[string]any{"artifact_count": len(docs), "memory_docs": len(docs) - diskReads, "disk_reads": diskReads})

	finish = stage("merge_subscriptions", nil)
	merged, renamed, err := mergeSubscriptions(config.Sources, docs)
	if err != nil {
		finish(err, nil)
		return RefreshResult{}, err
	}
	finish(nil, map[string]any{"renamed_proxies": renamed})

	finish = stage("write_merged_subscription", map[string]any{"merged": opts.MergedPath})
	if err := writeSubscriptionArtifact(opts.MergedPath, subscriptionDoc{Data: merged}); err != nil {
		finish(err, nil)
		return RefreshResult{}, err
	}
	result.Merged = summarizeArtifact(opts.MergedPath)
	result.Merged.RenamedProxiesCount = renamed
	result.MergedDoc = merged
	sort.Strings(result.Warnings)
	finish(nil, map[string]any{
		"proxies":         result.Merged.ProxiesCount,
		"renamed_proxies": renamed,
	})
	return result, nil
}

type sourceRefreshOutcome struct {
	sourceID  string
	refreshed bool
	doc       subscriptionDoc
	err       error
}

func refreshSelectedSources(ctx context.Context, sources []Source, selected map[string]bool, opts RefreshOptions, stage func(string, map[string]any) func(error, map[string]any)) (map[string]bool, map[string]subscriptionDoc, error) {
	selectedCount := 0
	for _, source := range sources {
		if selected[source.ID] {
			selectedCount++
		}
	}
	if selectedCount == 0 {
		return map[string]bool{}, map[string]subscriptionDoc{}, nil
	}
	workerCount := refreshFetchConcurrency
	if selectedCount < workerCount {
		workerCount = selectedCount
	}

	jobs := make(chan Source)
	results := make(chan sourceRefreshOutcome, selectedCount)
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for source := range jobs {
				results <- refreshOneSource(ctx, source, opts, stage)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, source := range sources {
			if !selected[source.ID] {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case jobs <- source:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	outcomes := map[string]sourceRefreshOutcome{}
	for result := range results {
		outcomes[result.sourceID] = result
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	for _, source := range sources {
		if !selected[source.ID] {
			continue
		}
		outcome, ok := outcomes[source.ID]
		if !ok {
			return nil, nil, fmt.Errorf("source %q was not refreshed", source.ID)
		}
		if outcome.err != nil {
			return nil, nil, outcome.err
		}
	}
	refreshed := map[string]bool{}
	docs := map[string]subscriptionDoc{}
	for _, source := range sources {
		if outcome, ok := outcomes[source.ID]; ok && outcome.refreshed {
			refreshed[source.ID] = true
			docs[source.ID] = outcome.doc
		}
	}
	return refreshed, docs, nil
}

func refreshOneSource(ctx context.Context, source Source, opts RefreshOptions, stage func(string, map[string]any) func(error, map[string]any)) sourceRefreshOutcome {
	finish := stage("refresh_source", map[string]any{"source_id": source.ID, "type": sourceType(source), "uri": MaskURI(sourcePrimaryURI(source))})
	doc, err := refreshSource(ctx, source, opts.UserAgent)
	if err != nil {
		finish(err, nil)
		return sourceRefreshOutcome{sourceID: source.ID, err: err}
	}
	finish(nil, nil)

	artifact := artifactPath(opts.RuntimeDir, source.ID)
	finish = stage("write_source_artifact", map[string]any{"source_id": source.ID, "artifact": artifact})
	if err := writeSubscriptionArtifact(artifact, doc); err != nil {
		finish(err, nil)
		return sourceRefreshOutcome{sourceID: source.ID, err: err}
	}
	summary := summarizeMap(doc.Data)
	finish(nil, map[string]any{
		"bytes":        len(doc.Raw),
		"format":       doc.Format,
		"proxies":      summary.ProxiesCount,
		"proxy_groups": summary.ProxyGroupsCount,
		"rules":        summary.RulesCount,
	})
	return sourceRefreshOutcome{sourceID: source.ID, refreshed: true, doc: doc}
}

func subscriptionStageEmitter(callback func(StageEvent)) func(string, map[string]any) func(error, map[string]any) {
	var mu sync.Mutex
	emit := func(event StageEvent) {
		mu.Lock()
		defer mu.Unlock()
		callback(event)
	}
	return func(stage string, fields map[string]any) func(error, map[string]any) {
		if callback == nil {
			return func(error, map[string]any) {}
		}
		started := time.Now()
		emit(StageEvent{Stage: stage, Event: "started", Fields: fields})
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
			emit(event)
		}
	}
}

func normalizeStatusOptions(opts StatusOptions) StatusOptions {
	if strings.TrimSpace(opts.ConfigPath) == "" {
		opts.ConfigPath = "localclash-subscriptions.json"
	}
	if strings.TrimSpace(opts.MergedPath) == "" {
		opts.MergedPath = "subscription.gob"
	}
	if strings.TrimSpace(opts.RuntimeDir) == "" {
		opts.RuntimeDir = ".runtime/subscriptions"
	}
	return opts
}

func normalizeConfigureOptions(opts ConfigureOptions) ConfigureOptions {
	if strings.TrimSpace(opts.ConfigPath) == "" {
		opts.ConfigPath = "localclash-subscriptions.json"
	}
	return opts
}

func normalizeRefreshOptions(opts RefreshOptions) RefreshOptions {
	if strings.TrimSpace(opts.ConfigPath) == "" {
		opts.ConfigPath = "localclash-subscriptions.json"
	}
	if strings.TrimSpace(opts.RuntimeDir) == "" {
		opts.RuntimeDir = ".runtime/subscriptions"
	}
	if strings.TrimSpace(opts.MergedPath) == "" {
		opts.MergedPath = "subscription.gob"
	}
	if strings.TrimSpace(opts.UserAgent) == "" {
		opts.UserAgent = DefaultUserAgent
	}
	return opts
}

func readConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func writeConfig(path string, config Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func normalizeSources(sources []Source) ([]Source, error) {
	if len(sources) > maxSourceDisplayIndex {
		return nil, fmt.Errorf("subscription sources support at most %d entries for two-digit display_name values", maxSourceDisplayIndex)
	}
	normalized := make([]Source, 0, len(sources))
	seenCanonicalURI := map[string]bool{}
	usedIDs := map[string]bool{}
	usedDisplayNames := map[string]bool{}
	canonicalURIs := make([]string, 0, len(sources))
	for i, source := range sources {
		canonicalURI, err := canonicalSourceURI(source)
		if err != nil {
			return nil, fmt.Errorf("sources[%d] %w", i, err)
		}
		if seenCanonicalURI[canonicalURI] {
			return nil, fmt.Errorf("duplicate subscription URI at sources[%d]", i)
		}
		seenCanonicalURI[canonicalURI] = true
		canonicalURIs = append(canonicalURIs, canonicalURI)
	}
	for i, source := range sources {
		id := sourceIDFromCanonicalURI(canonicalURIs[i], usedIDs)
		displayName, err := normalizeSourceDisplayName(source.DisplayName, i)
		if err != nil {
			return nil, fmt.Errorf("sources[%d] %w", i, err)
		}
		if usedDisplayNames[displayName] {
			return nil, fmt.Errorf("duplicate subscription source display_name %q at sources[%d]", displayName, i)
		}
		usedIDs[id] = true
		usedDisplayNames[displayName] = true
		normalized = append(normalized, normalizeSourceForConfig(source, id, displayName))
	}
	return normalized, nil
}

func normalizeSourceDisplayName(raw string, index int) (string, error) {
	displayName := strings.TrimSpace(raw)
	if displayName == "" {
		if index >= maxSourceDisplayIndex {
			return "", fmt.Errorf("source display_name is required after %d sources", maxSourceDisplayIndex)
		}
		displayName = fmt.Sprintf("%02d", index+1)
	}
	if !sourceDisplayNamePattern.MatchString(displayName) || displayName == "00" {
		return "", fmt.Errorf("source display_name %q is invalid; use two digits from 01 to 99", displayName)
	}
	return displayName, nil
}

func normalizeSourcesFromURIs(rawURIs []string) ([]Source, error) {
	var remoteSources []Source
	var inlineURIs []string
	seenRemote := map[string]bool{}
	seenInline := map[string]bool{}
	for i, rawURI := range rawURIs {
		kind, canonical, err := classifyInputURI(rawURI)
		if err != nil {
			return nil, fmt.Errorf("uris[%d] %w", i, err)
		}
		switch kind {
		case sourceTypeRemoteSubscription:
			if seenRemote[canonical] {
				continue
			}
			seenRemote[canonical] = true
			remoteSources = append(remoteSources, Source{Type: sourceTypeRemoteSubscription, URI: strings.TrimSpace(rawURI)})
		case sourceTypeInlineProxyURIs:
			trimmed := strings.TrimSpace(rawURI)
			if seenInline[trimmed] {
				continue
			}
			seenInline[trimmed] = true
			inlineURIs = append(inlineURIs, trimmed)
		default:
			return nil, fmt.Errorf("uris[%d] unsupported source type %q", i, kind)
		}
	}
	sources := make([]Source, 0, len(remoteSources)+1)
	sources = append(sources, remoteSources...)
	if len(inlineURIs) > 0 {
		sources = append(sources, Source{Type: sourceTypeInlineProxyURIs, URIs: inlineURIs})
	}
	return normalizeSources(sources)
}

func classifyInputURI(rawURI string) (string, string, error) {
	trimmed := strings.TrimSpace(rawURI)
	if trimmed == "" {
		return "", "", fmt.Errorf("uri is required")
	}
	scheme, rest, ok := strings.Cut(trimmed, "://")
	if !ok {
		return "", "", fmt.Errorf("uri must include a scheme")
	}
	scheme = strings.ToLower(scheme)
	switch {
	case scheme == "http" || scheme == "https":
		canonical, err := canonicalRemoteSubscriptionURI(trimmed)
		if err != nil {
			return "", "", err
		}
		return sourceTypeRemoteSubscription, canonical, nil
	case proxyURISchemes[scheme] && strings.TrimSpace(rest) != "":
		return sourceTypeInlineProxyURIs, "proxy:" + trimmed, nil
	default:
		return "", "", fmt.Errorf("uri scheme %q is not supported; use http/https or an MVP proxy URI scheme", scheme)
	}
}

func canonicalSourceURI(source Source) (string, error) {
	switch sourceType(source) {
	case sourceTypeRemoteSubscription:
		return canonicalRemoteSubscriptionURI(sourcePrimaryURI(source))
	case sourceTypeInlineProxyURIs:
		if len(source.URIs) == 0 {
			return "", fmt.Errorf("inline proxy URI source requires uris")
		}
		for i, uri := range source.URIs {
			kind, _, err := classifyInputURI(uri)
			if err != nil {
				return "", fmt.Errorf("inline proxy URI %d is invalid: %w", i, err)
			}
			if kind != sourceTypeInlineProxyURIs {
				return "", fmt.Errorf("inline proxy URI %d must use an MVP proxy URI scheme", i)
			}
		}
		return "inline:" + strings.Join(source.URIs, "\n"), nil
	default:
		return "", fmt.Errorf("source type %q is not supported", sourceType(source))
	}
}

func canonicalSubscriptionURL(rawURL string) (string, error) {
	return canonicalRemoteSubscriptionURI(rawURL)
}

func canonicalRemoteSubscriptionURI(rawURI string) (string, error) {
	if rawURI == "" {
		return "", fmt.Errorf("uri is required")
	}
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return "", fmt.Errorf("uri is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("uri must use http or https")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("uri must include a host")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	return parsed.String(), nil
}

func sourceIDFromCanonicalURL(canonicalURL string, used map[string]bool) string {
	return sourceIDFromCanonicalURI(canonicalURL, used)
}

func sourceIDFromCanonicalURI(canonicalURI string, used map[string]bool) string {
	sum := sha256.Sum256([]byte(canonicalURI))
	encoded := hex.EncodeToString(sum[:])
	for length := sourceIDHashLength; length <= len(encoded); length += 2 {
		id := sourceIDPrefix + encoded[:length]
		if !used[id] {
			return id
		}
	}
	return sourceIDPrefix + encoded
}

func validateSources(sources []Source) error {
	usedDisplayNames := map[string]bool{}
	for i, source := range sources {
		if err := validateSource(source); err != nil {
			return fmt.Errorf("sources[%d] %w", i, err)
		}
		displayName := strings.TrimSpace(source.DisplayName)
		if displayName == "" {
			continue
		}
		if usedDisplayNames[displayName] {
			return fmt.Errorf("duplicate subscription source display_name %q at sources[%d]", displayName, i)
		}
		usedDisplayNames[displayName] = true
	}
	return nil
}

func validateSource(source Source) error {
	if source.ID == "" {
		return fmt.Errorf("source id is required")
	}
	if !sourceIDPattern.MatchString(source.ID) {
		return fmt.Errorf("source id %q is invalid; use only letters, digits, underscore, and hyphen", source.ID)
	}
	displayName := strings.TrimSpace(source.DisplayName)
	if displayName != "" && (!sourceDisplayNamePattern.MatchString(displayName) || displayName == "00") {
		return fmt.Errorf("source %q display_name %q is invalid; use two digits from 01 to 99", source.ID, source.DisplayName)
	}
	_, err := canonicalSourceURI(source)
	return err
}

func sourceDisplayName(source Source) string {
	displayName := strings.TrimSpace(source.DisplayName)
	if displayName != "" {
		return displayName
	}
	id := strings.TrimSpace(source.ID)
	trimmed := strings.TrimPrefix(id, sourceIDPrefix)
	if trimmed == "" {
		trimmed = id
	}
	return firstSourceIDChars(trimmed, 2)
}

func firstSourceIDChars(value string, count int) string {
	if len(value) <= count {
		return value
	}
	return value[:count]
}

func selectedSourceIDs(sources []Source, ids []string) (map[string]bool, error) {
	known := map[string]bool{}
	for _, source := range sources {
		known[source.ID] = true
	}
	selected := map[string]bool{}
	if len(ids) == 0 {
		for _, source := range sources {
			selected[source.ID] = true
		}
		return selected, nil
	}
	for _, rawID := range ids {
		id := strings.TrimSpace(rawID)
		if !known[id] {
			return nil, fmt.Errorf("unknown subscription source id %q", id)
		}
		selected[id] = true
	}
	return selected, nil
}

func refreshSource(ctx context.Context, source Source, userAgent string) (subscriptionDoc, error) {
	switch sourceType(source) {
	case sourceTypeRemoteSubscription:
		return fetchSource(ctx, source, userAgent)
	case sourceTypeInlineProxyURIs:
		return parseProxyURIList(source.ID, []byte(strings.Join(source.URIs, "\n")))
	default:
		return subscriptionDoc{}, fmt.Errorf("source %q type %q is not supported", source.ID, sourceType(source))
	}
}

func fetchSource(ctx context.Context, source Source, userAgent string) (subscriptionDoc, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourcePrimaryURI(source), nil)
	if err != nil {
		return subscriptionDoc{}, fmt.Errorf("source %q request could not be created", source.ID)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "*/*")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return subscriptionDoc{}, fmt.Errorf("source %q request failed", source.ID)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return subscriptionDoc{}, fmt.Errorf("source %q request failed: %s", source.ID, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return subscriptionDoc{}, fmt.Errorf("source %q response could not be read", source.ID)
	}
	doc, err := parseRemoteSubscription(source.ID, body)
	if err != nil {
		return subscriptionDoc{}, err
	}
	doc.Raw = append([]byte(nil), body...)
	return doc, nil
}

func parseRemoteSubscription(sourceID string, data []byte) (subscriptionDoc, error) {
	doc, yamlErr := parseSubscription(sourceID, data)
	if yamlErr == nil {
		return doc, nil
	}
	doc, uriErr := parseProxyURIList(sourceID, data)
	if uriErr == nil {
		return doc, nil
	}
	return subscriptionDoc{}, fmt.Errorf("source %q response is neither Mihomo YAML nor MVP proxy URI lines: yaml: %v; proxy_uri_lines: %v", sourceID, yamlErr, uriErr)
}

func parseSubscription(sourceID string, data []byte) (subscriptionDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return subscriptionDoc{}, fmt.Errorf("source %q response was empty", sourceID)
	}
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return subscriptionDoc{}, fmt.Errorf("source %q response is not valid YAML", sourceID)
	}
	doc, ok := raw.(map[string]any)
	if !ok {
		return subscriptionDoc{}, fmt.Errorf("source %q subscription YAML must be a map", sourceID)
	}
	proxies, ok := doc["proxies"].([]any)
	if !ok || len(proxies) == 0 {
		return subscriptionDoc{}, fmt.Errorf("source %q subscription has no proxies", sourceID)
	}
	for _, rawProxy := range proxies {
		proxy, ok := rawProxy.(map[string]any)
		if !ok {
			return subscriptionDoc{}, fmt.Errorf("source %q subscription contains an invalid proxy entry", sourceID)
		}
		if strings.TrimSpace(stringValue(proxy["name"])) == "" {
			return subscriptionDoc{}, fmt.Errorf("source %q subscription contains a proxy without name", sourceID)
		}
	}
	return subscriptionDoc{Data: doc, Format: subscriptionFormatMihomoYAML}, nil
}

func readSubscription(path string) (subscriptionDoc, error) {
	file, err := os.Open(path)
	if err != nil {
		return subscriptionDoc{}, err
	}
	defer file.Close()
	var artifact subscriptionArtifact
	if err := gob.NewDecoder(file).Decode(&artifact); err != nil {
		return subscriptionDoc{}, err
	}
	if artifact.Version != 1 {
		return subscriptionDoc{}, fmt.Errorf("subscription artifact schema version mismatch: expected 1, got %d; run localclash subscriptions refresh", artifact.Version)
	}
	if len(artifact.Data) == 0 {
		return subscriptionDoc{}, fmt.Errorf("subscription artifact %q is empty; run localclash subscriptions refresh", path)
	}
	return subscriptionDoc{Data: artifact.Data, Raw: artifact.Raw, Format: artifact.Format}, nil
}

func writeSubscriptionArtifact(path string, doc subscriptionDoc) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	encodeErr := gob.NewEncoder(file).Encode(subscriptionArtifact{Version: 1, Data: doc.Data, Raw: doc.Raw, Format: doc.Format})
	closeErr := file.Close()
	if encodeErr != nil {
		_ = os.Remove(tmp)
		return encodeErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, path)
}

func mergeSubscriptions(sources []Source, docs map[string]subscriptionDoc) (map[string]any, int, error) {
	prefixSource := len(docs) > 1
	usedNames := map[string]bool{}
	var mergedProxies []any
	renamed := 0
	for _, source := range sources {
		doc, ok := docs[source.ID]
		if !ok {
			continue
		}
		for _, rawProxy := range anySlice(doc.Data["proxies"]) {
			proxy := cloneMap(rawProxy.(map[string]any))
			originalName := stringValue(proxy["name"])
			name := originalName
			if prefixSource {
				name = "[" + sourceDisplayName(source) + "] " + name
			}
			// Mihomo requires unique proxy names, but unsafe subscription
			// payloads can contain duplicates. Normalize duplicates during
			// merge so the generated artifact remains selector-safe.
			name = uniqueProxyName(name, usedNames)
			if name != originalName {
				renamed++
			}
			proxy["name"] = name
			usedNames[name] = true
			mergedProxies = append(mergedProxies, proxy)
		}
	}
	return map[string]any{"proxies": mergedProxies}, renamed, nil
}

func uniqueProxyName(name string, used map[string]bool) string {
	if !used[name] {
		return name
	}
	base := name
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s (%d)", base, i)
		if !used[candidate] {
			return candidate
		}
	}
}

func summarizeArtifact(path string) ArtifactSummary {
	result := ArtifactSummary{Path: path}
	info, err := os.Stat(path)
	if err != nil {
		return result
	}
	if info.IsDir() {
		return result
	}
	result.Exists = true
	result.UpdatedAt = info.ModTime().Format(time.RFC3339)
	doc, err := readSubscription(path)
	if err != nil {
		return result
	}
	counts := summarizeMap(doc.Data)
	result.ProxiesCount = counts.ProxiesCount
	result.ProxyGroupsCount = counts.ProxyGroupsCount
	result.RulesCount = counts.RulesCount
	return result
}

func summarizeMap(doc map[string]any) ArtifactSummary {
	return ArtifactSummary{
		ProxiesCount:     len(anySlice(doc["proxies"])),
		ProxyGroupsCount: len(anySlice(doc["proxy-groups"])),
		RulesCount:       len(anySlice(doc["rules"])),
	}
}

func MaskURL(rawURL string) string {
	return MaskURI(rawURL)
}

func MaskURI(rawURI string) string {
	kind, _, err := classifyInputURI(rawURI)
	if err == nil && kind == sourceTypeInlineProxyURIs {
		scheme, _, _ := strings.Cut(strings.TrimSpace(rawURI), "://")
		return strings.ToLower(scheme) + "://<redacted>"
	}
	parsed, err := url.Parse(rawURI)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "<invalid-uri>"
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	const maxPath = 40
	if len(path) > maxPath {
		path = path[:maxPath] + "..."
	}
	masked := parsed.Scheme + "://" + parsed.Host + path
	if parsed.RawQuery != "" {
		masked += "?..."
	}
	return masked
}

func sourceType(source Source) string {
	if source.Type != "" {
		return source.Type
	}
	if source.URL != "" || source.URI != "" {
		return sourceTypeRemoteSubscription
	}
	if len(source.URIs) > 0 {
		return sourceTypeInlineProxyURIs
	}
	return ""
}

func sourcePrimaryURI(source Source) string {
	if strings.TrimSpace(source.URI) != "" {
		return strings.TrimSpace(source.URI)
	}
	if strings.TrimSpace(source.URL) != "" {
		return strings.TrimSpace(source.URL)
	}
	if len(source.URIs) == 1 {
		return strings.TrimSpace(source.URIs[0])
	}
	return ""
}

func normalizeSourceForConfig(source Source, id, displayName string) Source {
	switch sourceType(source) {
	case sourceTypeRemoteSubscription:
		return Source{ID: id, DisplayName: displayName, Type: sourceTypeRemoteSubscription, URI: sourcePrimaryURI(source)}
	case sourceTypeInlineProxyURIs:
		uris := make([]string, 0, len(source.URIs))
		seen := map[string]bool{}
		for _, rawURI := range source.URIs {
			uri := strings.TrimSpace(rawURI)
			if uri == "" || seen[uri] {
				continue
			}
			seen[uri] = true
			uris = append(uris, uri)
		}
		return Source{ID: id, DisplayName: displayName, Type: sourceTypeInlineProxyURIs, URIs: uris}
	default:
		return Source{ID: id, DisplayName: displayName}
	}
}

func sourceURIHash(source Source) string {
	canonical, err := canonicalSourceURI(source)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])[:sourceIDHashLength]
}

func legacyURLForResult(source Source) string {
	if sourceType(source) != sourceTypeRemoteSubscription {
		return ""
	}
	return MaskURI(sourcePrimaryURI(source))
}

func legacyMaskedURL(source Source) string {
	return legacyURLForResult(source)
}

func legacyURLForGet(source Source) string {
	if sourceType(source) != sourceTypeRemoteSubscription {
		return ""
	}
	return sourcePrimaryURI(source)
}

func artifactPath(runtimeDir, id string) string {
	return filepath.Join(runtimeDir, id+".gob")
}

func anySlice(value any) []any {
	if values, ok := value.([]any); ok {
		return values
	}
	return nil
}

func proxyMaps(doc map[string]any) []map[string]any {
	raw := anySlice(doc["proxies"])
	proxies := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if proxy, ok := item.(map[string]any); ok {
			proxies = append(proxies, proxy)
		}
	}
	return proxies
}

func cloneMap(in map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range in {
		out[key] = value
	}
	return out
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
