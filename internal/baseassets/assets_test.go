package baseassets

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallDownloadsAndExtractsBaseAssets(t *testing.T) {
	archive := testArchive(t)
	sum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			fmt.Fprintf(w, `{"version":"v-test","base_assets":{"filename":"assets.tar.gz","url":"%s/assets.tar.gz","sha256":"%s"}}`, "http://"+r.Host, hex.EncodeToString(sum[:]))
		case "/assets.tar.gz":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	result, err := Install(context.Background(), Options{
		ManifestURL: server.URL + "/manifest.json",
		OutputDir:   dir,
	})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if result.Version != "v-test" || result.Extracted == 0 {
		t.Fatalf("result = %+v", result)
	}
	status := Status(dir)
	if !status.Installed {
		t.Fatalf("status = %+v, want installed", status)
	}
	if status.DefaultPatchCount != 1 || !status.DefaultPatchesInstalled {
		t.Fatalf("default patch status = count %d installed %v, want 1/true", status.DefaultPatchCount, status.DefaultPatchesInstalled)
	}
}

func TestStatusReportsMissingJSONAssets(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "policy-templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "policy-templates", "minimal.yaml"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	status := Status(dir)
	if status.Installed {
		t.Fatalf("status = %+v, want missing json assets", status)
	}
	if len(status.Missing) == 0 || status.Missing[0] != filepath.Join("policy-templates", "localclash-default.json") {
		t.Fatalf("missing = %+v", status.Missing)
	}
}

func testArchive(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := map[string]string{
		"policy-templates/localclash-default.json":      `{"patches":[{"path":"localclash-default.d/00.json"}]}`,
		"policy-templates/localclash-default.d/00.json": `{"id":"p","config":{}}`,
		"rule-sources/v2fly-dlc.json":                   `{"version":1}`,
		".runtime/mihomo/Country.mmdb":                  "country",
		".runtime/mihomo/geoip.dat":                     "geoip",
		".runtime/mihomo/geosite.dat":                   "geosite",
		".runtime/mihomo/ASN.mmdb":                      "asn",
	}
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
