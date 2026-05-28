package workspace

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	EnvVar     = "LOCALCLASH_WORKDIR"
	MarkerName = ".localclash-workspace"
)

func FromRuntimeRoot(runtimeRoot string) string {
	clean := filepath.Clean(strings.TrimSpace(runtimeRoot))
	if clean == "" || clean == "." {
		return ""
	}
	if filepath.Base(clean) != ".runtime" {
		return ""
	}
	return filepath.Dir(clean)
}

func EnsureMarker(root string) error {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." ||
		!filepath.IsAbs(root) ||
		IsProtectedPath(root) ||
		isTopLevelPath(root) ||
		LooksLikeSourceCheckout(root) {
		return nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	path := filepath.Join(root, MarkerName)
	if _, err := os.Lstat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, []byte("version=1\nowner=localclash\n"), 0o644)
}

func HasMarker(root string) bool {
	info, err := os.Lstat(filepath.Join(root, MarkerName))
	return err == nil && !info.IsDir()
}

func LooksLikeSourceCheckout(root string) bool {
	for _, marker := range []string{".git", "go.mod"} {
		if _, err := os.Lstat(filepath.Join(root, marker)); err == nil {
			return true
		}
	}
	return false
}

func IsProtectedPath(root string) bool {
	switch filepath.Clean(root) {
	case "/", "/bin", "/dev", "/etc", "/lib", "/lib64", "/opt", "/proc", "/root", "/sbin", "/sys", "/tmp", "/usr", "/var", "/Volumes", "/Users":
		return true
	default:
		return false
	}
}

func isTopLevelPath(root string) bool {
	parent := filepath.Dir(filepath.Clean(root))
	return parent == root || parent == string(filepath.Separator)
}
