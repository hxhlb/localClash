package corerun

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Options struct {
	CorePath   string
	ConfigPath string
	WorkDir    string
	LogPath    string
}

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
	logFile, err := os.OpenFile(opts.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
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
	fmt.Fprintf(os.Stderr, "mihomo log: %s\n", opts.LogPath)
	return cmd.Run()
}

func normalizeOptions(opts Options) Options {
	opts.CorePath = strings.TrimSpace(opts.CorePath)
	opts.ConfigPath = strings.TrimSpace(opts.ConfigPath)
	opts.WorkDir = strings.TrimSpace(opts.WorkDir)
	opts.LogPath = strings.TrimSpace(opts.LogPath)
	if opts.CorePath == "" {
		opts.CorePath = "bin/mihomo"
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = "generated/mihomo.yaml"
	}
	if opts.WorkDir == "" {
		opts.WorkDir = ".runtime/mihomo"
	}
	if opts.LogPath == "" {
		opts.LogPath = filepath.Join(opts.WorkDir, "mihomo.log")
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
	return nil
}
