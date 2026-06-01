package mihomoapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	ConnectionsModeSnapshot = "snapshot"
	ConnectionsModeStream   = "stream"
)

type ConnectionsOptions struct {
	Mode           string        `json:"mode"`
	Interval       time.Duration `json:"-"`
	Duration       time.Duration `json:"-"`
	MaxFrames      int           `json:"max_frames"`
	MaxConnections int           `json:"max_connections"`
	MaxBytes       int           `json:"max_bytes"`
	IncludeRaw     bool          `json:"include_raw"`
}

type ConnectionsResult struct {
	Mode       string             `json:"mode"`
	FrameCount int                `json:"frame_count"`
	ByteCount  int                `json:"byte_count"`
	Truncated  bool               `json:"truncated,omitempty"`
	ElapsedMS  int64              `json:"elapsed_ms"`
	Latest     *ConnectionSummary `json:"latest,omitempty"`
	Frames     []ConnectionFrame  `json:"frames,omitempty"`
}

type ConnectionFrame struct {
	Index       int               `json:"index"`
	ReceivedAt  string            `json:"received_at"`
	ByteCount   int               `json:"byte_count"`
	Truncated   bool              `json:"truncated,omitempty"`
	Summary     ConnectionSummary `json:"summary"`
	RawSnapshot any               `json:"raw_snapshot,omitempty"`
}

type ConnectionSummary struct {
	DownloadTotal   int64                          `json:"download_total"`
	UploadTotal     int64                          `json:"upload_total"`
	Memory          int64                          `json:"memory"`
	ConnectionCount int                            `json:"connection_count"`
	Truncated       bool                           `json:"truncated,omitempty"`
	Connections     []ConnectionInfo               `json:"connections"`
	ByProxy         map[string]ConnectionAggregate `json:"by_proxy,omitempty"`
	ByRule          map[string]ConnectionAggregate `json:"by_rule,omitempty"`
}

type ConnectionInfo struct {
	ID                string   `json:"id"`
	Network           string   `json:"network,omitempty"`
	Type              string   `json:"type,omitempty"`
	Source            string   `json:"source,omitempty"`
	Destination       string   `json:"destination,omitempty"`
	Host              string   `json:"host,omitempty"`
	SniffHost         string   `json:"sniff_host,omitempty"`
	Process           string   `json:"process,omitempty"`
	ProcessPath       string   `json:"process_path,omitempty"`
	InboundName       string   `json:"inbound_name,omitempty"`
	Rule              string   `json:"rule,omitempty"`
	RulePayload       string   `json:"rule_payload,omitempty"`
	SelectedProxy     string   `json:"selected_proxy,omitempty"`
	Chains            []string `json:"chains,omitempty"`
	ProviderChains    []string `json:"provider_chains,omitempty"`
	Upload            int64    `json:"upload"`
	Download          int64    `json:"download"`
	Start             string   `json:"start,omitempty"`
	AgeMS             int64    `json:"age_ms,omitempty"`
	RemoteDestination string   `json:"remote_destination,omitempty"`
}

type ConnectionAggregate struct {
	Connections int   `json:"connections"`
	Upload      int64 `json:"upload,omitempty"`
	Download    int64 `json:"download,omitempty"`
}

func (c *Client) Connections(ctx context.Context, opts ConnectionsOptions) (ConnectionsResult, error) {
	if c == nil {
		return ConnectionsResult{}, fmt.Errorf("mihomo api client is nil")
	}
	opts = normalizeConnectionsOptions(opts)
	started := time.Now()
	switch opts.Mode {
	case ConnectionsModeSnapshot:
		return c.connectionSnapshot(ctx, opts, started)
	case ConnectionsModeStream:
		return c.connectionStream(ctx, opts, started)
	default:
		return ConnectionsResult{}, fmt.Errorf("mihomo connections mode %q is not supported", opts.Mode)
	}
}

func normalizeConnectionsOptions(opts ConnectionsOptions) ConnectionsOptions {
	opts.Mode = strings.ToLower(strings.TrimSpace(opts.Mode))
	if opts.Mode == "" {
		opts.Mode = ConnectionsModeSnapshot
	}
	if opts.Interval <= 0 {
		opts.Interval = time.Second
	}
	if opts.Duration <= 0 {
		opts.Duration = 3 * time.Second
	}
	if opts.MaxFrames <= 0 {
		opts.MaxFrames = 3
	}
	if opts.MaxConnections <= 0 {
		opts.MaxConnections = 200
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultMaxBytes
	}
	return opts
}

