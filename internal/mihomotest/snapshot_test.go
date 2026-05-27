package mihomotest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotRuntimeDirCopiesValidationArtifactsWithoutLiveCache(t *testing.T) {
	source := t.TempDir()
	mustWrite(t, filepath.Join(source, "Model.bin"), "model")
	mustWrite(t, filepath.Join(source, "geoip.dat"), "geoip")
	mustWrite(t, filepath.Join(source, "cache.db"), "live cache")
	mustWrite(t, filepath.Join(source, "cache.db-shm"), "live cache shm")
	mustWrite(t, filepath.Join(source, "cache.db-wal"), "live cache wal")
	mustWrite(t, filepath.Join(source, "mihomo.pid"), "1234\n")
	mustWrite(t, filepath.Join(source, "mihomo.log"), "log")
	mustWrite(t, filepath.Join(source, "rule-packs", "pack.yaml"), "payload")
	mustWrite(t, filepath.Join(source, "rule-packs", "cache.db"), "nested cache")
	mustWrite(t, filepath.Join(source, "ui", "index.html"), "ui")
	mustWrite(t, filepath.Join(source, "logs", "mihomo.log"), "log")

	target, cleanup, err := SnapshotRuntimeDir(source, "localclash-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	for _, want := range []string{"Model.bin", "geoip.dat", filepath.Join("rule-packs", "pack.yaml")} {
		if _, err := os.Stat(filepath.Join(target, want)); err != nil {
			t.Fatalf("snapshot missing %s: %v", want, err)
		}
	}
	for _, unwanted := range []string{
		"cache.db",
		"cache.db-shm",
		"cache.db-wal",
		"mihomo.pid",
		"mihomo.log",
		filepath.Join("rule-packs", "cache.db"),
		filepath.Join("ui", "index.html"),
		filepath.Join("logs", "mihomo.log"),
	} {
		if _, err := os.Stat(filepath.Join(target, unwanted)); !os.IsNotExist(err) {
			t.Fatalf("snapshot contains %s, err=%v", unwanted, err)
		}
	}
}

func TestSnapshotRuntimeDirAllowsMissingSource(t *testing.T) {
	target, cleanup, err := SnapshotRuntimeDir(filepath.Join(t.TempDir(), "missing"), "localclash-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("snapshot target is not a directory: %s", target)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
