package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type asyncToolFunc func(context.Context, json.RawMessage) (toolResult, error)

type asyncToolInput struct {
	Background *bool `json:"background"`
	Wait       *bool `json:"wait"`
}

type asyncTaskResult struct {
	Queued      bool     `json:"queued"`
	TaskID      string   `json:"task_id"`
	Tool        string   `json:"tool"`
	Status      string   `json:"status"`
	LogFile     string   `json:"log_file"`
	StatusFile  string   `json:"status_file"`
	StartedAt   string   `json:"started_at"`
	NextActions []string `json:"next_actions"`
}

type asyncTaskStatus struct {
	TaskID     string `json:"task_id"`
	Tool       string `json:"tool"`
	Status     string `json:"status"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at,omitempty"`
	LogFile    string `json:"log_file"`
	Error      string `json:"error,omitempty"`
}

func (s *Server) callMaybeAsyncTool(ctx context.Context, tool string, args json.RawMessage, run asyncToolFunc) (toolResult, error) {
	if !backgroundToolRequested(args) {
		return run(ctx, args)
	}
	task, err := s.queueAsyncTool(tool, args, run)
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(task)
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
	logFile := filepath.Join(dir, taskID+".log")
	statusFile := filepath.Join(dir, taskID+".json")
	status := asyncTaskStatus{
		TaskID:    taskID,
		Tool:      tool,
		Status:    "queued",
		StartedAt: now.Format(time.RFC3339),
		LogFile:   logFile,
	}
	if err := writeTaskStatus(statusFile, status); err != nil {
		return asyncTaskResult{}, err
	}
	appendTaskLog(logFile, "queued", tool, nil)

	go func() {
		status.Status = "running"
		_ = writeTaskStatus(statusFile, status)
		appendTaskLog(logFile, "started", tool, nil)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		result, err := run(ctx, args)
		finished := time.Now().UTC()
		status.FinishedAt = finished.Format(time.RFC3339)
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
				"structured_content": result.StructuredContent,
				"is_error":           result.IsError,
			})
			return
		}
		status.Status = "done"
		_ = writeTaskStatus(statusFile, status)
		appendTaskLog(logFile, "done", tool, map[string]any{
			"structured_content": result.StructuredContent,
			"is_error":           result.IsError,
		})
	}()

	return asyncTaskResult{
		Queued:     true,
		TaskID:     taskID,
		Tool:       tool,
		Status:     "queued",
		LogFile:    logFile,
		StatusFile: statusFile,
		StartedAt:  status.StartedAt,
		NextActions: []string{
			fmt.Sprintf("call nl_file with path %q to watch readable task progress", logFile),
			fmt.Sprintf("call nl_file with path %q to read machine-readable task status", statusFile),
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
	result, ok := value.(map[string]any)
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
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(data)
}
