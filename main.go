package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	"localclash/internal/configrender"
	"localclash/internal/coredownload"
	"localclash/internal/corerun"
	"localclash/internal/subdownload"
)

const usage = `localclash

Usage:
  localclash core download [flags]
  localclash subscription download --url <url> [flags]
  localclash config render [flags]
  localclash run [flags]

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

Flags for config render:
  --source string   downloaded subscription source YAML (default "subscription.yaml")
  --policy string   localClash policy YAML (default "policies/loyalsoldier.yaml")
  --mode string     policy mode; empty means policy default
  --output string   generated Mihomo config path (default "generated/mihomo.yaml")
  --force          overwrite output if it exists

Flags for run:
  --core string     Mihomo core binary path (default "bin/mihomo")
  --config string   Mihomo runtime config path (default "generated/mihomo.yaml")
  --workdir string  Mihomo runtime data directory (default ".runtime/mihomo")
  --log string      Mihomo log file path (default "<workdir>/logs/mihomo-YYYY-MM-DD.log")
  --log-retention int
                 days of dated Mihomo logs to keep (default 7)
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
	if len(args) >= 2 && args[0] == "core" && args[1] == "download" {
		return runCoreDownload(args[2:])
	}
	if len(args) >= 2 && (args[0] == "subscription" || args[0] == "sub") && args[1] == "download" {
		return runSubscriptionDownload(args[2:])
	}
	if len(args) >= 2 && args[0] == "config" && args[1] == "render" {
		return runConfigRender(args[2:])
	}
	if len(args) >= 1 && args[0] == "run" {
		return runCore(args[1:])
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

func runConfigRender(args []string) error {
	fs := flag.NewFlagSet("config render", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := configrender.Options{}
	fs.StringVar(&opts.SourcePath, "source", "subscription.yaml", "downloaded subscription source YAML")
	fs.StringVar(&opts.PolicyPath, "policy", "policies/loyalsoldier.yaml", "localClash policy YAML")
	fs.StringVar(&opts.Mode, "mode", "", "policy mode; empty means policy default")
	fs.StringVar(&opts.OutputPath, "output", "generated/mihomo.yaml", "generated Mihomo config path")
	fs.BoolVar(&opts.Force, "force", false, "overwrite output if it exists")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	result, err := configrender.Render(opts)
	if err != nil {
		return err
	}

	fmt.Printf("rendered %s mode config to %s (%d proxies, %d rules)\n", result.Mode, result.OutputPath, result.ProxyCount, result.RuleCount)
	return nil
}

func runCore(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := corerun.Options{}
	fs.StringVar(&opts.CorePath, "core", "bin/mihomo", "Mihomo core binary path")
	fs.StringVar(&opts.ConfigPath, "config", "generated/mihomo.yaml", "Mihomo runtime config path")
	fs.StringVar(&opts.WorkDir, "workdir", ".runtime/mihomo", "Mihomo runtime data directory")
	fs.StringVar(&opts.LogPath, "log", "", "Mihomo log file path")
	fs.IntVar(&opts.LogRetentionDays, "log-retention", 7, "days of dated Mihomo logs to keep")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	return corerun.Run(opts)
}
