package corerun

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"localclash/internal/mihomoapi"
	"localclash/internal/mihomotest"
	"localclash/internal/runtimeprofile"
)

const (
	RestartStrategyProcess   = "process_restart"
	RestartStrategyHotReload = "hot_reload"
)

type StatusOptions struct {
	CorePath   string
	ConfigPath string
	WorkDir    string
	LogPath    string
}

type StatusResult struct {
	Running            bool     `json:"running"`
	PID                int      `json:"pid,omitempty"`
	PIDs               []int    `json:"pids,omitempty"`
	ProcessNames       []string `json:"process_names,omitempty"`
	RuntimeDir         string   `json:"runtime_dir"`
	Config             string   `json:"config"`
	LogFile            string   `json:"log_file"`
	ExternalController string   `json:"external_controller,omitempty"`
	ExternalUIURL      string   `json:"external_ui_url,omitempty"`
}

type StopOptions struct {
	CorePath   string
	ConfigPath string
	WorkDir    string
	Timeout    time.Duration
	ForceKill  bool
}

type RestartOptions struct {
	CorePath            string
	ConfigPath          string
	WorkDir             string
	LogPath             string
	Strategy            string
	ConfigSHA256        string
	AttestationPath     string
	ReloadTimeout       time.Duration
	ValidationCachePath string
	ForceConfigTest     bool
	StopTimeout         time.Duration
	ForceKill           bool
	OnStage             func(RestartStageEvent) `json:"-"`
}

type RestartStageEvent struct {
	Stage      string `json:"stage"`
	Event      string `json:"event"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	PID        int    `json:"pid,omitempty"`
	Error      string `json:"error,omitempty"`
}

type RestartTimings struct {
	ValidateMS int64 `json:"validate_ms,omitempty"`
	StopMS     int64 `json:"stop_ms"`
	StartMS    int64 `json:"start_ms"`
	StatusMS   int64 `json:"status_ms"`
	TotalMS    int64 `json:"total_ms"`
}

type StopResult struct {
	Stopped      bool     `json:"stopped"`
	WasRunning   bool     `json:"was_running"`
	Refused      bool     `json:"refused,omitempty"`
	PID          int      `json:"pid,omitempty"`
	PIDs         []int    `json:"pids,omitempty"`
	ProcessNames []string `json:"process_names,omitempty"`
	Signal       string   `json:"signal,omitempty"`
	Forced       bool     `json:"forced,omitempty"`
	StoppedPIDs  []int    `json:"stopped_pids,omitempty"`
	RuntimeDir   string   `json:"runtime_dir"`
	Error        string   `json:"error,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
	NextActions  []string `json:"next_actions,omitempty"`
}

type RestartResult struct {
	Restarted        bool                        `json:"restarted"`
	Reloaded         bool                        `json:"reloaded,omitempty"`
	AppliedStrategy  string                      `json:"applied_strategy"`
	ConfigSHA256     string                      `json:"config_sha256,omitempty"`
	ConfigValidation mihomotest.ValidationResult `json:"config_validation"`
	Stop             StopResult                  `json:"stop"`
	Start            StartResult                 `json:"start"`
	Status           StatusResult                `json:"status"`
	HotReload        *HotReloadResult            `json:"hot_reload,omitempty"`
	Timings          RestartTimings              `json:"timings"`
	Error            string                      `json:"error,omitempty"`
	Warnings         []string                    `json:"warnings,omitempty"`
	NextActions      []string                    `json:"next_actions,omitempty"`
}

type HotReloadResult struct {
	Config     string `json:"config"`
	StatusCode int    `json:"status_code"`
}

