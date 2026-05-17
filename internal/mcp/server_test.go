package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"localclash/internal/appinit"
	"localclash/internal/rules"
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

func TestHTTPHandlerServesJSONRPC(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	NewServer().HTTPHandler("/mcp").ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp rpcResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatalf("http initialize error = %+v", resp.Error)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("missing CORS header: %+v", w.Header())
	}
}

func TestHTTPHandlerServesHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	NewServer().HTTPHandler("/mcp").ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["status"] != "ok" {
		t.Fatalf("health = %+v, want ok", result)
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
	for _, name := range []string{"doctor", "environment_inspect", "config_base_inspect", "config_intent_inspect", "config_overlay_inspect", "config_draft_apply", "config_draft_render", "proxy_group_build", "custom_rules_build", "nl_file", "pack_rules_query", "pack_rules_prefetch", "pack_rules_read", "packs_list", "packs_get", "subscription_nodes_list", "subscription_nodes_search", "runtime_preset_status", "runtime_status", "subscriptions_status", "tools_list", "runtime_preset_configure", "subscriptions_configure", "subscriptions_refresh", "run_runtime", "sed_file", "stop_runtime"} {
		if byName[name].Name == "" {
			t.Fatalf("missing tool %q", name)
		}
		if byName[name].SafetyLevel == "" {
			t.Fatalf("tool %q has no safety level", name)
		}
	}
	for _, name := range removedMCPTools() {
		if byName[name].Name != "" {
			t.Fatalf("removed tool %q should not be listed", name)
		}
	}
}

