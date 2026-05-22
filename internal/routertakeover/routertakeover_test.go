package routertakeover

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"localclash/internal/runtimeprofile"
)

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
	for _, forbidden := range []string{"uci ", "/etc/config/firewall", "/var/etc/localclash", "fw4 reload"} {
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
