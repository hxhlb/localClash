package runtimeprofile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStatusForMissingFileUsesNormalDefault(t *testing.T) {
	status, err := StatusFor(filepath.Join(t.TempDir(), DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	if status.Exists {
		t.Fatal("missing profile file should report exists=false")
	}
	if status.Mode != ModeNormal {
		t.Fatalf("mode = %q, want %q", status.Mode, ModeNormal)
	}
	if status.Core != CoreMeta || status.CorePath != MetaCorePath {
		t.Fatalf("core = %q path = %q, want %q %q", status.Core, status.CorePath, CoreMeta, MetaCorePath)
	}
	if status.Summary["mixed-port"] != 7890 {
		t.Fatalf("summary mixed-port = %v, want 7890", status.Summary["mixed-port"])
	}
}

func TestConfigureWritesModeAndCore(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultPath)

	status, err := Configure(path, ModeRouter, CoreSmart)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Exists {
		t.Fatal("configured profile file should exist")
	}
	if status.Mode != ModeRouter || status.Core != CoreSmart || status.CorePath != SmartCorePath {
		t.Fatalf("status = %+v, want router smart", status)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}

	file, exists, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !exists || file.Version != 1 || file.Mode != ModeRouter || file.Core != CoreSmart {
		t.Fatalf("loaded file = %+v exists=%v", file, exists)
	}
}

func TestApplyToConfigSkipsDynamicKeys(t *testing.T) {
	config := map[string]any{
		"mixed-port": 7890,
		"proxies":    []any{"keep"},
		"dns":        map[string]any{"enable": false, "keep": true},
	}
	profile := Profile{Mihomo: map[string]any{
		"mixed-port": 7893,
		"proxies":    []any{"drop"},
		"dns":        map[string]any{"enable": true, "listen": "0.0.0.0:7874"},
	}}

	ApplyToConfig(config, profile)

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
