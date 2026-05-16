package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"localclash/internal/configrender"
	"localclash/internal/doctor"
	"localclash/internal/rules"

	"gopkg.in/yaml.v3"
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

		data, err := readMessage(reader)
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
		if err := writeMessage(out, response); err != nil {
			return err
		}
	}
}

func readMessage(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("invalid MCP header %q", line)
		}
		if strings.EqualFold(strings.TrimSpace(key), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n < 0 {
				return nil, fmt.Errorf("invalid Content-Length %q", strings.TrimSpace(value))
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, errors.New("missing Content-Length header")
	}
	data := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, data); err != nil {
		return nil, err
	}
	return data, nil
}

func writeMessage(out io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Content-Length: %d\r\n\r\n%s", len(data), data)
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
		return resultResponse(req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
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
	case "doctor":
		return callDoctor(ctx, args)
	case "rules_adapt":
		return callRulesAdapt(ctx, args)
	case "rules_render":
		return callRulesRender(args)
	case "inspect_generated_config":
		return callInspectGeneratedConfig(args)
	case "config_render":
		return callConfigRender(args)
	case "config_test":
		return callConfigTest(ctx, args)
	case "run_runtime", "switch_proxy_group", "apply_router_config":
		return errorToolResult("not implemented; requires explicit confirmation flow"), nil
	default:
		return toolResult{}, fmt.Errorf("unknown tool %q", call.Name)
	}
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

func callRulesAdapt(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var in struct {
		Sources string `json:"sources"`
		Cache   string `json:"cache"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	opts := rules.Options{SourcesDir: in.Sources, CacheDir: in.Cache}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	caches, err := rules.Adapt(ctx, opts)
	if err != nil {
		return toolResult{}, err
	}
	type adaptedSource struct {
		Source  string `json:"source"`
		Adapter string `json:"adapter"`
		Packs   int    `json:"packs"`
	}
	out := struct {
		Sources []adaptedSource `json:"sources"`
	}{}
	for _, cache := range caches {
		out.Sources = append(out.Sources, adaptedSource{Source: cache.Source, Adapter: cache.Adapter, Packs: len(cache.Packs)})
	}
	return jsonToolResult(out)
}

func callRulesRender(args json.RawMessage) (toolResult, error) {
	var in struct {
		Selection    string `json:"selection"`
		Cache        string `json:"cache"`
		Subscription string `json:"subscription"`
		Output       string `json:"output"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return toolResult{}, err
	}
	opts := rules.Options{SelectionPath: in.Selection, CacheDir: in.Cache, Subscription: in.Subscription, OutputPath: in.Output}
	fragment, err := rules.Render(opts)
	if err != nil {
		return toolResult{}, err
	}
	if opts.OutputPath != "" && opts.OutputPath != "-" {
		if err := rules.WriteFragment(opts.OutputPath, fragment); err != nil {
			return toolResult{}, err
		}
		summary := map[string]any{
			"output":         opts.OutputPath,
			"rule_providers": len(fragment.RuleProviders),
			"proxy_groups":   len(fragment.ProxyGroups),
			"rules":          len(fragment.Rules),
		}
		return jsonToolResult(summary)
	}
	return jsonToolResult(fragment)
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

func callInspectGeneratedConfig(args json.RawMessage) (toolResult, error) {
	var opts struct {
		Config     string `json:"config"`
		ConfigPath string `json:"config_path"`
	}
	if err := json.Unmarshal(args, &opts); err != nil {
		return toolResult{}, err
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = opts.Config
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = "generated/mihomo.yaml"
	}
	config, err := readYAMLMap(opts.ConfigPath)
	if err != nil {
		return toolResult{}, err
	}
	groupNames := []string{}
	for _, raw := range anySlice(config["proxy-groups"]) {
		if group, ok := raw.(map[string]any); ok {
			if name, ok := group["name"].(string); ok {
				groupNames = append(groupNames, name)
			}
		}
	}
	out := map[string]any{
		"config_path":          opts.ConfigPath,
		"proxies":              len(anySlice(config["proxies"])),
		"proxy_groups":         groupNames,
		"rule_providers_count": len(anyMap(config["rule-providers"])),
		"rules_count":          len(anySlice(config["rules"])),
	}
	return jsonToolResult(out)
}

func callConfigTest(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var opts struct {
		Core       string `json:"core"`
		CorePath   string `json:"core_path"`
		Config     string `json:"config"`
		ConfigPath string `json:"config_path"`
		WorkDir    string `json:"workdir"`
	}
	if err := json.Unmarshal(args, &opts); err != nil {
		return toolResult{}, err
	}
	if opts.CorePath == "" {
		opts.CorePath = opts.Core
	}
	if opts.CorePath == "" {
		opts.CorePath = "bin/mihomo"
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = opts.Config
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = "generated/mihomo.yaml"
	}
	if opts.WorkDir == "" {
		opts.WorkDir = ".runtime/mihomo"
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, opts.CorePath, "-d", opts.WorkDir, "-f", opts.ConfigPath, "-t")
	output, err := cmd.CombinedOutput()
	out := map[string]any{
		"pass":   err == nil,
		"output": compactOutput(output, err),
	}
	return jsonToolResult(out)
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

func errorToolResult(message string) toolResult {
	return toolResult{
		Content: []toolContent{{Type: "text", Text: message}},
		IsError: true,
	}
}

func readYAMLMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func anySlice(value any) []any {
	if values, ok := value.([]any); ok {
		return values
	}
	return nil
}

func anyMap(value any) map[string]any {
	if values, ok := value.(map[string]any); ok {
		return values
	}
	return nil
}

func compactOutput(output []byte, err error) string {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		if err != nil {
			return err.Error()
		}
		return ""
	}
	const maxLines = 8
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n")
}
