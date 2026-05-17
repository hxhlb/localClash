package runtimepreset

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStatusForMissingFileUsesNormalDefault(t *testing.T) {
	status, err := StatusFor(filepath.Join(t.TempDir(), "mihomo-preset.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if status.Exists {
		t.Fatal("missing preset file should report exists=false")
	}
	if status.Active != Normal {
		t.Fatalf("active = %q, want %q", status.Active, Normal)
	}
	if status.Summary["mixed-port"] != 7890 {
		t.Fatalf("summary mixed-port = %v, want 7890", status.Summary["mixed-port"])
	}
}

func TestConfigureWritesActivePreset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mihomo-preset.yaml")

	status, err := Configure(path, Router)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Exists {
		t.Fatal("configured preset file should exist")
	}
	if status.Active != Router {
		t.Fatalf("active = %q, want %q", status.Active, Router)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}

	file, exists, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !exists || file.Version != 1 || file.Active != Router {
		t.Fatalf("loaded file = %+v exists=%v", file, exists)
	}
}

func TestApplyToConfigSkipsDynamicKeys(t *testing.T) {
	config := map[string]any{
		"mixed-port": 7890,
		"proxies":    []any{"keep"},
		"dns":        map[string]any{"enable": false, "keep": true},
	}
	preset := Preset{Mihomo: map[string]any{
		"mixed-port": 7893,
		"proxies":    []any{"drop"},
		"dns":        map[string]any{"enable": true, "listen": "0.0.0.0:7874"},
	}}

	ApplyToConfig(config, preset)

	if config["mixed-port"] != 7893 {
		t.Fatalf("mixed-port = %v, want 7893", config["mixed-port"])
	}
	if config["proxies"].([]any)[0] != "keep" {
		t.Fatalf("proxies = %v, want original dynamic value", config["proxies"])
	}
	dns := config["dns"].(map[string]any)
	if dns["enable"] != true || dns["listen"] != "0.0.0.0:7874" || dns["keep"] != true {
		t.Fatalf("dns = %+v, want merged preset dns", dns)
	}
}
