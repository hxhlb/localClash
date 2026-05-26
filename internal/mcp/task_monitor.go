package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"sync"
	"syscall"
	"time"
)

const (
	defaultTaskMonitorInterval       = 500 * time.Millisecond
	defaultTaskMonitorCPUThreshold   = 90.0
	defaultTaskMonitorCPUBurst       = 5 * time.Second
	defaultTaskMonitorStageWarnAfter = 10 * time.Second
	defaultTaskMonitorRingSize       = 240
)

type taskMonitorOptions struct {
	TaskID         string
	Tool           string
	LogFile        string
	DiagnosticsDir string
}

type taskMonitor struct {
	opts taskMonitorOptions

	interval       time.Duration
	cpuThreshold   float64
	cpuBurst       time.Duration
	stageWarnAfter time.Duration
	ringSize       int

	startedAt time.Time
	done      chan struct{}
	stopOnce  sync.Once
	wg        sync.WaitGroup

	mu                 sync.Mutex
	currentStage       string
	stageStartedAt     time.Time
	samples            []taskCPUSample
	peakCPUPercent     float64
	diagnosticsCount   int
	diagnosticsWritten bool
	diagnosticsReason  string
	lastDiagnosticPath string
	cpuBurstStartedAt  time.Time
	stageWarned        map[string]bool
	lastCPUSeconds     float64
	lastSampleAt       time.Time
}

type taskCPUSample struct {
	TS             string  `json:"ts"`
	ElapsedMS      int64   `json:"elapsed_ms"`
	CPUPercent     float64 `json:"cpu_percent"`
	GoRoutines     int     `json:"goroutines"`
	HeapAllocBytes uint64  `json:"heap_alloc_bytes"`
	HeapSysBytes   uint64  `json:"heap_sys_bytes"`
	CurrentStage   string  `json:"current_stage,omitempty"`
	StageElapsedMS int64   `json:"stage_elapsed_ms,omitempty"`
}

func newTaskMonitor(opts taskMonitorOptions) *taskMonitor {
	return &taskMonitor{
		opts:           opts,
		interval:       taskMonitorDurationEnv("LOCALCLASH_TASK_MONITOR_INTERVAL_MS", defaultTaskMonitorInterval),
		cpuThreshold:   taskMonitorFloatEnv("LOCALCLASH_TASK_CPU_BURST_PCT", defaultTaskMonitorCPUThreshold),
		cpuBurst:       taskMonitorDurationEnv("LOCALCLASH_TASK_CPU_BURST_MS", defaultTaskMonitorCPUBurst),
		stageWarnAfter: taskMonitorDurationEnv("LOCALCLASH_TASK_STAGE_WARN_MS", defaultTaskMonitorStageWarnAfter),
		ringSize:       taskMonitorIntEnv("LOCALCLASH_TASK_MONITOR_RING_SIZE", defaultTaskMonitorRingSize),
		done:           make(chan struct{}),
		stageWarned:    make(map[string]bool),
	}
}

func (m *taskMonitor) Start() {
	if m.interval <= 0 || m.ringSize <= 0 {
		return
	}
	m.startedAt = time.Now()
	m.lastSampleAt = m.startedAt
	m.lastCPUSeconds = processCPUSeconds()
	m.wg.Add(1)
	go m.loop()
}

func (m *taskMonitor) Stop() {
	m.stopOnce.Do(func() {
		close(m.done)
		m.wg.Wait()
	})
}

func (m *taskMonitor) Record(event string, fields map[string]any) {
	if fields == nil {
		return
	}
	stage, _ := fields["stage"].(string)
	if stage == "" {
		return
	}
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	switch event {
	case "stage_started":
		m.currentStage = stage
		m.stageStartedAt = now
	case "stage_done", "stage_error":
		if m.currentStage == stage {
			m.currentStage = ""
			m.stageStartedAt = time.Time{}
		}
	}
}

