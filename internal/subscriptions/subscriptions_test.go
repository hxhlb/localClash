package subscriptions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStatusNoConfig(t *testing.T) {
	dir := t.TempDir()

	result, err := Status(StatusOptions{
		ConfigPath: filepath.Join(dir, "localclash-subscriptions.yaml"),
		MergedPath: filepath.Join(dir, "subscription.yaml"),
		RuntimeDir: filepath.Join(dir, ".runtime", "subscriptions"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Configured {
		t.Fatal("configured = true, want false")
	}
	if !strings.Contains(result.Message, "ask the user") {
		t.Fatalf("message = %q, want bootstrap hint", result.Message)
	}
}

func TestStatusConfigExistsArtifactsMissingAndMergedCounts(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "localclash-subscriptions.yaml")
	merged := filepath.Join(dir, "subscription.yaml")
	runtimeDir := filepath.Join(dir, ".runtime", "subscriptions")
	writeTestFile(t, config, `version: 1
sources:
  - id: primary
    url: https://example.com/sub?token=secret-token
`)
	writeTestFile(t, merged, `proxies:
  - name: SG 01
    type: ss
proxy-groups:
  - name: PROXY
    type: select
rules:
  - MATCH,PROXY
`)

	result, err := Status(StatusOptions{ConfigPath: config, MergedPath: merged, RuntimeDir: runtimeDir})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Configured || len(result.Sources) != 1 {
		t.Fatalf("status = %+v, want one configured source", result)
	}
	if result.Sources[0].Exists {
		t.Fatal("artifact exists = true, want false")
	}
	if result.Merged.ProxiesCount != 1 || result.Merged.ProxyGroupsCount != 1 || result.Merged.RulesCount != 1 {
		t.Fatalf("merged = %+v, want counts 1/1/1", result.Merged)
	}
	assertNoTokenLeak(t, result)
}

func TestConfigureWritesValidMultiSourcesAndMasksURLs(t *testing.T) {
	dir := t.TempDir()
	replace := true
	url1 := "https://example.com/sub?token=secret-token"
	url2 := "https://example.net/path/profile?token=backup-secret"

	result, err := Configure(ConfigureOptions{
		ConfigPath: filepath.Join(dir, "localclash-subscriptions.yaml"),
		Replace:    &replace,
		URLs:       []string{url1, url2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Configured || len(result.Sources) != 2 {
		t.Fatalf("result = %+v, want two configured sources", result)
	}
	if result.Sources[0].ID != mustSourceID(t, url1) || result.Sources[1].ID != mustSourceID(t, url2) {
		t.Fatalf("source ids = %+v, want generated short hash ids", result.Sources)
	}
	data := readTestFile(t, filepath.Join(dir, "localclash-subscriptions.yaml"))
	if !strings.Contains(data, "secret-token") {
		t.Fatal("config should contain the real URL token on disk")
	}
	assertNoTokenLeak(t, result)
}

func TestConfigureRejectsInvalidInputs(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		urls []string
	}{
		{name: "empty", urls: nil},
		{name: "duplicate url", urls: []string{"https://example.com/sub", "https://example.com/sub"}},
		{name: "bad scheme", urls: []string{"file:///tmp/sub.yaml"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Configure(ConfigureOptions{
				ConfigPath: filepath.Join(dir, tt.name+".yaml"),
				URLs:       tt.urls,
			})
			if err == nil {
				t.Fatal("expected configure error")
			}
		})
	}
}

func TestRefreshFetchesArtifactsAndPrefixesMultiSourceNodes(t *testing.T) {
	userAgents := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgents <- r.UserAgent()
		switch r.URL.Path {
		case "/primary":
			_, _ = w.Write([]byte(`proxies:
  - name: Same
    type: ss
    server: primary.example
    password: secret
proxy-groups:
  - name: PROXY
    type: select
rules:
  - MATCH,PROXY
`))
		case "/backup":
			_, _ = w.Write([]byte(`proxies:
  - name: Same
    type: trojan
    server: backup.example
    password: secret
  - name: Unique
    type: ss
    server: unique.example
    password: secret
`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	primaryURL := server.URL + "/primary?token=primary-secret"
	backupURL := server.URL + "/backup?token=backup-secret"
	primaryID := mustSourceID(t, primaryURL)
	backupID := mustSourceID(t, backupURL)
	paths := writeRefreshConfig(t, []Source{
		{ID: "primary", URL: primaryURL},
		{ID: "backup", URL: backupURL},
	})

	result, err := Refresh(context.Background(), RefreshOptions{
		ConfigPath: paths.config,
		RuntimeDir: paths.runtimeDir,
		MergedPath: paths.merged,
		UserAgent:  "test-clash-ua",
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if gotUA := <-userAgents; gotUA != "test-clash-ua" {
			t.Fatalf("User-Agent = %q, want test-clash-ua", gotUA)
		}
	}
	if len(result.Sources) != 2 {
		t.Fatalf("sources = %+v, want two summaries", result.Sources)
	}
	assertFileExists(t, filepath.Join(paths.runtimeDir, primaryID+".yaml"))
	assertFileExists(t, filepath.Join(paths.runtimeDir, backupID+".yaml"))
	assertFileExists(t, paths.merged)
	if result.Merged.ProxiesCount != 3 || result.Merged.RenamedProxiesCount != 3 {
		t.Fatalf("merged = %+v, want 3 proxies and 3 renamed", result.Merged)
	}
	merged := readTestFile(t, paths.merged)
	for _, want := range []string{"[" + primaryID + "] Same", "[" + backupID + "] Same", "[" + backupID + "] Unique"} {
		if !strings.Contains(merged, want) {
			t.Fatalf("merged subscription missing %q:\n%s", want, merged)
		}
	}
	assertNoTokenLeak(t, result)
}

func TestRefreshFetchesSelectedSourcesInParallel(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- r.URL.Path
		<-release
		_, _ = w.Write([]byte(`proxies:
  - name: ` + strings.TrimPrefix(r.URL.Path, "/") + `
    type: ss
    server: example.com
    password: secret
`))
	}))
	defer server.Close()
	paths := writeRefreshConfig(t, []Source{
		{URL: server.URL + "/first?token=primary-secret"},
		{URL: server.URL + "/second?token=backup-secret"},
	})

	errs := make(chan error, 1)
	go func() {
		_, err := Refresh(context.Background(), RefreshOptions{
			ConfigPath: paths.config,
			RuntimeDir: paths.runtimeDir,
			MergedPath: paths.merged,
		})
		errs <- err
	}()

	got := map[string]bool{}
	for len(got) < 2 {
		select {
		case path := <-started:
			got[path] = true
		case <-time.After(2 * time.Second):
			close(release)
			t.Fatalf("timed out waiting for both fetches to start, got %v", got)
		}
	}
	close(release)
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if !got["/first"] || !got["/second"] {
		t.Fatalf("started paths = %v, want both sources", got)
	}
}

func TestRefreshSingleSourcePreservesNodeNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`proxies:
  - name: HK 01
    type: ss
    server: hk.example
    password: secret
  - name: SG 01
    type: ss
    server: sg.example
    password: secret
`))
	}))
	defer server.Close()
	paths := writeRefreshConfig(t, []Source{{ID: "primary", URL: server.URL + "/sub?token=secret-token"}})

	result, err := Refresh(context.Background(), RefreshOptions{
		ConfigPath: paths.config,
		RuntimeDir: paths.runtimeDir,
		MergedPath: paths.merged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Merged.ProxiesCount != 2 || result.Merged.RenamedProxiesCount != 0 {
		t.Fatalf("merged = %+v, want 2 proxies and no renamed nodes", result.Merged)
	}
	merged := readTestFile(t, paths.merged)
	for _, want := range []string{"name: HK 01", "name: SG 01"} {
		if !strings.Contains(merged, want) {
			t.Fatalf("merged subscription missing %q:\n%s", want, merged)
		}
	}
	if strings.Contains(merged, "[primary]") {
		t.Fatalf("single-source merged subscription should not add source prefix:\n%s", merged)
	}
	assertNoTokenLeak(t, result)
}

func TestRefreshUnknownIDsReturnError(t *testing.T) {
	paths := writeRefreshConfig(t, []Source{{ID: "primary", URL: "https://example.com/sub?token=secret-token"}})

	_, err := Refresh(context.Background(), RefreshOptions{
		ConfigPath: paths.config,
		RuntimeDir: paths.runtimeDir,
		MergedPath: paths.merged,
		IDs:        []string{"missing"},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown subscription source id") {
		t.Fatalf("error = %v, want unknown id", err)
	}
	assertNoTokenLeak(t, err.Error())
}

func TestRefreshRejectsInvalidResponses(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty", body: ""},
		{name: "invalid yaml", body: ":\n"},
		{name: "no proxies", body: "rules: []\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			paths := writeRefreshConfig(t, []Source{{ID: "primary", URL: server.URL + "/sub?token=secret-token"}})

			_, err := Refresh(context.Background(), RefreshOptions{
				ConfigPath: paths.config,
				RuntimeDir: paths.runtimeDir,
				MergedPath: paths.merged,
			})
			if err == nil {
				t.Fatal("expected refresh error")
			}
			assertNoTokenLeak(t, err.Error())
		})
	}
}

type refreshPaths struct {
	dir        string
	config     string
	runtimeDir string
	merged     string
}

func writeRefreshConfig(t *testing.T, sources []Source) refreshPaths {
	t.Helper()
	dir := t.TempDir()
	paths := refreshPaths{
		dir:        dir,
		config:     filepath.Join(dir, "localclash-subscriptions.yaml"),
		runtimeDir: filepath.Join(dir, ".runtime", "subscriptions"),
		merged:     filepath.Join(dir, "subscription.yaml"),
	}
	urls := make([]string, 0, len(sources))
	for _, source := range sources {
		urls = append(urls, source.URL)
	}
	_, err := Configure(ConfigureOptions{ConfigPath: paths.config, URLs: urls})
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func mustSourceID(t *testing.T, rawURL string) string {
	t.Helper()
	canonicalURL, err := canonicalSubscriptionURL(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return sourceIDFromCanonicalURL(canonicalURL, map[string]bool{})
}

func assertNoTokenLeak(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, banned := range []string{"secret-token", "primary-secret", "backup-secret", "token=", "password: secret"} {
		if strings.Contains(text, banned) {
			t.Fatalf("value leaked %q in %s", banned, text)
		}
	}
}