func TestRegistrySafetyLevels(t *testing.T) {
	want := map[string]SafetyLevel{
		"doctor":                    SafeRead,
		"environment_inspect":       SafeRead,
		"config_base_inspect":       SafeRead,
		"config_intent_inspect":     SafeRead,
		"config_overlay_inspect":    SafeRead,
		"nl_file":                   SafeRead,
		"pack_rules_query":          SafeRead,
		"packs_get":                 SafeRead,
		"packs_list":                SafeRead,
		"subscription_nodes_list":   SafeRead,
		"subscription_nodes_search": SafeRead,
		"runtime_status":            SafeRead,
		"runtime_preset_status":     SafeRead,
		"subscriptions_status":      SafeRead,
		"tools_list":                SafeRead,
		"config_draft_apply":        SafeWrite,
		"config_draft_render":       SafeWrite,
		"proxy_group_build":         SafeWrite,
		"custom_rules_build":        SafeWrite,
		"pack_rules_prefetch":       SafeWrite,
		"pack_rules_read":           SafeWrite,
		"runtime_preset_configure":  SafeWrite,
		"sed_file":                  SafeWrite,
		"subscriptions_configure":   SafeWrite,
		"subscriptions_refresh":     SafeWrite,
		"run_runtime":               ConfirmRequired,
		"stop_runtime":              ConfirmRequired,
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
	for _, name := range removedMCPTools() {
		if _, ok := got[name]; ok {
			t.Fatalf("removed tool %q should not be registered", name)
		}
	}
	for _, tool := range Registry() {
		if tool.Name == "run_runtime" {
			if !strings.Contains(tool.Description, "network connectivity") || !strings.Contains(tool.Description, "Agent itself") || !strings.Contains(tool.Description, "disconnected") {
				t.Fatalf("run_runtime description missing risk warning: %q", tool.Description)
			}
		}
	}
}

func TestToolsCallToolsListReturnsSelfDescription(t *testing.T) {
	resp := NewServer().Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"tools_list","arguments":{}}}`))
	if resp == nil || resp.Error != nil {
		t.Fatalf("response error = %+v", resp)
	}
	result := marshalToolResult(t, resp.Result)
	var structured ToolsListResult
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &structured); err != nil {
		t.Fatal(err)
	}
	if structured.Count != len(structured.Tools) {
		t.Fatalf("count = %d, tools = %d", structured.Count, len(structured.Tools))
	}
	byName := map[string]ToolSummary{}
	for _, tool := range structured.Tools {
		byName[tool.Name] = tool
	}
	if byName["tools_list"].SafetyLevel != SafeRead {
		t.Fatalf("tools_list safety = %q, want %q", byName["tools_list"].SafetyLevel, SafeRead)
	}
	if byName["doctor"].Name == "" || byName["subscriptions_status"].Name == "" {
		t.Fatalf("tools_list missing expected tools: %+v", byName)
	}
	if !strings.Contains(structured.ClientNamingNote, "localclash_doctor") {
		t.Fatalf("client naming note = %q, want OpenWebUI-style prefix example", structured.ClientNamingNote)
	}
}

func TestToolsCallRuntimePresetConfigureAndStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mihomo-preset.yaml")
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{PresetPath: path},
	})

	configure := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "runtime_preset_configure",
			"arguments": map[string]any{"preset": "router"},
		},
	})
	if configure.Error != nil {
		t.Fatalf("runtime_preset_configure returned JSON-RPC error: %+v", configure.Error)
	}
	configureResult := marshalToolResult(t, configure.Result)
	configured := configureResult.StructuredContent.(map[string]any)
	if configured["active"] != "router" || configured["exists"] != true {
		t.Fatalf("configure content = %+v, want router active and exists", configured)
	}

	statusResp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "runtime_preset_status",
			"arguments": map[string]any{},
		},
	})
	if statusResp.Error != nil {
		t.Fatalf("runtime_preset_status returned JSON-RPC error: %+v", statusResp.Error)
	}
	statusResult := marshalToolResult(t, statusResp.Result)
	status := statusResult.StructuredContent.(map[string]any)
	if status["active"] != "router" || status["path"] != path {
		t.Fatalf("status = %+v, want router active at configured path", status)
	}
}

func TestToolsCallRuntimePresetConfigureRerendersGeneratedConfig(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	presetPath := filepath.Join(filepath.Dir(paths.subscription), "mihomo-preset.yaml")
	outputPath := filepath.Join(filepath.Dir(paths.subscription), "generated", "mihomo.yaml")
	server := NewServerWithState(appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			SubscriptionPath:    paths.subscription,
			PolicyPath:          paths.policy,
			RulesCacheDir:       paths.cache,
			PacksSelectionPath:  filepath.Join(filepath.Dir(paths.subscription), "localclash-packs.yaml"),
			GeneratedConfig:     outputPath,
			PresetPath:          presetPath,
			SubscriptionConfig:  filepath.Join(filepath.Dir(paths.subscription), "localclash-subscriptions.yaml"),
			SubscriptionRuntime: filepath.Join(filepath.Dir(paths.subscription), ".runtime", "subscriptions"),
		},
	})

	resp := callHandleWithServer(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "runtime_preset_configure",
			"arguments": map[string]any{"preset": "router"},
		},
	})
	if resp.Error != nil {
		t.Fatalf("runtime_preset_configure returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["active"] != "router" || content["rendered"] != true {
		t.Fatalf("configure content = %+v, want router active and rendered", content)
	}
	generated := readMCPFile(t, outputPath)
	if !strings.Contains(generated, "mixed-port: 7893") || !strings.Contains(generated, "redir-port: 7892") {
		t.Fatalf("generated config did not apply router preset:\n%s", generated)
	}
}

func TestToolsCallNLFileReturnsNumberedText(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("config.yaml", []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "nl_file",
			"arguments": map[string]any{
				"path":        "config.yaml",
				"start_line":  2,
				"limit_lines": 2,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("nl_file returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["text"] != "2: beta\n3: gamma" {
		t.Fatalf("nl_file text = %q", content["text"])
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("nl_file structured content is not serializable: %v", err)
	}
}

func TestToolsCallSedFileDefaultsToDryRun(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("config.yaml", []byte("target: PROXY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "sed_file",
			"arguments": map[string]any{
				"path": "config.yaml",
				"edits": []map[string]any{
					{"op": "replace", "old": "target: PROXY", "new": "target: DIRECT"},
				},
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("sed_file returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["dry_run"] != true || content["changed"] != true || !strings.Contains(content["diff"].(string), "+target: DIRECT") {
		t.Fatalf("sed_file content = %+v, want dry-run diff", content)
	}
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "target: PROXY\n" {
		t.Fatalf("file changed during dry-run: %q", data)
	}
}

func TestToolsCallSedFileCanWrite(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("config.yaml", []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "sed_file",
			"arguments": map[string]any{
				"path":    "config.yaml",
				"dry_run": false,
				"edits": []map[string]any{
					{"op": "insert_after", "line": 1, "text": "x"},
				},
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("sed_file returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["dry_run"] != false || content["changed"] != true {
		t.Fatalf("sed_file content = %+v, want applied change", content)
	}
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "a\nx\nb\n" {
		t.Fatalf("file = %q", data)
	}
}

func TestToolsCallSubscriptionsStatusReturnsSerializableResult(t *testing.T) {
	dir := t.TempDir()
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "subscriptions_status",
			"arguments": map[string]any{
				"config":      filepath.Join(dir, "localclash-subscriptions.yaml"),
				"merged":      filepath.Join(dir, "subscription.yaml"),
				"runtime_dir": filepath.Join(dir, ".runtime", "subscriptions"),
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("subscriptions_status returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["configured"] != false {
		t.Fatalf("configured = %v, want false", content["configured"])
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("subscriptions_status structured content is not serializable: %v", err)
	}
}

func TestToolsCallSubscriptionsConfigureReturnsSerializableResult(t *testing.T) {
	dir := t.TempDir()
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "subscriptions_configure",
			"arguments": map[string]any{
				"config": filepath.Join(dir, "localclash-subscriptions.yaml"),
				"sources": []map[string]any{
					{"id": "primary", "url": "https://example.com/sub?token=secret-token"},
				},
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("subscriptions_configure returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["configured"] != true {
		t.Fatalf("configured = %v, want true", content["configured"])
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("subscriptions_configure structured content is not serializable: %v", err)
	}
	if strings.Contains(string(data), "secret-token") || strings.Contains(string(data), "token=") {
		t.Fatalf("subscriptions_configure leaked token in %s", data)
	}
}

func TestToolsCallSubscriptionsRefreshReturnsSerializableResult(t *testing.T) {
	paths := setupMCPSubscriptionsFixture(t)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "subscriptions_refresh",
			"arguments": map[string]any{
				"config":      paths.config,
				"runtime_dir": paths.runtimeDir,
				"merged":      paths.merged,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("subscriptions_refresh returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["refreshed"] != true {
		t.Fatalf("refreshed = %v, want true", content["refreshed"])
	}
	nodeDiff := content["node_diff"].(map[string]any)
	if nodeDiff["after_count"] != float64(1) || nodeDiff["added_count"] != float64(1) {
		t.Fatalf("node_diff = %+v, want one added node", nodeDiff)
	}
	if _, err := os.Stat(paths.merged); err != nil {
		t.Fatalf("merged subscription missing: %v", err)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("subscriptions_refresh structured content is not serializable: %v", err)
	}
	if strings.Contains(string(data), "secret-token") || strings.Contains(string(data), "token=") {
		t.Fatalf("subscriptions_refresh leaked token in %s", data)
	}
}

func TestToolsCallSubscriptionsRefreshAutoAppliesValidLocalClashSelector(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	dir := filepath.Dir(paths.subscription)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`proxies:
  - name: SG 02
    type: ss
    server: sg2.example.com
    password: secret
`))
	}))
	t.Cleanup(server.Close)
	subConfig := filepath.Join(dir, "localclash-subscriptions.yaml")
	runtimeDir := filepath.Join(dir, ".runtime", "subscriptions")
	localClashConfig := filepath.Join(dir, "localclash.yaml")
	generated := filepath.Join(dir, "generated", "mihomo.yaml")
	writeMCPFile(t, subConfig, fmt.Sprintf(`version: 1
sources:
  - id: primary
    url: %s/sub?token=secret-token
`, server.URL))
	writeMCPFile(t, localClashConfig, `version: 1
proxy_groups:
  AI:
    mode: manual
    match:
      type: name_regex
      pattern: SG
      min: 1
    selected_nodes:
      - SG 01
    reason: Use Singapore-labelled nodes.
    boundary: name_based_hint_only
packs:
  - id: blackmatrix7_OpenAI
    target: AI
