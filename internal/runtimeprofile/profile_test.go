package runtimeprofile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestStatusForMissingFileInitializesEditableProfilesFromDefaults(t *testing.T) {
	dir := t.TempDir()
	status, err := StatusFor(filepath.Join(dir, DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	if !status.Exists {
		t.Fatal("missing runtime profile should be initialized")
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
	for _, path := range []string{
		filepath.Join(dir, "profiles", "normal.default.json"),
		filepath.Join(dir, "profiles", "router.default.json"),
		filepath.Join(dir, "profiles", "normal.json"),
		filepath.Join(dir, "profiles", "router.json"),
		filepath.Join(dir, DefaultPath),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected initialized profile file %s: %v", path, err)
		}
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
	if !status.RouterTakeoverRequired {
		t.Fatal("router profile should require router takeover after run_runtime")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !contains(text, `"path": "profiles/normal.json"`) || !contains(text, `"path": "profiles/router.json"`) || contains(text, `"mihomo"`) {
		t.Fatalf("runtime selector file =\n%s\nwant profile paths without embedded mihomo profile bodies", text)
	}

	file, exists, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !exists || file.Version != 1 || file.Mode != ModeRouter || file.Core != CoreSmart {
		t.Fatalf("loaded file = %+v exists=%v", file, exists)
	}
}

func TestDefaultNormalProfileCanResolveV2FlyGeoSitePacks(t *testing.T) {
	file := DefaultFile()
	mihomo := file.Profiles[ModeNormal].Mihomo

	if mihomo["geodata-mode"] != true || mihomo["geodata-loader"] != "memconservative" {
		t.Fatalf("normal geodata = mode %v loader %v, want enabled memconservative", mihomo["geodata-mode"], mihomo["geodata-loader"])
	}
	geoxURL := mihomo["geox-url"].(map[string]any)
	if !strings.Contains(fmt.Sprint(geoxURL["geosite"]), "Loyalsoldier/v2ray-rules-dat") || !strings.Contains(fmt.Sprint(geoxURL["geosite"]), "geosite.dat") {
		t.Fatalf("normal geox-url = %+v, want Loyalsoldier geosite.dat", geoxURL)
	}
}

func contains(text, needle string) bool {
	return strings.Contains(text, needle)
}

func TestDefaultRouterProfileMatchesRouterReferencePreferences(t *testing.T) {
	file := DefaultFile()
	profile := file.Profiles[ModeRouter]
	mihomo := profile.Mihomo

	for key, want := range map[string]any{
		"mixed-port":          7893,
		"redir-port":          7892,
		"tproxy-port":         7895,
		"allow-lan":           true,
		"bind-address":        "*",
		"external-controller": "0.0.0.0:9090",
		"ipv6":                true,
		"geodata-mode":        true,
		"geodata-loader":      "memconservative",
		"tcp-concurrent":      true,
		"unified-delay":       true,
		"find-process-mode":   "off",
		"keep-alive-interval": 15,
		"keep-alive-idle":     600,
	} {
		if got := mihomo[key]; got != want {
			t.Fatalf("router mihomo[%q] = %v, want %v", key, got, want)
		}
	}
	geoxURL := mihomo["geox-url"].(map[string]any)
	if !strings.Contains(fmt.Sprint(geoxURL["geosite"]), "Loyalsoldier/v2ray-rules-dat") || !strings.Contains(fmt.Sprint(geoxURL["geosite"]), "geosite.dat") {
		t.Fatalf("router geox-url = %+v, want Loyalsoldier geosite.dat", geoxURL)
	}

	dns := mihomo["dns"].(map[string]any)
	if dns["enhanced-mode"] != "redir-host" || dns["listen"] != "0.0.0.0:7874" || dns["respect-rules"] != true {
		t.Fatalf("router dns = %+v, want redir-host dns on 0.0.0.0:7874 with respect-rules", dns)
	}
	for _, key := range []string{"nameserver", "proxy-server-nameserver", "direct-nameserver", "default-nameserver"} {
		if strings.Contains(fmt.Sprint(dns[key]), "127.0.0.1:5335") {
			t.Fatalf("router dns %s = %+v, must not depend on Ronnie's local mosDNS", key, dns[key])
		}
	}
	if !strings.Contains(fmt.Sprint(dns["fallback"]), "tls://1.1.1.1") || !strings.Contains(fmt.Sprint(dns["fallback"]), "tls://8.8.8.8") {
		t.Fatalf("router dns fallback = %+v, want global encrypted fallback resolvers", dns["fallback"])
	}
	filter, ok := dns["fallback-filter"].(map[string]any)
	if !ok || filter["geoip"] != true || filter["geoip-code"] != "CN" || filter["geosite"] != nil {
		t.Fatalf("router dns fallback-filter = %+v, want geoip CN and no deprecated geosite filter", dns["fallback-filter"])
	}
	policy, ok := dns["nameserver-policy"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(policy["geosite:gfw"]), "tls://1.1.1.1") || !strings.Contains(fmt.Sprint(policy["geosite:gfw"]), "tls://8.8.8.8") {
		t.Fatalf("router dns nameserver-policy = %+v, want geosite:gfw to use global encrypted resolvers", dns["nameserver-policy"])
	}
	if _, ok := mihomo["interface-name"]; ok {
		t.Fatalf("router default must not pin Ronnie's WAN device: %+v", mihomo["interface-name"])
	}
	tun := mihomo["tun"].(map[string]any)
	if tun["stack"] != "mixed" || tun["device"] != "utun" || tun["auto-route"] != false || tun["auto-redirect"] != false {
		t.Fatalf("router tun = %+v, want mixed utun without Mihomo auto-route/auto-redirect", tun)
	}
	sniffer := mihomo["sniffer"].(map[string]any)
	if sniffer["enable"] != true || sniffer["override-destination"] != true || sniffer["force-dns-mapping"] != true || sniffer["parse-pure-ip"] != true {
		t.Fatalf("router sniffer = %+v, want enabled DNS mapping and pure-IP parsing", sniffer)
	}
	if _, ok := profile.Deploy["openclash-conflict"]; ok {
		t.Fatalf("router deploy must not contain OpenClash conflict policy: %+v", profile.Deploy)
	}
	if _, ok := profile.Deploy["wan-interface"]; ok {
		t.Fatalf("router deploy must not pin Ronnie's WAN interface: %+v", profile.Deploy)
	}
}

func TestLoadUsesUserProfileFileWithoutBackfillingDeletedSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultPath)
	writeRuntimeProfileTestFile(t, filepath.Join(dir, "profiles", "router.json"), `mihomo:
  mixed-port: 9000
  dns:
    listen: 127.0.0.1:5353
`)
	writeRuntimeProfileTestFile(t, path, `version: 1
mode: router
core: meta
profiles:
  router:
    path: profiles/router.json
cores:
  meta:
    path: custom-meta
  smart:
    path: custom-smart
`)

	file, exists, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("profile file should exist")
	}
	router := file.Profiles[ModeRouter]
	if router.Mihomo["mixed-port"] != 9000 {
		t.Fatalf("mixed-port = %v, want preserved user value 9000", router.Mihomo["mixed-port"])
	}
	dns := router.Mihomo["dns"].(map[string]any)
	if dns["listen"] != "127.0.0.1:5353" {
		t.Fatalf("dns.listen = %v, want preserved user value", dns["listen"])
	}
	if _, ok := dns["enhanced-mode"]; ok {
		t.Fatalf("dns.enhanced-mode should not be backfilled into user profile: %+v", dns)
	}
	if _, ok := router.Mihomo["interface-name"]; ok {
		t.Fatalf("interface-name should not be backfilled into user profile: %+v", router.Mihomo)
	}
	if _, ok := router.Mihomo["sniffer"].(map[string]any); ok {
		t.Fatalf("sniffer should not be backfilled into user profile: %+v", router.Mihomo)
	}
	if _, ok := file.Profiles[ModeNormal]; ok {
		t.Fatalf("normal profile should not be injected into explicit runtime file: %+v", file.Profiles)
	}
}

func TestLoadRejectsExistingRuntimeFileWithoutExplicitCorePaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultPath)
	writeRuntimeProfileTestFile(t, filepath.Join(dir, "profiles", "normal.json"), `mihomo:
  mixed-port: 7890
`)
	writeRuntimeProfileTestFile(t, path, `version: 1
mode: normal
core: meta
profiles:
  normal:
    path: profiles/normal.json
`)

	_, _, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), `runtime core "meta" is not defined`) {
		t.Fatalf("Load error = %v, want missing explicit core definition", err)
	}
}

func TestLoadRejectsExistingRuntimeFileWithEmptyActiveCorePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultPath)
	writeRuntimeProfileTestFile(t, filepath.Join(dir, "profiles", "normal.json"), `mihomo:
  mixed-port: 7890
`)
	writeRuntimeProfileTestFile(t, path, `version: 1
mode: normal
core: meta
profiles:
  normal:
    path: profiles/normal.json
cores:
  meta:
    path: ""
`)

	_, _, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), `runtime core "meta" has no path`) {
		t.Fatalf("Load error = %v, want empty active core path rejection", err)
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

func writeRuntimeProfileTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
