package subscriptions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
const refreshFetchConcurrency = 4

type Source struct {
	ID  string `json:"id" yaml:"id"`
	URL string `json:"url" yaml:"url"`
}

type Config struct {
	Version int      `yaml:"version"`
	Sources []Source `yaml:"sources"`
}

type StatusOptions struct {
	ConfigPath string
	MergedPath string
	RuntimeDir string
}

type ConfigureOptions struct {
	ConfigPath string
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
	URL              string `json:"url"`
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
	ID  string `json:"id"`
	URL string `json:"url"`
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
	Artifact         string `json:"artifact"`
	ProxiesCount     int    `json:"proxies_count,omitempty"`
	ProxyGroupsCount int    `json:"proxy_groups_count,omitempty"`
	RulesCount       int    `json:"rules_count,omitempty"`
	Status           string `json:"status"`
}

type RefreshArtifact struct {
	SourceID string
	Proxies  []map[string]any
}

type subscriptionDoc struct {
	Data map[string]any
	Raw  []byte
}

var sourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

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
			result.Message = "subscription sources are not configured; ask the user for one or more subscription URLs, then run subscriptions_configure"
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
			URL:              MaskURL(source.URL),
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

func Configure(opts ConfigureOptions) (ConfigureResult, error) {
	opts = normalizeConfigureOptions(opts)
	if opts.Replace != nil && !*opts.Replace {
		return ConfigureResult{}, fmt.Errorf("replace=false is not supported in this version")
	}
	if len(opts.URLs) == 0 {
		return ConfigureResult{}, fmt.Errorf("urls is required")
	}
	sources, err := SourcesFromURLs(opts.URLs)
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
		result.Sources = append(result.Sources, ConfiguredSource{ID: source.ID, URL: MaskURL(source.URL)})
	}
	return result, nil
}

