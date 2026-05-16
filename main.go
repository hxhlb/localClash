package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"localclash/internal/appinit"
	"localclash/internal/configrender"
	"localclash/internal/coredownload"
	"localclash/internal/corerun"
	"localclash/internal/dashboard"
	"localclash/internal/doctor"
	"localclash/internal/mcp"
	"localclash/internal/rules"
	"localclash/internal/subdownload"
)

const usage = `localclash

Usage:
  localclash core download [flags]
  localclash subscription download --url <url> [flags]
  localclash dashboard download [flags]
  localclash config render [flags]
  localclash rules adapt [flags]
  localclash rules render [flags]
  localclash run [flags]
  localclash doctor [flags]
  localclash mcp

Flags for core download:
  --version string   GitHub release tag, or "latest" (default "latest")
  --os string        target OS (default current OS)
  --arch string      target arch (default current arch)
  --output string    output binary path (default bin/mihomo or bin/mihomo.exe)
  --repo string      GitHub repo owner/name (default "MetaCubeX/mihomo")
  --force           overwrite output if it exists
  --dry-run         print selected release asset without downloading

Flags for subscription download:
  --url string          subscription URL
  --output string       output file path (default subscription.yaml)
  --user-agent string   subscription User-Agent (default "clash-verge/v1.5.1")
  --force              overwrite output if it exists

Flags for dashboard download:
  --version string   zashboard GitHub release tag, or "latest" (default "latest")
  --asset string     zashboard release asset name (default "dist.zip")
  --output string    output directory (default ".runtime/mihomo/ui/zashboard")
  --repo string      GitHub repo owner/name (default "Zephyruso/zashboard")
  --force           replace output directory if it exists

Flags for config render:
  --source string   downloaded subscription source YAML (default "subscription.yaml")
  --policy string   localClash policy YAML (default "policies/loyalsoldier.yaml")
  --mode string     policy mode; empty means policy default
  --output string   generated Mihomo config path (default "generated/mihomo.yaml")
  --packs-selection string
                 packs selection YAML; optional
  --rules-cache string
                 runtime pack cache directory (default ".runtime/rules/packs")
  --force          overwrite output if it exists

Flags for rules adapt:
  --sources string  rule source YAML directory (default "rule-sources")
  --cache string    runtime pack cache directory (default ".runtime/rules/packs")

Flags for rules render:
  --selection string  packs selection YAML (default "localclash-packs.yaml")
  --subscription string
                    subscription YAML for node label classification (default "subscription.yaml")
  --cache string      runtime pack cache directory (default ".runtime/rules/packs")
  --output string     output rules fragment path, or "-" for stdout (default "-")

Flags for run:
  --core string     Mihomo core binary path (default "bin/mihomo")
  --config string   Mihomo runtime config path (default "generated/mihomo.yaml")
  --workdir string  Mihomo runtime data directory (default ".runtime/mihomo")
  --log string      Mihomo log file path (default "<workdir>/logs/mihomo-YYYY-MM-DD.log")
  --log-retention int
                 days of dated Mihomo logs to keep (default 7)

Flags for doctor:
  --core string          Mihomo core binary path (default "bin/mihomo")
  --subscription string  downloaded subscription YAML (default "subscription.yaml")
  --config string        generated Mihomo config path (default "generated/mihomo.yaml")
  --policy string        localClash policy YAML (default "policies/loyalsoldier.yaml")
  --dashboard string     zashboard directory (default ".runtime/mihomo/ui/zashboard")
  --workdir string       Mihomo runtime data directory for config test (default ".runtime/mihomo")
  --json                print machine-readable JSON report
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return nil
	}
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
		fmt.Print(usage)
		return nil
	}
	bootstrapCtx, bootstrapCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer bootstrapCancel()
	state := appinit.Bootstrap(bootstrapCtx, appinit.Options{})
	if len(args) >= 2 && args[0] == "core" && args[1] == "download" {
		return runCoreDownload(args[2:])
	}
	if len(args) >= 2 && (args[0] == "subscription" || args[0] == "sub") && args[1] == "download" {
		return runSubscriptionDownload(args[2:])
	}
	if len(args) >= 2 && args[0] == "dashboard" && args[1] == "download" {
		return runDashboardDownload(args[2:])
	}
	if len(args) >= 2 && args[0] == "config" && args[1] == "render" {
		return runConfigRender(args[2:], state)
	}
	if len(args) >= 1 && args[0] == "rules" {
		return runRules(args[1:], state)
	}
	if len(args) >= 1 && args[0] == "run" {
		return runCore(args[1:], state)
	}
	if len(args) >= 1 && args[0] == "doctor" {
		return runDoctor(args[1:], state)
	}
	if len(args) >= 1 && args[0] == "mcp" {
		return runMCP(args[1:], state)
	}
	return fmt.Errorf("unknown command %q\n\n%s", args[0], usage)
}

func runCoreDownload(args []string) error {
	fs := flag.NewFlagSet("core download", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := coredownload.Options{}
	fs.StringVar(&opts.Version, "version", "latest", "GitHub release tag, or latest")
	fs.StringVar(&opts.TargetOS, "os", runtime.GOOS, "target OS")
	fs.StringVar(&opts.TargetArch, "arch", runtime.GOARCH, "target arch")
	fs.StringVar(&opts.OutputPath, "output", "", "output binary path")
	fs.StringVar(&opts.Repo, "repo", "MetaCubeX/mihomo", "GitHub repo owner/name")
	fs.BoolVar(&opts.Force, "force", false, "overwrite output if it exists")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "print selected release asset without downloading")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	result, err := coredownload.Download(ctx, opts)
	if err != nil {
		return err
	}

	if opts.DryRun {
		fmt.Printf("release: %s\nasset: %s\nurl: %s\n", result.Version, result.AssetName, result.DownloadURL)
		return nil
	}

	fmt.Printf("downloaded %s (%s) to %s\n", result.AssetName, result.Version, result.OutputPath)
	return nil
}

func runSubscriptionDownload(args []string) error {
	fs := flag.NewFlagSet("subscription download", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := subdownload.Options{}
	fs.StringVar(&opts.URL, "url", "", "subscription URL")
	fs.StringVar(&opts.OutputPath, "output", "subscription.yaml", "output file path")
	fs.StringVar(&opts.UserAgent, "user-agent", "clash-verge/v1.5.1", "subscription User-Agent")
	fs.BoolVar(&opts.Force, "force", false, "overwrite output if it exists")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := subdownload.Download(ctx, opts)
	if err != nil {
		return err
	}

	fmt.Printf("downloaded subscription to %s (%d bytes)\n", result.OutputPath, result.BytesWritten)
	return nil
}

func runDashboardDownload(args []string) error {
	fs := flag.NewFlagSet("dashboard download", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := dashboard.Options{}
	fs.StringVar(&opts.Version, "version", "latest", "zashboard GitHub release tag, or latest")
	fs.StringVar(&opts.AssetName, "asset", "dist.zip", "zashboard release asset name")
	fs.StringVar(&opts.OutputDir, "output", ".runtime/mihomo/ui/zashboard", "output directory")
	fs.StringVar(&opts.Repo, "repo", "Zephyruso/zashboard", "GitHub repo owner/name")
	fs.BoolVar(&opts.Force, "force", false, "replace output directory if it exists")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	result, err := dashboard.Download(ctx, opts)
	if err != nil {
		return err
	}

	fmt.Printf("downloaded %s (%s) to %s\n", result.AssetName, result.Version, result.OutputDir)
	return nil
}

func runConfigRender(args []string, state appinit.RuntimeState) error {
	fs := flag.NewFlagSet("config render", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := configrender.Options{}
	fs.StringVar(&opts.SourcePath, "source", state.Paths.SubscriptionPath, "downloaded subscription source YAML")
	fs.StringVar(&opts.PolicyPath, "policy", state.Paths.PolicyPath, "localClash policy YAML")
	fs.StringVar(&opts.Mode, "mode", "", "policy mode; empty means policy default")
	fs.StringVar(&opts.OutputPath, "output", state.Paths.GeneratedConfig, "generated Mihomo config path")
	fs.StringVar(&opts.PacksSelectionPath, "packs-selection", "", "packs selection YAML; optional")
	fs.StringVar(&opts.RulesCacheDir, "rules-cache", state.Paths.RulesCacheDir, "runtime pack cache directory")
	fs.BoolVar(&opts.Force, "force", false, "overwrite output if it exists")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}
	if opts.OutputPath == state.Paths.GeneratedConfig && state.Config.Rendered {
		opts.Force = true
	}

	result, err := configrender.Render(opts)
	if err != nil {
		return err
	}

	fmt.Printf("rendered %s mode config to %s (%d proxies, %d rules)\n", result.Mode, result.OutputPath, result.ProxyCount, result.RuleCount)
	return nil
}

func runRules(args []string, state appinit.RuntimeState) error {
	if len(args) == 0 {
		return fmt.Errorf("rules subcommand is required: adapt or render")
	}
	switch args[0] {
	case "adapt":
		return runRulesAdapt(args[1:], state)
	case "render":
		return runRulesRender(args[1:], state)
	default:
		return fmt.Errorf("unknown rules subcommand %q", args[0])
	}
}

func runRulesAdapt(args []string, state appinit.RuntimeState) error {
	fs := flag.NewFlagSet("rules adapt", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := rules.Options{}
	fs.StringVar(&opts.SourcesDir, "sources", state.Paths.RuleSourcesDir, "rule source YAML directory")
	fs.StringVar(&opts.CacheDir, "cache", state.Paths.RulesCacheDir, "runtime pack cache directory")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	caches, err := rules.Adapt(ctx, opts)
	if err != nil {
		return err
	}
	for _, cache := range caches {
		fmt.Printf("adapted %s (%s): %d packs\n", cache.Source, cache.Adapter, len(cache.Packs))
	}
	return nil
}

func runRulesRender(args []string, state appinit.RuntimeState) error {
	fs := flag.NewFlagSet("rules render", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := rules.Options{}
	fs.StringVar(&opts.SelectionPath, "selection", "localclash-packs.yaml", "packs selection YAML")
	fs.StringVar(&opts.Subscription, "subscription", state.Paths.SubscriptionPath, "subscription YAML for node label classification")
	fs.StringVar(&opts.CacheDir, "cache", state.Paths.RulesCacheDir, "runtime pack cache directory")
	fs.StringVar(&opts.OutputPath, "output", "-", "output rules fragment path, or - for stdout")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	fragment, err := rules.Render(opts)
	if err != nil {
		return err
	}
	return rules.WriteFragment(opts.OutputPath, fragment)
}

func runCore(args []string, state appinit.RuntimeState) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := corerun.Options{}
	fs.StringVar(&opts.CorePath, "core", state.Paths.CorePath, "Mihomo core binary path")
	fs.StringVar(&opts.ConfigPath, "config", state.Paths.GeneratedConfig, "Mihomo runtime config path")
	fs.StringVar(&opts.WorkDir, "workdir", state.Paths.MihomoRuntimeDir, "Mihomo runtime data directory")
	fs.StringVar(&opts.LogPath, "log", "", "Mihomo log file path")
	fs.IntVar(&opts.LogRetentionDays, "log-retention", 7, "days of dated Mihomo logs to keep")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report, err := doctor.Run(ctx, doctor.Options{
		CorePath:         opts.CorePath,
		SubscriptionPath: state.Paths.SubscriptionPath,
		ConfigPath:       opts.ConfigPath,
		PolicyPath:       state.Paths.PolicyPath,
		DashboardDir:     ".runtime/mihomo/ui/zashboard",
		WorkDir:          opts.WorkDir,
	})
	if err != nil {
		return err
	}
	if !doctorReportOK(report) {
		return fmt.Errorf("doctor checks failed; run localclash doctor for details")
	}
	return corerun.Run(opts)
}

func runDoctor(args []string, state appinit.RuntimeState) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := doctor.Options{}
	fs.StringVar(&opts.CorePath, "core", state.Paths.CorePath, "Mihomo core binary path")
	fs.StringVar(&opts.SubscriptionPath, "subscription", state.Paths.SubscriptionPath, "downloaded subscription YAML")
	fs.StringVar(&opts.ConfigPath, "config", state.Paths.GeneratedConfig, "generated Mihomo config path")
	fs.StringVar(&opts.PolicyPath, "policy", state.Paths.PolicyPath, "localClash policy YAML")
	fs.StringVar(&opts.DashboardDir, "dashboard", filepath.Join(state.Paths.MihomoRuntimeDir, "ui", "zashboard"), "zashboard directory")
	fs.StringVar(&opts.WorkDir, "workdir", state.Paths.MihomoRuntimeDir, "Mihomo runtime data directory for config test")
	fs.BoolVar(&opts.JSON, "json", false, "print machine-readable JSON report")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	report, err := doctor.Run(ctx, opts)
	if err != nil {
		return err
	}
	if opts.JSON {
		return doctor.PrintJSON(report)
	}
	doctor.PrintText(report)
	return nil
}

func runMCP(args []string, state appinit.RuntimeState) error {
	if len(args) != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", args)
	}
	return mcp.ServeStdioWithState(context.Background(), state, os.Stdin, os.Stdout)
}

func doctorReportOK(report doctor.Report) bool {
	return report.Status != "fail"
}