`)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "subscriptions_refresh",
			"arguments": map[string]any{
				"config":            subConfig,
				"runtime_dir":       runtimeDir,
				"merged":            paths.subscription,
				"localclash_config": localClashConfig,
				"selection":         filepath.Join(dir, "localclash-packs.yaml"),
				"policy":            paths.policy,
				"rules_cache":       paths.cache,
				"output":            generated,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("subscriptions_refresh returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	impact := content["localclash_config"].(map[string]any)
	if impact["applied_auto"] != true || impact["requires_agent_replan"] == true {
		t.Fatalf("localclash impact = %+v, want auto apply", impact)
	}
	if _, err := os.Stat(generated); err != nil {
		t.Fatalf("generated config missing after refresh: %v", err)
	}
	if !strings.Contains(readMCPFile(t, localClashConfig), "SG 02") {
		t.Fatalf("localclash config was not updated: %s", readMCPFile(t, localClashConfig))
	}
}

func TestToolsCallConfigIntentInspectReturnsDurableProxyGroups(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	dir := filepath.Dir(paths.subscription)
	localClashConfig := filepath.Join(dir, "localclash.yaml")
	writeMCPFile(t, localClashConfig, `version: 1
proxy_groups:
  AI:
    mode: manual
    match:
      type: name_regex
      pattern: SG
      min: 1
    reason: Use Singapore-labelled nodes.
    boundary: name_based_hint_only
custom_rules:
  - id: huggingface_temp
    target: AI
    rules:
      - type: domain_suffix
        value: huggingface.co
packs:
  - id: blackmatrix7_OpenAI
    target: AI
`)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "config_intent_inspect",
			"arguments": map[string]any{
				"config":       localClashConfig,
				"subscription": paths.subscription,
				"rules_cache":  paths.cache,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("config_intent_inspect returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["exists"] != true || content["valid"] != true || content["resolved"] != true {
		t.Fatalf("intent status = %+v, want exists/valid/resolved", content)
	}
	proxyGroups := content["proxy_groups"].([]any)
	if len(proxyGroups) != 1 || proxyGroups[0].(map[string]any)["id"] != "AI" {
		t.Fatalf("proxy_groups = %+v, want AI", proxyGroups)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("config_intent_inspect structured content is not serializable: %v", err)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "sg.example.com") {
		t.Fatalf("config_intent_inspect leaked subscription details in %s", data)
	}
}

func TestToolsCallSubscriptionsRefreshReportsStaleExactNodes(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	dir := filepath.Dir(paths.subscription)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`proxies:
  - name: SG 02
    type: ss
    server: sg2.example.com
    password: secret
`))
	}))
	t.Cleanup(server.Close)
	subConfig := filepath.Join(dir, "localclash-subscriptions.yaml")
	runtimeDir := filepath.Join(dir, ".runtime", "subscriptions")
	localClashConfig := filepath.Join(dir, "localclash.yaml")
	generated := filepath.Join(dir, "generated", "mihomo.yaml")
	writeMCPFile(t, subConfig, fmt.Sprintf(`version: 1
sources:
  - id: primary
    url: %s/sub?token=secret-token
`, server.URL))
	writeMCPFile(t, localClashConfig, `version: 1
proxy_groups:
  AI:
    mode: manual
    nodes:
      - SG 01
    selected_nodes:
      - SG 01
    reason: User explicitly selected this line.
packs:
  - id: blackmatrix7_OpenAI
    target: AI
