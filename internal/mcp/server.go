package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"localclash/internal/appinit"
	"localclash/internal/configinspect"
	"localclash/internal/configplan"
	"localclash/internal/configrender"
	"localclash/internal/corerun"
	"localclash/internal/doctor"
	"localclash/internal/envinspect"
	"localclash/internal/fileops"
	"localclash/internal/localconfig"
	"localclash/internal/rules"
	"localclash/internal/subscriptions"
)

type Server struct {
	state *appinit.RuntimeState
}

func NewServer() *Server {
	return &Server{}
}

func NewServerWithState(state appinit.RuntimeState) *Server {
	return &Server{state: &state}
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type toolResult struct {
	Content           []toolContent `json:"content"`
	StructuredContent any           `json:"structuredContent,omitempty"`
	IsError           bool          `json:"isError,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type HTTPOptions struct {
	Addr string
	Path string
}

func NormalizeHTTPOptions(opts HTTPOptions) HTTPOptions {
	if strings.TrimSpace(opts.Addr) == "" {
		opts.Addr = "127.0.0.1:8765"
	}
	if strings.TrimSpace(opts.Path) == "" {
		opts.Path = "/mcp"
	}
	if !strings.HasPrefix(opts.Path, "/") {
		opts.Path = "/" + opts.Path
	}
	return opts
}

func HTTPURL(opts HTTPOptions) string {
	opts = NormalizeHTTPOptions(opts)
	return "http://" + opts.Addr + opts.Path
}

func ListenAndServeHTTPWithState(ctx context.Context, state appinit.RuntimeState, opts HTTPOptions) error {
	opts = NormalizeHTTPOptions(opts)
	srv := &http.Server{
		Addr:              opts.Addr,
		Handler:           NewServerWithState(state).HTTPHandler(opts.Path),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func (s *Server) HTTPHandler(path string) http.Handler {
	opts := NormalizeHTTPOptions(HTTPOptions{Path: path})
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeCORSHeaders(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	mux.HandleFunc(opts.Path, func(w http.ResponseWriter, r *http.Request) {
		writeCORSHeaders(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()
		var raw json.RawMessage
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&raw); err != nil {
			writeJSON(w, http.StatusOK, errorResponse(nil, -32700, "parse error"))
			return
		}
		response := s.Handle(r.Context(), raw)
		if response == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSON(w, http.StatusOK, response)
	})
	return mux
}

func writeCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, MCP-Protocol-Version, Mcp-Session-Id")
	w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) Handle(ctx context.Context, data []byte) *rpcResponse {
	var req rpcRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return errorResponse(nil, -32700, "parse error")
	}
	if len(req.ID) == 0 {
		return nil
	}
	switch req.Method {
	case "initialize":
		protocolVersion := "2025-06-18"
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if len(req.Params) != 0 && json.Unmarshal(req.Params, &params) == nil && params.ProtocolVersion != "" {
			protocolVersion = params.ProtocolVersion
		}
		return resultResponse(req.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{
					"listChanged": false,
				},
			},
			"serverInfo": map[string]any{
				"name":    "localclash",
				"version": "dev",
			},
		})
	case "tools/list":
		return resultResponse(req.ID, map[string]any{"tools": ListedTools()})
	case "tools/call":
		result, err := s.callTool(ctx, req.Params)
		if err != nil {
			return errorResponse(req.ID, -32602, err.Error())
		}
		return resultResponse(req.ID, result)
	default:
		return errorResponse(req.ID, -32601, "method not found")
	}
}

func (s *Server) callTool(ctx context.Context, params json.RawMessage) (toolResult, error) {
	var call callParams
	if err := json.Unmarshal(params, &call); err != nil {
		return toolResult{}, err
	}
	if call.Name == "" {
		return toolResult{}, errors.New("tool name is required")
	}
	args := call.Arguments
	if len(args) == 0 {
		args = []byte("{}")
	}
	switch call.Name {
	case "config_base_inspect":
		return callConfigBaseInspect(args)
	case "config_overlay_inspect":
		return callConfigOverlayInspect(args)
	case "doctor":
		return s.callDoctor(ctx, args)
	case "environment_inspect":
		return s.callEnvironmentInspect(ctx, args)
	case "nl_file":
		return callNLFile(args)
	case "packs_list":
		return s.callPacksList(args)
	case "packs_get":
		return s.callPacksGet(args)
	case "subscription_nodes_list":
		return s.callSubscriptionNodesList(args)
	case "subscription_nodes_search":
		return s.callSubscriptionNodesSearch(args)
	case "runtime_status":
		return s.callRuntimeStatus(args)
	case "subscriptions_status":
		return s.callSubscriptionsStatus(args)
	case "tools_list":
		return jsonToolResult(ToolSummaries())
	case "subscriptions_configure":
		return s.callSubscriptionsConfigure(args)
	case "subscriptions_refresh":
		return s.callSubscriptionsRefresh(ctx, args)
	case "proxy_group_build":
		return s.callProxyGroupBuild(args)
	case "custom_rules_build":
		return callCustomRulesBuild(args)
	case "config_draft_apply":
		return s.callConfigDraftApply(ctx, args)
	case "config_draft_render":
		return s.callConfigDraftRender(ctx, args)
	case "run_runtime":
		return s.callRunRuntime(ctx, args)
	case "sed_file":
		return callSedFile(args)
	case "stop_runtime":
		return s.callStopRuntime(args)
	default:
		return toolResult{}, fmt.Errorf("unknown tool %q", call.Name)
	}
}

func callNLFile(args json.RawMessage) (toolResult, error) {
	var in struct {
		Path       string `json:"path"`
		StartLine  int    `json:"start_line"`
		LimitLines int    `json:"limit_lines"`
		MaxBytes   int    `json:"max_bytes"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	result, err := fileops.NLFile(fileops.NLFileOptions{
		Path:       in.Path,
		StartLine:  in.StartLine,
		LimitLines: in.LimitLines,
		MaxBytes:   in.MaxBytes,
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func callSedFile(args json.RawMessage) (toolResult, error) {
	var in struct {
		Path           string         `json:"path"`
		DryRun         *bool          `json:"dry_run"`
		ExpectedSHA256 string         `json:"expected_sha256"`
		Edits          []fileops.Edit `json:"edits"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	dryRun := true
	if in.DryRun != nil {
		dryRun = *in.DryRun
	}
	result, err := fileops.SedFile(fileops.SedFileOptions{
		Path:           in.Path,
		DryRun:         dryRun,
		ExpectedSHA256: in.ExpectedSHA256,
		Edits:          in.Edits,
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func callConfigBaseInspect(args json.RawMessage) (toolResult, error) {
	var in struct {
		Config string `json:"config"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	result, err := configinspect.InspectBase(configinspect.Options{ConfigPath: in.Config, Limit: in.Limit})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func callConfigOverlayInspect(args json.RawMessage) (toolResult, error) {
	var in struct {
		Config string `json:"config"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	result, err := configinspect.InspectOverlay(configinspect.Options{ConfigPath: in.Config, Limit: in.Limit})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callProxyGroupBuild(args json.RawMessage) (toolResult, error) {
	var in struct {
		ID                  string             `json:"id"`
		Mode                string             `json:"mode"`
		Match               *localconfig.Match `json:"match"`
		Nodes               []string           `json:"nodes"`
		Reason              string             `json:"reason"`
		Boundary            string             `json:"boundary"`
		Subscription        string             `json:"subscription"`
		SubscriptionConfig  string             `json:"subscription_config"`
		SubscriptionRuntime string             `json:"subscription_runtime"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil {
		if in.Subscription == "" {
			in.Subscription = s.state.Paths.SubscriptionPath
		}
		if in.SubscriptionConfig == "" {
			in.SubscriptionConfig = s.state.Paths.SubscriptionConfig
		}
		if in.SubscriptionRuntime == "" {
			in.SubscriptionRuntime = s.state.Paths.SubscriptionRuntime
		}
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return toolResult{}, fmt.Errorf("id is required")
	}
	group := localconfig.ProxyGroup{
		Mode:     in.Mode,
		Match:    in.Match,
		Nodes:    append([]string{}, in.Nodes...),
		Reason:   in.Reason,
		Boundary: in.Boundary,
	}
	resolved, err := localconfig.Resolve(localconfig.ResolveOptions{
		Config:              localconfig.Config{ProxyGroups: map[string]localconfig.ProxyGroup{id: group}},
		SubscriptionPath:    in.Subscription,
		SubscriptionConfig:  in.SubscriptionConfig,
		SubscriptionRuntime: in.SubscriptionRuntime,
	})
	if err != nil {
		return toolResult{}, err
	}
	resolvedGroup := resolved.Config.ProxyGroups[id]
	proxyGroupIntent := configplan.OverlayProxyGroupIntent{
		ID:       id,
		Mode:     resolvedGroup.Mode,
		Match:    resolvedGroup.Match,
		Nodes:    append([]string{}, resolvedGroup.Nodes...),
		Reason:   resolvedGroup.Reason,
		Boundary: resolvedGroup.Boundary,
	}
	return jsonToolResult(map[string]any{
		"proxy_group":    proxyGroupIntent,
		"id":             id,
		"target":         id,
		"selected_nodes": resolvedGroup.SelectedNodes,
	})
}

func callCustomRulesBuild(args json.RawMessage) (toolResult, error) {
	var in localconfig.CustomRule
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	id := strings.TrimSpace(in.ID)
	target := strings.TrimSpace(in.Target)
	if id == "" {
		return toolResult{}, fmt.Errorf("id is required")
	}
	if target == "" {
		return toolResult{}, fmt.Errorf("target is required")
	}
	if len(in.Rules) == 0 {
		return toolResult{}, fmt.Errorf("rules is required")
	}
	selection := rules.Selection{
		Version: 1,
		CustomRules: []rules.CustomRule{{
			ID:     id,
			Target: "DIRECT",
			Reason: in.Reason,
			Rules:  customRuleLinesForBuild(in.Rules),
		}},
	}
	if _, err := rules.RenderFragment(selection, map[string]rules.PackCache{}); err != nil {
		return toolResult{}, err
	}
	in.ID = id
	in.Target = target
	return jsonToolResult(map[string]any{
		"custom_rule": in,
		"id":          id,
		"target":      target,
		"rule_count":  len(in.Rules),
	})
}

func customRuleLinesForBuild(lines []localconfig.CustomRuleLine) []rules.CustomRuleLine {
	out := make([]rules.CustomRuleLine, 0, len(lines))
	for _, line := range lines {
		out = append(out, rules.CustomRuleLine{Type: line.Type, Value: line.Value, NoResolve: line.NoResolve})
	}
	return out
}

func (s *Server) callConfigDraftRender(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var in struct {
		DraftName           string                   `json:"draft_name"`
		Subscription        string                   `json:"subscription"`
		Policy              string                   `json:"policy"`
		Mode                string                   `json:"mode"`
		RulesCache          string                   `json:"rules_cache"`
		OutputDir           string                   `json:"drafts_dir"`
		ConfigPath          string                   `json:"config"`
		SubscriptionConfig  string                   `json:"subscription_config"`
		SubscriptionRuntime string                   `json:"subscription_runtime"`
		Test                *bool                    `json:"test"`
		Overlay             configplan.OverlayIntent `json:"overlay"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	test := true
	if in.Test != nil {
		test = *in.Test
	}
	if s.state != nil {
		if in.Subscription == "" {
			in.Subscription = s.state.Paths.SubscriptionPath
		}
		if in.Policy == "" {
			in.Policy = s.state.Paths.PolicyPath
		}
		if in.RulesCache == "" {
			in.RulesCache = s.state.Paths.RulesCacheDir
		}
		if in.SubscriptionConfig == "" {
			in.SubscriptionConfig = s.state.Paths.SubscriptionConfig
		}
		if in.SubscriptionRuntime == "" {
			in.SubscriptionRuntime = s.state.Paths.SubscriptionRuntime
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, err := configplan.Render(ctx, configplan.Options{
		PlanName:            in.DraftName,
		Subscription:        in.Subscription,
		Policy:              in.Policy,
		Mode:                in.Mode,
		RulesCache:          in.RulesCache,
		OutputDir:           in.OutputDir,
		ConfigPath:          in.ConfigPath,
		SubscriptionConfig:  in.SubscriptionConfig,
		SubscriptionRuntime: in.SubscriptionRuntime,
		Test:                test,
		Overlay:             in.Overlay,
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callConfigDraftApply(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var in struct {
		DraftID             string `json:"draft_id"`
		DraftsDir           string `json:"drafts_dir"`
		SummaryPath         string `json:"summary_path"`
		Subscription        string `json:"subscription"`
		Policy              string `json:"policy"`
		Mode                string `json:"mode"`
		RulesCache          string `json:"rules_cache"`
		ConfigPath          string `json:"config"`
		SubscriptionConfig  string `json:"subscription_config"`
		SubscriptionRuntime string `json:"subscription_runtime"`
		Selection           string `json:"selection"`
		Output              string `json:"output"`
		BackupDir           string `json:"backup_dir"`
		Test                *bool  `json:"test"`
		Core                string `json:"core"`
		RuntimeDir          string `json:"runtime_dir"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	test := true
	if in.Test != nil {
		test = *in.Test
	}
	if s.state != nil {
		if in.RulesCache == "" {
			in.RulesCache = s.state.Paths.RulesCacheDir
		}
		if in.SubscriptionConfig == "" {
			in.SubscriptionConfig = s.state.Paths.SubscriptionConfig
		}
		if in.SubscriptionRuntime == "" {
			in.SubscriptionRuntime = s.state.Paths.SubscriptionRuntime
		}
		if in.Selection == "" && s.state.Paths.PacksSelectionPath != "" {
			in.Selection = s.state.Paths.PacksSelectionPath
		}
		if in.Output == "" {
			in.Output = s.state.Paths.GeneratedConfig
		}
		if in.Core == "" {
			in.Core = s.state.Paths.CorePath
		}
		if in.RuntimeDir == "" {
			in.RuntimeDir = s.state.Paths.MihomoRuntimeDir
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, err := configplan.Apply(ctx, configplan.ApplyOptions{
		PlanID:              in.DraftID,
		PlansDir:            in.DraftsDir,
		SummaryPath:         in.SummaryPath,
		Subscription:        in.Subscription,
		Policy:              in.Policy,
		Mode:                in.Mode,
		RulesCache:          in.RulesCache,
		ConfigPath:          in.ConfigPath,
		SubscriptionConfig:  in.SubscriptionConfig,
		SubscriptionRuntime: in.SubscriptionRuntime,
		SelectionPath:       in.Selection,
		OutputPath:          in.Output,
		CorePath:            in.Core,
		WorkDir:             in.RuntimeDir,
		BackupDir:           in.BackupDir,
		Test:                test,
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callPacksList(args json.RawMessage) (toolResult, error) {
	var in struct {
		Source string `json:"source"`
		Name   string `json:"name"`
		Target string `json:"target"`
		Limit  int    `json:"limit"`
		Cache  string `json:"cache"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil {
		result, err := listPacksFromState(*s.state, in.Source, in.Name, in.Target, in.Limit)
		if err != nil {
			return toolResult{}, err
		}
		return jsonToolResult(result)
	}
	result, err := rules.ListPacks(rules.PackListOptions{
		CacheDir: in.Cache,
		Source:   in.Source,
		Name:     in.Name,
		Target:   in.Target,
		Limit:    in.Limit,
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callPacksGet(args json.RawMessage) (toolResult, error) {
	var in struct {
		ID         string `json:"id"`
		Cache      string `json:"cache"`
		RuntimeDir string `json:"runtime_dir"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil {
		if !s.state.Rules.CatalogAvailable {
			return toolResult{}, fmt.Errorf("packs catalog is unavailable: %s", s.state.Rules.Diagnostic)
		}
		id := strings.TrimSpace(in.ID)
		if id == "" {
			return toolResult{}, fmt.Errorf("pack id is required")
		}
		runtimeDir := in.RuntimeDir
		if runtimeDir == "" {
			runtimeDir = s.state.Paths.MihomoRuntimeDir
		}
		detail, ok := s.state.Rules.Details[id]
		if !ok {
			return toolResult{}, fmt.Errorf("pack %q not found in bootstrap packs catalog", id)
		}
		detail = rules.AnnotatePackRuntime(detail, runtimeDir)
		return jsonToolResult(rules.PackGetResult{Pack: detail})
	}
	result, err := rules.GetPack(rules.PackGetOptions{CacheDir: in.Cache, RuntimeDir: in.RuntimeDir, ID: in.ID})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func listPacksFromState(state appinit.RuntimeState, source, name, target string, limit int) (rules.PackListResult, error) {
	if !state.Rules.CatalogAvailable {
		return rules.PackListResult{}, fmt.Errorf("packs catalog is unavailable: %s", state.Rules.Diagnostic)
	}
	nameFilter := strings.ToLower(strings.TrimSpace(name))
	var packs []rules.PackSummary
	for _, pack := range state.Rules.Packs {
		if source != "" && pack.Source != source {
			continue
		}
		if target != "" && pack.Target != target {
			continue
		}
		if nameFilter != "" && !strings.Contains(strings.ToLower(pack.Name), nameFilter) && !strings.Contains(strings.ToLower(pack.ID), nameFilter) {
			continue
		}
		packs = append(packs, pack)
	}
	if limit > 0 && len(packs) > limit {
		packs = packs[:limit]
	}
	return rules.PackListResult{Total: len(packs), Packs: packs}, nil
}

func (s *Server) callSubscriptionsStatus(args json.RawMessage) (toolResult, error) {
	var in struct {
		Config     string `json:"config"`
		Merged     string `json:"merged"`
		RuntimeDir string `json:"runtime_dir"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil {
		if in.Config == "" {
			in.Config = s.state.Paths.SubscriptionConfig
		}
		if in.Merged == "" {
			in.Merged = s.state.Paths.SubscriptionPath
		}
		if in.RuntimeDir == "" {
			in.RuntimeDir = s.state.Paths.SubscriptionRuntime
		}
	}
	result, err := subscriptions.Status(subscriptions.StatusOptions{
		ConfigPath: in.Config,
		MergedPath: in.Merged,
		RuntimeDir: in.RuntimeDir,
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callSubscriptionNodesList(args json.RawMessage) (toolResult, error) {
	var in struct {
		Subscription string `json:"subscription"`
		Limit        int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil && in.Subscription == "" {
		in.Subscription = s.state.Paths.SubscriptionPath
	}
	result, err := rules.ListSubscriptionNodes(rules.SubscriptionNodesListOptions{
		Subscription: in.Subscription,
		Limit:        in.Limit,
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callSubscriptionNodesSearch(args json.RawMessage) (toolResult, error) {
	var in struct {
		Subscription  string   `json:"subscription"`
		Query         string   `json:"query"`
		Patterns      []string `json:"patterns"`
		CaseSensitive bool     `json:"case_sensitive"`
		Limit         int      `json:"limit"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil && in.Subscription == "" {
		in.Subscription = s.state.Paths.SubscriptionPath
	}
	result, err := rules.SearchSubscriptionNodes(rules.SubscriptionNodesSearchOptions{
		Subscription:  in.Subscription,
		Query:         in.Query,
		Patterns:      in.Patterns,
		CaseSensitive: in.CaseSensitive,
		Limit:         in.Limit,
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callSubscriptionsConfigure(args json.RawMessage) (toolResult, error) {
	var in struct {
		Config  string                 `json:"config"`
		Sources []subscriptions.Source `json:"sources"`
		Replace *bool                  `json:"replace"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil && in.Config == "" {
		in.Config = s.state.Paths.SubscriptionConfig
	}
	result, err := subscriptions.Configure(subscriptions.ConfigureOptions{
		ConfigPath: in.Config,
		Sources:    in.Sources,
		Replace:    in.Replace,
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callSubscriptionsRefresh(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var in struct {
		Config           string   `json:"config"`
		IDs              []string `json:"ids"`
		RuntimeDir       string   `json:"runtime_dir"`
		Merged           string   `json:"merged"`
		Force            bool     `json:"force"`
		UserAgent        string   `json:"user_agent"`
		LocalClashConfig string   `json:"localclash_config"`
		Selection        string   `json:"selection"`
		Policy           string   `json:"policy"`
		RulesCache       string   `json:"rules_cache"`
		Output           string   `json:"output"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil {
		if in.Config == "" {
			in.Config = s.state.Paths.SubscriptionConfig
		}
		if in.RuntimeDir == "" {
			in.RuntimeDir = s.state.Paths.SubscriptionRuntime
		}
		if in.Merged == "" {
			in.Merged = s.state.Paths.SubscriptionPath
		}
		if in.Selection == "" && s.state.Paths.PacksSelectionPath != "" {
			in.Selection = s.state.Paths.PacksSelectionPath
		}
		if in.Policy == "" {
			in.Policy = s.state.Paths.PolicyPath
		}
		if in.RulesCache == "" {
			in.RulesCache = s.state.Paths.RulesCacheDir
		}
		if in.Output == "" {
			in.Output = s.state.Paths.GeneratedConfig
		}
	}
	if in.Selection == "" {
		in.Selection = "localclash-packs.yaml"
	}
	if in.LocalClashConfig == "" {
		in.LocalClashConfig = "localclash.yaml"
	}
	beforeNodes, _ := localconfig.LoadSubscriptionNodes(localconfig.SubscriptionNodeOptions{
		SubscriptionPath:    in.Merged,
		SubscriptionConfig:  in.Config,
		SubscriptionRuntime: in.RuntimeDir,
	})
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, err := subscriptions.Refresh(ctx, subscriptions.RefreshOptions{
		ConfigPath: in.Config,
		IDs:        in.IDs,
		RuntimeDir: in.RuntimeDir,
		MergedPath: in.Merged,
		Force:      in.Force,
		UserAgent:  in.UserAgent,
	})
	if err != nil {
		return toolResult{}, err
	}
	afterNodes, _ := localconfig.LoadSubscriptionNodes(localconfig.SubscriptionNodeOptions{
		SubscriptionPath:    in.Merged,
		SubscriptionConfig:  in.Config,
		SubscriptionRuntime: in.RuntimeDir,
	})
	toolResultValue := subscriptionsRefreshToolResult{
		RefreshResult: result,
		NodeDiff:      buildNodeDiff(beforeNodes, afterNodes),
	}
	impact := s.evaluateLocalClashAfterRefresh(in.LocalClashConfig, in.Selection, in.Merged, in.Config, in.RuntimeDir, in.Policy, in.RulesCache, in.Output)
	if impact.Exists {
		toolResultValue.LocalClash = &impact
	}
	return jsonToolResult(toolResultValue)
}

type subscriptionsRefreshToolResult struct {
	subscriptions.RefreshResult
	NodeDiff   nodeDiff                 `json:"node_diff"`
	LocalClash *localClashRefreshImpact `json:"localclash_config,omitempty"`
}

type nodeDiff struct {
	BeforeCount  int      `json:"before_count"`
	AfterCount   int      `json:"after_count"`
	AddedCount   int      `json:"added_count"`
	RemovedCount int      `json:"removed_count"`
	KeptCount    int      `json:"kept_count"`
	Added        []string `json:"added,omitempty"`
	Removed      []string `json:"removed,omitempty"`
	Truncated    bool     `json:"truncated,omitempty"`
}

type localClashRefreshImpact struct {
	Exists              bool                         `json:"exists"`
	State               string                       `json:"state,omitempty"`
	ConfigPath          string                       `json:"config_path"`
	Valid               bool                         `json:"valid"`
	AppliedAuto         bool                         `json:"applied_auto"`
	RequiresAgentReplan bool                         `json:"requires_agent_replan"`
	Error               string                       `json:"error,omitempty"`
	MissingNodes        []string                     `json:"missing_nodes,omitempty"`
	GeneratedConfig     string                       `json:"generated_config,omitempty"`
	SelectionPath       string                       `json:"selection_path,omitempty"`
	ProxyGroups         []localClashProxyGroupImpact `json:"proxy_groups,omitempty"`
	NextActions         []string                     `json:"next_actions,omitempty"`
}

type localClashProxyGroupImpact struct {
	ID            string   `json:"id"`
	PreviousNodes []string `json:"previous_nodes,omitempty"`
	SelectedNodes []string `json:"selected_nodes,omitempty"`
	AddedNodes    []string `json:"added_nodes,omitempty"`
	RemovedNodes  []string `json:"removed_nodes,omitempty"`
}

func buildNodeDiff(before, after []localconfig.SubscriptionNode) nodeDiff {
	beforeSet := map[string]bool{}
	afterSet := map[string]bool{}
	for _, node := range before {
		beforeSet[node.Name] = true
	}
	for _, node := range after {
		afterSet[node.Name] = true
	}
	var added, removed []string
	kept := 0
	for name := range afterSet {
		if beforeSet[name] {
			kept++
		} else {
			added = append(added, name)
		}
	}
	for name := range beforeSet {
		if !afterSet[name] {
			removed = append(removed, name)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	diff := nodeDiff{
		BeforeCount:  len(beforeSet),
		AfterCount:   len(afterSet),
		AddedCount:   len(added),
		RemovedCount: len(removed),
		KeptCount:    kept,
	}
	const maxReturned = 100
	if len(added) > maxReturned || len(removed) > maxReturned {
		diff.Truncated = true
	}
	diff.Added = limitStrings(added, maxReturned)
	diff.Removed = limitStrings(removed, maxReturned)
	return diff
}

func (s *Server) evaluateLocalClashAfterRefresh(configPath, selectionPath, subscriptionPath, subscriptionConfig, subscriptionRuntime, policyPath, rulesCache, outputPath string) localClashRefreshImpact {
	impact := localClashRefreshImpact{ConfigPath: configPath, GeneratedConfig: outputPath, SelectionPath: selectionPath}
	config, err := localconfig.Load(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return impact
		}
		impact.Exists = true
		impact.RequiresAgentReplan = true
		impact.Error = err.Error()
		return impact
	}
	impact.Exists = true
	resolved, err := localconfig.Resolve(localconfig.ResolveOptions{
		Config:              config,
		SubscriptionPath:    subscriptionPath,
		SubscriptionConfig:  subscriptionConfig,
		SubscriptionRuntime: subscriptionRuntime,
		RulesCache:          rulesCache,
	})
	if err != nil {
		var missingNodes *localconfig.MissingNodesError
		if errors.As(err, &missingNodes) {
			impact.State = "stale_exact_nodes"
			impact.Error = err.Error()
			impact.MissingNodes = append([]string{}, missingNodes.Nodes...)
			impact.NextActions = []string{"ask the user to choose replacement nodes or switch this group to a match selector", "call proxy_group_build", "call config_draft_render", "call config_draft_apply after review"}
			return impact
		}
		impact.State = "requires_agent_replan"
		impact.RequiresAgentReplan = true
		impact.Error = err.Error()
		impact.NextActions = []string{"read localclash.yaml", "search replacement subscription nodes", "call proxy_group_build", "call config_draft_render", "call config_draft_apply after review"}
		return impact
	}
	impact.State = "auto_applied"
	impact.Valid = true
	impact.ProxyGroups = proxyGroupImpacts(config, resolved.Config)
	tempDir, err := os.MkdirTemp("", "localclash-refresh-render-*")
	if err != nil {
		impact.RequiresAgentReplan = true
		impact.Error = err.Error()
		return impact
	}
	defer os.RemoveAll(tempDir)
	tempSelection := filepath.Join(tempDir, "localclash-packs.yaml")
	if err := localconfig.WriteSelection(tempSelection, resolved.Selection); err != nil {
		impact.RequiresAgentReplan = true
		impact.Error = err.Error()
		return impact
	}
	_, err = configrender.Render(configrender.Options{
		SourcePath:         subscriptionPath,
		PolicyPath:         policyPath,
		OutputPath:         outputPath,
		PacksSelectionPath: tempSelection,
		RulesCacheDir:      rulesCache,
		Force:              true,
	})
	if err != nil {
		impact.RequiresAgentReplan = true
		impact.Error = err.Error()
		return impact
	}
	if err := localconfig.Write(configPath, resolved.Config); err != nil {
		impact.RequiresAgentReplan = true
		impact.Error = err.Error()
		return impact
	}
	if err := localconfig.WriteSelection(selectionPath, resolved.Selection); err != nil {
		impact.RequiresAgentReplan = true
		impact.Error = err.Error()
		return impact
	}
	impact.AppliedAuto = true
	return impact
}

func proxyGroupImpacts(before, after localconfig.Config) []localClashProxyGroupImpact {
	var ids []string
	for id := range after.ProxyGroups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	impacts := make([]localClashProxyGroupImpact, 0, len(ids))
	for _, id := range ids {
		previous := before.ProxyGroups[id].SelectedNodes
		selected := after.ProxyGroups[id].SelectedNodes
		added, removed := stringSetDiff(previous, selected)
		impacts = append(impacts, localClashProxyGroupImpact{
			ID:            id,
			PreviousNodes: append([]string{}, previous...),
			SelectedNodes: append([]string{}, selected...),
			AddedNodes:    added,
			RemovedNodes:  removed,
		})
	}
	return impacts
}

func stringSetDiff(before, after []string) ([]string, []string) {
	beforeSet := map[string]bool{}
	afterSet := map[string]bool{}
	for _, value := range before {
		beforeSet[value] = true
	}
	for _, value := range after {
		afterSet[value] = true
	}
	var added, removed []string
	for value := range afterSet {
		if !beforeSet[value] {
			added = append(added, value)
		}
	}
	for value := range beforeSet {
		if !afterSet[value] {
			removed = append(removed, value)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

func limitStrings(values []string, limit int) []string {
	if len(values) > limit {
		values = values[:limit]
	}
	return append([]string{}, values...)
}

func (s *Server) callDoctor(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var in struct {
		Core         string `json:"core"`
		CorePath     string `json:"core_path"`
		Subscription string `json:"subscription"`
		Config       string `json:"config"`
		ConfigPath   string `json:"config_path"`
		Policy       string `json:"policy"`
		PolicyPath   string `json:"policy_path"`
		Dashboard    string `json:"dashboard"`
		DashboardDir string `json:"dashboard_dir"`
		WorkDir      string `json:"workdir"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	opts := doctor.Options{
		CorePath:         firstNonEmpty(in.CorePath, in.Core),
		SubscriptionPath: in.Subscription,
		ConfigPath:       firstNonEmpty(in.ConfigPath, in.Config),
		PolicyPath:       firstNonEmpty(in.PolicyPath, in.Policy),
		DashboardDir:     firstNonEmpty(in.DashboardDir, in.Dashboard),
		WorkDir:          in.WorkDir,
	}
	if s.state != nil {
		if opts.CorePath == "" {
			opts.CorePath = s.state.Paths.CorePath
		}
		if opts.SubscriptionPath == "" {
			opts.SubscriptionPath = s.state.Paths.SubscriptionPath
		}
		if opts.ConfigPath == "" {
			opts.ConfigPath = s.state.Paths.GeneratedConfig
		}
		if opts.PolicyPath == "" {
			opts.PolicyPath = s.state.Paths.PolicyPath
		}
		if opts.DashboardDir == "" {
			opts.DashboardDir = filepath.Join(s.state.Paths.MihomoRuntimeDir, "ui", "zashboard")
		}
		if opts.WorkDir == "" {
			opts.WorkDir = s.state.Paths.MihomoRuntimeDir
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	report, err := doctor.Run(ctx, opts)
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(report)
}

func (s *Server) callEnvironmentInspect(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var in struct {
		OpenClashReferenceRoot string `json:"openclash_reference_root"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	opts := envinspect.Options{OpenClashReferenceRoot: in.OpenClashReferenceRoot}
	if s.state != nil {
		opts.Paths = s.state.Paths
	}
	result, err := envinspect.Inspect(ctx, opts)
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callConfigRender(args json.RawMessage) (toolResult, error) {
	var in struct {
		Source         string `json:"source"`
		Policy         string `json:"policy"`
		Mode           string `json:"mode"`
		Output         string `json:"output"`
		Force          bool   `json:"force"`
		PacksSelection string `json:"packs_selection"`
		RulesCache     string `json:"rules_cache"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	opts := configrender.Options{
		SourcePath:         in.Source,
		PolicyPath:         in.Policy,
		Mode:               in.Mode,
		OutputPath:         in.Output,
		Force:              in.Force,
		PacksSelectionPath: in.PacksSelection,
		RulesCacheDir:      in.RulesCache,
	}
	if s.state != nil {
		if opts.SourcePath == "" {
			opts.SourcePath = s.state.Paths.SubscriptionPath
		}
		if opts.PolicyPath == "" {
			opts.PolicyPath = s.state.Paths.PolicyPath
		}
		if opts.OutputPath == "" {
			opts.OutputPath = s.state.Paths.GeneratedConfig
		}
		if opts.RulesCacheDir == "" {
			opts.RulesCacheDir = s.state.Paths.RulesCacheDir
		}
		if opts.OutputPath == s.state.Paths.GeneratedConfig && s.state.Config.Rendered {
			opts.Force = true
		}
	}
	result, err := configrender.Render(opts)
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callRunRuntime(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var in struct {
		Config     string `json:"config"`
		RuntimeDir string `json:"runtime_dir"`
		Core       string `json:"core"`
		Foreground bool   `json:"foreground"`
		LogFile    string `json:"log_file"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil {
		if in.Config == "" {
			in.Config = s.state.Paths.GeneratedConfig
		}
		if in.RuntimeDir == "" {
			in.RuntimeDir = s.state.Paths.MihomoRuntimeDir
		}
		if in.Core == "" {
			in.Core = s.state.Paths.CorePath
		}
		if in.Config == s.state.Paths.GeneratedConfig && !s.state.Config.Available {
			return jsonToolResult(map[string]any{
				"started": false,
				"error":   "generated config is unavailable: " + s.state.Config.Diagnostic,
				"warnings": []string{
					"Starting or restarting the proxy runtime may temporarily interrupt network connectivity.",
					"The Agent itself may depend on the current network/proxy path and could be disconnected after this operation.",
				},
			})
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, err := corerun.Start(ctx, corerun.StartOptions{
		CorePath:   in.Core,
		ConfigPath: in.Config,
		WorkDir:    in.RuntimeDir,
		LogPath:    in.LogFile,
		Foreground: in.Foreground,
	})
	if err != nil {
		return jsonToolResult(map[string]any{
			"started": false,
			"error":   err.Error(),
			"warnings": []string{
				"Starting or restarting the proxy runtime may temporarily interrupt network connectivity.",
				"The Agent itself may depend on the current network/proxy path and could be disconnected after this operation.",
			},
		})
	}
	return jsonToolResult(result)
}

func (s *Server) callRuntimeStatus(args json.RawMessage) (toolResult, error) {
	var in struct {
		Config     string `json:"config"`
		RuntimeDir string `json:"runtime_dir"`
		LogFile    string `json:"log_file"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil {
		if in.Config == "" {
			in.Config = s.state.Paths.GeneratedConfig
		}
		if in.RuntimeDir == "" {
			in.RuntimeDir = s.state.Paths.MihomoRuntimeDir
		}
	}
	return jsonToolResult(corerun.Status(corerun.StatusOptions{
		ConfigPath: in.Config,
		WorkDir:    in.RuntimeDir,
		LogPath:    in.LogFile,
	}))
}

func (s *Server) callStopRuntime(args json.RawMessage) (toolResult, error) {
	var in struct {
		RuntimeDir string `json:"runtime_dir"`
		TimeoutMS  int    `json:"timeout_ms"`
		Force      bool   `json:"force"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil && in.RuntimeDir == "" {
		in.RuntimeDir = s.state.Paths.MihomoRuntimeDir
	}
	result, err := corerun.Stop(corerun.StopOptions{
		WorkDir:   in.RuntimeDir,
		Timeout:   time.Duration(in.TimeoutMS) * time.Millisecond,
		ForceKill: in.Force,
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func resultResponse(id json.RawMessage, result any) *rpcResponse {
	return &rpcResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func errorResponse(id json.RawMessage, code int, message string) *rpcResponse {
	return &rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}
}

func jsonToolResult(value any) (toolResult, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return toolResult{}, err
	}
	return toolResult{
		Content:           []toolContent{{Type: "text", Text: string(data)}},
		StructuredContent: value,
	}, nil
}
