package fileops

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultLimitLines = 120
	defaultMaxBytes   = 64 * 1024
	maxReadBytes      = 1024 * 1024
)

type NLFileOptions struct {
	Root       string
	Path       string
	StartLine  int
	LimitLines int
	MaxBytes   int
}

type NumberedLine struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
}

type NLFileResult struct {
	Path       string         `json:"path"`
	StartLine  int            `json:"start_line"`
	EndLine    int            `json:"end_line"`
	TotalLines int            `json:"total_lines"`
	SizeBytes  int64          `json:"size_bytes"`
	SHA256     string         `json:"sha256"`
	Truncated  bool           `json:"truncated"`
	Lines      []NumberedLine `json:"lines"`
	Text       string         `json:"text"`
}

type SedFileOptions struct {
	Root           string
	Path           string
	DryRun         bool
	ExpectedSHA256 string
	Edits          []Edit
}

type Edit struct {
	Op        string `json:"op"`
	Old       string `json:"old,omitempty"`
	New       string `json:"new,omitempty"`
	Count     int    `json:"count,omitempty"`
	Line      int    `json:"line,omitempty"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	Text      string `json:"text,omitempty"`
}

type SedFileResult struct {
	Path         string   `json:"path"`
	Changed      bool     `json:"changed"`
	DryRun       bool     `json:"dry_run"`
	EditCount    int      `json:"edit_count"`
	SHA256Before string   `json:"sha256_before"`
	SHA256After  string   `json:"sha256_after"`
	Diff         string   `json:"diff"`
	Warnings     []string `json:"warnings,omitempty"`
}

func NLFile(opts NLFileOptions) (NLFileResult, error) {
	path, displayPath, err := resolveExistingPath(opts.Root, opts.Path)
	if err != nil {
		return NLFileResult{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return NLFileResult{}, err
	}
	if info.IsDir() {
		return NLFileResult{}, fmt.Errorf("path %q is a directory", displayPath)
	}
	startLine := opts.StartLine
	if startLine <= 0 {
		startLine = 1
	}
	limitLines := opts.LimitLines
	if limitLines <= 0 {
		limitLines = defaultLimitLines
	}
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	if maxBytes > maxReadBytes {
		maxBytes = maxReadBytes
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return NLFileResult{}, err
	}
	hash := sha256.Sum256(data)
	lineTexts := splitLines(string(data))
	result := NLFileResult{
		Path:       displayPath,
		StartLine:  startLine,
		TotalLines: len(lineTexts),
		SizeBytes:  info.Size(),
		SHA256:     hex.EncodeToString(hash[:]),
	}
	var text bytes.Buffer
	bytesUsed := 0
	for index, line := range lineTexts {
		lineNumber := index + 1
		if lineNumber < startLine {
			continue
		}
		if len(result.Lines) >= limitLines {
			result.Truncated = true
			break
		}
		lineBytes := len([]byte(line))
		if bytesUsed+lineBytes > maxBytes && len(result.Lines) > 0 {
			result.Truncated = true
			break
		}
		result.Lines = append(result.Lines, NumberedLine{Number: lineNumber, Text: line})
		fmt.Fprintf(&text, "%d: %s\n", lineNumber, line)
		result.EndLine = lineNumber
		bytesUsed += lineBytes
		if bytesUsed >= maxBytes {
			result.Truncated = true
			break
		}
	}
	if result.EndLine == 0 {
		result.EndLine = startLine - 1
	}
	result.Text = strings.TrimSuffix(text.String(), "\n")
	return result, nil
}

func SedFile(opts SedFileOptions) (SedFileResult, error) {
	path, displayPath, err := resolveExistingPath(opts.Root, opts.Path)
	if err != nil {
		return SedFileResult{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return SedFileResult{}, err
	}
	if info.IsDir() {
		return SedFileResult{}, fmt.Errorf("path %q is a directory", displayPath)
	}
	if len(opts.Edits) == 0 {
		return SedFileResult{}, errors.New("at least one edit is required")
	}
	beforeBytes, err := os.ReadFile(path)
	if err != nil {
		return SedFileResult{}, err
	}
	beforeHash := sha256.Sum256(beforeBytes)
	beforeSHA := hex.EncodeToString(beforeHash[:])
	if opts.ExpectedSHA256 != "" && !strings.EqualFold(opts.ExpectedSHA256, beforeSHA) {
		return SedFileResult{}, fmt.Errorf("expected_sha256 mismatch: file is %s", beforeSHA)
	}

	before := string(beforeBytes)
	after := before
	warnings := []string{}
	for _, edit := range opts.Edits {
		next, editWarnings, err := applyEdit(after, edit)
		if err != nil {
			return SedFileResult{}, err
		}
		after = next
		warnings = append(warnings, editWarnings...)
	}
	afterHash := sha256.Sum256([]byte(after))
	afterSHA := hex.EncodeToString(afterHash[:])
	changed := before != after
	result := SedFileResult{
		Path:         displayPath,
		Changed:      changed,
		DryRun:       opts.DryRun,
		EditCount:    len(opts.Edits),
		SHA256Before: beforeSHA,
		SHA256After:  afterSHA,
		Diff:         unifiedFullDiff(displayPath, before, after),
		Warnings:     warnings,
	}
	if changed && !opts.DryRun {
		if err := os.WriteFile(path, []byte(after), info.Mode().Perm()); err != nil {
			return SedFileResult{}, err
		}
	}
	return result, nil
}

func applyEdit(content string, edit Edit) (string, []string, error) {
	switch edit.Op {
	case "replace":
		if edit.Old == "" {
			return "", nil, errors.New("replace edit requires old")
		}
		count := edit.Count
		if count <= 0 {
			count = 1
		}
		if !strings.Contains(content, edit.Old) {
			return "", nil, fmt.Errorf("replace old text was not found")
		}
		matches := strings.Count(content, edit.Old)
		warnings := []string{}
		if matches > count {
			warnings = append(warnings, fmt.Sprintf("replace matched %d occurrences; replaced first %d", matches, count))
		}
		return replaceN(content, edit.Old, edit.New, count), warnings, nil
	case "insert_before", "insert_after":
		return insertAtLine(content, edit.Line, edit.Text, edit.Op == "insert_after")
	case "delete_range":
		return deleteRange(content, edit.StartLine, edit.EndLine)
	case "append":
		if content == "" || strings.HasSuffix(content, "\n") {
			return content + edit.Text, nil, nil
		}
		return content + "\n" + edit.Text, nil, nil
	default:
		return "", nil, fmt.Errorf("unsupported op %q", edit.Op)
	}
}

func replaceN(content, old, new string, count int) string {
	out := content
	for i := 0; i < count; i++ {
		index := strings.Index(out, old)
		if index < 0 {
			return out
		}
		out = out[:index] + new + out[index+len(old):]
	}
	return out
}

func insertAtLine(content string, line int, text string, after bool) (string, []string, error) {
	if line <= 0 {
		return "", nil, errors.New("line must be greater than zero")
	}
	lines, trailingNewline := editableLines(content)
	if line > len(lines) {
		return "", nil, fmt.Errorf("line %d is outside file line range 1-%d", line, len(lines))
	}
	insertIndex := line - 1
	if after {
		insertIndex = line
	}
	insertLines := splitEditText(text)
	next := append([]string{}, lines[:insertIndex]...)
	next = append(next, insertLines...)
	next = append(next, lines[insertIndex:]...)
	return joinEditableLines(next, trailingNewline), nil, nil
}

func deleteRange(content string, startLine, endLine int) (string, []string, error) {
	if startLine <= 0 || endLine <= 0 || endLine < startLine {
		return "", nil, errors.New("delete_range requires start_line and end_line with end_line >= start_line")
	}
	lines, trailingNewline := editableLines(content)
	if startLine > len(lines) {
		return "", nil, fmt.Errorf("start_line %d is outside file line range 1-%d", startLine, len(lines))
	}
	if endLine > len(lines) {
		return "", nil, fmt.Errorf("end_line %d is outside file line range 1-%d", endLine, len(lines))
	}
	next := append([]string{}, lines[:startLine-1]...)
	next = append(next, lines[endLine:]...)
	return joinEditableLines(next, trailingNewline), nil, nil
}

func resolveExistingPath(root, path string) (string, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", "", errors.New("path is required")
	}
	root, hasExplicitRoot, err := resolveRoot(root)
	if err != nil {
		return "", "", err
	}
	pathIsAbs := filepath.IsAbs(path)
	if pathIsAbs && !hasExplicitRoot {
		return "", "", fmt.Errorf("absolute paths are not allowed: %q", path)
	}
	clean := filepath.Clean(path)
	if !pathIsAbs && (clean == "." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || clean == "..") {
		return "", "", fmt.Errorf("path escapes repository root: %q", path)
	}
	joined := clean
	if !pathIsAbs {
		joined = filepath.Join(root, clean)
	}
	resolved, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("path escapes repository root: %q", path)
	}
	displayPath := filepath.ToSlash(rel)
	if pathIsAbs {
		displayPath = filepath.ToSlash(clean)
	}
	return resolved, displayPath, nil
}

func resolveRoot(root string) (string, bool, error) {
	root = strings.TrimSpace(root)
	hasExplicitRoot := root != ""
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", false, err
		}
		root = cwd
	}
	if !filepath.IsAbs(root) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", false, err
		}
		root = filepath.Join(cwd, root)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return "", false, err
	}
	return resolved, hasExplicitRoot, nil
}

func splitLines(content string) []string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 1024), maxReadBytes)
	lines := []string{}
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if content == "" {
		return lines
	}
	if strings.HasSuffix(content, "\n") && len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func editableLines(content string) ([]string, bool) {
	trailingNewline := strings.HasSuffix(content, "\n")
	trimmed := strings.TrimSuffix(content, "\n")
	if trimmed == "" {
		return []string{}, trailingNewline
	}
	return strings.Split(trimmed, "\n"), trailingNewline
}

func splitEditText(text string) []string {
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return []string{""}
	}
	return strings.Split(text, "\n")
}

func joinEditableLines(lines []string, trailingNewline bool) string {
	if len(lines) == 0 {
		if trailingNewline {
			return "\n"
		}
		return ""
	}
	out := strings.Join(lines, "\n")
	if trailingNewline {
		out += "\n"
	}
	return out
}

func unifiedFullDiff(path, before, after string) string {
	if before == after {
		return ""
	}
	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n+++ %s\n@@\n", path, path)
	for _, line := range splitLines(before) {
		fmt.Fprintf(&out, "-%s\n", line)
	}
	for _, line := range splitLines(after) {
		fmt.Fprintf(&out, "+%s\n", line)
	}
	return out.String()
}
