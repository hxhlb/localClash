package corerun

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeOptionsDefaults(t *testing.T) {
	got := normalizeOptions(Options{})
	if got.CorePath != "bin/mihomo" {
		t.Fatalf("CorePath = %q", got.CorePath)
	}
	if got.ConfigPath != "generated/mihomo.yaml" {
		t.Fatalf("ConfigPath = %q", got.ConfigPath)
	}
	if got.WorkDir != ".runtime/mihomo" {
		t.Fatalf("WorkDir = %q", got.WorkDir)
	}
	if got.LogPath != filepath.Join(".runtime/mihomo", "mihomo.log") {
		t.Fatalf("LogPath = %q", got.LogPath)
	}
}

func TestValidateRejectsMissingConfig(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "mihomo")
	if err := os.WriteFile(core, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := normalizeOptions(Options{
		CorePath:   core,
		ConfigPath: filepath.Join(dir, "missing.yaml"),
		WorkDir:    filepath.Join(dir, "runtime"),
	}).validate()
	if err == nil {
		t.Fatal("expected missing config error")
	}
}
