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
	"strings"
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

func Download(ctx context.Context, opts Options) (Result, error) {
	opts = normalizeOptions(opts)
	if err := opts.validate(); err != nil {
		return Result{}, err
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "localclash-dashboard-downloader")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return release{}, fmt.Errorf("github release request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "localclash-dashboard-downloader")

	resp, err := http.DefaultClient.Do(req)
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
