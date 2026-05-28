package dashboard

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Options struct {
	Version   string
	AssetName string
	OutputDir string
	Repo      string
	Force     bool
}

type Result struct {
	Version   string
	AssetName string
	OutputDir string
}

type release struct {
	TagName string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

const defaultGitHubReleaseMirrors = "https://gh-proxy.com/https://github.com https://ghproxy.imciel.com/https://github.com https://gitproxy.mrhjx.cn/https://github.com https://gh.jasonzeng.dev/https://github.com https://gh.monlor.com/https://github.com https://gh.noki.icu/https://github.com https://ghfast.top/https://github.com"

func Download(ctx context.Context, opts Options) (Result, error) {
	opts = normalizeOptions(opts)
	if err := opts.validate(); err != nil {
		return Result{}, err
	}

	if opts.Version == "latest" {
		result, err := downloadDirectLatest(ctx, opts)
		if err == nil {
			return result, nil
		}
		fmt.Fprintf(os.Stderr, "download: dashboard latest asset direct path failed, falling back to github api: %v\n", err)
	}

	rel, err := fetchRelease(ctx, opts.Repo, opts.Version)
	if err != nil {
		return Result{}, err
	}
	selected, err := selectAsset(rel.Assets, opts.AssetName)
	if err != nil {
		return Result{}, err
	}

	if err := prepareOutputDir(opts.OutputDir, opts.Force); err != nil {
		return Result{}, err
	}

	tmpZip, err := downloadAsset(ctx, selected.BrowserDownloadURL)
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(tmpZip)

	if err := extractZip(tmpZip, opts.OutputDir); err != nil {
		return Result{}, err
	}
	if err := verifyDashboard(opts.OutputDir); err != nil {
		return Result{}, err
	}

	return Result{Version: rel.TagName, AssetName: selected.Name, OutputDir: opts.OutputDir}, nil
}

func downloadDirectLatest(ctx context.Context, opts Options) (Result, error) {
	if err := prepareOutputDir(opts.OutputDir, opts.Force); err != nil {
		return Result{}, err
	}
	url := fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", opts.Repo, opts.AssetName)
	tmpZip, err := downloadAsset(ctx, url)
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(tmpZip)
	if err := extractZip(tmpZip, opts.OutputDir); err != nil {
		return Result{}, err
	}
	if err := verifyDashboard(opts.OutputDir); err != nil {
		return Result{}, err
	}
	return Result{Version: "latest", AssetName: opts.AssetName, OutputDir: opts.OutputDir}, nil
}

func normalizeOptions(opts Options) Options {
	opts.Version = strings.TrimSpace(opts.Version)
	opts.AssetName = strings.TrimSpace(opts.AssetName)
	opts.OutputDir = strings.TrimSpace(opts.OutputDir)
	opts.Repo = strings.TrimSpace(opts.Repo)
	if opts.Version == "" {
		opts.Version = "latest"
	}
	if opts.AssetName == "" {
		opts.AssetName = "dist.zip"
	}
	if opts.OutputDir == "" {
		opts.OutputDir = ".runtime/mihomo/ui/zashboard"
	}
	if opts.Repo == "" {
		opts.Repo = "Zephyruso/zashboard"
	}
	return opts
}

func (opts Options) validate() error {
	if !strings.Contains(opts.Repo, "/") {
		return fmt.Errorf("repo must be owner/name, got %q", opts.Repo)
	}
	if opts.OutputDir == "." || opts.OutputDir == string(filepath.Separator) {
		return fmt.Errorf("output directory %q is too broad", opts.OutputDir)
	}
	if filepath.Clean(opts.OutputDir) == filepath.Clean(".runtime") {
		return fmt.Errorf("output directory %q is too broad", opts.OutputDir)
	}
	return nil
}

func fetchRelease(ctx context.Context, repo, version string) (release, error) {
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	if version != "latest" {
		endpoint = fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", repo, version)
	}
	var lastErr error
	for _, candidate := range downloadCandidates(endpoint) {
		rel, err := fetchReleaseURL(ctx, candidate)
		if err == nil {
			return rel, nil
		}
		lastErr = err
		fmt.Fprintf(os.Stderr, "download: dashboard release metadata failed from %s: %v\n", candidate, err)
	}
	return release{}, lastErr
}

func fetchReleaseURL(ctx context.Context, endpoint string) (release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "localclash-dashboard-downloader")
	fmt.Fprintf(os.Stderr, "download: requesting dashboard release metadata %s\n", endpoint)

	resp, err := httpClient().Do(req)
	if err != nil {
		return release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return release{}, fmt.Errorf("github release request failed: %s: %s", resp.Status, shortHTTPBody(body))
	}
	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return release{}, err
	}
	if rel.TagName == "" {
		return release{}, errors.New("github release response did not include tag_name")
	}
	return rel, nil
}

func selectAsset(assets []asset, name string) (asset, error) {
	for _, candidate := range assets {
		if candidate.Name == name {
			return candidate, nil
		}
	}
	return asset{}, fmt.Errorf("release asset %q not found", name)
}

