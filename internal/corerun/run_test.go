package corerun

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"localclash/internal/runtimeprofile"
)

func TestNormalizeOptionsDefaults(t *testing.T) {
	got := normalizeOptions(Options{})
	if got.CorePath != runtimeprofile.MetaCorePath {
		t.Fatalf("CorePath = %q", got.CorePath)
	}
	if got.ConfigPath != "generated/mihomo.yaml" {
		t.Fatalf("ConfigPath = %q", got.ConfigPath)
	}
	if got.WorkDir != ".runtime/mihomo" {
		t.Fatalf("WorkDir = %q", got.WorkDir)
	}
	wantLog := filepath.Join(".runtime/mihomo", "logs", "mihomo-"+time.Now().Format("2006-01-02")+".log")
	if got.LogPath != wantLog {
		t.Fatalf("LogPath = %q", got.LogPath)
	}
	if got.LogRetentionDays != 7 {
		t.Fatalf("LogRetentionDays = %d", got.LogRetentionDays)
	}
}

func TestValidateRejectsMissingConfig(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "mihomo")
	if err := os.WriteFile(core, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := normalizeOptions(Options{
		CorePath:   core,
		ConfigPath: filepath.Join(dir, "missing.yaml"),
		WorkDir:    filepath.Join(dir, "runtime"),
	}).validate()
	if err == nil {
		t.Fatal("expected missing config error")
	}
}

func TestCleanupOldLogsKeepsSevenDays(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	files := []string{
		"mihomo-2026-05-15.log",
		"mihomo-2026-05-09.log",
		"mihomo-2026-05-08.log",
		"mihomo.log",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := cleanupOldLogs(filepath.Join(dir, "mihomo-2026-05-15.log"), 7, now); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, "mihomo-2026-05-09.log")); err != nil {
		t.Fatalf("expected 7th day log to remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "mihomo-2026-05-08.log")); !os.IsNotExist(err) {
		t.Fatalf("expected old dated log to be removed, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "mihomo.log")); err != nil {
		t.Fatalf("expected non-dated log to remain: %v", err)
	}
}

func TestLogWriterRotatesAcrossDays(t *testing.T) {
	dir := t.TempDir()
	current := time.Date(2026, 5, 15, 23, 59, 0, 0, time.UTC)
	writer, err := newLogWriter(filepath.Join(dir, "mihomo-2026-05-15.log"), 7, func() time.Time {
		return current
	})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	if _, err := writer.Write([]byte("day one\n")); err != nil {
		t.Fatal(err)
	}
	current = current.Add(2 * time.Minute)
	if _, err := writer.Write([]byte("day two\n")); err != nil {
		t.Fatal(err)
	}

	dayOne, err := os.ReadFile(filepath.Join(dir, "mihomo-2026-05-15.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(dayOne) != "day one\n" {
		t.Fatalf("day one log = %q", string(dayOne))
	}
	dayTwo, err := os.ReadFile(filepath.Join(dir, "mihomo-2026-05-16.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(dayTwo) != "day two\n" {
		t.Fatalf("day two log = %q", string(dayTwo))
	}
}

func TestLogWriterKeepsExplicitNonDatedLogPathFixed(t *testing.T) {
	dir := t.TempDir()
	current := time.Date(2026, 5, 15, 23, 59, 0, 0, time.UTC)
	writer, err := newLogWriter(filepath.Join(dir, "custom.log"), 7, func() time.Time {
		return current
	})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	if _, err := writer.Write([]byte("day one\n")); err != nil {
		t.Fatal(err)
	}
	current = current.Add(2 * time.Minute)
	if _, err := writer.Write([]byte("day two\n")); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "custom.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "day one\nday two\n" {
		t.Fatalf("custom log = %q", string(data))
	}
	if _, err := os.Stat(filepath.Join(dir, "mihomo-2026-05-16.log")); !os.IsNotExist(err) {
		t.Fatalf("unexpected dated log, err=%v", err)
	}
}
