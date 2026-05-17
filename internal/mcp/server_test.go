package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	for _, name := range []string{"doctor", "config_base_inspect", "config_overlay_inspect", "config_plan_render", "packs_list", "packs_get", "subscription_nodes_list", "subscription_nodes_search", "subscriptions_status", "tools_list", "subscriptions_configure", "subscriptions_refresh", "virtual_nodes_list", "virtual_nodes_get", "config_render", "run_runtime"} {
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
		"config_base_inspect":       SafeRead,
		"config_overlay_inspect":    SafeRead,
		"packs_get":                 SafeRead,
		"packs_list":                SafeRead,
		"subscription_nodes_list":   SafeRead,
		"subscription_nodes_search": SafeRead,
		"subscriptions_status":      SafeRead,
		"tools_list":                SafeRead,
		"virtual_nodes_get":         SafeRead,
		"virtual_nodes_list":        SafeRead,
		"config_plan_render":        SafeWrite,
		"config_render":             SafeWrite,
		"subscriptions_configure":   SafeWrite,
		"subscriptions_refresh":     SafeWrite,
		"run_runtime":               ConfirmRequired,
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

func TestToolsCallConfigPlanRenderReturnsSerializableResult(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "config_plan_render",
			"arguments": map[string]any{
				"plan_name":    "ai-test",
				"subscription": paths.subscription,
				"policy":       paths.policy,
				"rules_cache":  paths.cache,
				"output_dir":   paths.outputDir,
				"test":         false,
				"overlay": map[string]any{
					"packs": []map[string]any{
						{"id": "blackmatrix7_OpenAI", "target": "AI"},
					},
					"virtual_targets": []map[string]any{
						{"id": "AI", "node_labels": []string{"SG"}, "mode": "manual"},
					},
				},
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("config_plan_render returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["valid"] != true {
		t.Fatalf("config_plan_render valid = %v, want true", content["valid"])
	}
	if _, err := os.Stat(content["output"].(string)); err != nil {
		t.Fatalf("plan output missing: %v", err)
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("config_plan_render structured content is not serializable: %v", err)
	}
}

func TestToolsCallConfigPlanRenderInvalidInputReturnsError(t *testing.T) {
	paths := setupMCPPlanFixture(t)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "config_plan_render",
			"arguments": map[string]any{
				"subscription": paths.subscription,
				"policy":       paths.policy,
				"rules_cache":  paths.cache,
				"output_dir":   paths.outputDir,
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
		t.Fatal("expected config_plan_render JSON-RPC error")
	}
	if !strings.Contains(resp.Error.Message, "missing_pack") {
		t.Fatalf("error = %+v, want missing pack", resp.Error)
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
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("packs_get structured content is not serializable: %v", err)
	}
}

func TestToolsCallPacksGetUsesBootstrapCatalog(t *testing.T) {
	state := appinit.RuntimeState{
		Rules: appinit.RulesState{
			CatalogAvailable: true,
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

func TestToolsCallVirtualNodesListReturnsSerializableResult(t *testing.T) {
	selection, subscription := setupMCPVirtualNodesFiles(t)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "virtual_nodes_list",
			"arguments": map[string]any{
				"selection":     selection,
				"subscription":  subscription,
				"include_empty": true,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("virtual_nodes_list returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	if content["total"] != float64(2) {
		t.Fatalf("virtual_nodes_list total = %v, want 2", content["total"])
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("virtual_nodes_list structured content is not serializable: %v", err)
	}
}

func TestToolsCallVirtualNodesGetReturnsSerializableResult(t *testing.T) {
	selection, subscription := setupMCPVirtualNodesFiles(t)
	resp := callHandle(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "virtual_nodes_get",
			"arguments": map[string]any{
				"id":           "SG",
				"selection":    selection,
				"subscription": subscription,
			},
		},
	})
	if resp.Error != nil {
		t.Fatalf("virtual_nodes_get returned JSON-RPC error: %+v", resp.Error)
	}
	result := marshalToolResult(t, resp.Result)
	content := result.StructuredContent.(map[string]any)
	node := content["virtual_node"].(map[string]any)
	if node["id"] != "SG" {
		t.Fatalf("virtual node id = %v, want SG", node["id"])
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("virtual_nodes_get structured content is not serializable: %v", err)
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
}

func setupMCPVirtualNodesFiles(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	selection := filepath.Join(dir, "selection.yaml")
	subscription := filepath.Join(dir, "subscription.yaml")
	if err := os.WriteFile(selection, []byte(`
version: 1
node_labels:
  EMPTY:
    match: ["(?i)empty"]
  SG:
    match: ["(?i)sg|singapore"]
virtual_targets: {}
enabled_packs: []
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subscription, []byte(`
proxies:
  - name: SG 01
    type: ss
    server: sg.example.com
    password: secret
`), 0o644); err != nil {
		t.Fatal(err)
	}
	return selection, subscription
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
    virtual_targets:
      - id: AI
        mode: manual
        node_labels: [SG]
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
node_labels:
  SG:
    match: ["(?i)sg|singapore"]
virtual_targets: {}
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