func (c *Client) connectionSnapshot(ctx context.Context, opts ConnectionsOptions, started time.Time) (ConnectionsResult, error) {
	reqCtx, cancel := context.WithTimeout(ctx, opts.Duration)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, c.requestURL("/connections/", nil), nil)
	if err != nil {
		return ConnectionsResult{}, err
	}
	if c.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.Secret)
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return ConnectionsResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ConnectionsResult{}, fmt.Errorf("mihomo connections snapshot failed with status %d", resp.StatusCode)
	}
	payload, truncated, err := readBounded(resp.Body, int64(opts.MaxBytes))
	if err != nil {
		return ConnectionsResult{}, err
	}
	if truncated {
		return ConnectionsResult{}, fmt.Errorf("mihomo connections snapshot exceeded max_bytes %d", opts.MaxBytes)
	}
	frame, err := decodeConnectionFrame(0, payload, opts, time.Now())
	if err != nil {
		return ConnectionsResult{}, err
	}
	result := ConnectionsResult{
		Mode:       ConnectionsModeSnapshot,
		FrameCount: 1,
		ByteCount:  len(payload),
		Truncated:  frame.Truncated,
		ElapsedMS:  time.Since(started).Milliseconds(),
		Latest:     &frame.Summary,
	}
	if opts.IncludeRaw {
		result.Frames = []ConnectionFrame{frame}
	}
	return result, nil
}

func (c *Client) connectionStream(ctx context.Context, opts ConnectionsOptions, started time.Time) (ConnectionsResult, error) {
	reqCtx, cancel := context.WithTimeout(ctx, opts.Duration)
	defer cancel()
	q := url.Values{}
	q.Set("interval", strconv.Itoa(int(opts.Interval/time.Millisecond)))
	conn, reader, err := c.openWebSocket(reqCtx, "/connections/", q, "mihomo connections")
	if err != nil {
		return ConnectionsResult{}, err
	}
	defer conn.Close()
	result := ConnectionsResult{Mode: ConnectionsModeStream}
	for result.FrameCount < opts.MaxFrames {
		if deadline, ok := reqCtx.Deadline(); ok {
			_ = conn.SetReadDeadline(deadline)
		}
		opcode, payload, err := readWebSocketFrame(reader)
		if err != nil {
			if reqCtx.Err() != nil || isTimeoutError(err) {
				break
			}
			return result, err
		}
		if opcode == 8 {
			break
		}
		if opcode != 1 {
			continue
		}
		if result.ByteCount+len(payload) > opts.MaxBytes {
			result.Truncated = true
			break
		}
		frame, err := decodeConnectionFrame(result.FrameCount, payload, opts, time.Now())
		if err != nil {
			return result, err
		}
		result.ByteCount += len(payload)
		result.FrameCount++
		if frame.Truncated {
			result.Truncated = true
		}
		result.Frames = append(result.Frames, frame)
		result.Latest = &result.Frames[len(result.Frames)-1].Summary
	}
	result.ElapsedMS = time.Since(started).Milliseconds()
	return result, nil
}

func decodeConnectionFrame(index int, payload []byte, opts ConnectionsOptions, receivedAt time.Time) (ConnectionFrame, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return ConnectionFrame{}, fmt.Errorf("mihomo connections snapshot JSON decode failed: %w", err)
	}
	summary, err := summarizeConnections(raw, opts.MaxConnections, receivedAt)
	if err != nil {
		return ConnectionFrame{}, err
	}
	frame := ConnectionFrame{
		Index:      index,
		ReceivedAt: receivedAt.Format(time.RFC3339Nano),
		ByteCount:  len(payload),
		Truncated:  summary.Truncated,
		Summary:    summary,
	}
	if opts.IncludeRaw {
		frame.RawSnapshot = raw
	}
	return frame, nil
}

