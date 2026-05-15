package subdownload

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadUsesClashUserAgent(t *testing.T) {
	var gotUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.UserAgent()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("proxies: []\n"))
	}))
	defer server.Close()

	output := filepath.Join(t.TempDir(), "subscription.yaml")
	result, err := Download(context.Background(), Options{
		URL:        server.URL,
		OutputPath: output,
		UserAgent:  "clash-verge/v1.5.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotUA != "clash-verge/v1.5.1" {
		t.Fatalf("User-Agent = %q, want clash-verge/v1.5.1", gotUA)
	}
	if result.BytesWritten == 0 {
		t.Fatal("expected non-empty download")
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadRejectsEmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	_, err := Download(context.Background(), Options{
		URL:        server.URL,
		OutputPath: filepath.Join(t.TempDir(), "subscription.yaml"),
	})
	if err == nil {
		t.Fatal("expected empty response error")
	}
}
