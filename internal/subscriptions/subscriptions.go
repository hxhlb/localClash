package subscriptions

import (
	"bytes"
	"context"
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
	"time"

	"gopkg.in/yaml.v3"
)

const DefaultUserAgent = "clash-verge/v1.5.1"

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
	Sources    []Source
	Replace    *bool
}

type RefreshOptions struct {
	ConfigPath string
	IDs        []string
	RuntimeDir string
	MergedPath string
	Force      bool
	UserAgent  string
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
}

type RefreshSourceSummary struct {
	ID               string `json:"id"`
	Artifact         string `json:"artifact"`
	ProxiesCount     int    `json:"proxies_count,omitempty"`
	ProxyGroupsCount int    `json:"proxy_groups_count,omitempty"`
	RulesCount       int    `json:"rules_count,omitempty"`
	Status           string `json:"status"`
}

type subscriptionDoc struct {
	Data map[string]any
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
	if len(opts.Sources) == 0 {
		return ConfigureResult{}, fmt.Errorf("sources is required")
	}
	seen := map[string]bool{}
	for i := range opts.Sources {
		opts.Sources[i].ID = strings.TrimSpace(opts.Sources[i].ID)
		opts.Sources[i].URL = strings.TrimSpace(opts.Sources[i].URL)
		if err := validateSource(opts.Sources[i]); err != nil {
			return ConfigureResult{}, err
		}
		if seen[opts.Sources[i].ID] {
			return ConfigureResult{}, fmt.Errorf("duplicate subscription source id %q", opts.Sources[i].ID)
		}
		seen[opts.Sources[i].ID] = true
	}
	config := Config{Version: 1, Sources: opts.Sources}
	if err := writeConfig(opts.ConfigPath, config); err != nil {
		return ConfigureResult{}, err
	}
	result := ConfigureResult{
		Config:     opts.ConfigPath,
		Configured: true,
		Message:    "Subscription sources configured. Run subscriptions_refresh to update local artifacts.",
	}
	for _, source := range opts.Sources {
		result.Sources = append(result.Sources, ConfiguredSource{ID: source.ID, URL: MaskURL(source.URL)})
	}
	return result, nil
}

func Refresh(ctx context.Context, opts RefreshOptions) (RefreshResult, error) {
	opts = normalizeRefreshOptions(opts)
	config, err := readConfig(opts.ConfigPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return RefreshResult{}, fmt.Errorf("subscription sources are not configured; run subscriptions_configure first")
		}
		return RefreshResult{}, err
	}
	if len(config.Sources) == 0 {
		return RefreshResult{}, fmt.Errorf("subscription sources are empty; run subscriptions_configure first")
	}
	for _, source := range config.Sources {
		if err := validateSource(source); err != nil {
			return RefreshResult{}, err
		}
	}
	selected, err := selectedSourceIDs(config.Sources, opts.IDs)
	if err != nil {
		return RefreshResult{}, err
	}
	if err := os.MkdirAll(opts.RuntimeDir, 0o755); err != nil {
		return RefreshResult{}, err
	}

	refreshed := map[string]bool{}
	for _, source := range config.Sources {
		if !selected[source.ID] {
			continue
		}
		doc, err := fetchSource(ctx, source, opts.UserAgent)
		if err != nil {
			return RefreshResult{}, err
		}
		if err := writeSubscriptionArtifact(artifactPath(opts.RuntimeDir, source.ID), doc.Data); err != nil {
			return RefreshResult{}, err
		}
		refreshed[source.ID] = true
	}

	docs := map[string]subscriptionDoc{}
	result := RefreshResult{Refreshed: true, Warnings: []string{}}
	for _, source := range config.Sources {
		path := artifactPath(opts.RuntimeDir, source.ID)
		doc, err := readSubscription(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				if selected[source.ID] {
					return RefreshResult{}, fmt.Errorf("source %q artifact was not written", source.ID)
				}
				result.Warnings = append(result.Warnings, fmt.Sprintf("source %q has no local artifact; run subscriptions_refresh for that source", source.ID))
				continue
			}
			return RefreshResult{}, err
		}
		docs[source.ID] = doc
		summary := summarizeMap(doc.Data)
		status := "existing"
		if refreshed[source.ID] {
			status = "ok"
		}
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
		return RefreshResult{}, fmt.Errorf("no subscription artifacts are available to merge")
	}
	merged, renamed, err := mergeSubscriptions(config.Sources, docs)
	if err != nil {
		return RefreshResult{}, err
	}
	if err := writeSubscriptionArtifact(opts.MergedPath, merged); err != nil {
		return RefreshResult{}, err
	}
	result.Merged = summarizeArtifact(opts.MergedPath)
	result.Merged.RenamedProxiesCount = renamed
	sort.Strings(result.Warnings)
	return result, nil
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
	return parseSubscription(source.ID, body)
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
	return parseSubscription(filepath.Base(path), data)
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
	nameCounts := map[string]int{}
	for _, doc := range docs {
		for _, rawProxy := range anySlice(doc.Data["proxies"]) {
			proxy := rawProxy.(map[string]any)
			nameCounts[stringValue(proxy["name"])]++
		}
	}
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
			name := stringValue(proxy["name"])
			if nameCounts[name] > 1 {
				name = "[" + source.ID + "] " + name
				renamed++
			}
			name = uniqueProxyName(name, usedNames)
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