`)
	writeMCPFile(t, generated, "sentinel: keep\n")
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "subscriptions_refresh",
			"arguments": map[string]any{
				"config":            subConfig,
				"runtime_dir":       runtimeDir,
				"merged":            paths.subscription,
				"localclash_config": localClashConfig,
				"selection":         filepath.Join(dir, "localclash-packs.yaml"),
				"policy":            paths.policy,
				"rules_cache":       paths.cache,
				"output":            generated,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("subscriptions_refresh returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	impact := content["localclash_config"].(map[string]any)
	if impact["state"] != "stale_exact_nodes" || impact["requires_agent_replan"] == true || impact["applied_auto"] == true {
		t.Fatalf("localclash impact = %+v, want stale exact nodes without agent replan", impact)
	}
	missing := impact["missing_nodes"].([]any)
	if len(missing) != 1 || missing[0] != "SG 01" {
		t.Fatalf("missing_nodes = %+v, want SG 01", missing)
	}
	if got := readMCPFile(t, generated); got != "sentinel: keep\n" {
		t.Fatalf("generated config was overwritten: %q", got)
	}
}

func TestToolsCallSubscriptionNodesListReturnsSafeSummaries(t *testing.T) {
	subscription := setupMCPSubscriptionNodesFixture(t)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "subscription_nodes_list",
			"arguments": map[string]any{
				"subscription": subscription,
				"limit":        1,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("subscription_nodes_list returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["match_basis"] != "subscription_proxy_name" {
		t.Fatalf("match_basis = %v, want subscription_proxy_name", content["match_basis"])
	}
	if content["total"] != float64(2) || content["returned"] != float64(1) {
		t.Fatalf("content = %+v, want total 2 returned 1", content)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("subscription_nodes_list structured content is not serializable: %v", err)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "server") || strings.Contains(string(data), "uuid") {
		t.Fatalf("subscription_nodes_list leaked unsafe fields in %s", data)
	}
}

func TestToolsCallSubscriptionNodesSearchReturnsNameMatches(t *testing.T) {
	subscription := setupMCPSubscriptionNodesFixture(t)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "subscription_nodes_search",
			"arguments": map[string]any{
				"subscription": subscription,
				"query":        "香港",
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("subscription_nodes_search returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["total"] != float64(1) {
		t.Fatalf("content = %+v, want one name match", content)
	}
	suggestion := content["selector_suggestion"].(map[string]any)
	if suggestion["type"] != "name_regex" || suggestion["boundary"] != "name_based_hint_only" {
		t.Fatalf("selector suggestion = %+v, want name_regex boundary", suggestion)
	}
	if !strings.Contains(fmt.Sprint(content["note"]), "do not verify network egress location") {
		t.Fatalf("note = %v, want egress boundary", content["note"])
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("subscription_nodes_search structured content is not serializable: %v", err)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "server") || strings.Contains(string(data), "uuid") {
		t.Fatalf("subscription_nodes_search leaked unsafe fields in %s", data)
	}
}

func TestToolsCallConfigDraftRenderReturnsSerializableResult(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "config_draft_render",
			"arguments": map[string]any{
				"draft_name":   "ai-test",
				"subscription": paths.subscription,
				"policy":       paths.policy,
				"rules_cache":  paths.cache,
				"drafts_dir":   paths.outputDir,
				"test":         false,
				"overlay": map[string]any{
					"packs": []map[string]any{
						{"id": "blackmatrix7_OpenAI", "target": "AI"},
					},
					"proxy_groups": []map[string]any{
						{"id": "AI", "nodes": []string{"SG 01"}, "mode": "manual"},
					},
				},
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("config_draft_render returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["valid"] != true {
		t.Fatalf("config_draft_render valid = %v, want true", content["valid"])
	}
	if _, err := os.Stat(content["output"].(string)); err != nil {
		t.Fatalf("plan output missing: %v", err)
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("config_draft_render structured content is not serializable: %v", err)
	}
}

func TestToolsCallProxyGroupBuildReturnsReusableIntent(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "proxy_group_build",
			"arguments": map[string]any{
				"id":           "TempLine",
				"mode":         "manual",
				"subscription": paths.subscription,
				"nodes":        []string{"SG 01"},
				"reason":       "User explicitly selected this line.",
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("proxy_group_build returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["target"] != "TempLine" {
		t.Fatalf("target = %v, want TempLine", content["target"])
	}
	proxyGroup := content["proxy_group"].(map[string]any)
	if proxyGroup["id"] != "TempLine" {
		t.Fatalf("proxy_group = %+v, want reusable intent with id", proxyGroup)
	}
	nodes := content["selected_nodes"].([]any)
	if len(nodes) != 1 || nodes[0] != "SG 01" {
		t.Fatalf("selected_nodes = %+v, want SG 01", nodes)
	}
}

func TestToolsCallCustomRulesBuildReturnsReusableIntent(t *testing.T) {
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "custom_rules_build",
			"arguments": map[string]any{
				"id":     "huggingface_temp",
				"target": "TempLine",
				"rules": []map[string]any{
					{"type": "domain_suffix", "value": "huggingface.co"},
				},
				"reason": "User asked huggingface.co to use the temporary line.",
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("custom_rules_build returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["target"] != "TempLine" || content["rule_count"] != float64(1) {
		t.Fatalf("custom rule content = %+v, want TempLine with one rule", content)
	}
}

func TestToolsCallConfigDraftRenderSupportsCustomRulesWithoutPacks(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "config_draft_render",
			"arguments": map[string]any{
				"draft_name":   "huggingface-temp",
				"subscription": paths.subscription,
				"policy":       paths.policy,
				"rules_cache":  paths.cache,
				"drafts_dir":   paths.outputDir,
				"test":         false,
				"overlay": map[string]any{
					"custom_rules": []map[string]any{
						{
							"id":     "huggingface_temp",
							"target": "TempLine",
							"rules": []map[string]any{
								{"type": "domain_suffix", "value": "huggingface.co"},
							},
						},
					},
					"proxy_groups": []map[string]any{
						{"id": "TempLine", "nodes": []string{"SG 01"}, "mode": "manual"},
					},
				},
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("config_draft_render returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["valid"] != true {
		t.Fatalf("config_draft_render valid = %v, want true", content["valid"])
	}
	config := readMCPFile(t, content["output"].(string))
	if !strings.Contains(config, "DOMAIN-SUFFIX,huggingface.co,TempLine") || !strings.Contains(config, "name: TempLine") {
		t.Fatalf("candidate config missing custom rule or proxy group:\n%s", config)
	}
}

func TestToolsCallConfigDraftApplyPersistsSelectionAndGeneratedConfig(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	renderResp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "config_draft_render",
			"arguments": map[string]any{
				"draft_name":   "ai-test",
				"subscription": paths.subscription,
				"policy":       paths.policy,
				"rules_cache":  paths.cache,
				"drafts_dir":   paths.outputDir,
				"test":         false,
				"overlay": map[string]any{
					"packs": []map[string]any{
						{"id": "blackmatrix7_OpenAI", "target": "AI"},
					},
					"proxy_groups": []map[string]any{
						{"id": "AI", "nodes": []string{"SG 01"}, "mode": "manual"},
					},
				},
			},
		},
	})
	if renderResp.Error != nil {
		t.Fatalf("config_draft_render returned JSON-RPC error: %+v", renderResp.Error)
	}
	renderResult := marshalToolResult(t, renderResp.Result)
	plan := renderResult.StructuredContent.(map[string]any)
	applyResp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "config_draft_apply",
			"arguments": map[string]any{
				"draft_id":   plan["draft_id"],
				"drafts_dir": paths.outputDir,
				"test":       false,
			},
		},
	})
	if applyResp.Error != nil {
		t.Fatalf("config_draft_apply returned JSON-RPC error: %+v", applyResp.Error)
	}
	result := marshalToolResult(t, applyResp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["applied"] != true || content["valid"] != true {
		t.Fatalf("config_draft_apply content = %+v, want applied valid", content)
	}
	if _, err := os.Stat("generated/mihomo.yaml"); err != nil {
		t.Fatalf("generated config missing after apply: %v", err)
	}
	if _, err := os.Stat("localclash.yaml"); err != nil {
		t.Fatalf("localclash config missing after apply: %v", err)
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("config_draft_apply structured content is not serializable: %v", err)
	}
	selection := readMCPFile(t, "localclash-packs.yaml")
	if !strings.Contains(selection, "pack: OpenAI") || !strings.Contains(selection, "target: AI") {
		t.Fatalf("selection was not updated: %s", selection)
	}
}

func TestToolsCallConfigDraftRenderInvalidInputReturnsError(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "config_draft_render",
			"arguments": map[string]any{
				"subscription": paths.subscription,
				"policy":       paths.policy,
				"rules_cache":  paths.cache,
				"drafts_dir":   paths.outputDir,
				"test":         false,
				"overlay": map[string]any{
					"packs": []map[string]any{
						{"id": "missing_pack", "target": "DIRECT"},
					},
				},
			},
		},
	})
	if resp.Error == nil {
		t.Fatal("expected config_draft_render JSON-RPC error")
	}
	if !strings.Contains(resp.Error.Message, "missing_pack") {
		t.Fatalf("error = %+v, want missing pack", resp.Error)
	}
}

func TestToolsCallRejectsStringifiedArguments(t *testing.T) {
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "config_draft_render",
			"arguments": `{"overlay":{"packs":[{"id":"sukkaw_ai","target":"AI_US_JP"}]}}`,
		},
	})
	if resp.Error == nil {
		t.Fatal("expected stringified arguments to be rejected")
	}
	if !strings.Contains(resp.Error.Message, "must be a JSON object") || !strings.Contains(resp.Error.Message, "not \"arguments\":\"{...}\"") {
		t.Fatalf("error = %+v, want object-construction guidance", resp.Error)
	}
}

func TestToolsCallPacksListReturnsSerializableResult(t *testing.T) {
	setupMCPPackCache(t)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "packs_list",
			"arguments": map[string]any{"name": "open"},
		},
	})
	if resp.Error != nil {
		t.Fatalf("packs_list returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["total"] != float64(1) {
		t.Fatalf("packs_list total = %v, want 1", content["total"])
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("packs_list structured content is not serializable: %v", err)
	}
}

func TestToolsCallPacksListUsesBootstrapCatalog(t *testing.T) {
	state := appinit.RuntimeState{
		Rules: appinit.RulesState{
			CatalogAvailable: true,
			Packs: []rules.PackSummary{
				{ID: "blackmatrix7_OpenAI", Source: "blackmatrix7", Name: "OpenAI", Target: "AI", ProviderCount: 1, RuleCount: 1},
			},
			Details: map[string]rules.PackDetail{
				"blackmatrix7_OpenAI": {ID: "blackmatrix7_OpenAI", Source: "blackmatrix7", Name: "OpenAI", Target: "AI"},
			},
		},
	}
	resp := callHandleWithServer(t, NewServerWithState(state), map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "packs_list",
			"arguments": map[string]any{"name": "open"},
		},
	})
	if resp.Error != nil {
		t.Fatalf("packs_list returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["total"] != float64(1) {
		t.Fatalf("packs_list total = %v, want 1", content["total"])
	}
}

func TestToolsCallPacksListReturnsBootstrapDiagnostics(t *testing.T) {
	state := appinit.RuntimeState{
		Rules: appinit.RulesState{
			CatalogAvailable: false,
			Diagnostic:       "rules cache unavailable",
		},
	}
	resp := callHandleWithServer(t, NewServerWithState(state), map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "packs_list",
			"arguments": map[string]any{},
		},
	})
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "rules cache unavailable") {
		t.Fatalf("response error = %+v, want bootstrap diagnostic", resp.Error)
	}
}

func TestToolsCallPacksGetReturnsSerializableResult(t *testing.T) {
	setupMCPPackCache(t)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "packs_get",
			"arguments": map[string]any{"id": "blackmatrix7_OpenAI"},
		},
	})
	if resp.Error != nil {
		t.Fatalf("packs_get returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	pack := content["pack"].(map[string]any)
	if pack["id"] != "blackmatrix7_OpenAI" {
		t.Fatalf("pack id = %v, want blackmatrix7_OpenAI", pack["id"])
	}
	if _, ok := pack["catalog_path"]; ok {
		t.Fatalf("pack contains catalog_path: %+v", pack)
	}
	providers := pack["providers"].([]any)
	provider := providers[0].(map[string]any)
	if provider["path"] != ".runtime/mihomo/rule-packs/blackmatrix7/OpenAI.yaml" {
		t.Fatalf("provider path = %v", provider["path"])
	}
	for _, key := range []string{"url", "provider_path", "resolved_runtime_path", "provider_file_exists"} {
		if _, ok := provider[key]; ok {
			t.Fatalf("provider contains %s: %+v", key, provider)
		}
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("packs_get structured content is not serializable: %v", err)
	}
}

func TestToolsCallPacksGetUsesBootstrapCatalog(t *testing.T) {
	state := appinit.RuntimeState{
		Rules: appinit.RulesState{
			CatalogAvailable: true,
			Details: map[string]rules.PackDetail{
				"blackmatrix7_OpenAI": {
					ID:     "blackmatrix7_OpenAI",
					Source: "blackmatrix7",
					Name:   "OpenAI",
					Target: "AI",
					Providers: []rules.ProviderSummary{
						{Name: "blackmatrix7_OpenAI", Path: "./rule-packs/blackmatrix7/OpenAI.yaml"},
					},
				},
			},
		},
		Paths: appinit.RuntimePaths{MihomoRuntimeDir: ".runtime/mihomo"},
	}
	resp := callHandleWithServer(t, NewServerWithState(state), map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "packs_get",
			"arguments": map[string]any{"id": "blackmatrix7_OpenAI"},
		},
	})
	if resp.Error != nil {
		t.Fatalf("packs_get returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	pack := result.StructuredContent.(map[string]any)["pack"].(map[string]any)
	if pack["id"] != "blackmatrix7_OpenAI" {
		t.Fatalf("pack = %+v, want OpenAI", pack)
	}
	providers := pack["providers"].([]any)
	provider := providers[0].(map[string]any)
	if provider["path"] != ".runtime/mihomo/rule-packs/blackmatrix7/OpenAI.yaml" {
		t.Fatalf("provider = %+v, want runtime-local path", provider)
	}
	if _, ok := provider["provider_file_exists"]; ok {
		t.Fatalf("provider contains provider_file_exists: %+v", provider)
	}
}

func TestToolsCallPackRulesReadReturnsRuleSamples(t *testing.T) {
	cache, providerCache, server := setupMCPPackRulesFixture(t, "DOMAIN-SUFFIX,openai.com\nDOMAIN-SUFFIX,chatgpt.com\n")
	defer server.Close()
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "pack_rules_read",
			"arguments": map[string]any{
				"id":             "sukkaw_ai",
				"cache":          cache,
				"provider_cache": providerCache,
				"limit":          1,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("pack_rules_read returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	summary := content["summary"].(map[string]any)
	if summary["rule_count"] != float64(2) || summary["domain_suffix_count"] != float64(2) {
		t.Fatalf("summary = %+v, want two domain suffix rules", summary)
	}
	components := content["components"].([]any)
	component := components[0].(map[string]any)
	if component["available"] != true || component["truncated"] != true {
		t.Fatalf("component = %+v, want available truncated sample", component)
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("pack_rules_read structured content is not serializable: %v", err)
	}
}

func TestToolsCallPackRulesPrefetchThenQuery(t *testing.T) {
	cache, providerCache, server := setupMCPPackRulesFixture(t, "DOMAIN-SUFFIX,huggingface.co\n")
	defer server.Close()
	prefetch := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "pack_rules_prefetch",
			"arguments": map[string]any{
				"ids":            []string{"sukkaw_ai"},
				"cache":          cache,
				"provider_cache": providerCache,
			},
		},
	})
	if prefetch.Error != nil {
		t.Fatalf("pack_rules_prefetch returned JSON-RPC error: %+v", prefetch.Error)
	}
	query := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "pack_rules_query",
			"arguments": map[string]any{
				"query":          "cdn.huggingface.co",
				"cache":          cache,
				"provider_cache": providerCache,
			},
		},
	})
	if query.Error != nil {
		t.Fatalf("pack_rules_query returned JSON-RPC error: %+v", query.Error)
	}
	result := marshalToolResult(t, query.Result)
	content := result.StructuredContent.(map[string]any)
	matches := content["matches"].([]any)
	if len(matches) != 1 || matches[0].(map[string]any)["pack_id"] != "sukkaw_ai" {
		t.Fatalf("matches = %+v, want sukkaw_ai hit", matches)
	}
	if content["cache_complete"] != true {
		t.Fatalf("content = %+v, want complete local cache", content)
	}
}

func TestToolsCallConfigBaseInspectReturnsSerializableResult(t *testing.T) {
	config := setupMCPInspectConfig(t)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "config_base_inspect",
			"arguments": map[string]any{
				"config": config,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("config_base_inspect returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["layer"] != "base" || content["modifiable"] != false {
		t.Fatalf("content = %+v, want base non-modifiable", content)
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("config_base_inspect structured content is not serializable: %v", err)
	}
}

func TestToolsCallConfigOverlayInspectReturnsSerializableResult(t *testing.T) {
	config := setupMCPInspectConfig(t)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "config_overlay_inspect",
			"arguments": map[string]any{
				"config": config,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("config_overlay_inspect returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["layer"] != "overlay" || content["modifiable"] != true {
		t.Fatalf("content = %+v, want overlay modifiable", content)
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("config_overlay_inspect structured content is not serializable: %v", err)
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

func TestToolsCallEnvironmentInspectReturnsSerializableResult(t *testing.T) {
	dir := t.TempDir()
	refRoot := filepath.Join(dir, "openclash-reference")
	if err := os.MkdirAll(filepath.Join(refRoot, "snapshot"), 0o755); err != nil {
		t.Fatal(err)
	}
	state := appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			SubscriptionPath:   filepath.Join(dir, "subscription.yaml"),
			SubscriptionConfig: filepath.Join(dir, "localclash-subscriptions.yaml"),
			GeneratedConfig:    filepath.Join(dir, "generated", "mihomo.yaml"),
			MihomoRuntimeDir:   filepath.Join(dir, ".runtime", "mihomo"),
			RulesCacheDir:      filepath.Join(dir, ".runtime", "rules", "packs"),
			CorePath:           filepath.Join(dir, "bin", "mihomo"),
		},
	}
	server := NewServerWithState(state)
	req, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "environment_inspect",
			"arguments": map[string]any{
				"openclash_reference_root": refRoot,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := server.Handle(context.Background(), req)
	if resp.Error != nil {
		t.Fatalf("environment_inspect returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if _, ok := content["host"].(map[string]any); !ok {
		t.Fatalf("content = %+v, want host object", content)
	}
	if _, ok := content["network_capabilities"]; ok {
		t.Fatalf("content uses old network_capabilities field: %+v", content)
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("environment_inspect structured content is not serializable: %v", err)
	}
	data, _ := json.Marshal(result.StructuredContent)
	for _, secret := range []string{"subscription-url", "server.example.com"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("environment_inspect leaked %q in %s", secret, data)
		}
	}
}

func TestRunRuntimeToolReturnsSerializableResult(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "mihomo")
	writeTestExecutable(t, core, `#!/bin/sh