func (m *taskMonitor) SummaryFields() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	fields := map[string]any{
		"diagnostics_dir":     m.opts.DiagnosticsDir,
		"diagnostics_written": m.diagnosticsWritten,
		"peak_cpu_percent":    round1(m.peakCPUPercent),
		"sample_count":        len(m.samples),
		"sample_interval_ms":  m.interval.Milliseconds(),
		"cpu_burst_pct":       m.cpuThreshold,
		"cpu_burst_ms":        m.cpuBurst.Milliseconds(),
		"stage_warn_ms":       m.stageWarnAfter.Milliseconds(),
	}
	if m.diagnosticsReason != "" {
		fields["diagnostics_reason"] = m.diagnosticsReason
	}
	if m.lastDiagnosticPath != "" {
		fields["diagnostics_snapshot"] = m.lastDiagnosticPath
	}
	if m.currentStage != "" {
		fields["current_stage"] = m.currentStage
		fields["current_stage_elapsed_ms"] = time.Since(m.stageStartedAt).Milliseconds()
	}
	return fields
}

func (m *taskMonitor) loop() {
	defer m.wg.Done()
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.sample()
		case <-m.done:
			return
		}
	}
}

func (m *taskMonitor) sample() {
	now := time.Now()
	cpuSeconds := processCPUSeconds()

	m.mu.Lock()
	dt := now.Sub(m.lastSampleAt).Seconds()
	dCPU := cpuSeconds - m.lastCPUSeconds
	cpuPercent := 0.0
	if dt > 0 && dCPU >= 0 {
		cpuPercent = dCPU / dt * 100
	}
	m.lastSampleAt = now
	m.lastCPUSeconds = cpuSeconds

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	stageElapsedMS := int64(0)
	if !m.stageStartedAt.IsZero() {
		stageElapsedMS = now.Sub(m.stageStartedAt).Milliseconds()
	}
	sample := taskCPUSample{
		TS:             now.UTC().Format(time.RFC3339Nano),
		ElapsedMS:      now.Sub(m.startedAt).Milliseconds(),
		CPUPercent:     round1(cpuPercent),
		GoRoutines:     runtime.NumGoroutine(),
		HeapAllocBytes: mem.HeapAlloc,
		HeapSysBytes:   mem.HeapSys,
		CurrentStage:   m.currentStage,
		StageElapsedMS: stageElapsedMS,
	}
	m.samples = append(m.samples, sample)
	if len(m.samples) > m.ringSize {
		copy(m.samples, m.samples[len(m.samples)-m.ringSize:])
		m.samples = m.samples[:m.ringSize]
	}
	if cpuPercent > m.peakCPUPercent {
		m.peakCPUPercent = cpuPercent
	}

	trigger := ""
	if m.cpuThreshold > 0 && m.cpuBurst > 0 {
		if cpuPercent >= m.cpuThreshold {
			if m.cpuBurstStartedAt.IsZero() {
				m.cpuBurstStartedAt = now
			} else if now.Sub(m.cpuBurstStartedAt) >= m.cpuBurst {
				trigger = fmt.Sprintf("cpu_burst_over_%.1f_percent_for_%dms", m.cpuThreshold, m.cpuBurst.Milliseconds())
			}
		} else {
			m.cpuBurstStartedAt = time.Time{}
		}
	}
	if trigger == "" && m.currentStage != "" && m.stageWarnAfter > 0 && now.Sub(m.stageStartedAt) >= m.stageWarnAfter && !m.stageWarned[m.currentStage] {
		trigger = fmt.Sprintf("stage_timeout_%s_%dms", sanitizeTaskTool(m.currentStage), m.stageWarnAfter.Milliseconds())
		m.stageWarned[m.currentStage] = true
	}
	if trigger == "" || m.diagnosticsWritten {
		m.mu.Unlock()
		return
	}

	m.diagnosticsWritten = true
	m.diagnosticsReason = trigger
	m.diagnosticsCount++
	snapshot := m.snapshotLocked(now, trigger)
	samples := append([]taskCPUSample(nil), m.samples...)
	m.mu.Unlock()

	if path, err := m.writeDiagnostics(snapshot, samples); err == nil {
		m.mu.Lock()
		m.lastDiagnosticPath = path
		m.mu.Unlock()
		appendTaskLog(m.opts.LogFile, "task_diagnostics_written", m.opts.Tool, map[string]any{
			"diagnostics_dir": m.opts.DiagnosticsDir,
			"snapshot":        path,
			"reason":          trigger,
		})
	} else {
		appendTaskLog(m.opts.LogFile, "task_diagnostics_error", m.opts.Tool, map[string]any{
			"diagnostics_dir": m.opts.DiagnosticsDir,
			"reason":          trigger,
			"error":           err.Error(),
		})
	}
}

