package envinspect

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectOpenWrtRootReportsCapabilitiesWithoutSecrets(t *testing.T) {
	root := t.TempDir()
	work := t.TempDir()
	writeEnvFile(t, filepath.Join(root, "etc", "openwrt_release"), "DISTRIB_DESCRIPTION='OpenWrt test build'\n")
	writeEnvFile(t, filepath.Join(root, "sbin", "procd"), "")
	writeEnvFile(t, filepath.Join(root, "etc", "init.d", "dnsmasq"), "#!/bin/sh\n")
	writeEnvFile(t, filepath.Join(root, "etc", "init.d", "odhcpd"), "#!/bin/sh\n")
	writeEnvFile(t, filepath.Join(root, "etc", "init.d", "openclash"), "#!/bin/sh\n")
	writeEnvFile(t, filepath.Join(root, "etc", "openclash", "core", "clash_meta"), "")
	writeEnvFile(t, filepath.Join(root, "etc", "config", "dhcp"), `
config dnsmasq
	option server '127.0.0.1#7874'

config dhcp 'lan'
	option interface 'lan'
	option dhcpv4 'server'
	option dhcpv6 'server'
	option ra 'server'

config dhcp 'wan'
	option interface 'wan'
	option ignore '1'
`)
	writeEnvFile(t, filepath.Join(root, "etc", "config", "openclash"), `
config openclash 'config'
	option enable '1'
	option operation_mode 'redir-host'
	option en_mode 'redir-host-mix'
	option proxy_mode 'rule'
	option dashboard_type 'Meta'
	option config_path '/etc/openclash/config/degyax.yaml'
	option dashboard_password 'secret-password'
	option subscription_url 'https://secret.example/sub'

config config_subscribe
	option address 'https://secret.example/sub'
`)
	writeEnvFile(t, filepath.Join(root, "etc", "openclash", "config", "degyax.yaml"), `
allow-lan: true
mode: Rule
proxies:
  - name: HK 01
    type: ss
    server: hk.example.com
    password: secret
proxy-groups:
  - name: HK
    type: url-test
    proxies: [HK 01]
rule-providers:
  custom:
    type: http
rules:
  - MATCH,HK
`)
	writeEnvFile(t, filepath.Join(work, "subscription.yaml"), "proxies: []\n")
	writeEnvFile(t, filepath.Join(work, "generated", "mihomo.yaml"), "mode: rule\n")
	writeEnvFile(t, filepath.Join(work, "bin", "mihomo"), "")

	result, err := Inspect(context.Background(), Options{RootDir: root, WorkDir: work})
	if err != nil {
		t.Fatal(err)
	}
	if result.Host.OpenWrtRelease != "OpenWrt test build" {
		t.Fatalf("openwrt release = %q", result.Host.OpenWrtRelease)
	}
	if result.Host.ServiceManager != "procd" {
		t.Fatalf("service manager = %q, want procd", result.Host.ServiceManager)
	}
	if len(result.Capabilities.DHCP.LANServers) != 1 || result.Capabilities.DHCP.LANServers[0].IPv4Mode != "server" {
		t.Fatalf("dhcp capability = %+v, want LAN server evidence", result.Capabilities.DHCP)
	}
	if !result.OpenClashState.Present || result.OpenClashState.Features["proxy_mode"] != "rule" {
		t.Fatalf("openclash state = %+v, want safe proxy mode", result.OpenClashState)
	}
	if result.OpenClashState.Features["dashboard_password"] != "" || result.OpenClashState.Features["subscription_url"] != "" {
		t.Fatalf("unsafe openclash features leaked: %+v", result.OpenClashState.Features)
	}
	if result.OpenClashState.ActiveProfile == nil || result.OpenClashState.ActiveProfile.ProxiesCount != 1 || result.OpenClashState.ActiveProfile.RulesCount != 1 {
		t.Fatalf("active profile = %+v, want counts", result.OpenClashState.ActiveProfile)
	}
	if !result.Capabilities.ProxyRuntime.MihomoCorePresent {
		t.Fatalf("proxy runtime capability = %+v, want OpenClash core evidence", result.Capabilities.ProxyRuntime)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret.example", "secret-password", "hk.example.com", "password: secret"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("environment inspect leaked %q in %s", secret, data)
		}
	}
}

func writeEnvFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
