package corerun

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestStatusReportsRunningRuntime(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := writeStartConfig(t, dir)
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer killProcess(cmd.Process.Pid)
	go func() { _ = cmd.Wait() }()
	if err := os.WriteFile(runtimePIDPath(workDir), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubProcessCommandLine(t, cmd.Process.Pid, []string{"mihomo", "-d", workDir, "-f", config})

	result := Status(StatusOptions{
		ConfigPath: config,
		WorkDir:    workDir,
		LogPath:    filepath.Join(workDir, "mihomo.log"),
	})
	if !result.Running || result.PID != cmd.Process.Pid || result.StalePIDFile {
		t.Fatalf("status = %+v, want running pid %d", result, cmd.Process.Pid)
	}
	if result.ExternalUIURL != "http://127.0.0.1:9090/ui" {
		t.Fatalf("external ui url = %q", result.ExternalUIURL)
	}
}

func TestStatusReportsStalePIDWhenProcessDoesNotMatchRuntime(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := writeStartConfig(t, dir)
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer killProcess(cmd.Process.Pid)
	go func() { _ = cmd.Wait() }()
	if err := os.WriteFile(runtimePIDPath(workDir), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubProcessCommandLine(t, cmd.Process.Pid, []string{"sleep", "30"})

	result := Status(StatusOptions{
		ConfigPath: config,
		WorkDir:    workDir,
		LogPath:    filepath.Join(workDir, "mihomo.log"),
	})
	if result.Running || !result.ProcessAlive || !result.StalePIDFile || !strings.Contains(result.StalePIDFileReason, "not the configured Mihomo core") {
		t.Fatalf("status = %+v, want live non-runtime pid reported stale", result)
	}
}

func TestStatusReportsStalePIDFile(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtimePIDPath(workDir), []byte("99999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := Status(StatusOptions{WorkDir: workDir})
	if result.Running || !result.StalePIDFile || result.PID != 99999999 {
		t.Fatalf("status = %+v, want stale pid file", result)
	}
}

func TestStatusReportsStalePIDForZombieProcess(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer killProcess(cmd.Process.Pid)
	go func() { _ = cmd.Wait() }()
	if err := os.WriteFile(runtimePIDPath(workDir), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubProcessZombie(t, cmd.Process.Pid, true)

	result := Status(StatusOptions{WorkDir: workDir})
	if result.Running || result.ProcessAlive || !result.ProcessZombie || !result.StalePIDFile || !strings.Contains(result.StalePIDFileReason, "zombie") {
		t.Fatalf("status = %+v, want zombie pid reported stale", result)
	}
}

func TestStatusReportsOrphanRuntimeWithoutPIDFile(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := writeStartConfig(t, dir)
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer killProcess(cmd.Process.Pid)
	go func() { _ = cmd.Wait() }()
	stubProcessCommandLine(t, cmd.Process.Pid, []string{"mihomo", "-d", workDir, "-f", config})
	stubProcessList(t, cmd.Process.Pid)

	result := Status(StatusOptions{
		CorePath:   "mihomo",
		ConfigPath: config,
		WorkDir:    workDir,
	})
	if !result.Running || !result.OrphanRuntime || result.PID != cmd.Process.Pid || len(result.OrphanPIDs) != 1 {
		t.Fatalf("status = %+v, want orphan runtime pid %d", result, cmd.Process.Pid)
	}
}

func TestStopTerminatesRunningRuntime(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := writeStartConfig(t, dir)
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	if err := os.WriteFile(runtimePIDPath(workDir), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubProcessCommandLine(t, cmd.Process.Pid, []string{"mihomo", "-d", workDir, "-f", config})

	result, err := Stop(StopOptions{CorePath: "mihomo", ConfigPath: config, WorkDir: workDir, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Stopped || !result.WasRunning || result.PID != cmd.Process.Pid || !result.RemovedPIDFile {
		t.Fatalf("stop = %+v, want stopped pid %d", result, cmd.Process.Pid)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("process pid %d was not reaped", cmd.Process.Pid)
	}
	if _, err := os.Stat(runtimePIDPath(workDir)); !os.IsNotExist(err) {
		t.Fatalf("pid file should be removed, err=%v", err)
	}
}

func TestStopTerminatesOrphanRuntimeWithoutPIDFile(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := writeStartConfig(t, dir)
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	stubProcessCommandLine(t, cmd.Process.Pid, []string{"mihomo", "-d", workDir, "-f", config})
	stubProcessList(t, cmd.Process.Pid)

	result, err := Stop(StopOptions{
		CorePath:   "mihomo",
		ConfigPath: config,
		WorkDir:    workDir,
		Timeout:    2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Stopped || !result.WasRunning || !result.OrphanRuntime || len(result.StoppedPIDs) != 1 || result.StoppedPIDs[0] != cmd.Process.Pid {
		t.Fatalf("stop = %+v, want stopped orphan pid %d", result, cmd.Process.Pid)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("orphan process pid %d was not reaped", cmd.Process.Pid)
	}
}

func TestStopRemovesZombiePIDFile(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer killProcess(cmd.Process.Pid)
	go func() { _ = cmd.Wait() }()
	if err := os.WriteFile(runtimePIDPath(workDir), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubProcessZombie(t, cmd.Process.Pid, true)

	result, err := Stop(StopOptions{WorkDir: workDir, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stopped || result.WasRunning || !result.ProcessZombie || !result.StalePIDFile || !result.RemovedPIDFile {
		t.Fatalf("stop = %+v, want zombie pid cleanup without stop timeout", result)
	}
	if _, err := os.Stat(runtimePIDPath(workDir)); !os.IsNotExist(err) {
		t.Fatalf("pid file should be removed, err=%v", err)
	}
}

func TestStopRemovesStalePIDFile(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtimePIDPath(workDir), []byte("not-a-pid\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Stop(StopOptions{WorkDir: workDir})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stopped || result.WasRunning || !result.StalePIDFile || !result.RemovedPIDFile {
		t.Fatalf("stop = %+v, want stale pid cleanup", result)
	}
	if _, err := os.Stat(runtimePIDPath(workDir)); !os.IsNotExist(err) {
		t.Fatalf("pid file should be removed, err=%v", err)
	}
}

func TestRestartPretestsConfigBeforeStoppingRuntime(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer killProcess(cmd.Process.Pid)
	go func() { _ = cmd.Wait() }()
	if err := os.WriteFile(runtimePIDPath(workDir), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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

	_, err := Restart(context.Background(), RestartOptions{
		CorePath:   core,
		ConfigPath: config,
		WorkDir:    workDir,
	})
	if err == nil || !strings.Contains(err.Error(), "mihomo config test failed") {
		t.Fatalf("restart error = %v, want pretest failure", err)
	}
	stubProcessCommandLine(t, cmd.Process.Pid, []string{"mihomo", "-d", workDir, "-f", config})
	status := Status(StatusOptions{ConfigPath: config, WorkDir: workDir})
	if !status.Running || status.PID != cmd.Process.Pid {
		t.Fatalf("status after failed restart = %+v, want original runtime still running", status)
	}
}

func TestParseProcStatState(t *testing.T) {
	tests := []struct {
		name string
		stat string
		want byte
	}{
		{name: "simple", stat: "3866 (mihomo-meta) Z 1 2 3", want: 'Z'},
		{name: "space in command", stat: "3866 (mihomo meta) S 1 2 3", want: 'S'},
		{name: "paren in command", stat: "3866 (name with ) paren) R 1 2 3", want: 'R'},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseProcStatState(tt.stat)
			if !ok || got != tt.want {
				t.Fatalf("parseProcStatState(%q) = %q, %v; want %q, true", tt.stat, got, ok, tt.want)
			}
		})
	}
}

func stubProcessCommandLine(t *testing.T, pid int, args []string) {
	t.Helper()
	original := readProcessCommandLine
	readProcessCommandLine = func(candidate int) ([]string, bool, error) {
		if candidate == pid {
			return args, true, nil
		}
		return original(candidate)
	}
	t.Cleanup(func() {
		readProcessCommandLine = original
	})
}

func stubProcessZombie(t *testing.T, pid int, zombie bool) {
	t.Helper()
	original := processZombie
	processZombie = func(candidate int) bool {
		if candidate == pid {
			return zombie
		}
		return original(candidate)
	}
	t.Cleanup(func() {
		processZombie = original
	})
}

func stubProcessList(t *testing.T, pids ...int) {
	t.Helper()
	original := listProcessIDs
	listProcessIDs = func() []int {
		return append([]int(nil), pids...)
	}
	t.Cleanup(func() {
		listProcessIDs = original
	})
}

func TestRestartStopsExistingRuntimeAndStartsNewRuntime(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := exec.Command("sleep", "30")
	if err := old.Start(); err != nil {
		t.Fatal(err)
	}
	oldDone := make(chan struct{})
	go func() {
		_ = old.Wait()
		close(oldDone)
	}()
	if err := os.WriteFile(runtimePIDPath(workDir), []byte(strconv.Itoa(old.Process.Pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	core := filepath.Join(dir, "mihomo")
	writeStartExecutable(t, core, `#!/bin/sh
for arg in "$@"; do
  if [ "$arg" = "-t" ]; then
    count_file="$(dirname "$0")/config-test-count"
    count="$(cat "$count_file" 2>/dev/null || echo 0)"
    expr "$count" + 1 > "$count_file"
    echo configuration test is successful
    exit 0
  fi
done
sleep 30
`)
	config := writeStartConfig(t, dir)
	stubProcessCommandLine(t, old.Process.Pid, []string{core, "-d", workDir, "-f", config})

	result, err := Restart(context.Background(), RestartOptions{
		CorePath:    core,
		ConfigPath:  config,
		WorkDir:     workDir,
		StopTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer killProcess(result.Start.PID)
	if !result.Restarted || !result.Stop.Stopped || !result.Start.Started || result.Start.PID == old.Process.Pid {
		t.Fatalf("restart = %+v, want stopped old pid %d and started new pid", result, old.Process.Pid)
	}
	if !result.Start.ConfigTestSkipped {
		t.Fatalf("restart start = %+v, want second config test skipped after pretest", result.Start)
	}
	count, err := os.ReadFile(filepath.Join(dir, "config-test-count"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(count)) != "1" {
		t.Fatalf("config test count = %q, want one pre-stop validation", strings.TrimSpace(string(count)))
	}
	select {
	case <-oldDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("old process pid %d was not reaped", old.Process.Pid)
	}
}
