package fileops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNLFileReturnsNumberedRange(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("sample.yaml", []byte("one\n\nthree\nfour\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := NLFile(NLFileOptions{Path: "sample.yaml", StartLine: 2, LimitLines: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.StartLine != 2 || result.EndLine != 3 || result.TotalLines != 4 || !result.Truncated {
		t.Fatalf("result = %+v, want truncated lines 2-3 of 4", result)
	}
	if result.Text != "2: \n3: three" {
		t.Fatalf("text = %q", result.Text)
	}
	if result.Lines[0].Number != 2 || result.Lines[0].Text != "" {
		t.Fatalf("first line = %+v", result.Lines[0])
	}
}

func TestNLFileRejectsPathEscape(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if _, err := NLFile(NLFileOptions{Path: "../outside.yaml"}); err == nil {
		t.Fatal("expected path escape error")
	}
	if _, err := NLFile(NLFileOptions{Path: filepath.Join(dir, "absolute.yaml")}); err == nil {
		t.Fatal("expected absolute path error")
	}
}

func TestSedFileDryRunReplaceDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("config.yaml", []byte("target: PROXY\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := SedFile(SedFileOptions{
		Path:   "config.yaml",
		DryRun: true,
		Edits:  []Edit{{Op: "replace", Old: "target: PROXY", New: "target: DIRECT"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || !result.DryRun || !strings.Contains(result.Diff, "+target: DIRECT") {
		t.Fatalf("result = %+v, want dry-run diff", result)
	}
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "target: PROXY\n" {
		t.Fatalf("file changed during dry-run: %q", data)
	}
}

func TestSedFileAppliesLineEdits(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("config.yaml", []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := SedFile(SedFileOptions{
		Path:   "config.yaml",
		DryRun: false,
		Edits: []Edit{
			{Op: "insert_after", Line: 1, Text: "x"},
			{Op: "delete_range", StartLine: 3, EndLine: 3},
			{Op: "append", Text: "z\n"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.DryRun || result.SHA256Before == result.SHA256After {
		t.Fatalf("result = %+v, want applied change", result)
	}
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "a\nx\nc\nz\n" {
		t.Fatalf("file = %q", data)
	}
}

func TestSedFileExpectedSHARejectsStaleEdit(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("config.yaml", []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := SedFile(SedFileOptions{
		Path:           "config.yaml",
		DryRun:         false,
		ExpectedSHA256: strings.Repeat("0", 64),
		Edits:          []Edit{{Op: "append", Text: "b\n"}},
	})
	if err == nil || !strings.Contains(err.Error(), "expected_sha256 mismatch") {
		t.Fatalf("error = %v, want expected_sha256 mismatch", err)
	}
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "a\n" {
		t.Fatalf("file changed after stale edit: %q", data)
	}
}

func TestSedFileRejectsMissingReplaceText(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("config.yaml", []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := SedFile(SedFileOptions{
		Path:   "config.yaml",
		DryRun: true,
		Edits:  []Edit{{Op: "replace", Old: "missing", New: "b"}},
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v, want not found", err)
	}
}
