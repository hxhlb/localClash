package mihomoapi

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRequestUsesConfiguredControllerAndBearer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxies" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-secret" {
			t.Fatalf("authorization = %q", got)
		}
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer server.Close()
	config := writeMihomoAPIConfig(t, server.URL, "test-secret")
	client, err := NewFromConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Request(context.Background(), RequestOptions{Path: "/proxies"})
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != 200 || result.Bytes == 0 || result.JSON == nil {
		t.Fatalf("result = %+v", result)
	}
}

func TestRequestRejectsFullURLPath(t *testing.T) {
	client, err := New("127.0.0.1:9090", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Request(context.Background(), RequestOptions{Path: "http://example.com/proxies"})
	if err == nil || !strings.Contains(err.Error(), "full URL") {
		t.Fatalf("error = %v, want full URL rejection", err)
	}
}

func TestLogsHTTPStreamReadsBoundedLines(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/logs" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer log-secret" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"type":"info","payload":"first"}`)
		fmt.Fprintln(w, `{"type":"info","payload":"second"}`)
	}))
	defer server.Close()
	config := writeMihomoAPIConfig(t, server.URL, "log-secret")
	client, err := NewFromConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Logs(context.Background(), LogsOptions{
		Transport: TransportHTTPStream,
		Duration:  time.Second,
		MaxLines:  1,
		MaxBytes:  1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.LineCount != 1 || !result.Truncated {
		t.Fatalf("result = %+v, want one truncated line", result)
	}
}

func TestLogsWebSocketHandshakeFailureIsExplicit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	config := writeMihomoAPIConfig(t, server.URL, "")
	client, err := NewFromConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Logs(context.Background(), LogsOptions{Transport: TransportWebSocket, Duration: time.Second})
	if err == nil || !strings.Contains(err.Error(), "handshake failed") {
		t.Fatalf("error = %v, want websocket handshake failure", err)
	}
}

func TestLogsWebSocketNoFramesEndsAtDuration(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		var wsKey string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			key, value, ok := strings.Cut(line, ":")
			if ok && strings.EqualFold(strings.TrimSpace(key), "Sec-WebSocket-Key") {
				wsKey = strings.TrimSpace(value)
			}
		}
		if wsKey == "" {
			return
		}
		_, _ = io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Accept: "+webSocketAccept(wsKey)+"\r\n\r\n")
		time.Sleep(200 * time.Millisecond)
	}()
	client, err := New(listener.Addr().String(), "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Logs(context.Background(), LogsOptions{Transport: TransportWebSocket, Duration: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if result.LineCount != 0 || result.Transport != TransportWebSocket {
		t.Fatalf("result = %+v, want empty websocket log result", result)
	}
	<-done
}

func TestConnectionsSnapshotSummarizesActiveConnections(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/connections/" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		fmt.Fprint(w, `{
			"downloadTotal": 9000,
			"uploadTotal": 1200,
			"memory": 3456,
			"connections": [{
				"id": "conn-1",
				"metadata": {
					"network": "tcp",
					"type": "HTTP",
					"sourceIP": "192.168.6.10",
					"sourcePort": "50001",
					"destinationIP": "1.1.1.1",
					"destinationPort": "443",
					"host": "example.com",
					"process": "Safari",
					"remoteDestination": "203.0.113.10"
				},
				"upload": 100,
				"download": 200,
				"start": "2026-06-02T10:00:00Z",
				"chains": ["香港-Vmess-ARGO", "SyncnextProxy", "GLOBAL"],
				"providerChains": ["provider-a"],
				"rule": "RuleSet",
				"rulePayload": "syncnext_SyncnextProxy"
			}]
		}`)
	}))
	defer server.Close()
	config := writeMihomoAPIConfig(t, server.URL, "conn-secret")
	client, err := NewFromConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Connections(context.Background(), ConnectionsOptions{Mode: ConnectionsModeSnapshot})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer conn-secret" {
		t.Fatalf("authorization = %q, want bearer secret", gotAuth)
	}
	if result.FrameCount != 1 || result.Latest == nil || result.Latest.ConnectionCount != 1 {
		t.Fatalf("result = %+v, want one connection snapshot", result)
	}
	conn := result.Latest.Connections[0]
	if conn.SelectedProxy != "香港-Vmess-ARGO" || conn.RulePayload != "syncnext_SyncnextProxy" {
		t.Fatalf("connection = %+v, want selected proxy and rule payload summary", conn)
	}
	if result.Latest.ByProxy["香港-Vmess-ARGO"].Connections != 1 {
		t.Fatalf("by_proxy = %+v, want selected proxy aggregate", result.Latest.ByProxy)
	}
	if result.Latest.ByRule["RuleSet/syncnext_SyncnextProxy"].Connections != 1 {
		t.Fatalf("by_rule = %+v, want rule aggregate", result.Latest.ByRule)
	}
}

func TestConnectionsWebSocketStreamReadsBoundedFrames(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		requestLine, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		if !strings.Contains(requestLine, "/connections/") || !strings.Contains(requestLine, "interval=200") || !strings.Contains(requestLine, "token=stream-secret") {
			t.Errorf("request line = %q, want connections stream with interval and token", requestLine)
			return
		}
		var wsKey string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			key, value, ok := strings.Cut(line, ":")
			if ok && strings.EqualFold(strings.TrimSpace(key), "Sec-WebSocket-Key") {
				wsKey = strings.TrimSpace(value)
			}
		}
		if wsKey == "" {
			return
		}
		_, _ = io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Accept: "+webSocketAccept(wsKey)+"\r\n\r\n")
		_ = writeServerTextFrame(conn, `{"downloadTotal":1,"uploadTotal":2,"connections":[{"id":"one","metadata":{"network":"tcp","destinationPort":"443","host":"one.example"},"chains":["Proxy-A"],"upload":2,"download":3}]}`)
		_ = writeServerTextFrame(conn, `{"downloadTotal":3,"uploadTotal":4,"connections":[{"id":"two","metadata":{"network":"tcp","destinationPort":"443","host":"two.example"},"chains":["Proxy-B"],"upload":4,"download":5}]}`)
		time.Sleep(100 * time.Millisecond)
	}()
	client, err := New(listener.Addr().String(), "stream-secret")
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Connections(context.Background(), ConnectionsOptions{
		Mode:      ConnectionsModeStream,
		Interval:  200 * time.Millisecond,
		Duration:  time.Second,
		MaxFrames: 2,
		MaxBytes:  4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FrameCount != 2 || result.Latest == nil {
		t.Fatalf("result = %+v, want two frames", result)
	}
	if got := result.Latest.Connections[0].SelectedProxy; got != "Proxy-B" {
		t.Fatalf("latest proxy = %q, want Proxy-B", got)
	}
	<-done
}

func TestConnectionsWebSocketHandshakeFailureIsExplicit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	config := writeMihomoAPIConfig(t, server.URL, "")
	client, err := NewFromConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Connections(context.Background(), ConnectionsOptions{Mode: ConnectionsModeStream, Duration: time.Second})
	if err == nil || !strings.Contains(err.Error(), "handshake failed") {
		t.Fatalf("error = %v, want websocket handshake failure", err)
	}
}

func writeServerTextFrame(w io.Writer, text string) error {
	payload := []byte(text)
	header := []byte{0x81}
	switch {
	case len(payload) < 126:
		header = append(header, byte(len(payload)))
	case len(payload) <= 0xffff:
		header = append(header, 126, 0, 0)
		binary.BigEndian.PutUint16(header[len(header)-2:], uint16(len(payload)))
	default:
		header = append(header, 127, 0, 0, 0, 0, 0, 0, 0, 0)
		binary.BigEndian.PutUint64(header[len(header)-8:], uint64(len(payload)))
	}
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func writeMihomoAPIConfig(t *testing.T, controllerURL, secret string) string {
	t.Helper()
	controller := strings.TrimPrefix(controllerURL, "http://")
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := "external-controller: " + controller + "\n"
	if secret != "" {
		data += "secret: " + secret + "\n"
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