for arg in "$@"; do
  if [ "$arg" = "-t" ]; then
    echo configuration test is successful
    exit 0
  fi
done
echo runtime started
sleep 30
`)
	config := filepath.Join(dir, "mihomo.yaml")
	if err := os.WriteFile(config, []byte("external-controller: 127.0.0.1:9090\nexternal-ui: ui/zashboard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(dir, "runtime")
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "run_runtime",
			"arguments": map[string]any{
				"core":        core,
				"config":      config,
				"runtime_dir": workDir,
				"log_file":    filepath.Join(workDir, "mihomo.log"),
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("run_runtime returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	defer killMCPProcess(int(content["pid"].(float64)))
	if content["started"] != true || content["already_running"] != false {
		t.Fatalf("run_runtime content = %+v, want started", content)
	}
	if content["external_ui_url"] != "http://127.0.0.1:9090/ui" {
		t.Fatalf("external ui url = %v", content["external_ui_url"])
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("run_runtime structured content is not serializable: %v", err)
	}
}

func TestRunRuntimeToolPreflightErrorReturnsToolResult(t *testing.T) {
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "run_runtime",
			"arguments": map[string]any{
				"config": filepath.Join(t.TempDir(), "missing.yaml"),
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("run_runtime preflight returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["started"] != false || content["error"] == "" {
		t.Fatalf("content = %+v, want started false error", content)
	}
}

func TestRuntimeStatusToolReturnsSerializableResult(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "mihomo.pid"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "mihomo.yaml")
	if err := os.WriteFile(config, []byte("external-controller: 127.0.0.1:9090\nexternal-ui: ui/zashboard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "runtime_status",
			"arguments": map[string]any{
				"config":      config,
				"runtime_dir": workDir,
				"log_file":    filepath.Join(workDir, "mihomo.log"),
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("runtime_status returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["running"] != true || int(content["pid"].(float64)) != os.Getpid() {
		t.Fatalf("runtime_status content = %+v, want current pid running", content)
	}
	if content["external_ui_url"] != "http://127.0.0.1:9090/ui" {
		t.Fatalf("external ui url = %v", content["external_ui_url"])
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("runtime_status structured content is not serializable: %v", err)
	}
}

func TestStopRuntimeToolStopsStartedRuntime(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "mihomo")
	writeTestExecutable(t, core, `#!/bin/sh
