package coredownload

import (
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestSelectAssetPrefersDefaultVariant(t *testing.T) {
	assets := []asset{
		{Name: "mihomo-darwin-arm64-go124-v1.19.24.gz"},
		{Name: "mihomo-darwin-arm64-v1.19.24.gz", BrowserDownloadURL: "https://example.test/darwin-arm64"},
	}

	got, err := selectAsset(assets, "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if got.BrowserDownloadURL != "https://example.test/darwin-arm64" {
		t.Fatalf("selected %q, want default darwin arm64 asset", got.Name)
	}
}

func TestSelectAssetPrefersCompatibleLinuxAMD64(t *testing.T) {
	assets := []asset{
		{Name: "mihomo-linux-amd64-v3-v1.19.25.gz", BrowserDownloadURL: "https://example.test/linux-amd64-v3"},
		{Name: "mihomo-linux-amd64-v1.19.25.gz", BrowserDownloadURL: "https://example.test/linux-amd64"},
		{Name: "mihomo-linux-amd64-compatible-v1.19.25.gz", BrowserDownloadURL: "https://example.test/linux-amd64-compatible"},
	}

	got, err := selectAsset(assets, "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if got.BrowserDownloadURL != "https://example.test/linux-amd64-compatible" {
		t.Fatalf("selected %q, want compatible linux amd64 asset", got.Name)
	}
}

func TestSelectAssetNormalizesAarch64(t *testing.T) {
	assets := []asset{
		{Name: "mihomo-linux-arm64-v1.19.24.gz", BrowserDownloadURL: "https://example.test/linux-arm64"},
	}

	got, err := selectAsset(assets, "linux", "aarch64")
	if err != nil {
		t.Fatal(err)
	}
	if got.BrowserDownloadURL != "https://example.test/linux-arm64" {
		t.Fatalf("selected %q, want linux arm64 asset", got.Name)
	}
}

func TestNormalizeOptionsUsesCurrentPlatformDefaults(t *testing.T) {
	got := normalizeOptions(Options{})
	if got.TargetOS == "" {
		t.Fatal("TargetOS should default to runtime OS")
	}
	if got.TargetArch == "" {
		t.Fatal("TargetArch should default to runtime arch")
	}
	if got.OutputDir != "bin" {
		t.Fatalf("OutputDir = %q, want bin", got.OutputDir)
	}
	if got.Flavor != FlavorAll {
		t.Fatalf("Flavor = %q, want all", got.Flavor)
	}
	if got.Target != TargetHost {
		t.Fatalf("Target = %q, want host", got.Target)
	}
}

func TestNormalizeOptionsUsesWindowsExeDefault(t *testing.T) {
	got := normalizeOptions(Options{TargetOS: "Windows", TargetArch: "amd64", Flavor: FlavorMeta})
	want := filepath.Join("bin", "windows-amd64", "mihomo-meta.exe")
	if path := outputPath(got, FlavorMeta); path != want {
		t.Fatalf("OutputPath = %q, want %q", path, want)
	}
}

func TestOutputPathUsesPlatformFlavorNames(t *testing.T) {
	opts := normalizeOptions(Options{TargetOS: "linux", TargetArch: "arm64"})
	if got := outputPath(opts, FlavorMeta); got != filepath.Join("bin", "linux-arm64", "mihomo-meta") {
		t.Fatalf("meta output path = %q", got)
	}
	if got := outputPath(opts, FlavorSmart); got != filepath.Join("bin", "linux-arm64", "mihomo-smart") {
		t.Fatalf("smart output path = %q", got)
	}
}

func TestEffectiveFlavorsDefaultsHostToMetaOnly(t *testing.T) {
	opts := normalizeOptions(Options{Target: TargetHost, TargetOS: "darwin", TargetArch: "arm64"})
	got := effectiveFlavors(opts)
	if len(got) != 1 || got[0] != FlavorMeta {
		t.Fatalf("flavors = %+v, want host meta only", got)
	}
}

func TestEffectiveFlavorsDefaultsRouterToMetaAndSmart(t *testing.T) {
	opts := normalizeOptions(Options{Target: TargetRouter, TargetArch: "arm64"})
	got := effectiveFlavors(opts)
	if opts.TargetOS != "linux" {
		t.Fatalf("router OS = %q, want linux", opts.TargetOS)
	}
	if len(got) != 2 || got[0] != FlavorMeta || got[1] != FlavorSmart {
		t.Fatalf("flavors = %+v, want router meta and smart", got)
	}
}

func TestOpenClashCoreAssetNameRejectsNonLinuxSmart(t *testing.T) {
	if _, err := openClashCoreAssetName("darwin", "arm64"); err == nil {
		t.Fatal("expected non-linux smart core error")
	}
}

func TestOpenClashCoreAssetNameUsesLinuxArm64(t *testing.T) {
	got, err := openClashCoreAssetName("linux", "aarch64")
	if err != nil {
		t.Fatal(err)
	}
	if got != "clash-linux-arm64.tar.gz" {
		t.Fatalf("asset = %q, want clash-linux-arm64.tar.gz", got)
	}
}

func TestValidateRejectsHostSmartOnNonLinux(t *testing.T) {
	opts := normalizeOptions(Options{Flavor: FlavorSmart, TargetOS: "darwin", TargetArch: "arm64"})
	if err := opts.validate(); err == nil {
		t.Fatal("expected non-linux smart validation error")
	}
}

func TestDefaultHostOutputPathIncludesCurrentPlatform(t *testing.T) {
	opts := normalizeOptions(Options{})
	want := filepath.Join("bin", runtime.GOOS+"-"+runtime.GOARCH, "mihomo-meta")
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	if got := outputPath(opts, FlavorMeta); got != want {
		t.Fatalf("host output path = %q, want %q", got, want)
	}
}

func TestDownloadCandidatesMirrorsGitHubReleaseBeforeDirect(t *testing.T) {
	t.Setenv("LOCALCLASH_GITHUB_RELEASE_MIRRORS", "https://mirror.example/https://github.com")
	t.Setenv("LOCALCLASH_GITHUB_MIRROR", "")

	got := downloadCandidates("https://github.com/MetaCubeX/mihomo/releases/download/v1/mihomo.gz")
	want := []string{
		"https://mirror.example/https://github.com/MetaCubeX/mihomo/releases/download/v1/mihomo.gz",
		"https://github.com/MetaCubeX/mihomo/releases/download/v1/mihomo.gz",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
}

func TestDownloadCandidatesCanDisableMirrors(t *testing.T) {
	t.Setenv("LOCALCLASH_GITHUB_RELEASE_MIRRORS", "https://mirror.example/https://github.com")
	t.Setenv("LOCALCLASH_GITHUB_MIRROR", "direct")

	got := downloadCandidates("https://github.com/MetaCubeX/mihomo/releases/download/v1/mihomo.gz")
	want := []string{"https://github.com/MetaCubeX/mihomo/releases/download/v1/mihomo.gz"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
}

func TestRawMirrorCandidatesIncludeJsdelivrShape(t *testing.T) {
	t.Setenv("LOCALCLASH_GITHUB_RAW_MIRRORS", "https://fastly.jsdelivr.net/gh")
	t.Setenv("LOCALCLASH_GITHUB_MIRROR", "")

	got := downloadCandidates("https://raw.githubusercontent.com/vernesong/OpenClash/core/master/core_version")
	want := []string{
		"https://fastly.jsdelivr.net/gh/vernesong/OpenClash@core/master/core_version",
		"https://raw.githubusercontent.com/vernesong/OpenClash/core/master/core_version",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
}
