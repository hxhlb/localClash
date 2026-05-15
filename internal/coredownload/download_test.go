package coredownload

import "testing"

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
	if got.OutputPath == "" {
		t.Fatal("OutputPath should default to a binary path")
	}
}

func TestNormalizeOptionsUsesWindowsExeDefault(t *testing.T) {
	got := normalizeOptions(Options{TargetOS: "Windows", TargetArch: "amd64"})
	if got.OutputPath != "bin/mihomo.exe" {
		t.Fatalf("OutputPath = %q, want bin/mihomo.exe", got.OutputPath)
	}
}