func (m *taskMonitor) snapshotLocked(now time.Time, trigger string) map[string]any {
	out := map[string]any{
		"task_id":             m.opts.TaskID,
		"tool":                m.opts.Tool,
		"trigger":             trigger,
		"snapshot_at":         now.UTC().Format(time.RFC3339Nano),
		"elapsed_ms":          now.Sub(m.startedAt).Milliseconds(),
		"log_file":            m.opts.LogFile,
		"diagnostics_dir":     m.opts.DiagnosticsDir,
		"current_stage":       m.currentStage,
		"sample_count":        len(m.samples),
		"peak_cpu_percent":    round1(m.peakCPUPercent),
		"goroutines":          runtime.NumGoroutine(),
		"sample_interval_ms":  m.interval.Milliseconds(),
		"cpu_burst_pct":       m.cpuThreshold,
		"cpu_burst_ms":        m.cpuBurst.Milliseconds(),
		"stage_warn_ms":       m.stageWarnAfter.Milliseconds(),
		"diagnostics_count":   m.diagnosticsCount,
		"diagnostics_written": true,
	}
	if !m.stageStartedAt.IsZero() {
		out["current_stage_elapsed_ms"] = now.Sub(m.stageStartedAt).Milliseconds()
	}
	return out
}

func (m *taskMonitor) writeDiagnostics(snapshot map[string]any, samples []taskCPUSample) (string, error) {
	if err := os.MkdirAll(m.opts.DiagnosticsDir, 0o755); err != nil {
		return "", err
	}
	snapshotPath := filepath.Join(m.opts.DiagnosticsDir, "task_snapshot.json")
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	if err := os.WriteFile(snapshotPath, data, 0o644); err != nil {
		return "", err
	}

	if err := writeCPUSamples(filepath.Join(m.opts.DiagnosticsDir, "cpu_samples.jsonl"), samples); err != nil {
		return "", err
	}
	if err := writeProfile(filepath.Join(m.opts.DiagnosticsDir, "goroutine.txt"), "goroutine", 2); err != nil {
		return "", err
	}
	if err := writeProfile(filepath.Join(m.opts.DiagnosticsDir, "heap.txt"), "heap", 1); err != nil {
		return "", err
	}
	return snapshotPath, nil
}

func writeCPUSamples(path string, samples []taskCPUSample) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	for _, sample := range samples {
		if err := encoder.Encode(sample); err != nil {
			return err
		}
	}
	return nil
}

func writeProfile(path, name string, debug int) error {
	profile := pprof.Lookup(name)
	if profile == nil {
		return fmt.Errorf("profile %q not found", name)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	return profile.WriteTo(file, debug)
}

func processCPUSeconds() float64 {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0
	}
	return timevalSeconds(usage.Utime) + timevalSeconds(usage.Stime)
}

func timevalSeconds(tv syscall.Timeval) float64 {
	return float64(tv.Sec) + float64(tv.Usec)/1_000_000
}

func taskMonitorDurationEnv(name string, fallback time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	ms, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return time.Duration(ms) * time.Millisecond
}

func taskMonitorFloatEnv(name string, fallback float64) float64 {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	out, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return out
}

func taskMonitorIntEnv(name string, fallback int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	out, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return out
}

func round1(value float64) float64 {
	return float64(int(value*10+0.5)) / 10
}
