package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type asyncToolFunc func(context.Context, json.RawMessage) (toolResult, error)

type asyncToolInput struct {
	Background *bool `json:"background"`
	Wait       *bool `json:"wait"`
}

type taskLogContextKey struct{}

type taskLogger struct {
	tool    string
	path    string
	monitor *taskMonitor
}

func (logger taskLogger) Append(event string, fields map[string]any) {
	appendTaskLog(logger.path, event, logger.tool, fields)
	if logger.monitor != nil {
		logger.monitor.Record(event, fields)
	}
}

func taskLoggerFromContext(ctx context.Context) (taskLogger, bool) {
	logger, ok := ctx.Value(taskLogContextKey{}).(taskLogger)
	return logger, ok
}

func appendTaskStage(ctx context.Context, event, stage string, fields map[string]any) {
	logger, ok := taskLoggerFromContext(ctx)
	if !ok {
		return
	}
	out := map[string]any{"stage": stage}
	for key, value := range fields {
		out[key] = value
	}
	logger.Append(event, out)
}

func startTaskStage(ctx context.Context, stage string, fields map[string]any) func(string, map[string]any) {
	started := time.Now()
	appendTaskStage(ctx, "stage_started", stage, fields)
	return func(event string, doneFields map[string]any) {
		out := map[string]any{"duration_ms": time.Since(started).Milliseconds()}
		for key, value := range doneFields {
			out[key] = value
		}
		appendTaskStage(ctx, event, stage, out)
	}
}

func finishTaskStage(finish func(string, map[string]any), err error, fields map[string]any) {
	if finish == nil {
		return
	}
	if err != nil {
		out := map[string]any{"error": err.Error()}
		for key, value := range fields {
			out[key] = value
		}
		finish("stage_error", out)
		return
	}
	finish("stage_done", fields)
}

type asyncTaskResult struct {
	Queued         bool     `json:"queued"`
	TaskID         string   `json:"task_id"`
	Tool           string   `json:"tool"`
	Status         string   `json:"status"`
	LogFile        string   `json:"log_file"`
	StatusFile     string   `json:"status_file"`
	DiagnosticsDir string   `json:"diagnostics_dir"`
	StartedAt      string   `json:"started_at"`
	NextActions    []string `json:"next_actions"`
}

type asyncTaskStatus struct {
	TaskID         string `json:"task_id"`
	Tool           string `json:"tool"`
	Status         string `json:"status"`
	StartedAt      string `json:"started_at"`
	FinishedAt     string `json:"finished_at,omitempty"`
	LogFile        string `json:"log_file"`
	DiagnosticsDir string `json:"diagnostics_dir"`
	Error          string `json:"error,omitempty"`
}

func (s *Server) callMaybeAsyncTool(ctx context.Context, tool string, args json.RawMessage, run asyncToolFunc) (toolResult, error) {
	if !backgroundToolRequested(args) {
		return s.runLoggedSyncTool(ctx, tool, args, run)
	}
	task, err := s.queueAsyncTool(tool, args, run)
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(task)
}

func (s *Server) runLoggedSyncTool(ctx context.Context, tool string, args json.RawMessage, run asyncToolFunc) (toolResult, error) {
	now := time.Now().UTC()
	taskID := fmt.Sprintf("%s-%s-sync-%d", now.Format("20060102-150405"), sanitizeTaskTool(tool), now.UnixNano()%1_000_000_000)
	dir := s.taskLogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return toolResult{}, err
	}
	cleanupTaskLogDir(dir, taskLogRetainedTasks())
	logFile := filepath.Join(dir, taskID+".log")
	statusFile := filepath.Join(dir, taskID+".json")
	diagnosticsDir := s.taskDiagnosticsDir(taskID)
	status := asyncTaskStatus{
		TaskID:         taskID,
		Tool:           tool,
		Status:         "running",
		StartedAt:      now.Format(time.RFC3339),
		LogFile:        logFile,
		DiagnosticsDir: diagnosticsDir,
	}
	_ = writeTaskStatus(statusFile, status)
	appendTaskLog(logFile, "started", tool, map[string]any{"mode": "sync"})
	monitor := newTaskMonitor(taskMonitorOptions{
		TaskID:         taskID,
		Tool:           tool,
		LogFile:        logFile,
		DiagnosticsDir: diagnosticsDir,
	})
	monitor.Start()
	defer monitor.Stop()
	ctx = context.WithValue(ctx, taskLogContextKey{}, taskLogger{tool: tool, path: logFile, monitor: monitor})
	result, err := run(ctx, args)
	status.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	appendTaskLog(logFile, "task_monitor_summary", tool, monitor.SummaryFields())
	if err != nil {
		status.Status = "error"
		status.Error = err.Error()
		_ = writeTaskStatus(statusFile, status)
		appendTaskLog(logFile, "error", tool, map[string]any{"error": err.Error()})
		return result, err
	}
	if toolErr := structuredToolError(result.StructuredContent); toolErr != "" {
		status.Status = "error"
		status.Error = toolErr
		_ = writeTaskStatus(statusFile, status)
		appendTaskLog(logFile, "error", tool, map[string]any{
			"structured_summary": summarizeStructuredContent(result.StructuredContent),
			"is_error":           result.IsError,
		})
	} else {
		status.Status = "done"
		_ = writeTaskStatus(statusFile, status)
		appendTaskLog(logFile, "done", tool, map[string]any{
			"structured_summary": summarizeStructuredContent(result.StructuredContent),
			"is_error":           result.IsError,
		})
	}
	addSyncTaskArtifacts(&result, logFile, statusFile, diagnosticsDir)
	return result, nil
}