func summarizeConnections(raw map[string]any, maxConnections int, now time.Time) (ConnectionSummary, error) {
	summary := ConnectionSummary{
		DownloadTotal: int64FromAny(raw["downloadTotal"]),
		UploadTotal:   int64FromAny(raw["uploadTotal"]),
		Memory:        int64FromAny(raw["memory"]),
		ByProxy:       map[string]ConnectionAggregate{},
		ByRule:        map[string]ConnectionAggregate{},
	}
	rawConnectionsValue, ok := raw["connections"]
	if !ok {
		return ConnectionSummary{}, fmt.Errorf("mihomo connections snapshot missing connections")
	}
	rawConnections, ok := rawConnectionsValue.([]any)
	if !ok {
		return ConnectionSummary{}, fmt.Errorf("mihomo connections snapshot connections must be an array")
	}
	summary.ConnectionCount = len(rawConnections)
	limit := len(rawConnections)
	if maxConnections > 0 && limit > maxConnections {
		limit = maxConnections
		summary.Truncated = true
	}
	summary.Connections = make([]ConnectionInfo, 0, limit)
	for _, rawConnection := range rawConnections[:limit] {
		connection, ok := rawConnection.(map[string]any)
		if !ok {
			return ConnectionSummary{}, fmt.Errorf("mihomo connections snapshot item must be an object")
		}
		info, err := summarizeConnection(connection, now)
		if err != nil {
			return ConnectionSummary{}, err
		}
		summary.Connections = append(summary.Connections, info)
		if info.SelectedProxy != "" {
			aggregate := summary.ByProxy[info.SelectedProxy]
			aggregate.Connections++
			aggregate.Upload += info.Upload
			aggregate.Download += info.Download
			summary.ByProxy[info.SelectedProxy] = aggregate
		}
		ruleKey := info.Rule
		if info.RulePayload != "" {
			ruleKey += "/" + info.RulePayload
		}
		if strings.TrimSpace(ruleKey) != "" {
			aggregate := summary.ByRule[ruleKey]
			aggregate.Connections++
			aggregate.Upload += info.Upload
			aggregate.Download += info.Download
			summary.ByRule[ruleKey] = aggregate
		}
	}
	if len(summary.ByProxy) == 0 {
		summary.ByProxy = nil
	}
	if len(summary.ByRule) == 0 {
		summary.ByRule = nil
	}
	return summary, nil
}

func summarizeConnection(raw map[string]any, now time.Time) (ConnectionInfo, error) {
	metadata, ok := raw["metadata"].(map[string]any)
	if !ok {
		return ConnectionInfo{}, fmt.Errorf("mihomo connections snapshot item missing metadata object")
	}
	chains, err := stringSliceFromAny(raw["chains"])
	if err != nil {
		return ConnectionInfo{}, fmt.Errorf("mihomo connections snapshot item chains invalid: %w", err)
	}
	providerChains, err := optionalStringSliceFromAny(raw["providerChains"])
	if err != nil {
		return ConnectionInfo{}, fmt.Errorf("mihomo connections snapshot item providerChains invalid: %w", err)
	}
	info := ConnectionInfo{
		ID:                stringFromAny(raw["id"]),
		Network:           stringFromAny(metadata["network"]),
		Type:              stringFromAny(metadata["type"]),
		Source:            endpointFromMetadata(metadata, "sourceIP", "sourcePort"),
		Destination:       endpointFromMetadata(metadata, "destinationIP", "destinationPort"),
		Host:              stringFromAny(metadata["host"]),
		SniffHost:         stringFromAny(metadata["sniffHost"]),
		Process:           stringFromAny(metadata["process"]),
		ProcessPath:       stringFromAny(metadata["processPath"]),
		InboundName:       stringFromAny(metadata["inboundName"]),
		Rule:              stringFromAny(raw["rule"]),
		RulePayload:       stringFromAny(raw["rulePayload"]),
		SelectedProxy:     firstString(chains),
		Chains:            chains,
		ProviderChains:    providerChains,
		Upload:            int64FromAny(raw["upload"]),
		Download:          int64FromAny(raw["download"]),
		Start:             stringFromAny(raw["start"]),
		RemoteDestination: stringFromAny(metadata["remoteDestination"]),
	}
	if info.Host != "" && int64FromAny(metadata["destinationPort"]) > 0 {
		info.Destination = net.JoinHostPort(info.Host, strconv.FormatInt(int64FromAny(metadata["destinationPort"]), 10))
	}
	if start, err := time.Parse(time.RFC3339Nano, info.Start); err == nil {
		info.AgeMS = now.Sub(start).Milliseconds()
	}
	return info, nil
}

func endpointFromMetadata(metadata map[string]any, ipKey, portKey string) string {
	host := strings.TrimSpace(stringFromAny(metadata[ipKey]))
	port := int64FromAny(metadata[portKey])
	if host == "" || host == "<nil>" || host == "::" || host == "0.0.0.0" {
		if port > 0 {
			return strconv.FormatInt(port, 10)
		}
		return ""
	}
	if port <= 0 {
		return host
	}
	return net.JoinHostPort(host, strconv.FormatInt(port, 10))
}

func stringFromAny(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

func int64FromAny(value any) int64 {
	switch v := value.(type) {
	case nil:
		return 0
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	default:
		return 0
	}
}

func optionalStringSliceFromAny(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	return stringSliceFromAny(value)
}

func stringSliceFromAny(value any) ([]string, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("must be an array")
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text := strings.TrimSpace(stringFromAny(item)); text != "" {
			result = append(result, text)
		}
	}
	return result, nil
}

func firstString(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return items[0]
}
