package coredownload

import (
	"archive/tar"
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
	Version     string
	Target      string
	TargetOS    string
	TargetArch  string
	OutputPath  string
	OutputDir   string
	Repo        string
	SmartBranch string
	Flavor      string
	Force       bool
	DryRun      bool
}

type Result struct {
	Version     string
	AssetName   string
	DownloadURL string
	OutputPath  string
	Flavor      string
	Target      string
}

type release struct {
	TagName string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

const (
	FlavorAll    = "all"
	FlavorMeta   = "meta"
	FlavorSmart  = "smart"
	TargetHost   = "host"
	TargetRouter = "router"
)

func Download(ctx context.Context, opts Options) ([]Result, error) {
	opts = normalizeOptions(opts)
	if err := opts.validate(); err != nil {
		return nil, err
	}
	flavors := effectiveFlavors(opts)
	results := make([]Result, 0, len(flavors))
	for _, flavor := range flavors {
		result, err := downloadFlavor(ctx, opts, flavor)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func downloadFlavor(ctx context.Context, opts Options, flavor string) (Result, error) {
	if flavor == FlavorSmart {
		return downloadSmart(ctx, opts)
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
		OutputPath:  outputPath(opts, flavor),
		Flavor:      flavor,
		Target:      opts.Target,
	}
	if opts.DryRun {
		return result, nil
	}

	if err := ensureWritableOutput(result.OutputPath, opts.Force); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(result.OutputPath), 0o755); err != nil {
		return Result{}, err
	}

	tmpPath := result.OutputPath + ".download"
	defer os.Remove(tmpPath)

	if err := downloadAsset(ctx, selected.BrowserDownloadURL, selected.Name, tmpPath); err != nil {
		return Result{}, err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return Result{}, err
	}
	if err := os.Rename(tmpPath, result.OutputPath); err != nil {
		return Result{}, err
	}

	return result, nil
}

func downloadSmart(ctx context.Context, opts Options) (Result, error) {
	name, err := openClashCoreAssetName(opts.TargetOS, opts.TargetArch)
	if err != nil {
		return Result{}, err
	}
	version, err := fetchOpenClashSmartVersion(ctx, opts.SmartBranch)
	if err != nil {
		return Result{}, err
	}
	url := fmt.Sprintf("https://raw.githubusercontent.com/vernesong/OpenClash/core/%s/smart/%s", opts.SmartBranch, name)
	result := Result{
		Version:     version,
		AssetName:   name,
		DownloadURL: url,
		OutputPath:  outputPath(opts, FlavorSmart),
		Flavor:      FlavorSmart,
		Target:      opts.Target,
	}
	if opts.DryRun {
		return result, nil
	}
	if err := ensureWritableOutput(result.OutputPath, opts.Force); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(result.OutputPath), 0o755); err != nil {
		return Result{}, err
	}
	tmpPath := result.OutputPath + ".download"
	defer os.Remove(tmpPath)
	if err := downloadAsset(ctx, url, name, tmpPath); err != nil {
		return Result{}, err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return Result{}, err
	}
	if err := os.Rename(tmpPath, result.OutputPath); err != nil {
		return Result{}, err
	}
	return result, nil
}

func normalizeOptions(opts Options) Options {
	if opts.Version == "" {
		opts.Version = "latest"
	}
	if opts.Target == "" {
		opts.Target = TargetHost
	}
	opts.Target = strings.ToLower(strings.TrimSpace(opts.Target))
	if opts.TargetOS == "" {
		if opts.Target == TargetRouter {
			opts.TargetOS = "linux"
		} else {
			opts.TargetOS = runtime.GOOS
		}
	}
	if opts.TargetArch == "" {
		opts.TargetArch = runtime.GOARCH
	}
	if opts.Flavor == "" {
		opts.Flavor = FlavorAll
	}
	opts.Flavor = strings.ToLower(strings.TrimSpace(opts.Flavor))
	opts.TargetOS = strings.ToLower(opts.TargetOS)
	opts.TargetArch = normalizeArch(opts.TargetArch)
	if opts.OutputDir == "" {
		opts.OutputDir = "bin"
	}
	if opts.Repo == "" {
		opts.Repo = "MetaCubeX/mihomo"
	}
	if opts.SmartBranch == "" {
		opts.SmartBranch = "master"
	}
	return opts
}

func (opts Options) validate() error {
	if !strings.Contains(opts.Repo, "/") {
		return fmt.Errorf("repo must be owner/name, got %q", opts.Repo)
	}
	switch opts.Flavor {
	case FlavorAll, FlavorMeta, FlavorSmart:
	default:
		return fmt.Errorf("flavor must be %q, %q, or %q, got %q", FlavorAll, FlavorMeta, FlavorSmart, opts.Flavor)
	}
	switch opts.Target {
	case TargetHost, TargetRouter:
	default:
		return fmt.Errorf("target must be %q or %q, got %q", TargetHost, TargetRouter, opts.Target)
	}
	if opts.Target == TargetRouter && opts.TargetOS != "linux" {
		return fmt.Errorf("router target requires linux OS, got %s/%s", opts.TargetOS, opts.TargetArch)
	}
	if opts.Flavor == FlavorSmart && opts.TargetOS != "linux" {
		return fmt.Errorf("smart core is available only for linux/router targets, got %s/%s; use --target router --arch %s", opts.TargetOS, opts.TargetArch, opts.TargetArch)
	}
	if opts.Flavor == FlavorAll && strings.TrimSpace(opts.OutputPath) != "" {
		return errors.New("output path is only valid with --flavor meta or --flavor smart; use --output-dir with --flavor all")
	}
	if opts.Flavor != FlavorAll && opts.OutputPath == "." {
		return errors.New("output path must be a file path")
	}
	if opts.OutputDir == "." || strings.TrimSpace(opts.OutputDir) == "" {
		return errors.New("output dir must be a directory path")
	}
	return nil
}

func outputPath(opts Options, flavor string) string {
	if opts.OutputPath != "" && opts.Flavor != FlavorAll {
		return opts.OutputPath
	}
	name := "mihomo-" + flavor
	if opts.TargetOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(opts.OutputDir, opts.TargetOS+"-"+opts.TargetArch, name)
}

func effectiveFlavors(opts Options) []string {
	if opts.Flavor != FlavorAll {
		return []string{opts.Flavor}
	}
	if opts.Target == TargetRouter {
		return []string{FlavorMeta, FlavorSmart}
	}
	return []string{FlavorMeta}
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

func fetchOpenClashSmartVersion(ctx context.Context, branch string) (string, error) {
	endpoint := fmt.Sprintf("https://raw.githubusercontent.com/vernesong/OpenClash/core/%s/core_version", branch)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "localclash-core-downloader")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("openclash core version request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[1]) == "" {
		return "", errors.New("openclash core_version did not include smart version")
	}
	return strings.TrimSpace(lines[1]), nil
}

func openClashCoreAssetName(targetOS, targetArch string) (string, error) {
	if targetOS != "linux" {
		return "", fmt.Errorf("smart core is available from OpenClash only for linux targets, got %s/%s", targetOS, targetArch)
	}
	switch normalizeArch(targetArch) {
	case "amd64", "386", "arm64", "armv5", "armv6", "armv7", "loong64-abi1", "loong64-abi2", "mips64", "mips64le", "riscv64", "s390x":
		return fmt.Sprintf("clash-linux-%s.tar.gz", normalizeArch(targetArch)), nil
	case "mips", "mipsle":
		return fmt.Sprintf("clash-linux-%s-softfloat.tar.gz", normalizeArch(targetArch)), nil
	default:
		return "", fmt.Errorf("unsupported OpenClash smart core arch %q", targetArch)
	}
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
	case strings.HasSuffix(name, ".tar.gz"):
		return extractFirstTarGzFile(resp.Body, out)
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

func extractFirstTarGzFile(in io.Reader, out io.Writer) error {
	gz, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return errors.New("tar.gz archive did not contain a file")
		}
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeReg {
			_, err = io.Copy(out, tr)
			return err
		}
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
