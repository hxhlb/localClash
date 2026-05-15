package subdownload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Options struct {
	URL        string
	OutputPath string
	UserAgent  string
	Force      bool
}

type Result struct {
	OutputPath   string
	BytesWritten int64
}

func Download(ctx context.Context, opts Options) (Result, error) {
	opts = normalizeOptions(opts)
	if err := opts.validate(); err != nil {
		return Result{}, err
	}
	if err := ensureWritableOutput(opts.OutputPath, opts.Force); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(opts.OutputPath), 0o755); err != nil {
		return Result{}, err
	}

	tmpPath := opts.OutputPath + ".download"
	defer os.Remove(tmpPath)

	bytesWritten, err := download(ctx, opts, tmpPath)
	if err != nil {
		return Result{}, err
	}
	if bytesWritten == 0 {
		return Result{}, errors.New("subscription response was empty")
	}
	if err := os.Rename(tmpPath, opts.OutputPath); err != nil {
		return Result{}, err
	}

	return Result{OutputPath: opts.OutputPath, BytesWritten: bytesWritten}, nil
}

func normalizeOptions(opts Options) Options {
	opts.URL = strings.TrimSpace(opts.URL)
	opts.OutputPath = strings.TrimSpace(opts.OutputPath)
	opts.UserAgent = strings.TrimSpace(opts.UserAgent)
	if opts.OutputPath == "" {
		opts.OutputPath = "subscription.yaml"
	}
	if opts.UserAgent == "" {
		opts.UserAgent = "clash-verge/v1.5.1"
	}
	return opts
}

func (opts Options) validate() error {
	if opts.URL == "" {
		return errors.New("subscription URL is required")
	}
	parsed, err := url.Parse(opts.URL)
	if err != nil {
		return fmt.Errorf("invalid subscription URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("subscription URL must use http or https, got %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return errors.New("subscription URL must include a host")
	}
	if opts.OutputPath == "." || opts.OutputPath == string(filepath.Separator) {
		return fmt.Errorf("output path %q is not a file path", opts.OutputPath)
	}
	return nil
}

func ensureWritableOutput(path string, force bool) error {
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("output path %q is a directory", path)
		}
		if !force {
			return fmt.Errorf("output path %q already exists; pass --force to overwrite", path)
		}
		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func download(ctx context.Context, opts Options, outputPath string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, opts.URL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", opts.UserAgent)
	req.Header.Set("Accept", "*/*")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return 0, fmt.Errorf("subscription request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	out, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	defer out.Close()

	return io.Copy(out, resp.Body)
}
