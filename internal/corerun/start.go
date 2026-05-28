package corerun

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"localclash/internal/mihomotest"
	"localclash/internal/runtimeprofile"
)

type StartOptions struct {
	CorePath            string
	ConfigPath          string
	WorkDir             string
	LogPath             string
	Foreground          bool
	SkipConfigTest      bool
	ValidationCachePath string
	ForceConfigTest     bool
	OnStage             func(StartStageEvent) `json:"-"`
}

type StartStageEvent struct {
	Stage      string         `json:"stage"`
	Event      string         `json:"event"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	PID        int            `json:"pid,omitempty"`
	Error      string         `json:"error,omitempty"`
	Fields     map[string]any `json:"fields,omitempty"`
}

type StartResult struct {
	Started            bool                        `json:"started"`
	AlreadyRunning     bool                        `json:"already_running"`
	PID                int                         `json:"pid,omitempty"`
	Config             string                      `json:"config"`
	RuntimeDir         string                      `json:"runtime_dir"`
	LogFile            string                      `json:"log_file"`
	ExternalController string                      `json:"external_controller,omitempty"`
	ExternalUIURL      string                      `json:"external_ui_url,omitempty"`
	ConfigTestSkipped  bool                        `json:"config_test_skipped,omitempty"`
	ConfigValidation   mihomotest.ValidationResult `json:"config_validation"`
	Warnings           []string                    `json:"warnings"`
	NextActions        []string                    `json:"next_actions,omitempty"`
}

var NetworkInterruptionWarnings = []string{
	"Starting or restarting the proxy runtime may temporarily interrupt network connectivity.",
	"The Agent itself may depend on the current network/proxy path and could be disconnected after this operation.",
}

var afterProcessStart = func(*exec.Cmd) {}

func Start(ctx context.Context, opts StartOptions) (StartResult, error) {
	opts = normalizeStartOptions(opts)
	stage := startStageEmitter(opts.OnStage)
	if opts.Foreground {
		return StartResult{}, fmt.Errorf("foreground=true is not supported by MCP run_runtime; use the CLI run command for foreground execution")
	}
	finish := stage("prepare", nil)
	runOpts := normalizeOptions(Options{
		CorePath:   opts.CorePath,
		ConfigPath: opts.ConfigPath,
		WorkDir:    opts.WorkDir,
		LogPath:    opts.LogPath,
	})
	if err := runOpts.validate(); err != nil {
		finish(err, 0)
		return StartResult{}, err
	}
	if err := os.MkdirAll(runOpts.WorkDir, 0o755); err != nil {
		finish(err, 0)
		return StartResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(runOpts.LogPath), 0o755); err != nil {
		finish(err, 0)
		return StartResult{}, err
	}
	finish(nil, 0)
	baseResult := StartResult{
		Config:     runOpts.ConfigPath,
		RuntimeDir: runOpts.WorkDir,
		LogFile:    runOpts.LogPath,
		Warnings:   append([]string(nil), NetworkInterruptionWarnings...),
	}
	endpoints := readRuntimeConfigEndpoints(runOpts.ConfigPath)
	baseResult.ExternalController = endpoints.ExternalController
	baseResult.ExternalUIURL = externalUIURL(baseResult.ExternalController, endpoints.ExternalUI)

	finish = stage("status_check", nil)
	pidPath := runtimePIDPath(runOpts.WorkDir)
	if pid, ok := readRunningPID(pidPath); ok {
		if match, _ := processMatchesRuntime(pid, StartOptions{
			CorePath:   runOpts.CorePath,
			ConfigPath: runOpts.ConfigPath,
			WorkDir:    runOpts.WorkDir,
			LogPath:    runOpts.LogPath,
		}); !match {
			_ = os.Remove(pidPath)
		} else {
			baseResult.AlreadyRunning = true
			baseResult.PID = pid
			baseResult.Warnings = append(baseResult.Warnings, "Runtime is already running; run_runtime did not start a second process.")
			finish(nil, pid)
			return baseResult, nil
		}
	}
	finish(nil, 0)
	if opts.SkipConfigTest {
		baseResult.ConfigTestSkipped = true
	} else {
		finish := stage("config_test", map[string]any{"cache": validationCachePath(opts.ValidationCachePath, runOpts.WorkDir)})
		validation, err := validateConfig(ctx, runOpts, opts.ValidationCachePath, opts.ForceConfigTest)
		baseResult.ConfigValidation = validation
		if err != nil {
			finish(err, 0)
			return baseResult, err
		}
		finish(nil, 0)
	}

	finish = stage("open_log", map[string]any{"log_file": runOpts.LogPath})
	logFile, err := os.OpenFile(runOpts.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		finish(err, 0)
		return StartResult{}, err
	}
	finish(nil, 0)

	finish = stage("start_process", map[string]any{"core": runOpts.CorePath, "config": runOpts.ConfigPath, "work_dir": runOpts.WorkDir})
	cmd := exec.Command(runOpts.CorePath, "-d", runOpts.WorkDir, "-f", runOpts.ConfigPath)
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		finish(err, 0)
		return StartResult{}, err
	}
	afterProcessStart(cmd)
	finish(nil, cmd.Process.Pid)

	finish = stage("write_pid_file", map[string]any{"pid_file": pidPath})
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		_ = logFile.Close()
		finish(err, cmd.Process.Pid)
		return StartResult{}, err
	}
	finish(nil, cmd.Process.Pid)
	_ = logFile.Close()
	go func() {
		_ = cmd.Wait()
		_ = os.Remove(pidPath)
	}()
	baseResult.Started = true
	baseResult.PID = cmd.Process.Pid
	return baseResult, nil
}

func startStageEmitter(callback func(StartStageEvent)) func(string, map[string]any) func(error, int) {
	return func(stage string, fields map[string]any) func(error, int) {
		if callback == nil {
			return func(error, int) {}
		}
		started := time.Now()
		callback(StartStageEvent{Stage: stage, Event: "started", Fields: fields})
		return func(err error, pid int) {
			event := StartStageEvent{
				Stage:      stage,
				Event:      "done",
				DurationMS: time.Since(started).Milliseconds(),
				PID:        pid,
			}
			if err != nil {
				event.Event = "error"
				event.Error = err.Error()
			}
			callback(event)
		}
	}
}

func normalizeStartOptions(opts StartOptions) StartOptions {
	opts.CorePath = strings.TrimSpace(opts.CorePath)
	opts.ConfigPath = strings.TrimSpace(opts.ConfigPath)
	opts.WorkDir = strings.TrimSpace(opts.WorkDir)
	opts.LogPath = strings.TrimSpace(opts.LogPath)
	if opts.CorePath == "" {
		opts.CorePath = runtimeprofile.MetaCorePath
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = "generated/mihomo.yaml"
	}
	if opts.WorkDir == "" {
		opts.WorkDir = ".runtime/mihomo"
	}
	if opts.LogPath == "" {
		opts.LogPath = filepath.Join(opts.WorkDir, "mihomo.log")
	}
	return opts
}

func runtimePIDPath(workDir string) string {
	return filepath.Join(workDir, "mihomo.pid")
}

func readRunningPID(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	if processRunning(pid) && !processZombie(pid) {
		return pid, true
	}
	return 0, false
}

func processRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

func validateConfig(ctx context.Context, opts Options, cachePath string, force bool) (mihomotest.ValidationResult, error) {
	result, err := mihomotest.ValidateCached(ctx, mihomotest.ValidationOptions{
		CorePath:   opts.CorePath,
		ConfigPath: opts.ConfigPath,
		WorkDir:    opts.WorkDir,
		CachePath:  validationCachePath(cachePath, opts.WorkDir),
		Force:      force,
	})
	if err != nil {
		if result.Output != "" {
			return result, fmt.Errorf("mihomo config test failed: %s", compactStartOutput([]byte(result.Output), err))
		}
		return result, fmt.Errorf("mihomo config test failed: %w", err)
	}
	if !result.Passed {
		return result, fmt.Errorf("mihomo config test failed: %s", result.Output)
	}
	return result, nil
}

func validationCachePath(path, workDir string) string {
	if strings.TrimSpace(path) != "" {
		return path
	}
	return mihomotest.DefaultCachePath(workDir)
}

func compactStartOutput(output []byte, err error) string {
	text := strings.TrimSpace(string(output))
	if text == "" {
		if err != nil {
			return err.Error()
		}
		return ""
	}
	lines := strings.Split(text, "\n")
	const maxLines = 8
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n")
}

func readExternalController(path string) string {
	return readRuntimeConfigEndpoints(path).ExternalController
}

func externalUIURL(controller, ui string) string {
	if controller == "" || strings.TrimSpace(ui) == "" {
		return ""
	}
	return "http://" + controller + "/ui"
}

type runtimeConfigEndpoints struct {
	ExternalController string
	ExternalUI         string
}

func readRuntimeConfigEndpoints(path string) runtimeConfigEndpoints {
	file, err := os.Open(path)
	if err != nil {
		return runtimeConfigEndpoints{}
	}
	defer file.Close()

	var endpoints runtimeConfigEndpoints
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		key, value, ok := splitTopLevelYAMLScalar(line)
		if !ok {
			continue
		}
		switch key {
		case "external-controller":
			endpoints.ExternalController = value
		case "external-ui":
			endpoints.ExternalUI = value
		}
		if endpoints.ExternalController != "" && endpoints.ExternalUI != "" {
			break
		}
	}
	return endpoints
}

func splitTopLevelYAMLScalar(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	idx := strings.Index(trimmed, ":")
	if idx <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(trimmed[:idx])
	value := strings.TrimSpace(stripInlineYAMLComment(trimmed[idx+1:]))
	value = strings.Trim(value, `"'`)
	return key, value, true
}

func stripInlineYAMLComment(value string) string {
	inSingle := false
	inDouble := false
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble && (i == 0 || value[i-1] == ' ' || value[i-1] == '\t') {
				return value[:i]
			}
		}
	}
	return value
}