func backgroundToolRequested(args json.RawMessage) bool {
	var in asyncToolInput
	if err := json.Unmarshal(args, &in); err != nil {
		return true
	}
	if in.Background != nil {
		return *in.Background
	}
	if in.Wait != nil {
		return !*in.Wait
	}
	return true
}

func (s *Server) queueAsyncTool(tool string, args json.RawMessage, run asyncToolFunc) (asyncTaskResult, error) {
	now := time.Now().UTC()
	taskID := fmt.Sprintf("%s-%s-%d", now.Format("20060102-150405"), sanitizeTaskTool(tool), now.UnixNano()%1_000_000_000)
	dir := s.taskLogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return asyncTaskResult{}, err
	}
	cleanupTaskLogDir(dir, taskLogRetainedTasks())
	logFile := filepath.Join(dir, taskID+".log")
	statusFile := filepath.Join(dir, taskID+".json")
	diagnosticsDir := s.taskDiagnosticsDir(taskID)
	status := asyncTaskStatus{
		TaskID:         taskID,
		Tool:           tool,
		Status:         "queued",
		StartedAt:      now.Format(time.RFC3339),
		LogFile:        logFile,
		DiagnosticsDir: diagnosticsDir,
	}
	if err := writeTaskStatus(statusFile, status); err != nil {
		return asyncTaskResult{}, err
	}
	appendTaskLog(logFile, "queued", tool, nil)

	go func() {
		monitor := newTaskMonitor(taskMonitorOptions{
			TaskID:         taskID,
			Tool:           tool,
			LogFile:        logFile,
			DiagnosticsDir: diagnosticsDir,
		})
		monitor.Start()
		defer monitor.Stop()
		status.Status = "running"
		_ = writeTaskStatus(statusFile, status)
		appendTaskLog(logFile, "started", tool, nil)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		ctx = context.WithValue(ctx, taskLogContextKey{}, taskLogger{tool: tool, path: logFile, monitor: monitor})
		result, err := run(ctx, args)
		finished := time.Now().UTC()
		status.FinishedAt = finished.Format(time.RFC3339)
		appendTaskLog(logFile, "task_monitor_summary", tool, monitor.SummaryFields())
		if err != nil {
			status.Status = "error"
			status.Error = err.Error()
			_ = writeTaskStatus(statusFile, status)
			appendTaskLog(logFile, "error", tool, map[string]any{"error": err.Error()})
			return
		}
		if toolErr := structuredToolError(result.StructuredContent); toolErr != "" {
			status.Status = "error"
			status.Error = toolErr
			_ = writeTaskStatus(statusFile, status)
			appendTaskLog(logFile, "error", tool, map[string]any{
				"structured_summary": summarizeStructuredContent(result.StructuredContent),
				"is_error":           result.IsError,
			})
			return
		}
		status.Status = "done"
		_ = writeTaskStatus(statusFile, status)
		appendTaskLog(logFile, "done", tool, map[string]any{
			"structured_summary": summarizeStructuredContent(result.StructuredContent),
			"is_error":           result.IsError,
		})
	}()

	return asyncTaskResult{
		Queued:         true,
		TaskID:         taskID,
		Tool:           tool,
		Status:         "queued",
		LogFile:        logFile,
		StatusFile:     statusFile,
		DiagnosticsDir: diagnosticsDir,
		StartedAt:      status.StartedAt,
		NextActions: []string{
			fmt.Sprintf("call nl_file with path %q to watch readable task progress", logFile),
			fmt.Sprintf("call nl_file with path %q to read machine-readable task status", statusFile),
			fmt.Sprintf("if task log reports diagnostics_written=true, inspect files under %q", diagnosticsDir),
			"do not assume the operation is complete until the task log says done or error",
		},
	}, nil
}

