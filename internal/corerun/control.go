package corerun

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type StatusOptions struct {
	CorePath   string
	ConfigPath string
	WorkDir    string
	LogPath    string
}

type StatusResult struct {
	Running            bool   `json:"running"`
	PID                int    `json:"pid,omitempty"`
	ProcessAlive       bool   `json:"process_alive,omitempty"`
	ProcessZombie      bool   `json:"process_zombie,omitempty"`
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

type RestartOptions struct {
	CorePath    string
	ConfigPath  string
	WorkDir     string
	LogPath     string
	StopTimeout time.Duration
	ForceKill   bool
}

type StopResult struct {
	Stopped            bool     `json:"stopped"`
	WasRunning         bool     `json:"was_running"`
	Refused            bool     `json:"refused,omitempty"`
	PID                int      `json:"pid,omitempty"`
	Signal             string   `json:"signal,omitempty"`
	Forced             bool     `json:"forced,omitempty"`
	ProcessZombie      bool     `json:"process_zombie,omitempty"`
	RuntimeDir         string   `json:"runtime_dir"`
	PIDFile            string   `json:"pid_file"`
	RemovedPIDFile     bool     `json:"removed_pid_file,omitempty"`
	StalePIDFile       bool     `json:"stale_pid_file,omitempty"`
	StalePIDFileReason string   `json:"stale_pid_file_reason,omitempty"`
	Error              string   `json:"error,omitempty"`
	Warnings           []string `json:"warnings,omitempty"`
	NextActions        []string `json:"next_actions,omitempty"`
}

type RestartResult struct {
	Restarted   bool        `json:"restarted"`
	Stop        StopResult  `json:"stop"`
	Start       StartResult `json:"start"`
	Error       string      `json:"error,omitempty"`
	Warnings    []string    `json:"warnings,omitempty"`
	NextActions []string    `json:"next_actions,omitempty"`
}

func Status(opts StatusOptions) StatusResult {
	normalized := normalizeStartOptions(StartOptions{
		CorePath:   opts.CorePath,
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
	if !processRunning(pid) {
		result.StalePIDFile = true
		result.StalePIDFileReason = "pid file points to a process that is not running"
		return result
	}
	if processZombie(pid) {
		result.ProcessZombie = true
		result.StalePIDFile = true
		result.StalePIDFileReason = "pid file points to a zombie process"
		return result
	}
	result.ProcessAlive = true
	if ok, reason := processMatchesRuntime(pid, normalized); !ok {
		result.StalePIDFile = true
		result.StalePIDFileReason = reason
		return result
	}
	result.Running = true
	return result
}

func Restart(ctx context.Context, opts RestartOptions) (RestartResult, error) {
	startOpts := normalizeStartOptions(StartOptions{
		CorePath:   opts.CorePath,
		ConfigPath: opts.ConfigPath,
		WorkDir:    opts.WorkDir,
		LogPath:    opts.LogPath,
	})
	runOpts := normalizeOptions(Options{
		CorePath:   startOpts.CorePath,
		ConfigPath: startOpts.ConfigPath,
		WorkDir:    startOpts.WorkDir,
		LogPath:    startOpts.LogPath,
	})
	result := RestartResult{
		Warnings: append([]string(nil), NetworkInterruptionWarnings...),
		NextActions: []string{
			"call runtime_status to verify the restarted Mihomo process",
		},
	}
	if err := runOpts.validate(); err != nil {
		return result, err
	}
	if err := os.MkdirAll(runOpts.WorkDir, 0o755); err != nil {
		return result, err
	}
	if err := os.MkdirAll(filepath.Dir(runOpts.LogPath), 0o755); err != nil {
		return result, err
	}
	if err := testConfig(ctx, runOpts); err != nil {
		return result, err
	}

	stop, err := Stop(StopOptions{
		WorkDir:   runOpts.WorkDir,
		Timeout:   opts.StopTimeout,
		ForceKill: opts.ForceKill,
	})
	result.Stop = stop
	if err != nil {
		return result, err
	}
	if stop.Error != "" {
		result.Error = stop.Error
		return result, nil
	}

	start, err := Start(ctx, startOpts)
	result.Start = start
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	result.Restarted = start.Started
	return result, nil
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
	if processZombie(pid) {
		result.ProcessZombie = true
		result.StalePIDFile = true
		result.StalePIDFileReason = "pid file points to a zombie process"
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
		if !processRunning(pid) || processZombie(pid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

var processZombie = defaultProcessZombie

func defaultProcessZombie(pid int) bool {
	state, ok := readProcStatState(pid)
	return ok && state == 'Z'
}

func readProcStatState(pid int) (byte, bool) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, false
	}
	return parseProcStatState(string(data))
}

func parseProcStatState(stat string) (byte, bool) {
	closeParen := strings.LastIndex(stat, ")")
	if closeParen < 0 || closeParen+1 >= len(stat) {
		return 0, false
	}
	fields := strings.Fields(stat[closeParen+1:])
	if len(fields) == 0 || len(fields[0]) != 1 {
		return 0, false
	}
	return fields[0][0], true
}

func processMatchesRuntime(pid int, opts StartOptions) (bool, string) {
	args, ok, err := readProcessCommandLine(pid)
	if err != nil || !ok || len(args) == 0 {
		return true, ""
	}
	if !processCommandArgsLookLikeCore(args, opts.CorePath) {
		return false, "pid file points to a live process, but it is not the configured Mihomo core"
	}
	if !processCommandHasArg(args, "-d", opts.WorkDir) {
		return false, "pid file points to a live process, but it is not using the configured runtime directory"
	}
	if !processCommandHasArg(args, "-f", opts.ConfigPath) {
		return false, "pid file points to a live process, but it is not using the configured config"
	}
	return true, ""
}

var readProcessCommandLine = func(pid int) ([]string, bool, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	fields := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
	if len(fields) == 1 && fields[0] == "" {
		return nil, true, nil
	}
	return fields, true, nil
}

func processCommandLooksLikeCore(command, corePath string) bool {
	commandBase := filepath.Base(command)
	coreBase := filepath.Base(corePath)
	if command == corePath || commandBase == coreBase {
		return true
	}
	lower := strings.ToLower(commandBase)
	return strings.Contains(lower, "mihomo")
}

func processCommandArgsLookLikeCore(args []string, corePath string) bool {
	limit := len(args)
	if limit > 2 {
		limit = 2
	}
	for i := 0; i < limit; i++ {
		if processCommandLooksLikeCore(args[i], corePath) {
			return true
		}
	}
	return false
}

func processCommandHasArg(args []string, flag, expected string) bool {
	expected = filepath.Clean(expected)
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && filepath.Clean(args[i+1]) == expected {
			return true
		}
	}
	return false
}
