package routertakeover

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"localclash/internal/runtimeprofile"
)

// NOTE: These Go tests are not a functional acceptance gate for router takeover.
// Any behavior change in this package must also be exercised in the Docker
// OpenWrt environment; do not treat go test alone as enough validation.
func TestApplyDryRunBuildsLocalClashOwnedScript(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, runtimeprofile.DefaultPath)
	if _, err := runtimeprofile.Configure(profilePath, runtimeprofile.ModeRouter, runtimeprofile.CoreSmart); err != nil {
		t.Fatal(err)
	}

	result, err := Apply(context.Background(), Options{
		RuntimeProfile: profilePath,
		DryRun:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun {
		t.Fatal("dry-run apply should not execute takeover")
	}
	for _, want := range []string{"localclash_mangle", "localClash DNS hijack", "ip rule add fwmark", "router_takeover_apply without dry_run"} {
		if !strings.Contains(result.Script+strings.Join(result.NextActions, " "), want) {
			t.Fatalf("dry-run output missing %q:\n%s\n%v", want, result.Script, result.NextActions)
		}
	}
	if !strings.Contains(result.Script, `comment "localClash TCP redirect"`) {
		t.Fatalf("nft comments must be quoted for nft parser, got:\n%s", result.Script)
	}
	for _, forbidden := range []string{"uci set", "uci add", "uci delete", "uci commit", "/etc/config/firewall", "/var/etc/localclash", "fw4 reload"} {
		if strings.Contains(result.Script, forbidden) {
			t.Fatalf("router takeover must not persist firewall config; found %q in:\n%s", forbidden, result.Script)
		}
	}
	for _, want := range []string{"STATE_DIR='/tmp/localclash/router-takeover'", "wait_tun_ready()", "trap 'cleanup_localclash_state' ERR", "nft -f - <<EOF_NFT"} {
		if !strings.Contains(result.Script, want) {
			t.Fatalf("runtime takeover script missing %q:\n%s", want, result.Script)
		}
	}
	for _, want := range []string{"check_fw4_ready()", "firewall table inet fw4 is not active", "firewall chain inet fw4 $chain is missing"} {
		if !strings.Contains(result.Script, want) {
			t.Fatalf("runtime takeover script missing firewall preflight %q:\n%s", want, result.Script)
		}
	}
	for _, want := range []string{"add_dynamic_localnetwork4()", "add_dynamic_localnetwork6()", "ip -o -4 addr show scope global", "ip -o -6 addr show scope global"} {
		if !strings.Contains(result.Script, want) {
			t.Fatalf("runtime takeover script missing dynamic localnetwork refresh %q:\n%s", want, result.Script)
		}
	}
	for _, want := range []string{"discover_lan_networks()", "discover_lan_domains()", "add_dynamic_localdns4()", "add_dynamic_localdns6()", "localclash_dns_redirect", "localClash local DNS bypass"} {
		if !strings.Contains(result.Script, want) {
			t.Fatalf("runtime takeover script missing local DNS preservation %q:\n%s", want, result.Script)
		}
	}
	dnsBypass := strings.Index(result.Script, `comment "localClash local DNS bypass"`)
	dnsRedirect := strings.Index(result.Script, `redirect to $DNS_PORT comment "localClash DNS hijack"`)
	if dnsBypass < 0 || dnsRedirect < 0 || dnsBypass > dnsRedirect {
		t.Fatalf("local DNS bypass must be installed before DNS hijack redirect:\n%s", result.Script)
	}
	for _, line := range strings.Split(result.Script, "\n") {
		if strings.Contains(line, "localclash_dns_redirect") && strings.Contains(line, "redirect to $DNS_PORT") {
			if !strings.Contains(line, "meta l4proto") || !strings.Contains(line, "th dport 53") {
				t.Fatalf("DNS redirect chain rule must carry its own transport match for nft type checking:\n%s", line)
			}
		}
	}
	if strings.Contains(result.Script, "OpenClash") {
		t.Fatalf("router takeover script should not special-case OpenClash:\n%s", result.Script)
	}
}

func TestApplyDryRunScriptHasShellSyntax(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, runtimeprofile.DefaultPath)
	if _, err := runtimeprofile.Configure(profilePath, runtimeprofile.ModeRouter, runtimeprofile.CoreSmart); err != nil {
		t.Fatal(err)
	}
	result, err := Apply(context.Background(), Options{
		RuntimeProfile: profilePath,
		DryRun:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-n")
	cmd.Stdin = strings.NewReader(result.Script)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated shell script has syntax error: %v\n%s\nscript:\n%s", err, output, result.Script)
	}
}

func TestBaseResultReportsLocalDNSState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "local_dns4"), []byte("192.168.6.1\n192.168.6.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "local_dns6"), []byte("fd00::1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "local_domains"), []byte("lan\nlocal\nlan\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := baseResult(Options{
		StateDir:  dir,
		DNSPort:   7874,
		RedirPort: 7892,
		TunDevice: "utun",
	}, runtimeprofile.Status{Mode: runtimeprofile.ModeRouter})

	if got := strings.Join(result.LocalDNS, ","); got != "192.168.6.1,fd00::1" {
		t.Fatalf("LocalDNS = %q", got)
	}
	if got := strings.Join(result.LocalDomains, ","); got != "lan,local" {
		t.Fatalf("LocalDomains = %q", got)
	}
}

func TestApplyRejectsNormalProfileBeforeSystemChanges(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, runtimeprofile.DefaultPath)
	if _, err := runtimeprofile.Configure(profilePath, runtimeprofile.ModeNormal, runtimeprofile.CoreMeta); err != nil {
		t.Fatal(err)
	}

	result, err := Apply(context.Background(), Options{RuntimeProfile: profilePath})
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied {
		t.Fatal("normal profile must not apply router takeover")
	}
	if len(result.Checks) == 0 || result.Checks[0].ID != "profile_router" || result.Checks[0].OK {
		t.Fatalf("checks = %+v, want profile_router failure", result.Checks)
	}
}

func TestApplyDryRunRejectsNormalProfile(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, runtimeprofile.DefaultPath)
	if _, err := runtimeprofile.Configure(profilePath, runtimeprofile.ModeNormal, runtimeprofile.CoreMeta); err != nil {
		t.Fatal(err)
	}

	result, err := Apply(context.Background(), Options{
		RuntimeProfile: profilePath,
		DryRun:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Script != "" {
		t.Fatalf("normal profile dry-run should not expose takeover script:\n%s", result.Script)
	}
	if len(result.Checks) == 0 || result.Checks[0].ID != "profile_router" || result.Checks[0].OK {
		t.Fatalf("checks = %+v, want profile_router failure", result.Checks)
	}
}
