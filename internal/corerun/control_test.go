package corerun

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

func TestStopTerminatesRunningRuntime(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
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

	result, err := Stop(StopOptions{WorkDir: workDir, Timeout: 2 * time.Second})
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
