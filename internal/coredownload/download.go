package coredownload

import (
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Options struct {
	Version    string
	TargetOS   string
	TargetArch string
	OutputPath string
	Repo       string
	Force      bool
	DryRun     bool
}

type Result struct {
	Version     string
	AssetName   string
	DownloadURL string
	OutputPath  string
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

	selected, err := selectAsset(rel.Assets, opts.TargetOS, opts.TargetArch)
	if err != nil {
		return Result{}, fmt.Errorf("%w for %s/%s in %s", err, opts.TargetOS, opts.TargetArch, rel.TagName)
	}

	result := Result{
		Version:     rel.TagName,
		AssetName:   selected.Name,
		DownloadURL: selected.BrowserDownloadURL,
		OutputPath:  opts.OutputPath,
	}
	if opts.DryRun {
		return result, nil
	}

	if err := ensureWritableOutput(opts.OutputPath, opts.Force); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(opts.OutputPath), 0o755); err != nil {
		return Result{}, err
	}

	tmpPath := opts.OutputPath + ".download"
	defer os.Remove(tmpPath)

	if err := downloadAsset(ctx, selected.BrowserDownloadURL, selected.Name, tmpPath); err != nil {
		return Result{}, err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return Result{}, err
	}
	if err := os.Rename(tmpPath, opts.OutputPath); err != nil {
		return Result{}, err
	}

	return result, nil
}

func normalizeOptions(opts Options) Options {
	if opts.Version == "" {
		opts.Version = "latest"
	}
	if opts.TargetOS == "" {
		opts.TargetOS = runtime.GOOS
	}
	if opts.TargetArch == "" {
		opts.TargetArch = runtime.GOARCH
	}
	opts.TargetOS = strings.ToLower(opts.TargetOS)
	opts.TargetArch = normalizeArch(opts.TargetArch)
	if opts.OutputPath == "" {
		opts.OutputPath = "bin/mihomo"
		if opts.TargetOS == "windows" {
			opts.OutputPath += ".exe"
		}
	}
	if opts.Repo == "" {
		opts.Repo = "MetaCubeX/mihomo"
	}
	return opts
}

func (opts Options) validate() error {
	if !strings.Contains(opts.Repo, "/") {
		return fmt.Errorf("repo must be owner/name, got %q", opts.Repo)
	}
	if opts.OutputPath == "." || strings.TrimSpace(opts.OutputPath) == "" {
		return errors.New("output path must be a file path")
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
	req.Header.Set("User-Agent", "localclash-core-downloader")

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

func selectAsset(assets []asset, targetOS, targetArch string) (asset, error) {
	targetArch = normalizeArch(targetArch)
	exact := fmt.Sprintf("mihomo-%s-%s-", targetOS, targetArch)
	for _, candidate := range assets {
		name := candidate.Name
		if strings.HasPrefix(name, exact) && (strings.HasSuffix(name, ".gz") || strings.HasSuffix(name, ".zip")) {
			if isSpecialVariant(name, targetOS, targetArch) {
				continue
			}
			return candidate, nil
		}
	}
	return asset{}, errors.New("no matching mihomo release asset")
}

func normalizeArch(arch string) string {
	switch strings.ToLower(arch) {
	case "aarch64":
		return "arm64"
	case "x86_64", "x64":
		return "amd64"
	default:
		return strings.ToLower(arch)
	}
}

func isSpecialVariant(name, targetOS, targetArch string) bool {
	prefix := fmt.Sprintf("mihomo-%s-%s-", targetOS, targetArch)
	remainder := strings.TrimPrefix(name, prefix)
	for _, marker := range []string{"compatible-", "softfloat-", "hardfloat-", "go120-", "go121-", "go122-", "go123-", "go124-", "go125-"} {
		if strings.HasPrefix(remainder, marker) {
			return true
		}
	}
	if targetArch == "amd64" {
		for _, marker := range []string{"v1-", "v2-", "v3-"} {
			if strings.HasPrefix(remainder, marker) {
				return true
			}
		}
	}
	return false
}

func ensureWritableOutput(path string, force bool) error {
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("output path %q is a directory", path)
		}
		if !force {
			return fmt.Errorf("output path %q already exists; pass --force to overwrite", path)
		}
		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func downloadAsset(ctx context.Context, url, name, outputPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "localclash-core-downloader")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	out, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()

	switch {
	case strings.HasSuffix(name, ".gz"):
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return err
		}
		defer gz.Close()
		_, err = io.Copy(out, gz)
		return err
	case strings.HasSuffix(name, ".zip"):
		return extractFirstZipFile(resp.Body, out)
	default:
		_, err = io.Copy(out, resp.Body)
		return err
	}
}

func extractFirstZipFile(r io.Reader, out io.Writer) error {
	tmp, err := os.CreateTemp("", "localclash-mihomo-*.zip")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	zr, err := zip.OpenReader(tmpPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, rc)
		closeErr := rc.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	return errors.New("zip asset did not contain a file")
}
