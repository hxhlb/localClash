package mihomotest

import (
	"os"
	"path/filepath"
	"strings"
)

var skippedRuntimeNames = map[string]bool{
	"logs":       true,
	"mihomo.log": true,
	"ui":         true,
}

func SnapshotRuntimeDir(sourceDir, tempPattern string) (string, func(), error) {
	if tempPattern == "" {
		tempPattern = "localclash-mihomo-test-*"
	}
	targetDir, err := os.MkdirTemp("", tempPattern)
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(targetDir) }
	if sourceDir == "" {
		return targetDir, cleanup, nil
	}
	info, err := os.Stat(sourceDir)
	if err != nil {
		if os.IsNotExist(err) {
			return targetDir, cleanup, nil
		}
		cleanup()
		return "", func() {}, err
	}
	if !info.IsDir() {
		cleanup()
		return "", func() {}, &os.PathError{Op: "snapshot", Path: sourceDir, Err: os.ErrInvalid}
	}
	if err := copyRuntimeArtifacts(sourceDir, targetDir); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return targetDir, cleanup, nil
}

func copyRuntimeArtifacts(sourceDir, targetDir string) error {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if shouldSkipRuntimeName(entry.Name()) || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		sourcePath := filepath.Join(sourceDir, entry.Name())
		targetPath := filepath.Join(targetDir, entry.Name())
		if entry.IsDir() {
			if err := copyRuntimeDir(sourcePath, targetPath); err != nil {
				return err
			}
			continue
		}
		if err := copyRuntimeFile(sourcePath, targetPath); err != nil {
			return err
		}
	}
	return nil
}

func shouldSkipRuntimeName(name string) bool {
	return skippedRuntimeNames[name] || strings.HasPrefix(name, "cache.db")
}

func copyRuntimeDir(sourceDir, targetDir string) error {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	return copyRuntimeArtifacts(sourceDir, targetDir)
}

func copyRuntimeFile(sourcePath, targetPath string) error {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	perm := info.Mode().Perm()
	if perm == 0 {
		perm = 0o644
	}
	return os.WriteFile(targetPath, data, perm)
}