func prepareOutputDir(path string, force bool) error {
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("output path %q exists and is not a directory", path)
		}
		if !force {
			return fmt.Errorf("output directory %q already exists; pass --force to replace it", path)
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.MkdirAll(path, 0o755)
}

func downloadAsset(ctx context.Context, url string) (string, error) {
	var lastErr error
	for _, candidate := range downloadCandidates(url) {
		tmp, err := downloadAssetURL(ctx, candidate)
		if err == nil {
			return tmp, nil
		}
		lastErr = err
		fmt.Fprintf(os.Stderr, "download: dashboard asset failed from %s: %v\n", candidate, err)
	}
	return "", lastErr
}

func downloadAssetURL(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "localclash-dashboard-downloader")
	fmt.Fprintf(os.Stderr, "download: requesting dashboard asset %s\n", url)

	resp, err := httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download failed: %s", resp.Status)
	}

	tmp, err := os.CreateTemp("", "localclash-zashboard-*.zip")
	if err != nil {
		return "", err
	}
	defer tmp.Close()
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

func downloadCandidates(url string) []string {
	if mirrorModeDisabled() {
		return []string{url}
	}
	candidates := make([]string, 0, 6)
	candidates = append(candidates, mirroredURLs(url)...)
	candidates = append(candidates, url)
	return uniqueStrings(candidates)
}

func mirrorModeDisabled() bool {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("LOCALCLASH_GITHUB_MIRROR")))
	return mode == "off" || mode == "none" || mode == "direct"
}

func mirroredURLs(url string) []string {
	switch {
	case strings.HasPrefix(url, "https://github.com/"):
		return mirrorByPrefix(url, "https://github.com", envWords("LOCALCLASH_GITHUB_RELEASE_MIRRORS", defaultGitHubReleaseMirrors))
	case strings.HasPrefix(url, "https://api.github.com/"):
		return mirrorByPrefix(url, "https://api.github.com", envWords("LOCALCLASH_GITHUB_API_MIRRORS", strings.ReplaceAll(defaultGitHubReleaseMirrors, "https://github.com", "https://api.github.com")))
	default:
		return nil
	}
}

func mirrorByPrefix(url, upstreamPrefix string, mirrors []string) []string {
	out := make([]string, 0, len(mirrors))
	suffix := strings.TrimPrefix(url, upstreamPrefix)
	for _, mirror := range mirrors {
		mirror = strings.TrimRight(strings.TrimSpace(mirror), "/")
		if mirror == "" {
			continue
		}
		out = append(out, mirror+suffix)
	}
	return out
}

func envWords(name, fallback string) []string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		value = fallback
	}
	return strings.Fields(value)
}

func httpClient() *http.Client {
	return &http.Client{Timeout: httpTimeout()}
}

func httpTimeout() time.Duration {
	value := strings.TrimSpace(os.Getenv("LOCALCLASH_HTTP_TIMEOUT"))
	if value == "" {
		return 45 * time.Second
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return 45 * time.Second
	}
	return time.Duration(seconds) * time.Second
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func shortHTTPBody(body []byte) string {
	text := strings.Join(strings.Fields(strings.TrimSpace(string(body))), " ")
	if len(text) > 240 {
		return text[:240] + "..."
	}
	return text
}

func extractZip(zipPath, outputDir string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	stripPrefix := singleTopLevelDir(reader.File)
	for _, file := range reader.File {
		if unsafeZipName(file.Name) {
			return fmt.Errorf("zip entry %q is unsafe", file.Name)
		}
		name := strings.TrimPrefix(file.Name, stripPrefix)
		if name == "" {
			continue
		}
		targetPath, err := safeJoin(outputDir, name)
		if err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, file.FileInfo().Mode())
		if err != nil {
			src.Close()
			return err
		}
		_, copyErr := io.Copy(dst, src)
		closeSrcErr := src.Close()
		closeDstErr := dst.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeSrcErr != nil {
			return closeSrcErr
		}
		if closeDstErr != nil {
			return closeDstErr
		}
	}
	return nil
}

func unsafeZipName(name string) bool {
	if filepath.IsAbs(name) {
		return true
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func singleTopLevelDir(files []*zip.File) string {
	var top string
	for _, file := range files {
		name := strings.TrimLeft(file.Name, "/")
		if name == "" {
			continue
		}
		parts := strings.SplitN(name, "/", 2)
		if len(parts) < 2 {
			return ""
		}
		if top == "" {
			top = parts[0]
			continue
		}
		if parts[0] != top {
			return ""
		}
	}
	if top == "" {
		return ""
	}
	return top + "/"
}

func safeJoin(base, name string) (string, error) {
	target := filepath.Join(base, name)
	cleanBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	cleanTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if cleanTarget != cleanBase && !strings.HasPrefix(cleanTarget, cleanBase+string(filepath.Separator)) {
		return "", fmt.Errorf("zip entry %q escapes output directory", name)
	}
	return target, nil
}

func verifyDashboard(outputDir string) error {
	if _, err := os.Stat(filepath.Join(outputDir, "index.html")); err != nil {
		return fmt.Errorf("dashboard index.html not found after extraction: %w", err)
	}
	return nil
}