func SourcesFromURLs(rawURLs []string) ([]Source, error) {
	sources := make([]Source, 0, len(rawURLs))
	for i, raw := range rawURLs {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return nil, fmt.Errorf("urls[%d] must not be empty", i)
		}
		sources = append(sources, Source{URL: trimmed})
	}
	return normalizeSources(sources)
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
	for _, source := range config.Sources {
		if err := validateSource(source); err != nil {
			finish(err, map[string]any{"source_id": source.ID})
			return RefreshResult{}, err
		}
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
			SourceID: source.ID,
			Proxies:  proxyMaps(doc.Data),
		})
		result.Sources = append(result.Sources, RefreshSourceSummary{
			ID:               source.ID,
			Artifact:         path,
			ProxiesCount:     summary.ProxiesCount,
			ProxyGroupsCount: summary.ProxyGroupsCount,
			RulesCount:       summary.RulesCount,
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
	if err := writeSubscriptionArtifact(opts.MergedPath, merged); err != nil {
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
	finish := stage("fetch_source", map[string]any{"source_id": source.ID, "url": MaskURL(source.URL)})
	doc, err := fetchSource(ctx, source, opts.UserAgent)
	if err != nil {
		finish(err, nil)
		return sourceRefreshOutcome{sourceID: source.ID, err: err}
	}
	finish(nil, nil)

	artifact := artifactPath(opts.RuntimeDir, source.ID)
	finish = stage("write_source_artifact", map[string]any{"source_id": source.ID, "artifact": artifact})
	if err := writeRawSubscriptionArtifact(artifact, doc.Raw); err != nil {
		finish(err, nil)
		return sourceRefreshOutcome{sourceID: source.ID, err: err}
	}
	summary := summarizeMap(doc.Data)
	finish(nil, map[string]any{
		"bytes":        len(doc.Raw),
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
		opts.ConfigPath = "localclash-subscriptions.yaml"
	}
	if strings.TrimSpace(opts.MergedPath) == "" {
		opts.MergedPath = "subscription.yaml"
	}
	if strings.TrimSpace(opts.RuntimeDir) == "" {
		opts.RuntimeDir = ".runtime/subscriptions"
	}
	return opts
}

func normalizeConfigureOptions(opts ConfigureOptions) ConfigureOptions {
	if strings.TrimSpace(opts.ConfigPath) == "" {
		opts.ConfigPath = "localclash-subscriptions.yaml"
	}
	return opts
}

func normalizeRefreshOptions(opts RefreshOptions) RefreshOptions {
	if strings.TrimSpace(opts.ConfigPath) == "" {
		opts.ConfigPath = "localclash-subscriptions.yaml"
	}
	if strings.TrimSpace(opts.RuntimeDir) == "" {
		opts.RuntimeDir = ".runtime/subscriptions"
	}
	if strings.TrimSpace(opts.MergedPath) == "" {
		opts.MergedPath = "subscription.yaml"
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
	if err := yaml.Unmarshal(data, &config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func writeConfig(path string, config Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func normalizeSources(sources []Source) ([]Source, error) {
	normalized := make([]Source, 0, len(sources))
	seenCanonicalURL := map[string]bool{}
	usedIDs := map[string]bool{}
	canonicalURLs := make([]string, 0, len(sources))
	for i, source := range sources {
		rawURL := strings.TrimSpace(source.URL)
		canonicalURL, err := canonicalSubscriptionURL(rawURL)
		if err != nil {
			return nil, fmt.Errorf("sources[%d] %w", i, err)
		}
		if seenCanonicalURL[canonicalURL] {
			return nil, fmt.Errorf("duplicate subscription URL at sources[%d]", i)
		}
		seenCanonicalURL[canonicalURL] = true
		canonicalURLs = append(canonicalURLs, canonicalURL)
	}
	for i, source := range sources {
		id := sourceIDFromCanonicalURL(canonicalURLs[i], usedIDs)
		usedIDs[id] = true
		normalized = append(normalized, Source{
			ID:  id,
			URL: strings.TrimSpace(source.URL),
		})
	}
	return normalized, nil
}

func canonicalSubscriptionURL(rawURL string) (string, error) {
	if rawURL == "" {
		return "", fmt.Errorf("url is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("url is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("url must use http or https")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("url must include a host")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	return parsed.String(), nil
}

func sourceIDFromCanonicalURL(canonicalURL string, used map[string]bool) string {
	sum := sha256.Sum256([]byte(canonicalURL))
	encoded := hex.EncodeToString(sum[:])
	for length := sourceIDHashLength; length <= len(encoded); length += 2 {
		id := sourceIDPrefix + encoded[:length]
		if !used[id] {
			return id
		}
	}
	return sourceIDPrefix + encoded
}

func validateSource(source Source) error {
	if source.ID == "" {
		return fmt.Errorf("source id is required")
	}
	if !sourceIDPattern.MatchString(source.ID) {
		return fmt.Errorf("source id %q is invalid; use only letters, digits, underscore, and hyphen", source.ID)
	}
	if source.URL == "" {
		return fmt.Errorf("source %q url is required", source.ID)
	}
	parsed, err := url.Parse(source.URL)
	if err != nil {
		return fmt.Errorf("source %q url is invalid", source.ID)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("source %q url must use http or https", source.ID)
	}
	if parsed.Host == "" {
		return fmt.Errorf("source %q url must include a host", source.ID)
	}
	return nil
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

func fetchSource(ctx context.Context, source Source, userAgent string) (subscriptionDoc, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
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
	doc, err := parseSubscription(source.ID, body)
	if err != nil {
		return subscriptionDoc{}, err
	}
	doc.Raw = append([]byte(nil), body...)
	return doc, nil
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
	return subscriptionDoc{Data: doc}, nil
}

func readSubscription(path string) (subscriptionDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return subscriptionDoc{}, err
	}
	doc, err := parseSubscription(filepath.Base(path), data)
	if err != nil {
		return subscriptionDoc{}, err
	}
	doc.Raw = append([]byte(nil), data...)
	return doc, nil
}

func writeRawSubscriptionArtifact(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func writeSubscriptionArtifact(path string, doc map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
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
				name = "[" + source.ID + "] " + name
			}
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
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "<invalid-url>"
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

func artifactPath(runtimeDir, id string) string {
	return filepath.Join(runtimeDir, id+".yaml")
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
