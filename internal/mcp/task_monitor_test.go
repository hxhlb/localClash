package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTaskMonitorWritesDiagnosticsOnStageTimeout(t *testing.T) {
	t.Setenv("LOCALCLASH_TASK_MONITOR_INTERVAL_MS", "5")
	t.Setenv("LOCALCLASH_TASK_STAGE_WARN_MS", "10")
	t.Setenv("LOCALCLASH_TASK_CPU_BURST_PCT", "100000")
	t.Setenv("LOCALCLASH_TASK_CPU_BURST_MS", "100000")

	dir := t.TempDir()
	logFile := filepath.Join(dir, "task.log")
	diagnosticsDir := filepath.Join(dir, "diagnostics", "task-1")
	monitor := newTaskMonitor(taskMonitorOptions{
		TaskID:         "task-1",
		Tool:           "subscriptions_refresh",
		LogFile:        logFile,
		DiagnosticsDir: diagnosticsDir,
	})
	monitor.Start()
	defer monitor.Stop()

	monitor.Record("stage_started", map[string]any{"stage": "slow_stage"})
	snapshotPath := filepath.Join(diagnosticsDir, "task_snapshot.json")
	requiredDiagnostics := []string{"task_snapshot.json", "cpu_samples.jsonl", "goroutine.txt", "heap.txt"}
	for i := 0; i < 50; i++ {
		if diagnosticsExist(diagnosticsDir, requiredDiagnostics) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	fields := monitor.SummaryFields()
	if fields["diagnostics_written"] != true {
		t.Fatalf("summary = %+v, want diagnostics_written", fields)
	}
	if reason, _ := fields["diagnostics_reason"].(string); !strings.Contains(reason, "stage_timeout_slow_stage") {
		t.Fatalf("summary = %+v, want slow_stage timeout reason", fields)
	}

	for _, name := range requiredDiagnostics {
		if _, err := os.Stat(filepath.Join(diagnosticsDir, name)); err != nil {
			t.Fatalf("diagnostic %s missing: %v", name, err)
		}
	}
	snapshotData, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(snapshotData, &snapshot); err != nil {
		t.Fatalf("snapshot is not json: %v", err)
	}
	if snapshot["current_stage"] != "slow_stage" {
		t.Fatalf("snapshot = %+v, want current_stage slow_stage", snapshot)
	}
	var logText []byte
	for i := 0; i < 50; i++ {
		data, err := os.ReadFile(logFile)
		if err == nil && strings.Contains(string(data), `"event":"task_diagnostics_written"`) {
			logText = data
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(logText) == 0 {
		data, _ := os.ReadFile(logFile)
		t.Fatalf("log = %s, want task_diagnostics_written", data)
	}
}

func diagnosticsExist(dir string, names []string) bool {
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			return false
		}
	}
	return true
}
