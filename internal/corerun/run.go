package corerun

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"localclash/internal/runtimeprofile"
)

type Options struct {
	CorePath         string
	ConfigPath       string
	WorkDir          string
	LogPath          string
	LogRetentionDays int
}

var datedMihomoLogPattern = regexp.MustCompile(`^mihomo-\d{4}-\d{2}-\d{2}\.log$`)

func Run(opts Options) error {
	opts = normalizeOptions(opts)
	if err := opts.validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(opts.WorkDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(opts.LogPath), 0o755); err != nil {
		return err
	}
	logFile, err := newLogWriter(opts.LogPath, opts.LogRetentionDays, time.Now)
	if err != nil {
		return err
	}
	defer logFile.Close()

	output := io.MultiWriter(os.Stdout, logFile)
	errorOutput := io.MultiWriter(os.Stderr, logFile)

	cmd := exec.Command(opts.CorePath, "-d", opts.WorkDir, "-f", opts.ConfigPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = output
	cmd.Stderr = errorOutput
	fmt.Fprintf(os.Stderr, "mihomo log: %s\n", logFile.Path())
	return cmd.Run()
}

func normalizeOptions(opts Options) Options {
	opts.CorePath = strings.TrimSpace(opts.CorePath)
	opts.ConfigPath = strings.TrimSpace(opts.ConfigPath)
	opts.WorkDir = strings.TrimSpace(opts.WorkDir)
	opts.LogPath = strings.TrimSpace(opts.LogPath)
	if opts.CorePath == "" {
		opts.CorePath = runtimeprofile.MetaCorePath
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = filepath.Join(".runtime", "mihomo", "config.yaml")
	}
	if opts.WorkDir == "" {
		opts.WorkDir = ".runtime/mihomo"
	}
	if opts.LogRetentionDays == 0 {
		opts.LogRetentionDays = 7
	}
	if opts.LogPath == "" {
		opts.LogPath = defaultLogPath(opts.WorkDir, time.Now())
	}
	return opts
}

func (opts Options) validate() error {
	if opts.CorePath == "" || opts.ConfigPath == "" || opts.WorkDir == "" || opts.LogPath == "" {
		return errors.New("core, config, workdir, and log are required")
	}
	coreInfo, err := os.Stat(opts.CorePath)
	if err != nil {
		return fmt.Errorf("core %q is not available: %w", opts.CorePath, err)
	}
	if coreInfo.IsDir() {
		return fmt.Errorf("core %q is a directory", opts.CorePath)
	}
	if coreInfo.Mode()&0o111 == 0 {
		return fmt.Errorf("core %q is not executable", opts.CorePath)
	}
	configInfo, err := os.Stat(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("config %q is not available: %w", opts.ConfigPath, err)
	}
	if configInfo.IsDir() {
		return fmt.Errorf("config %q is a directory", opts.ConfigPath)
	}
	if parent := filepath.Dir(opts.WorkDir); parent != "." {
		if info, err := os.Stat(parent); err == nil && !info.IsDir() {
			return fmt.Errorf("workdir parent %q is not a directory", parent)
		}
	}
	if parent := filepath.Dir(opts.LogPath); parent != "." {
		if info, err := os.Stat(parent); err == nil && !info.IsDir() {
			return fmt.Errorf("log parent %q is not a directory", parent)
		}
	}
	if opts.LogRetentionDays < 0 {
		return fmt.Errorf("log retention must be non-negative, got %d", opts.LogRetentionDays)
	}
	return nil
}

func defaultLogPath(workDir string, now time.Time) string {
	return filepath.Join(workDir, "logs", "mihomo-"+now.Format("2006-01-02")+".log")
}

type logWriter struct {
	mu            sync.Mutex
	path          string
	file          *os.File
	retentionDays int
	now           func() time.Time
	rotating      bool
}

func newLogWriter(path string, retentionDays int, now func() time.Time) (*logWriter, error) {
	writer := &logWriter{
		path:          path,
		retentionDays: retentionDays,
		now:           now,
		rotating:      datedMihomoLogPattern.MatchString(filepath.Base(path)),
	}
	if err := writer.rotateLocked(); err != nil {
		return nil, err
	}
	return writer, nil
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.rotateLocked(); err != nil {
		return 0, err
	}
	return w.file.Write(p)
}

func (w *logWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *logWriter) Path() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.path
}

func (w *logWriter) rotateLocked() error {
	path := w.path
	if w.rotating {
		path = filepath.Join(filepath.Dir(w.path), "mihomo-"+w.now().Format("2006-01-02")+".log")
	}
	if w.file != nil && path == w.path {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if w.rotating {
		if err := cleanupOldLogs(path, w.retentionDays, w.now()); err != nil {
			return err
		}
	}
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return err
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	w.file = file
	w.path = path
	return nil
}

func cleanupOldLogs(logPath string, retentionDays int, now time.Time) error {
	if retentionDays == 0 {
		return nil
	}
	logDir := filepath.Dir(logPath)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	cutoff := now.AddDate(0, 0, -retentionDays+1)
	for _, entry := range entries {
		if entry.IsDir() || !datedMihomoLogPattern.MatchString(entry.Name()) {
			continue
		}
		datePart := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "mihomo-"), ".log")
		logDate, err := time.ParseInLocation("2006-01-02", datePart, now.Location())
		if err != nil {
			continue
		}
		if logDate.Before(startOfDay(cutoff)) {
			if err := os.Remove(filepath.Join(logDir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