func (s *Server) taskLogDir() string {
	if s.state != nil {
		runtimeDir := strings.TrimSpace(s.state.Paths.MihomoRuntimeDir)
		if runtimeDir != "" && runtimeDir != "." {
			parent := filepath.Dir(runtimeDir)
			if parent != "." && parent != "" {
				return filepath.Join(parent, "mcp-tasks")
			}
		}
	}
	return filepath.Join(".runtime", "mcp-tasks")
}

func (s *Server) taskDiagnosticsDir(taskID string) string {
	logDir := s.taskLogDir()
	parent := filepath.Dir(logDir)
	if parent == "." || parent == "" {
		parent = ".runtime"
	}
	return filepath.Join(parent, "diagnostics", taskID)
}

func sanitizeTaskTool(tool string) string {
	tool = strings.TrimSpace(tool)
	var b strings.Builder
	for _, r := range tool {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	if b.Len() == 0 {
		return "task"
	}
	return b.String()
}

func writeTaskStatus(path string, status asyncTaskStatus) error {
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func structuredToolError(value any) string {
	result, ok := structuredContentMap(value)
	if !ok {
		return ""
	}
	message, _ := result["error"].(string)
	return strings.TrimSpace(message)
}

func appendTaskLog(path, event, tool string, fields map[string]any) {
	line := map[string]any{
		"ts":    time.Now().UTC().Format(time.RFC3339),
		"event": event,
		"tool":  tool,
	}
	for key, value := range fields {
		line[key] = value
	}
	data, err := json.Marshal(line)
	if err != nil {
		data = []byte(fmt.Sprintf(`{"ts":%q,"event":%q,"tool":%q,"error":%q}`, time.Now().UTC().Format(time.RFC3339), event, tool, err.Error()))
	}
	data = append(data, '\n')
	rotateBoundedLog(path, taskLogMaxBytes())
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(data)
}

func appendBoundedJSONLog(path string, fields map[string]any, maxBytes int64) {
	data, err := json.Marshal(fields)
	if err != nil {
		data = []byte(fmt.Sprintf(`{"ts":%q,"event":"log_encode_error","error":%q}`, time.Now().UTC().Format(time.RFC3339), err.Error()))
	}
	data = append(data, '\n')
	rotateBoundedLog(path, maxBytes)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(data)
}

func rotateBoundedLog(path string, maxBytes int64) {
	if maxBytes <= 0 {
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() < maxBytes {
		return
	}
	rotated := path + ".1"
	_ = os.Remove(rotated)
	_ = os.Rename(path, rotated)
}

func cleanupTaskLogDir(dir string, retain int) {
	if retain <= 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type taskFile struct {
		name    string
		modTime time.Time
	}
	var files []taskFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".log") && !strings.HasSuffix(name, ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, taskFile{name: name, modTime: info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.After(files[j].modTime) })
	keepFiles := retain * 2
	for i := keepFiles; i < len(files); i++ {
		_ = os.Remove(filepath.Join(dir, files[i].name))
	}
}

func summarizeStructuredContent(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{"type": "object", "keys": sortedMapKeys(v)}
		for _, key := range []string{"error", "status", "task_id", "log_file", "status_file", "diagnostics_dir"} {
			if item, ok := v[key]; ok {
				out[key] = item
			}
		}
		return out
	case []any:
		return map[string]any{"type": "array", "length": len(v)}
	case string:
		if len(v) > 160 {
			v = v[:157] + "..."
		}
		return v
	case nil:
		return nil
	default:
		return fmt.Sprintf("%T", value)
	}
}

func addSyncTaskArtifacts(result *toolResult, logFile, statusFile, diagnosticsDir string) {
	content, ok := structuredContentMap(result.StructuredContent)
	if !ok {
		return
	}
	content["task_log_file"] = logFile
	content["task_status_file"] = statusFile
	content["diagnostics_dir"] = diagnosticsDir
	content["observability"] = map[string]any{
		"log_file":        logFile,
		"status_file":     statusFile,
		"diagnostics_dir": diagnosticsDir,
	}
	data, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		return
	}
	result.StructuredContent = content
	if len(result.Content) == 1 && result.Content[0].Type == "text" {
		result.Content[0].Text = string(data)
	}
}

func structuredContentMap(value any) (map[string]any, bool) {
	if content, ok := value.(map[string]any); ok {
		return content, true
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	var content map[string]any
	if err := json.Unmarshal(data, &content); err != nil {
		return nil, false
	}
	if content == nil {
		return nil, false
	}
	return content, true
}

func sortedMapKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func taskLogMaxBytes() int64 {
	return int64(envInt("LOCALCLASH_TASK_LOG_MAX_BYTES", 512*1024))
}

func serviceLogMaxBytes() int64 {
	return int64(envInt("LOCALCLASH_SERVICE_LOG_MAX_BYTES", 512*1024))
}

func taskLogRetainedTasks() int {
	return envInt("LOCALCLASH_TASK_LOG_RETAINED_TASKS", 200)
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}