for arg in "$@"; do
  if [ "$arg" = "-t" ]; then
    echo configuration test is successful
    exit 0
  fi
done
sleep 30
`)
	config := filepath.Join(dir, "mihomo.yaml")
	if err := os.WriteFile(config, []byte("external-controller: 127.0.0.1:9090\nexternal-ui: ui/zashboard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(dir, "runtime")
	runResp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "run_runtime",
			"arguments": map[string]any{
				"core":        core,
				"config":      config,
				"runtime_dir": workDir,
				"log_file":    filepath.Join(workDir, "mihomo.log"),
			},
		},
	})
	if runResp.Error != nil {
		t.Fatalf("run_runtime returned JSON-RPC error: %+v", runResp.Error)
	}
	runResult := marshalToolResult(t, runResp.Result)
	runContent := runResult.StructuredContent.(map[string]any)
	pid := int(runContent["pid"].(float64))
	defer killMCPProcess(pid)

	stopResp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "stop_runtime",
			"arguments": map[string]any{
				"runtime_dir": workDir,
				"timeout_ms":  2000,
			},
		},
	})
	if stopResp.Error != nil {
		t.Fatalf("stop_runtime returned JSON-RPC error: %+v", stopResp.Error)
	}
	stopResult := marshalToolResult(t, stopResp.Result)
	stopContent := stopResult.StructuredContent.(map[string]any)
	if stopContent["stopped"] != true || stopContent["was_running"] != true || int(stopContent["pid"].(float64)) != pid {
		t.Fatalf("stop_runtime content = %+v, want stopped pid %d", stopContent, pid)
	}
	if _, err := json.Marshal(stopResult.StructuredContent); err != nil {
		t.Fatalf("stop_runtime structured content is not serializable: %v", err)
	}
}

func TestRunRuntimeToolUsesBootstrapDiagnostics(t *testing.T) {
	state := appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			GeneratedConfig:  "generated/mihomo.yaml",
			MihomoRuntimeDir: ".runtime/mihomo",
			CorePath:         "bin/mihomo",
		},
		Config: appinit.ConfigState{
			Available:  false,
			Diagnostic: "config render skipped because effective subscription is unavailable",
		},
	}
	resp := callHandleWithServer(t, NewServerWithState(state), map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "run_runtime",
			"arguments": map[string]any{},
		},
	})
	if resp.Error != nil {
		t.Fatalf("run_runtime returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if !strings.Contains(content["error"].(string), "effective subscription is unavailable") {
		t.Fatalf("content = %+v, want bootstrap diagnostic", content)
	}
	nextActions := content["next_actions"].([]any)
	if len(nextActions) == 0 {
		t.Fatalf("content = %+v, want next actions", content)
	}
}

func TestRunRuntimeToolRendersMissingGeneratedConfigFromSubscription(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	dir := filepath.Dir(paths.subscription)
	core := filepath.Join(dir, "mihomo")
	writeTestExecutable(t, core, `#!/bin/sh
