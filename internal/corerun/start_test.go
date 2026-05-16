package corerun

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestStartMissingConfigReturnsError(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "mihomo")
	writeStartExecutable(t, core, "#!/bin/sh\nexit 0\n")

	_, err := Start(context.Background(), StartOptions{
		CorePath:   core,
		ConfigPath: filepath.Join(dir, "missing.yaml"),
		WorkDir:    filepath.Join(dir, "runtime"),
	})
	if err == nil || !strings.Contains(err.Error(), "config") {
		t.Fatalf("error = %v, want missing config error", err)
	}
}

func TestStartRejectsConfigTestFailure(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "mihomo")
	writeStartExecutable(t, core, `#!/bin/sh
for arg in "$@"; do
  if [ "$arg" = "-t" ]; then
    echo bad config >&2
    exit 9
  fi
done
sleep 30
`)
	config := writeStartConfig(t, dir)

	_, err := Start(context.Background(), StartOptions{
		CorePath:   core,
		ConfigPath: config,
		WorkDir:    filepath.Join(dir, "runtime"),
	})
	if err == nil || !strings.Contains(err.Error(), "mihomo config test failed") {
		t.Fatalf("error = %v, want config test failure", err)
	}
	if _, err := os.Stat(runtimePIDPath(filepath.Join(dir, "runtime"))); !os.IsNotExist(err) {
		t.Fatalf("pid file should not exist, err=%v", err)
	}
}

func TestStartAlreadyRunningDoesNotStartSecondRuntime(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "mihomo")
	writeStartExecutable(t, core, "#!/bin/sh\nexit 44\n")
	config := writeStartConfig(t, dir)
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	currentPID := os.Getpid()
	if err := os.WriteFile(runtimePIDPath(workDir), []byte(strconv.Itoa(currentPID)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Start(context.Background(), StartOptions{
		CorePath:   core,
		ConfigPath: config,
		WorkDir:    workDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Started || !result.AlreadyRunning || result.PID != currentPID {
		t.Fatalf("result = %+v, want already running pid %d", result, currentPID)
	}
}

func TestStartLaunchesBackgroundRuntime(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "mihomo")
	writeStartExecutable(t, core, `#!/bin/sh
for arg in "$@"; do
  if [ "$arg" = "-t" ]; then
    echo configuration test is successful
    exit 0
  fi
done
echo runtime started
sleep 30
`)
	config := writeStartConfig(t, dir)
	workDir := filepath.Join(dir, "runtime")
	logPath := filepath.Join(workDir, "mihomo.log")

	result, err := Start(context.Background(), StartOptions{
		CorePath:   core,
		ConfigPath: config,
		WorkDir:    workDir,
		LogPath:    logPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer killProcess(result.PID)
	if !result.Started || result.AlreadyRunning || result.PID == 0 {
		t.Fatalf("result = %+v, want started pid", result)
	}
	if result.LogFile != logPath || result.Config != config || result.ExternalUIURL != "http://127.0.0.1:9090/ui" {
		t.Fatalf("result = %+v, want paths and ui url", result)
	}
	if len(result.Warnings) < 2 || !strings.Contains(result.Warnings[0], "network connectivity") {
		t.Fatalf("warnings = %+v, want network warning", result.Warnings)
	}
	if _, err := os.Stat(runtimePIDPath(workDir)); err != nil {
		t.Fatalf("pid file missing: %v", err)
	}
}

func writeStartConfig(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "mihomo.yaml")
	if err := os.WriteFile(path, []byte("external-controller: 127.0.0.1:9090\nexternal-ui: ui/zashboard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeStartExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func killProcess(pid int) {
	process, err := os.FindProcess(pid)
	if err == nil {
		_ = process.Kill()
	}
}
