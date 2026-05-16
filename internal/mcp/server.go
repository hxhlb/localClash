package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"localclash/internal/configinspect"
	"localclash/internal/configplan"
	"localclash/internal/configrender"
	"localclash/internal/corerun"
	"localclash/internal/doctor"
	"localclash/internal/rules"
	"localclash/internal/subscriptions"
)

type Server struct{}

func NewServer() *Server {
	return &Server{}
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

type messageFormat int

const (
	messageFormatFramed messageFormat = iota
	messageFormatJSONLine
)

func ServeStdio(ctx context.Context, in io.Reader, out io.Writer) error {
	return NewServer().Serve(ctx, in, out)
}

func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		data, format, err := readMessage(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		response := s.Handle(ctx, data)
		if response == nil {
			continue
		}
		if err := writeMessage(out, response, format); err != nil {
			return err
		}
	}
}

func readMessage(reader *bufio.Reader) ([]byte, messageFormat, error) {
	for {
		next, err := reader.Peek(1)
		if err != nil {
			return nil, messageFormatFramed, err
		}
		if next[0] != '\r' && next[0] != '\n' && next[0] != ' ' && next[0] != '\t' {
			break
		}
		if _, err := reader.ReadByte(); err != nil {
			return nil, messageFormatFramed, err
		}
	}

	next, err := reader.Peek(1)
	if err != nil {
		return nil, messageFormatFramed, err
	}
	if next[0] == '{' {
		line, err := reader.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, messageFormatJSONLine, err
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 && err != nil {
			return nil, messageFormatJSONLine, err
		}
		return line, messageFormatJSONLine, nil
	}

	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, messageFormatFramed, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, messageFormatFramed, fmt.Errorf("invalid MCP header %q", line)
		}
		if strings.EqualFold(strings.TrimSpace(key), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n < 0 {
				return nil, messageFormatFramed, fmt.Errorf("invalid Content-Length %q", strings.TrimSpace(value))
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, messageFormatFramed, errors.New("missing Content-Length header")
	}
	data := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, data); err != nil {
		return nil, messageFormatFramed, err
	}
	return data, messageFormatFramed, nil
}

func writeMessage(out io.Writer, value any, format messageFormat) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if format == messageFormatJSONLine {
		_, err = fmt.Fprintf(out, "%s\n", data)
		return err
	}
	_, err = fmt.Fprintf(out, "Content-Length: %d\r\n\r\n%s\r\n", len(data), data)
	return err
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
		return callDoctor(ctx, args)
	case "packs_list":
		return callPacksList(args)
	case "packs_get":
		return callPacksGet(args)
	case "subscriptions_status":
		return callSubscriptionsStatus(args)
	case "subscriptions_configure":
		return callSubscriptionsConfigure(args)
	case "subscriptions_refresh":
		return callSubscriptionsRefresh(ctx, args)
	case "virtual_nodes_list":
		return callVirtualNodesList(args)
	case "virtual_nodes_get":
		return callVirtualNodesGet(args)
	case "config_plan_render":
		return callConfigPlanRender(ctx, args)
	case "config_render":
		return callConfigRender(args)
	case "run_runtime":
		return callRunRuntime(ctx, args)
	default:
		return toolResult{}, fmt.Errorf("unknown tool %q", call.Name)
	}
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

func callConfigPlanRender(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var in struct {
		PlanName     string                   `json:"plan_name"`
		Subscription string                   `json:"subscription"`
		Policy       string                   `json:"policy"`
		Mode         string                   `json:"mode"`
		RulesCache   string                   `json:"rules_cache"`
		OutputDir    string                   `json:"output_dir"`
		Test         *bool                    `json:"test"`
		Overlay      configplan.OverlayIntent `json:"overlay"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	test := true
	if in.Test != nil {
		test = *in.Test
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, err := configplan.Render(ctx, configplan.Options{
		PlanName:     in.PlanName,
		Subscription: in.Subscription,
		Policy:       in.Policy,
		Mode:         in.Mode,
		RulesCache:   in.RulesCache,
		OutputDir:    in.OutputDir,
		Test:         test,
		Overlay:      in.Overlay,
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func callPacksList(args json.RawMessage) (toolResult, error) {
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

func callPacksGet(args json.RawMessage) (toolResult, error) {
	var in struct {
		ID    string `json:"id"`
		Cache string `json:"cache"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	result, err := rules.GetPack(rules.PackGetOptions{CacheDir: in.Cache, ID: in.ID})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func callSubscriptionsStatus(args json.RawMessage) (toolResult, error) {
	var in struct {
		Config     string `json:"config"`
		Merged     string `json:"merged"`
		RuntimeDir string `json:"runtime_dir"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
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

func callSubscriptionsConfigure(args json.RawMessage) (toolResult, error) {
	var in struct {
		Config  string                 `json:"config"`
		Sources []subscriptions.Source `json:"sources"`
		Replace *bool                  `json:"replace"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
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

func callSubscriptionsRefresh(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var in struct {
		Config     string   `json:"config"`
		IDs        []string `json:"ids"`
		RuntimeDir string   `json:"runtime_dir"`
		Merged     string   `json:"merged"`
		Force      bool     `json:"force"`
		UserAgent  string   `json:"user_agent"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
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
	return jsonToolResult(result)
}

func callVirtualNodesList(args json.RawMessage) (toolResult, error) {
	var in struct {
		Subscription string `json:"subscription"`
		Selection    string `json:"selection"`
		IncludeEmpty bool   `json:"include_empty"`
		SampleLimit  int    `json:"sample_limit"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	result, err := rules.ListVirtualNodes(rules.VirtualNodesListOptions{
		Subscription: in.Subscription,
		Selection:    in.Selection,
		IncludeEmpty: in.IncludeEmpty,
		SampleLimit:  in.SampleLimit,
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func callVirtualNodesGet(args json.RawMessage) (toolResult, error) {
	var in struct {
		ID           string `json:"id"`
		Subscription string `json:"subscription"`
		Selection    string `json:"selection"`
		Limit        int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	result, err := rules.GetVirtualNode(rules.VirtualNodesGetOptions{
		ID:           in.ID,
		Subscription: in.Subscription,
		Selection:    in.Selection,
		Limit:        in.Limit,
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func callDoctor(ctx context.Context, args json.RawMessage) (toolResult, error) {
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
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	report, err := doctor.Run(ctx, opts)
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(report)
}

func callConfigRender(args json.RawMessage) (toolResult, error) {
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
	result, err := configrender.Render(opts)
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func callRunRuntime(ctx context.Context, args json.RawMessage) (toolResult, error) {
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
