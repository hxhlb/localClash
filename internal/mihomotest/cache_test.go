package mihomotest

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestValidateCachedReusesPassingResultForSameConfigAndCore(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "mihomo")
	counter := filepath.Join(dir, "count")
	writeTestFile(t, core, "#!/bin/sh\nif [ \"$1\" = \"-v\" ]; then echo 'Mihomo Meta test'; exit 0; fi\ncount=0\n[ -f "+counter+" ] && count=$(cat "+counter+")\ncount=$((count + 1))\necho \"$count\" > "+counter+"\necho ok\nexit 0\n", 0o755)
	config := filepath.Join(dir, "mihomo.yaml")
	writeTestFile(t, config, "mode: rule\n", 0o644)
	workDir := filepath.Join(dir, "runtime")
	writeTestFile(t, filepath.Join(workDir, "geoip.dat"), "geoip", 0o644)
	cache := filepath.Join(dir, ".runtime", "validation", "mihomo-test-cache.json")

	first, err := ValidateCached(context.Background(), ValidationOptions{
		CorePath:   core,
		ConfigPath: config,
		WorkDir:    workDir,
		CachePath:  cache,
		Now:        fixedValidationNow(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Passed || first.Cached {
		t.Fatalf("first validation = %+v, want uncached pass", first)
	}
	second, err := ValidateCached(context.Background(), ValidationOptions{
		CorePath:   core,
		ConfigPath: config,
		WorkDir:    workDir,
		CachePath:  cache,
		Now:        fixedValidationNow(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Passed || !second.Cached {
		t.Fatalf("second validation = %+v, want cached pass", second)
	}
	if got := strings.TrimSpace(readTestFile(t, counter)); got != "1" {
		t.Fatalf("core validation count = %q, want one actual -t run", got)
	}
}

func TestValidateCachedInvalidatesWhenGeneratedConfigChanges(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "mihomo")
	counter := filepath.Join(dir, "count")
	writeTestFile(t, core, "#!/bin/sh\nif [ \"$1\" = \"-v\" ]; then echo 'Mihomo Meta test'; exit 0; fi\ncount=0\n[ -f "+counter+" ] && count=$(cat "+counter+")\ncount=$((count + 1))\necho \"$count\" > "+counter+"\necho ok\nexit 0\n", 0o755)
	config := filepath.Join(dir, "mihomo.yaml")
	writeTestFile(t, config, "mode: rule\n", 0o644)
	cache := filepath.Join(dir, "cache.json")

	if _, err := ValidateCached(context.Background(), ValidationOptions{CorePath: core, ConfigPath: config, WorkDir: dir, CachePath: cache, Now: fixedValidationNow()}); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, config, "mode: global\n", 0o644)
	second, err := ValidateCached(context.Background(), ValidationOptions{CorePath: core, ConfigPath: config, WorkDir: dir, CachePath: cache, Now: fixedValidationNow()})
	if err != nil {
		t.Fatal(err)
	}
	if second.Cached {
		t.Fatalf("second validation = %+v, want cache miss after config change", second)
	}
	if got := strings.TrimSpace(readTestFile(t, counter)); got != "2" {
		t.Fatalf("core validation count = %q, want second actual -t run", got)
	}
}

func TestValidateCachedPassSurvivesCacheWriteFailure(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "mihomo")
	writeTestFile(t, core, "#!/bin/sh\nif [ \"$1\" = \"-v\" ]; then echo 'Mihomo Meta test'; exit 0; fi\necho ok\nexit 0\n", 0o755)
	config := filepath.Join(dir, "mihomo.yaml")
	writeTestFile(t, config, "mode: rule\n", 0o644)
	workDir := filepath.Join(dir, "runtime")
	writeTestFile(t, filepath.Join(workDir, "geoip.dat"), "geoip", 0o644)
	blocker := filepath.Join(dir, "not-a-directory")
	writeTestFile(t, blocker, "block cache dir\n", 0o644)

	result, err := ValidateCached(context.Background(), ValidationOptions{
		CorePath:   core,
		ConfigPath: config,
		WorkDir:    workDir,
		CachePath:  filepath.Join(blocker, "cache.json"),
		Now:        fixedValidationNow(),
	})
	if err != nil {
		t.Fatalf("ValidateCached error = %v, want nil after passing mihomo -t", err)
	}
	if !result.Passed {
		t.Fatalf("result = %+v, want passing validation", result)
	}
	if result.Error != "" {
		t.Fatalf("result error = %q, want no validation error", result.Error)
	}
	if result.CacheWriteError == "" {
		t.Fatalf("result = %+v, want cache_write_error diagnostic", result)
	}
}

func TestValidateCachedUsesMetadataFastHitBeforeHashingConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 000 does not reliably block same-user file reads on Windows")
	}
	dir := t.TempDir()
	core := filepath.Join(dir, "mihomo")
	counter := filepath.Join(dir, "count")
	writeTestFile(t, core, "#!/bin/sh\nif [ \"$1\" = \"-v\" ]; then echo 'Mihomo Meta test'; exit 0; fi\ncount=0\n[ -f "+counter+" ] && count=$(cat "+counter+")\ncount=$((count + 1))\necho \"$count\" > "+counter+"\necho ok\nexit 0\n", 0o755)
	config := filepath.Join(dir, "mihomo.yaml")
	writeTestFile(t, config, "mode: rule\n", 0o644)
	workDir := filepath.Join(dir, "runtime")
	writeTestFile(t, filepath.Join(workDir, "geoip.dat"), "geoip", 0o644)
	cache := filepath.Join(dir, "cache.json")

	first, err := ValidateCached(context.Background(), ValidationOptions{
		CorePath:   core,
		ConfigPath: config,
		WorkDir:    workDir,
		CachePath:  cache,
		Now:        fixedValidationNow(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Passed || first.Cached {
		t.Fatalf("first validation = %+v, want uncached pass", first)
	}
	if err := os.Chmod(config, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(config, 0o644) })

	second, err := ValidateCached(context.Background(), ValidationOptions{
		CorePath:   core,
		ConfigPath: config,
		WorkDir:    workDir,
		CachePath:  cache,
		Now:        fixedValidationNow(),
	})
	if err != nil {
		t.Fatalf("ValidateCached error = %v, want metadata cache hit before hashing config", err)
	}
	if !second.Passed || !second.Cached || second.CacheHitMode != "metadata" {
		t.Fatalf("second validation = %+v, want metadata cached pass", second)
	}
	if got := strings.TrimSpace(readTestFile(t, counter)); got != "1" {
		t.Fatalf("core validation count = %q, want one actual -t run", got)
	}
}

func fixedValidationNow() func() time.Time {
	return func() time.Time {
		return time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	}
}

func writeTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
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