for arg in "$@"; do
  if [ "$arg" = "-t" ]; then
    echo configuration test is successful
    exit 0
  fi
done
echo runtime started
sleep 30
`)
	generated := filepath.Join(dir, "generated", "mihomo.yaml")
	state := appinit.RuntimeState{
		Paths: appinit.RuntimePaths{
			GeneratedConfig:     generated,
			SubscriptionPath:    paths.subscription,
			PolicyPath:          paths.policy,
			RulesCacheDir:       paths.cache,
			MihomoRuntimeDir:    filepath.Join(dir, ".runtime", "mihomo"),
			CorePath:            core,
			PacksSelectionPath:  filepath.Join(dir, "missing-packs.yaml"),
			SubscriptionConfig:  filepath.Join(dir, "localclash-subscriptions.yaml"),
			SubscriptionRuntime: filepath.Join(dir, ".runtime", "subscriptions"),
		},
		Config: appinit.ConfigState{
			Available:  false,
			Diagnostic: "config render skipped because effective subscription is unavailable",
		},
	}

	resp := callHandleWithServer(t, NewServerWithState(state), map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "run_runtime",
			"arguments": map[string]any{},
		},
	})
	if resp.Error != nil {
		t.Fatalf("run_runtime returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	defer killMCPProcess(int(content["pid"].(float64)))
	if content["started"] != true {
		t.Fatalf("content = %+v, want started after auto render", content)
	}
	if _, err := os.Stat(generated); err != nil {
		t.Fatalf("generated config missing after run_runtime auto render: %v", err)
	}
}

func TestRemovedMCPToolsReturnUnknownTool(t *testing.T) {
	for _, name := range removedMCPTools() {
		resp := callHandle(t, map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      name,
				"arguments": map[string]any{},
			},
		})
		if resp.Error == nil {
			t.Fatalf("%s expected unknown tool JSON-RPC error", name)
		}
		if !strings.Contains(resp.Error.Message, "unknown tool") {
			t.Fatalf("%s error = %+v, want unknown tool", name, resp.Error)
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

func callHandle(t *testing.T, value any) *rpcResponse {
	t.Helper()
	return callHandleWithServer(t, NewServer(), value)
}

func callHandleWithServer(t *testing.T, server *Server, value any) *rpcResponse {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	resp := server.Handle(context.Background(), data)
	if resp == nil {
		t.Fatal("nil response")
	}
	return resp
}

func removedMCPTools() []string {
	return []string{
		"config_test",
		"config_render",
		"config_plan_apply",
		"config_plan_render",
		"inspect_generated_config",
		"rules_adapt",
		"rules_render",
		"switch_proxy_group",
		"apply_router_config",
	}
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

func killMCPProcess(pid int) {
	process, err := os.FindProcess(pid)
	if err == nil {
		_ = process.Kill()
	}
}

func setupMCPPackCache(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	cacheDir := filepath.Join(dir, ".runtime", "rules", "packs")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(`
version: 1
source: blackmatrix7
adapter: blackmatrix7
renderable: true
packs:
  - id: OpenAI
    name: OpenAI
    target: AI
    renderable: true
    components:
      - id: OpenAI
        behavior: classical
        format: yaml
        order_class: mixed
        url: https://example.com/OpenAI.yaml
        path: ./rule-packs/blackmatrix7/OpenAI.yaml
