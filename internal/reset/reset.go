package reset

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"localclash/internal/corerun"
)

const ConfirmationPhrase = "reset localclash"

type Options struct {
	DryRun bool
	Yes    bool
	In     io.Reader
	Out    io.Writer
}

type Target struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Exists bool   `json:"exists"`
}

type Result struct {
	DryRun  bool     `json:"dry_run"`
	Deleted []Target `json:"deleted"`
	Skipped []Target `json:"skipped"`
}

func Run(opts Options) (Result, error) {
	if opts.In == nil {
		opts.In = os.Stdin
	}
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if !opts.DryRun {
		if status := corerun.Status(corerun.StatusOptions{WorkDir: filepath.Join(".runtime", "mihomo")}); status.Running {
			return Result{}, fmt.Errorf("mihomo runtime is running (pid %d); stop it before reset", status.PID)
		}
	}
	targets, err := BuildPlan()
	if err != nil {
		return Result{}, err
	}
	result := Result{DryRun: opts.DryRun}
	for _, target := range targets {
		if target.Exists {
			result.Deleted = append(result.Deleted, target)
		} else {
			result.Skipped = append(result.Skipped, target)
		}
	}
	printPlan(opts.Out, result)
	if opts.DryRun || len(result.Deleted) == 0 {
		return result, nil
	}
	if !opts.Yes {
		if err := confirm(opts.In, opts.Out); err != nil {
			return Result{}, err
		}
	}
	for _, target := range result.Deleted {
		if err := os.RemoveAll(target.Path); err != nil {
			return Result{}, fmt.Errorf("remove %s: %w", target.Path, err)
		}
	}
	fmt.Fprintln(opts.Out, "Reset complete.")
	return result, nil
}

func BuildPlan() ([]Target, error) {
	paths := []Target{
		{Path: ".runtime", Kind: "directory"},
		{Path: "generated", Kind: "directory"},
		{Path: "localclash.yaml", Kind: "file"},
		{Path: "localclash-packs.yaml", Kind: "file"},
		{Path: "localclash-subscriptions.yaml", Kind: "file"},
		{Path: "localclash-runtime.yaml", Kind: "file"},
		{Path: "profiles", Kind: "directory"},
	}
	subscriptions, err := filepath.Glob("subscription*.yaml")
	if err != nil {
		return nil, err
	}
	sort.Strings(subscriptions)
	for _, path := range subscriptions {
		paths = append(paths, Target{Path: path, Kind: "file"})
	}
	paths = dedupeTargets(paths)
	for i := range paths {
		exists, kind := inspect(paths[i].Path, paths[i].Kind)
		paths[i].Exists = exists
		paths[i].Kind = kind
	}
	return paths, nil
}

func dedupeTargets(targets []Target) []Target {
	seen := map[string]bool{}
	out := make([]Target, 0, len(targets))
	for _, target := range targets {
		clean := filepath.Clean(strings.TrimSpace(target.Path))
		if clean == "." || clean == string(filepath.Separator) || strings.HasPrefix(clean, "..") || seen[clean] {
			continue
		}
		target.Path = clean
		seen[clean] = true
		out = append(out, target)
	}
	return out
}

func inspect(path, fallbackKind string) (bool, string) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, fallbackKind
	}
	if info.IsDir() {
		return true, "directory"
	}
	return true, "file"
}

func printPlan(out io.Writer, result Result) {
	if result.DryRun {
		fmt.Fprintln(out, "localClash factory reset dry run.")
	} else {
		fmt.Fprintln(out, "localClash factory reset.")
	}
	fmt.Fprintln(out)
	if len(result.Deleted) == 0 {
		fmt.Fprintln(out, "Will delete: nothing")
	} else {
		fmt.Fprintln(out, "Will delete:")
		for _, target := range result.Deleted {
			fmt.Fprintf(out, "  - %s (%s)\n", target.Path, target.Kind)
		}
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Will keep:")
	fmt.Fprintln(out, "  - bin/")
	fmt.Fprintln(out, "  - policies/")
	fmt.Fprintln(out, "  - policy-templates/")
	fmt.Fprintln(out, "  - rule-sources/")
	fmt.Fprintln(out, "  - source code, docs, and scripts")
}

func confirm(in io.Reader, out io.Writer) error {
	fmt.Fprintf(out, "\nType %q to continue: ", ConfirmationPhrase)
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return err
		}
		return fmt.Errorf("reset cancelled")
	}
	if strings.TrimSpace(scanner.Text()) != ConfirmationPhrase {
		return fmt.Errorf("reset cancelled")
	}
	return nil
}
