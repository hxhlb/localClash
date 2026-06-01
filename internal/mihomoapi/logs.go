package mihomoapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	TransportWebSocket  = "websocket"
	TransportHTTPStream = "http_stream"
)

type LogsOptions struct {
	Level     string        `json:"level"`
	Format    string        `json:"format"`
	Transport string        `json:"transport"`
	Duration  time.Duration `json:"-"`
	MaxLines  int           `json:"max_lines"`
	MaxBytes  int           `json:"max_bytes"`
}

type LogLine struct {
	JSON       any    `json:"json,omitempty"`
	Raw        string `json:"raw,omitempty"`
	ParseError string `json:"parse_error,omitempty"`
}

type LogsResult struct {
	Transport string    `json:"transport"`
	Level     string    `json:"level"`
	Format    string    `json:"format"`
	LineCount int       `json:"line_count"`
	ByteCount int       `json:"byte_count"`
	Truncated bool      `json:"truncated,omitempty"`
	ElapsedMS int64     `json:"elapsed_ms"`
	Lines     []LogLine `json:"lines"`
}

func (c *Client) Logs(ctx context.Context, opts LogsOptions) (LogsResult, error) {
	if c == nil {
		return LogsResult{}, fmt.Errorf("mihomo api client is nil")
	}
	opts = normalizeLogsOptions(opts)
	if err := validateLogLevel(opts.Level); err != nil {
		return LogsResult{}, err
	}
	if opts.Format != "default" && opts.Format != "structured" {
		return LogsResult{}, fmt.Errorf("mihomo logs format %q is not supported", opts.Format)
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, opts.Duration)
	defer cancel()
	switch opts.Transport {
	case TransportWebSocket:
		return c.readWebSocketLogs(ctx, opts, started)
	case TransportHTTPStream:
		return c.readHTTPStreamLogs(ctx, opts, started)
	default:
		return LogsResult{}, fmt.Errorf("mihomo logs transport %q is not supported", opts.Transport)
	}
}

func normalizeLogsOptions(opts LogsOptions) LogsOptions {
	opts.Level = strings.ToLower(strings.TrimSpace(opts.Level))
	if opts.Level == "" {
		opts.Level = "info"
	}
	opts.Format = strings.ToLower(strings.TrimSpace(opts.Format))
	if opts.Format == "" {
		opts.Format = "default"
	}
	opts.Transport = strings.ToLower(strings.TrimSpace(opts.Transport))
	if opts.Transport == "" {
		opts.Transport = TransportWebSocket
	}
	if opts.Duration <= 0 {
		opts.Duration = 3 * time.Second
	}
	if opts.MaxLines <= 0 {
		opts.MaxLines = 200
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = 128 * 1024
	}
	return opts
}

func validateLogLevel(level string) error {
	switch level {
	case "debug", "info", "warning", "warn", "error", "silent":
		return nil
	default:
		return fmt.Errorf("mihomo logs level %q is not supported", level)
	}
}

func (c *Client) readHTTPStreamLogs(ctx context.Context, opts LogsOptions, started time.Time) (LogsResult, error) {
	u := url.URL{Scheme: "http", Host: c.Controller, Path: "/logs"}
	q := u.Query()
	q.Set("level", opts.Level)
	if opts.Format == "structured" {
		q.Set("format", "structured")
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return LogsResult{}, err
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
		if ctx.Err() != nil {
			return LogsResult{Transport: TransportHTTPStream, Level: opts.Level, Format: opts.Format, ElapsedMS: time.Since(started).Milliseconds()}, nil
		}
		return LogsResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return LogsResult{}, fmt.Errorf("mihomo logs stream failed with status %d", resp.StatusCode)
	}
	return collectLogLines(ctx, resp.Body, opts, started, TransportHTTPStream)
}

func (c *Client) readWebSocketLogs(ctx context.Context, opts LogsOptions, started time.Time) (LogsResult, error) {
	host := c.Controller
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", host)
	if err != nil {
		return LogsResult{}, err
	}
	defer conn.Close()
	key, err := randomWebSocketKey()
	if err != nil {
		return LogsResult{}, err
	}
	path := "/logs"
	q := url.Values{}
	q.Set("level", opts.Level)
	if opts.Format == "structured" {
		q.Set("format", "structured")
	}
	if c.Secret != "" {
		q.Set("token", c.Secret)
	}
	path += "?" + q.Encode()
	request := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n\r\n"
	if _, err := io.WriteString(conn, request); err != nil {
		return LogsResult{}, err
	}
	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		return LogsResult{}, err
	}
	status, err := parseHTTPStatus(strings.TrimSpace(statusLine))
	if err != nil {
		return LogsResult{}, err
	}
	headers := http.Header{}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return LogsResult{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if ok {
			headers.Add(strings.TrimSpace(key), strings.TrimSpace(value))
		}
	}
	if status != http.StatusSwitchingProtocols {
		return LogsResult{}, fmt.Errorf("mihomo logs websocket handshake failed with status %d", status)
	}
	if got := headers.Get("Sec-WebSocket-Accept"); got != webSocketAccept(key) {
		return LogsResult{}, fmt.Errorf("mihomo logs websocket handshake returned invalid accept header")
	}
	result := LogsResult{Transport: TransportWebSocket, Level: opts.Level, Format: opts.Format}
	for {
		if deadline, ok := ctx.Deadline(); ok {
			_ = conn.SetReadDeadline(deadline)
		}
		opcode, payload, err := readWebSocketFrame(reader)
		if err != nil {
			if ctx.Err() != nil || isTimeoutError(err) {
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
		appendLogPayload(&result, payload, opts)
		if result.Truncated {
			break
		}
	}
	result.ElapsedMS = time.Since(started).Milliseconds()
	return result, nil
}

func isTimeoutError(err error) bool {
	netErr, ok := err.(net.Error)
	return ok && netErr.Timeout()
}

func collectLogLines(ctx context.Context, r io.Reader, opts LogsOptions, started time.Time, transport string) (LogsResult, error) {
	result := LogsResult{Transport: transport, Level: opts.Level, Format: opts.Format}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 4096), opts.MaxBytes)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			result.ElapsedMS = time.Since(started).Milliseconds()
			return result, nil
		default:
		}
		appendLogPayload(&result, append(scanner.Bytes(), '\n'), opts)
		if result.Truncated {
			break
		}
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		return result, err
	}
	result.ElapsedMS = time.Since(started).Milliseconds()
	return result, nil
}

func appendLogPayload(result *LogsResult, payload []byte, opts LogsOptions) {
	for _, raw := range strings.Split(string(payload), "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if result.LineCount >= opts.MaxLines || result.ByteCount+len(raw) > opts.MaxBytes {
			result.Truncated = true
			return
		}
		line := LogLine{}
		var decoded any
		if err := json.Unmarshal([]byte(raw), &decoded); err == nil {
			line.JSON = decoded
		} else {
			line.Raw = raw
			line.ParseError = "line is not valid JSON"
		}
		result.Lines = append(result.Lines, line)
		result.LineCount++
		result.ByteCount += len(raw)
	}
}