func Status(opts StatusOptions) StatusResult {
	normalized := normalizeStartOptions(StartOptions{
		CorePath:   opts.CorePath,
		ConfigPath: opts.ConfigPath,
		WorkDir:    opts.WorkDir,
		LogPath:    opts.LogPath,
	})
	result := StatusResult{
		RuntimeDir: normalized.WorkDir,
		Config:     normalized.ConfigPath,
		LogFile:    normalized.LogPath,
	}
	endpoints := readRuntimeConfigEndpoints(normalized.ConfigPath)
	result.ExternalController = endpoints.ExternalController
	result.ExternalUIURL = externalUIURL(result.ExternalController, endpoints.ExternalUI)

	processes := findManagedRuntimeProcesses()
	if len(processes) == 0 {
		return result
	}
	result.Running = true
	for _, process := range processes {
		result.PIDs = appendUniquePIDs(result.PIDs, process.PID)
		result.ProcessNames = appendUniqueStrings(result.ProcessNames, process.Name)
	}
	result.PID = processes[0].PID
	return result
}

func Restart(ctx context.Context, opts RestartOptions) (RestartResult, error) {
	totalStarted := time.Now()
	opts = normalizeRestartOptions(opts)
	stage := func(event RestartStageEvent) {
		if opts.OnStage != nil {
			opts.OnStage(event)
		}
	}
	startOpts := normalizeStartOptions(StartOptions{
		CorePath:       opts.CorePath,
		ConfigPath:     opts.ConfigPath,
		WorkDir:        opts.WorkDir,
		LogPath:        opts.LogPath,
		SkipConfigTest: true,
	})
	startOpts.ValidationCachePath = opts.ValidationCachePath
	startOpts.ForceConfigTest = opts.ForceConfigTest
	runOpts := normalizeOptions(Options{
		CorePath:   startOpts.CorePath,
		ConfigPath: startOpts.ConfigPath,
		WorkDir:    startOpts.WorkDir,
		LogPath:    startOpts.LogPath,
	})
	result := RestartResult{
		AppliedStrategy: opts.Strategy,
		Warnings:        append([]string(nil), NetworkInterruptionWarnings...),
		NextActions: []string{
			"call runtime_status to verify the restarted Mihomo process",
		},
	}
	if opts.Strategy == RestartStrategyHotReload {
		return hotReload(ctx, opts, runOpts, result, stage, totalStarted)
	}
	if err := validateManagedCorePath(runOpts.CorePath); err != nil {
		return result, err
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
	validateStarted := time.Now()
	stage(RestartStageEvent{Stage: "config_test", Event: "started"})
	validation, err := validateConfig(ctx, runOpts, opts.ValidationCachePath, opts.ForceConfigTest)
	result.ConfigValidation = validation
	result.Timings.ValidateMS = elapsedMS(validateStarted)
	if err != nil {
		result.Error = err.Error()
		result.Timings.TotalMS = elapsedMS(totalStarted)
		result.NextActions = []string{
			"inspect config_validation and fix generated config before restarting runtime",
			"call config_render after durable localClash intent changes",
			"call doctor --json for a full validation report",
		}
		stage(RestartStageEvent{Stage: "config_test", Event: "error", DurationMS: result.Timings.ValidateMS, Error: err.Error()})
		return result, nil
	}
	stage(RestartStageEvent{Stage: "config_test", Event: "done", DurationMS: result.Timings.ValidateMS})

	stopStarted := time.Now()
	stage(RestartStageEvent{Stage: "stop", Event: "started"})
	stop, err := Stop(StopOptions{
		CorePath:   runOpts.CorePath,
		ConfigPath: runOpts.ConfigPath,
		WorkDir:    runOpts.WorkDir,
		Timeout:    opts.StopTimeout,
		ForceKill:  opts.ForceKill,
	})
	result.Stop = stop
	result.Timings.StopMS = elapsedMS(stopStarted)
	if err != nil {
		result.Timings.TotalMS = elapsedMS(totalStarted)
		stage(RestartStageEvent{Stage: "stop", Event: "error", DurationMS: result.Timings.StopMS, PID: stop.PID, Error: err.Error()})
		return result, err
	}
	if stop.Error != "" {
		result.Error = stop.Error
		result.Timings.TotalMS = elapsedMS(totalStarted)
		stage(RestartStageEvent{Stage: "stop", Event: "error", DurationMS: result.Timings.StopMS, PID: stop.PID, Error: stop.Error})
		return result, nil
	}
	stage(RestartStageEvent{Stage: "stop", Event: "done", DurationMS: result.Timings.StopMS, PID: stop.PID})

	startStarted := time.Now()
	stage(RestartStageEvent{Stage: "start", Event: "started"})
	start, err := Start(ctx, startOpts)
	result.Start = start
	result.Timings.StartMS = elapsedMS(startStarted)
	if err != nil {
		result.Error = err.Error()
		result.Timings.TotalMS = elapsedMS(totalStarted)
		stage(RestartStageEvent{Stage: "start", Event: "error", DurationMS: result.Timings.StartMS, PID: start.PID, Error: err.Error()})
		return result, nil
	}
	start.ConfigValidation = validation
	result.Start = start
	stage(RestartStageEvent{Stage: "start", Event: "done", DurationMS: result.Timings.StartMS, PID: start.PID})

	statusStarted := time.Now()
	stage(RestartStageEvent{Stage: "status", Event: "started"})
	status := Status(StatusOptions{
		CorePath:   runOpts.CorePath,
		ConfigPath: runOpts.ConfigPath,
		WorkDir:    runOpts.WorkDir,
		LogPath:    runOpts.LogPath,
	})
	result.Status = status
	result.Timings.StatusMS = elapsedMS(statusStarted)
	stage(RestartStageEvent{Stage: "status", Event: "done", DurationMS: result.Timings.StatusMS, PID: status.PID})
	result.Restarted = start.Started
	result.Timings.TotalMS = elapsedMS(totalStarted)
	return result, nil
}

func normalizeRestartOptions(opts RestartOptions) RestartOptions {
	opts.Strategy = strings.TrimSpace(opts.Strategy)
	if opts.Strategy == "" {
		opts.Strategy = RestartStrategyProcess
	}
	if opts.ReloadTimeout <= 0 {
		opts.ReloadTimeout = 10 * time.Second
	}
	return opts
}

func hotReload(ctx context.Context, opts RestartOptions, runOpts Options, result RestartResult, stage func(RestartStageEvent), totalStarted time.Time) (RestartResult, error) {
	if opts.ForceConfigTest {
		result.Error = "force_config_test is not supported for hot_reload; call mihomo_config_test first"
		result.Timings.TotalMS = elapsedMS(totalStarted)
		return result, nil
	}
	if _, err := os.Stat(runOpts.ConfigPath); err != nil {
		result.Error = fmt.Sprintf("config %q is not available: %v", runOpts.ConfigPath, err)
		result.Timings.TotalMS = elapsedMS(totalStarted)
		return result, nil
	}
	hashStarted := time.Now()
	stage(RestartStageEvent{Stage: "hash_check", Event: "started"})
	expected := strings.TrimSpace(opts.ConfigSHA256)
	if expected == "" {
		attestationPath := opts.AttestationPath
		if strings.TrimSpace(attestationPath) == "" {
			attestationPath = mihomotest.DefaultAttestationPath(runOpts.WorkDir)
		}
		attestation, err := mihomotest.ReadAttestation(attestationPath)
		if err != nil {
			result.Error = "cannot read mihomo_config_test attestation: " + err.Error()
			result.Timings.TotalMS = elapsedMS(totalStarted)
			stage(RestartStageEvent{Stage: "hash_check", Event: "error", DurationMS: elapsedMS(hashStarted), Error: result.Error})
			return result, nil
		}
		expected = attestation.ConfigSHA256
	}
	actual, err := mihomotest.VerifyConfigHash(runOpts.ConfigPath, expected)
	if err != nil {
		result.ConfigSHA256 = actual
		result.Error = err.Error()
		result.Timings.TotalMS = elapsedMS(totalStarted)
		stage(RestartStageEvent{Stage: "hash_check", Event: "error", DurationMS: elapsedMS(hashStarted), Error: err.Error()})
		return result, nil
	}
	result.ConfigSHA256 = actual
	stage(RestartStageEvent{Stage: "hash_check", Event: "done", DurationMS: elapsedMS(hashStarted)})

	statusStarted := time.Now()
	stage(RestartStageEvent{Stage: "status", Event: "started"})
	status := Status(StatusOptions{
		CorePath:   runOpts.CorePath,
		ConfigPath: runOpts.ConfigPath,
		WorkDir:    runOpts.WorkDir,
		LogPath:    runOpts.LogPath,
	})
	result.Status = status
	result.Timings.StatusMS = elapsedMS(statusStarted)
	if !status.Running {
		result.Error = "runtime is not running; hot_reload requires an active Mihomo controller"
		result.Timings.TotalMS = elapsedMS(totalStarted)
		stage(RestartStageEvent{Stage: "status", Event: "error", DurationMS: result.Timings.StatusMS, Error: result.Error})
		return result, nil
	}
	stage(RestartStageEvent{Stage: "status", Event: "done", DurationMS: result.Timings.StatusMS, PID: status.PID})

	reloadStarted := time.Now()
	stage(RestartStageEvent{Stage: "hot_reload", Event: "started"})
	client, err := mihomoapi.NewFromConfig(runOpts.ConfigPath)
	if err != nil {
		result.Error = err.Error()
		result.Timings.TotalMS = elapsedMS(totalStarted)
		stage(RestartStageEvent{Stage: "hot_reload", Event: "error", DurationMS: elapsedMS(reloadStarted), Error: err.Error()})
		return result, nil
	}
	configPath, err := filepath.Abs(runOpts.ConfigPath)
	if err != nil {
		result.Error = err.Error()
		result.Timings.TotalMS = elapsedMS(totalStarted)
		stage(RestartStageEvent{Stage: "hot_reload", Event: "error", DurationMS: elapsedMS(reloadStarted), Error: err.Error()})
		return result, nil
	}
	response, err := client.Request(ctx, mihomoapi.RequestOptions{
		Method:  "PUT",
		Path:    "/configs",
		Query:   map[string]any{"force": true},
		Body:    map[string]any{"path": configPath},
		Timeout: opts.ReloadTimeout,
	})
	result.HotReload = &HotReloadResult{Config: configPath, StatusCode: response.StatusCode}
	if err != nil {
		result.Error = err.Error()
		result.Timings.TotalMS = elapsedMS(totalStarted)
		stage(RestartStageEvent{Stage: "hot_reload", Event: "error", DurationMS: elapsedMS(reloadStarted), Error: err.Error()})
		return result, nil
	}
	result.Reloaded = true
	result.Timings.TotalMS = elapsedMS(totalStarted)
	stage(RestartStageEvent{Stage: "hot_reload", Event: "done", DurationMS: elapsedMS(reloadStarted), PID: status.PID})
	return result, nil
}

func elapsedMS(started time.Time) int64 {
	return time.Since(started).Milliseconds()
}

func Stop(opts StopOptions) (StopResult, error) {
	normalized := normalizeStartOptions(StartOptions{
		CorePath:   opts.CorePath,
		ConfigPath: opts.ConfigPath,
		WorkDir:    opts.WorkDir,
	})
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	result := StopResult{
		RuntimeDir: normalized.WorkDir,
	}

	processes := findManagedRuntimeProcesses()
	if len(processes) == 0 {
		return result, nil
	}
	result.PID = processes[0].PID
	for _, process := range processes {
		result.PIDs = appendUniquePIDs(result.PIDs, process.PID)
		result.ProcessNames = appendUniqueStrings(result.ProcessNames, process.Name)
	}
	return stopRuntimePIDs(result.PIDs, result, timeout, opts.ForceKill)
}

func stopRuntimePIDs(pids []int, result StopResult, timeout time.Duration, forceKill bool) (StopResult, error) {
	result.PIDs = appendUniquePIDs(result.PIDs, pids...)
	if len(pids) == 0 {
		return result, nil
	}
	result.WasRunning = true
	result.Signal = "SIGTERM"
	for _, pid := range pids {
		process, err := os.FindProcess(pid)
		if err != nil {
			return result, err
		}
		if err := process.Signal(syscall.SIGTERM); err != nil {
			return result, fmt.Errorf("send SIGTERM to pid %d: %w", pid, err)
		}
	}
	if waitForAllExit(pids, timeout) {
		result.Stopped = true
		result.StoppedPIDs = appendUniquePIDs(result.StoppedPIDs, pids...)
		return result, nil
	}
	if !forceKill {
		result.Error = "runtime did not stop before timeout"
		return result, nil
	}
	result.Forced = true
	result.Signal = "SIGKILL"
	for _, pid := range pids {
		if !processRunning(pid) || processZombie(pid) {
			continue
		}
		process, err := os.FindProcess(pid)
		if err != nil {
			return result, err
		}
		if err := process.Kill(); err != nil {
			return result, fmt.Errorf("send SIGKILL to pid %d: %w", pid, err)
		}
	}
	if waitForAllExit(pids, timeout) {
		result.Stopped = true
		result.StoppedPIDs = appendUniquePIDs(result.StoppedPIDs, pids...)
		return result, nil
	}
	result.Error = "runtime did not stop after SIGKILL"
	return result, nil
}

func validateManagedCorePath(corePath string) error {
	if managedProcessNames[filepath.Base(corePath)] {
		return nil
	}
	return fmt.Errorf("background runtime core %q is not a localClash managed core name; use %s or %s", corePath, runtimeprofile.ManagedMetaCoreName, runtimeprofile.ManagedSmartCoreName)
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

func waitForAllExit(pids []int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		allExited := true
		for _, pid := range pids {
			if processRunning(pid) && !processZombie(pid) {
				allExited = false
				break
			}
		}
		if allExited {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

type runtimeProcess struct {
	PID  int
	Name string
}

var managedProcessNames = map[string]bool{
	runtimeprofile.ManagedMetaCoreName:  true,
	runtimeprofile.ManagedSmartCoreName: true,
}

func findManagedRuntimeProcesses() []runtimeProcess {
	return findRuntimeProcessesByName(managedProcessNames)
}

func findRuntimeProcessesByName(names map[string]bool) []runtimeProcess {
	var processes []runtimeProcess
	for _, pid := range listProcessIDs() {
		if pid <= 0 || pid == os.Getpid() {
			continue
		}
		if !processRunning(pid) || processZombie(pid) {
			continue
		}
		name, ok, err := readProcessComm(pid)
		if err != nil || !ok || !names[name] {
			continue
		}
		if processCommandHasExactArg(pid, "-t") {
			continue
		}
		processes = append(processes, runtimeProcess{PID: pid, Name: name})
	}
	sort.Slice(processes, func(i, j int) bool {
		if processes[i].PID == processes[j].PID {
			return processes[i].Name < processes[j].Name
		}
		return processes[i].PID < processes[j].PID
	})
	return processes
}

func processCommandHasExactArg(pid int, arg string) bool {
	args, ok, err := readProcessCommandLine(pid)
	if err != nil || !ok {
		return false
	}
	for _, value := range args {
		if value == arg {
			return true
		}
	}
	return false
}

var readProcessComm = func(pid int) (string, bool, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "comm"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	name := strings.TrimSpace(string(data))
	if name == "" {
		return "", true, nil
	}
	return name, true, nil
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

var listProcessIDs = defaultListProcessIDs

func defaultListProcessIDs() []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var pids []int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids
}

func appendUniquePIDs(existing []int, pids ...int) []int {
	seen := map[int]bool{}
	for _, pid := range existing {
		if pid > 0 {
			seen[pid] = true
		}
	}
	for _, pid := range pids {
		if pid <= 0 || seen[pid] {
			continue
		}
		existing = append(existing, pid)
		seen[pid] = true
	}
	return existing
}

func appendUniqueStrings(existing []string, values ...string) []string {
	seen := map[string]bool{}
	for _, value := range existing {
		if value != "" {
			seen[value] = true
		}
	}
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		existing = append(existing, value)
		seen[value] = true
	}
	return existing
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
