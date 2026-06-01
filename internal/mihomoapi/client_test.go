package mihomoapi

import (
	"context"
	"fmt"
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
