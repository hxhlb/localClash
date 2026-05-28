package appinit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const coreVersionCacheFile = "core-version.json"

type coreVersionCache struct {
	CorePath       string `json:"core_path"`
	Version        string `json:"version"`
	SmartSupported bool   `json:"smart_supported"`
	UpdatedAt      string `json:"updated_at"`
}

var timeNow = time.Now

func CoreVersionCachePath(runtimeRoot string) string {
	runtimeRoot = strings.TrimSpace(runtimeRoot)
	if runtimeRoot == "" {
		runtimeRoot = ".runtime"
	}
	return filepath.Join(runtimeRoot, coreVersionCacheFile)
}

func ClearCoreVersionCache(runtimeRoot string) error {
	err := os.Remove(CoreVersionCachePath(runtimeRoot))
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return err
}

func RefreshCoreVersionCache(ctx context.Context, runtimeRoot, corePath string) (CoreState, error) {
	_ = ClearCoreVersionCache(runtimeRoot)
	core, err := inspectCoreLive(ctx, corePath)
	if err != nil {
		return core, err
	}
	return core, writeCoreVersionCache(CoreVersionCachePath(runtimeRoot), core, timeNow())
}

func inspectCoreLive(ctx context.Context, corePath string) (CoreState, error) {
	core := CoreState{Path: corePath}
	info, err := os.Stat(corePath)
	if err != nil || info.IsDir() {
		core.Missing = true
		if err != nil {
			return core, err
		}
		return core, fmt.Errorf("core path is a directory")
	}
	core.Exists = true
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(runCtx, corePath, "-v").CombinedOutput()
	if err != nil {
		return core, err
	}
	core.Version = strings.TrimSpace(string(output))
	core.SmartSupported = strings.Contains(strings.ToLower(core.Version), "smart")
	return core, nil
}

func readCoreVersionCache(path, corePath string) (CoreState, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CoreState{}, false
	}
	var cache coreVersionCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return CoreState{}, false
	}
	if cache.CorePath != corePath || strings.TrimSpace(cache.Version) == "" {
		return CoreState{}, false
	}
	return CoreState{
		Path:           cache.CorePath,
		Exists:         true,
		Version:        cache.Version,
		SmartSupported: cache.SmartSupported,
	}, true
}

func writeCoreVersionCache(path string, core CoreState, now time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	cache := coreVersionCache{
		CorePath:       core.Path,
		Version:        core.Version,
		SmartSupported: core.SmartSupported,
		UpdatedAt:      now.UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