`)
	if err := os.WriteFile(filepath.Join(cacheDir, "blackmatrix7.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	providerPath := filepath.Join(dir, ".runtime", "mihomo", "rule-packs", "blackmatrix7", "OpenAI.yaml")
	if err := os.MkdirAll(filepath.Dir(providerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(providerPath, []byte("payload:\n  - DOMAIN,openai.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setupMCPPackRulesFixture(t *testing.T, providerBody string) (string, string, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "packs")
	providerCache := filepath.Join(dir, "provider-cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(providerBody))
	}))
	if err := rules.WritePackCache(cacheDir, rules.PackCache{
		Version:    1,
		Source:     "sukkaw",
		Adapter:    "sukkaw",
		Renderable: true,
		Packs: []rules.Pack{
			{
				ID:         "ai",
				Name:       "ai",
				Target:     "AI",
				Renderable: true,
				Components: []rules.Component{
					{
						ID:         "non_ip",
						Behavior:   "classical",
						Format:     "text",
						OrderClass: "non_ip",
						URL:        server.URL + "/ai.txt",
						Path:       "./rule-packs/sukkaw/ai_non_ip.txt",
					},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	return cacheDir, providerCache, server
}

func setupMCPInspectConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mihomo.yaml")
	if err := os.WriteFile(path, []byte(`
mode: rule
external-controller: 127.0.0.1:9090
proxies:
  - name: SG 01
    type: ss
    server: sg.example.com
    password: secret
proxy-groups:
  - name: PROXY
    type: select
    proxies: [SG 01]
rule-providers:
  blackmatrix7_OpenAI:
    type: http
    behavior: classical
rules:
  - RULE-SET,blackmatrix7_OpenAI,AI
x-localclash:
  version: 1
  base:
    modifiable: false
    description: localClash generated base config
  overlay:
    modifiable: true
    packs:
      - id: blackmatrix7_OpenAI
        source: blackmatrix7
        target: AI
    proxy_groups:
      - id: AI
        mode: manual
        nodes: [SG 01]
    rule_providers:
      - name: blackmatrix7_OpenAI
        behavior: classical
        type: http
    rules:
      - type: RULE-SET
        provider: blackmatrix7_OpenAI
        target: AI
    insertion: after local safety baseline, before base rules
`), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

type mcpPlanFixture struct {
	subscription string
	policy       string
	cache        string
	outputDir    string
}

func setupMCPPlanFixture(t *testing.T) mcpPlanFixture {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	paths := mcpPlanFixture{
		subscription: filepath.Join(dir, "subscription.yaml"),
		policy:       filepath.Join(dir, "policy.yaml"),
		cache:        filepath.Join(dir, ".runtime", "rules", "packs"),
		outputDir:    filepath.Join(dir, ".runtime", "plans"),
	}
	if err := os.MkdirAll(paths.cache, 0o755); err != nil {
		t.Fatal(err)
	}
	writeMCPFile(t, paths.subscription, `proxies:
  - name: SG 01
    type: ss
    server: sg.example.com
    password: secret
`)
	writeMCPFile(t, paths.policy, `rule_source:
  base_url: https://example.com/rules
groups:
  direct: DIRECT
  reject: REJECT
  proxy: PROXY
  auto: AUTO
  manual: MANUAL
  apple: Apple
provider_mapping:
  applications:
    path: applications.txt
    behavior: classical
    target: direct
modes:
  default: whitelist
  whitelist:
    rules:
      - provider: applications
        target: direct
      - match: true
        target: proxy
  blacklist:
    rules:
      - match: true
        target: direct
`)
	writeMCPFile(t, filepath.Join(dir, "localclash-packs.yaml"), `version: 1
proxy_groups: {}
enabled_packs: []
`)
	writeMCPFile(t, filepath.Join(paths.cache, "blackmatrix7.yaml"), `version: 1
source: blackmatrix7
adapter: blackmatrix7
renderable: true
packs:
  - id: OpenAI
    name: OpenAI
    target: AI
    renderable: true
    components:
      - id: OpenAI
        behavior: classical
        format: yaml
        order_class: mixed
        url: https://example.com/OpenAI.yaml
        path: ./rule-packs/blackmatrix7/OpenAI.yaml
`)
	return paths
}

type mcpSubscriptionsFixture struct {
	config     string
	runtimeDir string
	merged     string
}

func setupMCPSubscriptionsFixture(t *testing.T) mcpSubscriptionsFixture {
	t.Helper()
	dir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`proxies:
  - name: SG 01
    type: ss
    server: sg.example.com
    password: secret
`))
	}))
	t.Cleanup(server.Close)
	paths := mcpSubscriptionsFixture{
		config:     filepath.Join(dir, "localclash-subscriptions.yaml"),
		runtimeDir: filepath.Join(dir, ".runtime", "subscriptions"),
		merged:     filepath.Join(dir, "subscription.yaml"),
	}
	writeMCPFile(t, paths.config, fmt.Sprintf(`version: 1
sources:
  - id: primary
    url: %s/sub?token=secret-token
`, server.URL))
	return paths
}

func setupMCPSubscriptionNodesFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	subscription := filepath.Join(dir, "subscription.yaml")
	writeMCPFile(t, subscription, `proxies:
  - name: SG 01
    type: ss
    server: sg.example.com
    password: secret
  - name: 🇭🇰香港 01 | HK
    type: vmess
    server: hk.example.com
    uuid: private-uuid
`)
	return subscription
}

func writeMCPFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readMCPFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
