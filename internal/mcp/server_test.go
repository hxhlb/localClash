package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleInitialize(t *testing.T) {
	resp := NewServer().Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	if resp == nil || resp.Error != nil {
		t.Fatalf("response error = %+v", resp)
	}
	result := resp.Result.(map[string]any)
	if result["protocolVersion"] == "" {
		t.Fatalf("initialize result = %+v, want protocolVersion", result)
	}
}

func TestServeUsesMCPStdioFraming(t *testing.T) {
	request := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	in := strings.NewReader(fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(request), request))
	var out bytes.Buffer

	if err := NewServer().Serve(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	raw := out.String()
	if !strings.HasPrefix(raw, "Content-Length: ") {
		t.Fatalf("response %q does not use MCP stdio framing", raw)
	}
	parts := strings.SplitN(raw, "\r\n\r\n", 2)
	if len(parts) != 2 {
		t.Fatalf("response %q missing header separator", raw)
	}
	var resp rpcResponse
	if err := json.Unmarshal([]byte(parts[1]), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatalf("framed initialize error = %+v", resp.Error)
	}
}

func TestToolsListIncludesCoreTools(t *testing.T) {
	resp := NewServer().Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	if resp == nil || resp.Error != nil {
		t.Fatalf("response error = %+v", resp)
	}
	data, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Tools []ListedTool `json:"tools"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	byName := map[string]ListedTool{}
	for _, tool := range result.Tools {
		byName[tool.Name] = tool
	}
	for _, name := range []string{"doctor", "rules_adapt", "rules_render", "config_render", "config_test"} {
		if byName[name].Name == "" {
			t.Fatalf("missing tool %q", name)
		}
		if byName[name].SafetyLevel == "" {
			t.Fatalf("tool %q has no safety level", name)
		}
	}
}

func TestRegistrySafetyLevels(t *testing.T) {
	want := map[string]SafetyLevel{
		"doctor":                   SafeRead,
		"inspect_generated_config": SafeRead,
		"rules_adapt":              SafeRead,
		"rules_render":             SafeRead,
		"config_render":            SafeWrite,
		"config_test":              SafeWrite,
		"run_runtime":              ConfirmRequired,
		"switch_proxy_group":       ConfirmRequired,
		"apply_router_config":      HighRisk,
	}
	got := map[string]SafetyLevel{}
	for _, tool := range Registry() {
		got[tool.Name] = tool.SafetyLevel
	}
	for name, level := range want {
		if got[name] != level {
			t.Fatalf("%s safety level = %q, want %q", name, got[name], level)
		}
	}
}

func TestToolsCallDoctorReturnsSerializableResult(t *testing.T) {
	dir := t.TempDir()
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "doctor",
			"arguments": map[string]any{
				"core":         filepath.Join(dir, "missing-core"),
				"subscription": filepath.Join(dir, "missing-subscription.yaml"),
				"config":       filepath.Join(dir, "missing-generated.yaml"),
				"policy":       filepath.Join(dir, "missing-policy.yaml"),
				"dashboard":    filepath.Join(dir, "missing-dashboard"),
				"workdir":      dir,
			},
		},
	}
	resp := callHandle(t, req)
	if resp.Error != nil {
		t.Fatalf("doctor call returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	if len(result.Content) == 0 || result.Content[0].Type != "text" {
		t.Fatalf("doctor result content = %+v", result.Content)
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("doctor structured content is not serializable: %v", err)
	}
}

func TestConfirmRequiredAndHighRiskToolsReturnToolError(t *testing.T) {
	for _, name := range []string{"run_runtime", "switch_proxy_group", "apply_router_config"} {
		resp := callHandle(t, map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      name,
				"arguments": map[string]any{},
			},
		})
		if resp.Error != nil {
			t.Fatalf("%s returned protocol error: %+v", name, resp.Error)
		}
		result := marshalToolResult(t, resp.Result)
		if !result.IsError {
			t.Fatalf("%s IsError = false, want true", name)
		}
		if !strings.Contains(result.Content[0].Text, "requires explicit confirmation flow") {
			t.Fatalf("%s error text = %q", name, result.Content[0].Text)
		}
	}
}

func TestUnknownToolReturnsError(t *testing.T) {
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "missing_tool",
		},
	})
	if resp.Error == nil {
		t.Fatal("expected unknown tool JSON-RPC error")
	}
}

func TestConfigTestToolRunsMihomoTestCommand(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "mihomo")
	writeTestExecutable(t, core, "#!/bin/sh\nif [ \"$1\" = \"-v\" ]; then echo test; exit 0; fi\necho configuration test is successful\n")
	config := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(config, []byte("proxies: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "config_test",
			"arguments": map[string]any{
				"core":    core,
				"config":  config,
				"workdir": dir,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("config_test returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["pass"] != true {
		t.Fatalf("config_test pass = %v, want true", content["pass"])
	}
}

func callHandle(t *testing.T, value any) *rpcResponse {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	resp := NewServer().Handle(context.Background(), data)
	if resp == nil {
		t.Fatal("nil response")
	}
	return resp
}

func marshalToolResult(t *testing.T, value any) toolResult {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result toolResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func writeTestExecutable(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
