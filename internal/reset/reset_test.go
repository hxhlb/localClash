package reset

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestRunDryRunDoesNotDeleteFactoryResetTargets(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeResetFile(t, filepath.Join(".runtime", "logs", "mcp.log"), "log")
	writeResetFile(t, filepath.Join("generated", "mihomo.yaml"), "config")
	writeResetFile(t, "localclash.yaml", "version: 1\n")
	writeResetFile(t, "localclash-packs.yaml", "version: 1\n")
	writeResetFile(t, "localclash-subscriptions.yaml", "sources: []\n")
	writeResetFile(t, "localclash-runtime.yaml", "active: router\n")
	writeResetFile(t, filepath.Join("profiles", "router.yaml"), "mihomo: {}\n")
	writeResetFile(t, "subscription.yaml", "proxies: []\n")
	writeResetFile(t, "subscription-backup.yaml", "proxies: []\n")

	var out bytes.Buffer
	result, err := Run(Options{DryRun: true, Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun || len(result.Deleted) != 9 {
		t.Fatalf("result = %+v, want dry-run with nine delete targets", result)
	}
	for _, path := range []string{".runtime", "generated", "localclash.yaml", "localclash-subscriptions.yaml", "localclash-runtime.yaml", "profiles", "subscription.yaml", "subscription-backup.yaml"} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s should remain after dry-run: %v", path, err)
		}
	}
	if !strings.Contains(out.String(), "localClash factory reset dry run") || !strings.Contains(out.String(), "subscription-backup.yaml") {
		t.Fatalf("output = %q, want dry-run plan", out.String())
	}
}

func TestRunDeletesFactoryResetTargetsWithYes(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeResetFile(t, filepath.Join(".runtime", "mihomo", "logs", "mihomo.log"), "log")
	writeResetFile(t, filepath.Join("generated", "mihomo.yaml"), "config")
	writeResetFile(t, filepath.Join("bin", "mihomo"), "binary")
	writeResetFile(t, filepath.Join("policies", "loyalsoldier.yaml"), "policy")
	writeResetFile(t, filepath.Join("policy-templates", "minimal.yaml"), "template")
	writeResetFile(t, filepath.Join("rule-sources", "source.yaml"), "source")
	writeResetFile(t, "localclash.yaml", "version: 1\n")
	writeResetFile(t, "localclash-packs.yaml", "version: 1\n")
	writeResetFile(t, "localclash-subscriptions.yaml", "sources: []\n")
	writeResetFile(t, "localclash-runtime.yaml", "active: router\n")
	writeResetFile(t, filepath.Join("profiles", "normal.yaml"), "mihomo: {}\n")
	writeResetFile(t, "subscription.yaml", "proxies: []\n")

	var out bytes.Buffer
	if _, err := Run(Options{Yes: true, Out: &out}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{".runtime", "generated", "localclash.yaml", "localclash-packs.yaml", "localclash-subscriptions.yaml", "localclash-runtime.yaml", "profiles", "subscription.yaml"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s should be deleted, err=%v", path, err)
		}
	}
	for _, path := range []string{filepath.Join("bin", "mihomo"), filepath.Join("policies", "loyalsoldier.yaml"), filepath.Join("policy-templates", "minimal.yaml"), filepath.Join("rule-sources", "source.yaml")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s should be kept: %v", path, err)
		}
	}
	if !strings.Contains(out.String(), "Reset complete.") {
		t.Fatalf("output = %q, want completion", out.String())
	}
}

func TestRunRequiresConfirmation(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeResetFile(t, "localclash.yaml", "version: 1\n")

	_, err := Run(Options{In: strings.NewReader("no\n"), Out: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("error = %v, want cancelled", err)
	}
	if _, err := os.Stat("localclash.yaml"); err != nil {
		t.Fatalf("localclash.yaml should remain after cancelled reset: %v", err)
	}

	if _, err := Run(Options{In: strings.NewReader(ConfirmationPhrase + "\n"), Out: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("localclash.yaml"); !os.IsNotExist(err) {
		t.Fatalf("localclash.yaml should be deleted after confirmed reset, err=%v", err)
	}
}

func TestRunRefusesWhileRuntimeIsRunning(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	cmd := startResetFakeRuntime(t, dir)
	writeResetFile(t, filepath.Join(".runtime", "mihomo", "mihomo.pid"), strconv.Itoa(cmd.Process.Pid)+"\n")

	_, err := Run(Options{Yes: true, Out: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "runtime is running") {
		t.Fatalf("error = %v, want running runtime refusal", err)
	}
	if _, err := os.Stat(filepath.Join(".runtime", "mihomo", "mihomo.pid")); err != nil {
		t.Fatalf("pid file should remain after refused reset: %v", err)
	}
}

func TestRunDryRunAllowsRunningRuntime(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	cmd := startResetFakeRuntime(t, dir)
	writeResetFile(t, filepath.Join(".runtime", "mihomo", "mihomo.pid"), strconv.Itoa(cmd.Process.Pid)+"\n")

	result, err := Run(Options{DryRun: true, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun || len(result.Deleted) != 2 {
		t.Fatalf("result = %+v, want dry-run plan for runtime dir", result)
	}
	if _, err := os.Stat(filepath.Join(".runtime", "mihomo", "mihomo.pid")); err != nil {
		t.Fatalf("pid file should remain after dry-run: %v", err)
	}
}

func writeResetFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func startResetFakeRuntime(t *testing.T, dir string) *exec.Cmd {
	t.Helper()
	workDir := filepath.Join(dir, ".runtime", "mihomo")
	config := filepath.Join(dir, "generated", "mihomo.yaml")
	core := filepath.Join(dir, "bin", "linux-"+runtime.GOARCH, "mihomo-meta")
	writeResetFile(t, config, "external-controller: 127.0.0.1:9090\n")
	writeResetFile(t, core, "#!/bin/sh\nsleep 30\n")
	if err := os.Chmod(core, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(core, "-d", workDir, "-f", config)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	return cmd
}
