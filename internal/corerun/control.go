package corerun

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type StatusOptions struct {
	ConfigPath string
	WorkDir    string
	LogPath    string
}

type StatusResult struct {
	Running            bool   `json:"running"`
	PID                int    `json:"pid,omitempty"`
	PIDFile            string `json:"pid_file"`
	StalePIDFile       bool   `json:"stale_pid_file,omitempty"`
	StalePIDFileReason string `json:"stale_pid_file_reason,omitempty"`
	RuntimeDir         string `json:"runtime_dir"`
	Config             string `json:"config"`
	LogFile            string `json:"log_file"`
	ExternalController string `json:"external_controller,omitempty"`
	ExternalUIURL      string `json:"external_ui_url,omitempty"`
}

type StopOptions struct {
	WorkDir   string
	Timeout   time.Duration
	ForceKill bool
}

type StopResult struct {
	Stopped            bool   `json:"stopped"`
	WasRunning         bool   `json:"was_running"`
	PID                int    `json:"pid,omitempty"`
	Signal             string `json:"signal,omitempty"`
	Forced             bool   `json:"forced,omitempty"`
	RuntimeDir         string `json:"runtime_dir"`
	PIDFile            string `json:"pid_file"`
	RemovedPIDFile     bool   `json:"removed_pid_file,omitempty"`
	StalePIDFile       bool   `json:"stale_pid_file,omitempty"`
	StalePIDFileReason string `json:"stale_pid_file_reason,omitempty"`
	Error              string `json:"error,omitempty"`
}

func Status(opts StatusOptions) StatusResult {
	normalized := normalizeStartOptions(StartOptions{
		ConfigPath: opts.ConfigPath,
		WorkDir:    opts.WorkDir,
		LogPath:    opts.LogPath,
	})
	pidPath := runtimePIDPath(normalized.WorkDir)
	result := StatusResult{
		PIDFile:            pidPath,
		RuntimeDir:         normalized.WorkDir,
		Config:             normalized.ConfigPath,
		LogFile:            normalized.LogPath,
		ExternalController: readExternalController(normalized.ConfigPath),
	}
	result.ExternalUIURL = externalUIURL(normalized.ConfigPath, result.ExternalController)

	pid, exists, err := readPIDFile(pidPath)
	if !exists {
		return result
	}
	if err != nil {
		result.StalePIDFile = true
		result.StalePIDFileReason = err.Error()
		return result
	}
	result.PID = pid
	if processRunning(pid) {
		result.Running = true
		return result
	}
	result.StalePIDFile = true
	result.StalePIDFileReason = "pid file points to a process that is not running"
	return result
}

func Stop(opts StopOptions) (StopResult, error) {
	normalized := normalizeStartOptions(StartOptions{WorkDir: opts.WorkDir})
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	pidPath := runtimePIDPath(normalized.WorkDir)
	result := StopResult{
		RuntimeDir: normalized.WorkDir,
		PIDFile:    pidPath,
	}

	pid, exists, err := readPIDFile(pidPath)
	if !exists {
		return result, nil
	}
	if err != nil {
		result.StalePIDFile = true
		result.StalePIDFileReason = err.Error()
		result.RemovedPIDFile = removePIDFile(pidPath)
		return result, nil
	}
	result.PID = pid
	if !processRunning(pid) {
		result.StalePIDFile = true
		result.StalePIDFileReason = "pid file points to a process that is not running"
		result.RemovedPIDFile = removePIDFile(pidPath)
		return result, nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return result, err
	}
	result.WasRunning = true
	result.Signal = "SIGTERM"
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return result, fmt.Errorf("send SIGTERM to pid %d: %w", pid, err)
	}
	if waitForExit(pid, timeout) {
		result.Stopped = true
		result.RemovedPIDFile = removePIDFile(pidPath)
		return result, nil
	}
	if !opts.ForceKill {
		result.Error = "runtime did not stop before timeout"
		return result, nil
	}
	result.Forced = true
	result.Signal = "SIGKILL"
	if err := process.Kill(); err != nil {
		return result, fmt.Errorf("send SIGKILL to pid %d: %w", pid, err)
	}
	if waitForExit(pid, timeout) {
		result.Stopped = true
		result.RemovedPIDFile = removePIDFile(pidPath)
		return result, nil
	}
	result.Error = "runtime did not stop after SIGKILL"
	return result, nil
}

func readPIDFile(path string) (int, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, true, err
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return 0, true, fmt.Errorf("pid file is empty")
	}
	pid, err := strconv.Atoi(text)
	if err != nil || pid <= 0 {
		return 0, true, fmt.Errorf("pid file contains invalid pid %q", text)
	}
	return pid, true, nil
}

func removePIDFile(path string) bool {
	if err := os.Remove(path); err == nil || os.IsNotExist(err) {
		return true
	}
	return false
}

func waitForExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !processRunning(pid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}
