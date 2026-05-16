package corerun

import (
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

	"gopkg.in/yaml.v3"
)

type StartOptions struct {
	CorePath   string
	ConfigPath string
	WorkDir    string
	LogPath    string
	Foreground bool
}

type StartResult struct {
	Started            bool     `json:"started"`
	AlreadyRunning     bool     `json:"already_running"`
	PID                int      `json:"pid,omitempty"`
	Config             string   `json:"config"`
	RuntimeDir         string   `json:"runtime_dir"`
	LogFile            string   `json:"log_file"`
	ExternalController string   `json:"external_controller,omitempty"`
	ExternalUIURL      string   `json:"external_ui_url,omitempty"`
	Warnings           []string `json:"warnings"`
}

var NetworkInterruptionWarnings = []string{
	"Starting or restarting the proxy runtime may temporarily interrupt network connectivity.",
	"The Agent itself may depend on the current network/proxy path and could be disconnected after this operation.",
}

func Start(ctx context.Context, opts StartOptions) (StartResult, error) {
	opts = normalizeStartOptions(opts)
	if opts.Foreground {
		return StartResult{}, fmt.Errorf("foreground=true is not supported by MCP run_runtime; use the CLI run command for foreground execution")
	}
	runOpts := normalizeOptions(Options{
		CorePath:   opts.CorePath,
		ConfigPath: opts.ConfigPath,
		WorkDir:    opts.WorkDir,
		LogPath:    opts.LogPath,
	})
	if err := runOpts.validate(); err != nil {
		return StartResult{}, err
	}
	if err := os.MkdirAll(runOpts.WorkDir, 0o755); err != nil {
		return StartResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(runOpts.LogPath), 0o755); err != nil {
		return StartResult{}, err
	}
	baseResult := StartResult{
		Config:             runOpts.ConfigPath,
		RuntimeDir:         runOpts.WorkDir,
		LogFile:            runOpts.LogPath,
		ExternalController: readExternalController(runOpts.ConfigPath),
		Warnings:           append([]string(nil), NetworkInterruptionWarnings...),
	}
	baseResult.ExternalUIURL = externalUIURL(runOpts.ConfigPath, baseResult.ExternalController)

	pidPath := runtimePIDPath(runOpts.WorkDir)
	if pid, ok := readRunningPID(pidPath); ok {
		baseResult.AlreadyRunning = true
		baseResult.PID = pid
		baseResult.Warnings = append(baseResult.Warnings, "Runtime is already running; run_runtime did not start a second process.")
		return baseResult, nil
	}
	if err := testConfig(ctx, runOpts); err != nil {
		return StartResult{}, err
	}

	logFile, err := os.OpenFile(runOpts.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return StartResult{}, err
	}
	cmd := exec.Command(runOpts.CorePath, "-d", runOpts.WorkDir, "-f", runOpts.ConfigPath)
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return StartResult{}, err
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
		_ = logFile.Close()
		return StartResult{}, err
	}
	_ = logFile.Close()
	go func() {
		_ = cmd.Wait()
		_ = os.Remove(pidPath)
	}()
	baseResult.Started = true
	baseResult.PID = cmd.Process.Pid
	return baseResult, nil
}

func normalizeStartOptions(opts StartOptions) StartOptions {
	opts.CorePath = strings.TrimSpace(opts.CorePath)
	opts.ConfigPath = strings.TrimSpace(opts.ConfigPath)
	opts.WorkDir = strings.TrimSpace(opts.WorkDir)
	opts.LogPath = strings.TrimSpace(opts.LogPath)
	if opts.CorePath == "" {
		opts.CorePath = "bin/mihomo"
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
	if processRunning(pid) {
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

func testConfig(ctx context.Context, opts Options) error {
	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	output, err := exec.CommandContext(runCtx, opts.CorePath, "-d", opts.WorkDir, "-f", opts.ConfigPath, "-t").CombinedOutput()
	if err != nil {
		return fmt.Errorf("mihomo config test failed: %s", compactStartOutput(output, err))
	}
	return nil
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
	config := readConfigForRuntime(path)
	if controller, ok := config["external-controller"].(string); ok {
		return controller
	}
	return ""
}

func externalUIURL(path, controller string) string {
	if controller == "" {
		return ""
	}
	config := readConfigForRuntime(path)
	ui, _ := config["external-ui"].(string)
	if strings.TrimSpace(ui) == "" {
		return ""
	}
	return "http://" + controller + "/ui"
}

func readConfigForRuntime(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var config map[string]any
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil
	}
	return config
}
