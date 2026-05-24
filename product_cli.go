package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"localclash/internal/appinit"
	"localclash/internal/configinspect"
	"localclash/internal/configrender"
	"localclash/internal/coredownload"
	"localclash/internal/corerun"
	"localclash/internal/dashboard"
	"localclash/internal/localconfig"
	"localclash/internal/policytemplate"
	"localclash/internal/reset"
	"localclash/internal/routertakeover"
	"localclash/internal/runtimeprofile"
	"localclash/internal/subscriptions"
)

type productEnvelope struct {
	OK          bool     `json:"ok"`
	Changed     bool     `json:"changed"`
	Summary     string   `json:"summary"`
	Status      any      `json:"status,omitempty"`
	Changes     []string `json:"changes"`
	Warnings    []string `json:"warnings"`
	NextActions []string `json:"next_actions"`
}

type productErrorEnvelope struct {
	OK          bool     `json:"ok"`
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	Details     any      `json:"details,omitempty"`
	NextActions []string `json:"next_actions"`
}

type codedProductError struct {
	code        string
	message     string
	details     any
	nextActions []string
}

func (err codedProductError) Error() string {
	return err.message
}

func runProductCommand(args []string, state appinit.RuntimeState) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	var err error
	switch args[0] {
	case "status":
		if hasFlag(args[1:], "json") {
			err = runProductStatus(args[1:], state)
		}
	case "subscription":
		if len(args) >= 2 && args[1] != "download" {
			err = runProductSubscription(args[1:], state)
		}
	case "component":
		err = runProductComponent(args[1:], state)
	case "config":
		if len(args) >= 2 && (args[1] == "status" || args[1] == "apply-template" || (args[1] == "render" && hasFlag(args[2:], "json"))) {
			err = runProductConfig(args[1:], state)
		}
	case "runtime":
		err = runProductRuntime(args[1:], state)
	case "takeover":
		err = runProductTakeover(args[1:], state)
	case "apply":
		err = runProductApply(args[1:], state)
	case "reset":
		if hasFlag(args[1:], "json") {
			err = runProductReset(args[1:])
		}
	case "mcp":
		if len(args) >= 2 && args[1] == "serve" {
			err = runMCP(args[2:], state)
		}
	}
	if err == nil {
		return productCommandWasHandled(args), nil
	}
	if productCommandWasHandled(args) {
		_ = printProductError(err)
		return true, err
	}
	return false, nil
}

func productCommandWasHandled(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "component", "runtime", "takeover", "apply":
		return true
	case "status":
		return hasFlag(args[1:], "json")
	case "subscription":
		return len(args) >= 2 && args[1] != "download"
	case "config":
		return len(args) >= 2 && (args[1] == "status" || args[1] == "apply-template" || (args[1] == "render" && hasFlag(args[2:], "json")))
	case "reset":
		return hasFlag(args[1:], "json")
	case "mcp":
		return len(args) >= 2 && args[1] == "serve"
	default:
		return false
	}
}

func runProductStatus(args []string, state appinit.RuntimeState) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var asJSON bool
	fs.BoolVar(&asJSON, "json", false, "print product JSON status")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !asJSON || fs.NArg() != 0 {
		return fmt.Errorf("usage: localclash status --json")
	}
	status, warnings := productStatus(state)
	return printProductOK(productEnvelope{
		OK:       true,
		Changed:  false,
		Summary:  "localClash product status read.",
		Status:   status,
		Warnings: warnings,
		Changes:  []string{},
	})
}

func runProductSubscription(args []string, state appinit.RuntimeState) error {
	if len(args) == 0 {
		return fmt.Errorf("subscription subcommand is required: status, set, or refresh")
	}
	switch args[0] {
	case "status":
		if err := parseJSONOnly("subscription status", args[1:]); err != nil {
			return err
		}
		result, err := subscriptions.Status(subscriptionStatusOptions(state))
		if err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Summary: "Subscription status read.", Status: result, Changes: []string{}, Warnings: []string{}})
	case "set":
		input, err := parseSubscriptionInput(args[1:])
		if err != nil {
			return err
		}
		sources, err := sourcesFromURLs(input.URLs)
		if err != nil {
			return err
		}
		replace := true
		result, err := subscriptions.Configure(subscriptions.ConfigureOptions{
			ConfigPath: state.Paths.SubscriptionConfig,
			Sources:    sources,
			Replace:    &replace,
		})
		if err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Changed: true, Summary: "Subscription sources configured.", Status: result, Changes: []string{"subscription_sources_replaced"}, Warnings: []string{}})
	case "refresh":
		if err := parseJSONOnly("subscription refresh", args[1:]); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		result, err := subscriptions.Refresh(ctx, subscriptions.RefreshOptions{
			ConfigPath: state.Paths.SubscriptionConfig,
			RuntimeDir: state.Paths.SubscriptionRuntime,
			MergedPath: state.Paths.SubscriptionPath,
			Force:      true,
			UserAgent:  subscriptions.DefaultUserAgent,
		})
		if err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Changed: true, Summary: "Subscription artifacts refreshed.", Status: result, Changes: []string{"subscriptions_refreshed"}, Warnings: result.Warnings})
	default:
		return fmt.Errorf("unknown subscription subcommand %q", args[0])
	}
}

func runProductComponent(args []string, state appinit.RuntimeState) error {
	if len(args) == 0 {
		return fmt.Errorf("component subcommand is required: status or update")
	}
	switch args[0] {
	case "status":
		if err := parseJSONOnly("component status", args[1:]); err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Summary: "Component status read.", Status: componentStatus(state), Changes: []string{}, Warnings: []string{}})
	case "update":
		return runProductComponentUpdate(args[1:])
	default:
		return fmt.Errorf("unknown component subcommand %q", args[0])
	}
}

func runProductComponentUpdate(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("component update requires component name")
	}
	component := args[0]
	if err := parseJSONOnly("component update "+component, args[1:]); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	switch component {
	case "localclash":
		return codedProductError{
			code:        "localclash_component_update_helper_owned",
			message:     "localClash core install/update is owned by the LuCI helper when the core is missing; Go self-update is not implemented yet.",
			nextActions: []string{"Use the LuCI helper bootstrap_core method to install or update /usr/local/bin/localclash."},
		}
	case "mihomo":
		result, err := coredownload.Download(ctx, coredownload.Options{
			Version:    "latest",
			Flavor:     coredownload.FlavorAll,
			Target:     coredownload.TargetRouter,
			TargetOS:   "linux",
			TargetArch: runtime.GOARCH,
			OutputDir:  "bin",
			Repo:       "MetaCubeX/mihomo",
			Force:      true,
		})
		if err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Changed: true, Summary: "Mihomo components updated.", Status: result, Changes: []string{"mihomo_updated"}, Warnings: []string{}})
	case "dashboard":
		result, err := dashboard.Download(ctx, dashboard.Options{
			Version:   "latest",
			AssetName: "dist.zip",
			OutputDir: filepath.Join(".runtime", "mihomo", "ui", "zashboard"),
			Repo:      "Zephyruso/zashboard",
			Force:     true,
		})
		if err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Changed: true, Summary: "Dashboard assets updated.", Status: result, Changes: []string{"dashboard_updated"}, Warnings: []string{}})
	default:
		return fmt.Errorf("unknown component %q", component)
	}
}

func runProductConfig(args []string, state appinit.RuntimeState) error {
	if len(args) == 0 {
		return fmt.Errorf("config subcommand is required: status, apply-template, or render")
	}
	switch args[0] {
	case "status":
		if err := parseJSONOnly("config status", args[1:]); err != nil {
			return err
		}
		status, warnings := configStatus(state)
		return printProductOK(productEnvelope{OK: true, Summary: "Config status read.", Status: status, Changes: []string{}, Warnings: warnings})
	case "apply-template":
		input, err := parseConfigInput(args[1:])
		if err != nil {
			return err
		}
		result, err := applyTemplateInput(input, state)
		if err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Changed: true, Summary: "Config template applied.", Status: result, Changes: []string{"config_template_applied"}, Warnings: []string{}})
	case "render":
		if err := parseJSONOnly("config render", args[1:]); err != nil {
			return err
		}
		result, warnings, err := renderProductConfig(state)
		if err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Changed: true, Summary: "Generated Mihomo config rendered.", Status: result, Changes: []string{"config_rendered"}, Warnings: warnings})
	default:
		return fmt.Errorf("unknown config subcommand %q", args[0])
	}
}

func runProductRuntime(args []string, state appinit.RuntimeState) error {
	if len(args) == 0 {
		return fmt.Errorf("runtime subcommand is required: status, start, restart, or stop")
	}
	if err := parseJSONOnly("runtime "+args[0], args[1:]); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	switch args[0] {
	case "status":
		result := corerun.Status(runtimeStatusOptions(state))
		return printProductOK(productEnvelope{OK: true, Summary: "Runtime status read.", Status: result, Changes: []string{}, Warnings: []string{}})
	case "start":
		result, err := corerun.Start(ctx, runtimeStartOptions(state))
		if err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Changed: result.Started, Summary: "Runtime start completed.", Status: result, Changes: changedIf(result.Started, "runtime_started"), Warnings: result.Warnings, NextActions: result.NextActions})
	case "restart":
		result, err := corerun.Restart(ctx, runtimeRestartOptions(state))
		if err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Changed: result.Restarted, Summary: "Runtime restart completed.", Status: result, Changes: changedIf(result.Restarted, "runtime_restarted"), Warnings: result.Warnings, NextActions: result.NextActions})
	case "stop":
		result, err := corerun.Stop(corerun.StopOptions{WorkDir: state.Paths.MihomoRuntimeDir, Timeout: 5 * time.Second})
		if err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Changed: result.Stopped || result.RemovedPIDFile, Summary: "Runtime stop completed.", Status: result, Changes: changedIf(result.Stopped || result.RemovedPIDFile, "runtime_stopped"), Warnings: result.Warnings, NextActions: result.NextActions})
	default:
		return fmt.Errorf("unknown runtime subcommand %q", args[0])
	}
}

func runProductTakeover(args []string, state appinit.RuntimeState) error {
	if len(args) == 0 {
		return fmt.Errorf("takeover subcommand is required: status, apply, or stop")
	}
	if err := parseJSONOnly("takeover "+args[0], args[1:]); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	opts := routerTakeoverOptions(state)
	switch args[0] {
	case "status":
		result, err := routertakeover.Status(ctx, opts)
		if err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Summary: "Router takeover status read.", Status: result, Changes: []string{}, Warnings: result.Warnings, NextActions: result.NextActions})
	case "apply":
		result, err := routertakeover.Apply(ctx, opts)
		if err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Changed: result.Applied, Summary: "Router takeover apply completed.", Status: result, Changes: changedIf(result.Applied, "takeover_applied"), Warnings: result.Warnings, NextActions: result.NextActions})
	case "stop":
		result, err := routertakeover.Stop(ctx, opts)
		if err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Changed: result.Stopped, Summary: "Router takeover stop completed.", Status: result, Changes: changedIf(result.Stopped, "takeover_stopped"), Warnings: result.Warnings, NextActions: result.NextActions})
	default:
		return fmt.Errorf("unknown takeover subcommand %q", args[0])
	}
}

type desiredStateInput struct {
	Version       int                   `json:"version"`
	Mode          string                `json:"mode"`
	Subscriptions *desiredSubscriptions `json:"subscriptions,omitempty"`
	Components    *desiredComponents    `json:"components,omitempty"`
	Config        *desiredConfig        `json:"config,omitempty"`
	Runtime       *desiredRuntime       `json:"runtime,omitempty"`
}

type desiredSubscriptions struct {
	URLs    []string `json:"urls,omitempty"`
	Refresh *bool    `json:"refresh,omitempty"`
}

type desiredComponents struct {
	LocalClash string `json:"localclash,omitempty"`
	Mihomo     string `json:"mihomo,omitempty"`
	Dashboard  string `json:"dashboard,omitempty"`
}

type desiredConfig struct {
	Template               string `json:"template,omitempty"`
	RuntimeProfile         string `json:"runtime_profile,omitempty"`
	Core                   string `json:"core,omitempty"`
	AllowOverwriteModified *bool  `json:"allow_overwrite_modified,omitempty"`
}

type desiredRuntime struct {
	Service        string `json:"service,omitempty"`
	RouterTakeover string `json:"router_takeover,omitempty"`
}

func runProductApply(args []string, state appinit.RuntimeState) error {
	var input desiredStateInput
	if err := parseInputJSON("apply", args, &input); err != nil {
		return err
	}
	if err := validateDesiredState(input); err != nil {
		return err
	}
	if input.Mode == "preview" {
		status, warnings := productStatus(state)
		return printProductOK(productEnvelope{
			OK:          true,
			Changed:     false,
			Summary:     "Apply preview generated. No runtime state was changed.",
			Status:      map[string]any{"desired": input, "current": status},
			Changes:     desiredChanges(input),
			Warnings:    warnings,
			NextActions: []string{"Run apply with mode=execute after user confirmation."},
		})
	}
	changes, warnings, err := executeDesiredState(input, state)
	if err != nil {
		return err
	}
	status, statusWarnings := productStatus(state)
	warnings = append(warnings, statusWarnings...)
	return printProductOK(productEnvelope{
		OK:       true,
		Changed:  len(changes) > 0,
		Summary:  "Desired state applied.",
		Status:   status,
		Changes:  changes,
		Warnings: warnings,
	})
}

func validateDesiredState(input desiredStateInput) error {
	if input.Version != 1 {
		return fmt.Errorf("version must be 1")
	}
	if input.Mode != "preview" && input.Mode != "execute" {
		return fmt.Errorf("mode must be preview or execute")
	}
	if input.Components != nil {
		if err := validateLeaveOrInstalled("components.localclash", input.Components.LocalClash); err != nil {
			return err
		}
		if err := validateLeaveOrInstalled("components.mihomo", input.Components.Mihomo); err != nil {
			return err
		}
		if err := validateLeaveOrInstalled("components.dashboard", input.Components.Dashboard); err != nil {
			return err
		}
	}
	if input.Config != nil {
		if err := validateOneOf("config.template", input.Config.Template, "leave", policytemplate.TemplateMinimal, policytemplate.TemplateLocalClashDefault); err != nil {
			return err
		}
		if err := validateOneOf("config.runtime_profile", input.Config.RuntimeProfile, "leave", runtimeprofile.ModeNormal, runtimeprofile.ModeRouter); err != nil {
			return err
		}
		if err := validateOneOf("config.core", input.Config.Core, "leave", runtimeprofile.CoreMeta, runtimeprofile.CoreSmart); err != nil {
			return err
		}
	}
	if input.Runtime != nil {
		if err := validateOneOf("runtime.service", input.Runtime.Service, "leave", "start", "restart", "restart_if_needed", "stop"); err != nil {
			return err
		}
		if err := validateOneOf("runtime.router_takeover", input.Runtime.RouterTakeover, "leave", "enabled", "disabled"); err != nil {
			return err
		}
	}
	if input.Subscriptions != nil && len(input.Subscriptions.URLs) > 0 {
		if _, err := sourcesFromURLs(input.Subscriptions.URLs); err != nil {
			return err
		}
	}
	return nil
}

func validateLeaveOrInstalled(field, value string) error {
	return validateOneOf(field, value, "leave", "installed_or_latest")
}

func validateOneOf(field, value string, allowed ...string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}
	return fmt.Errorf("%s must be one of: %s", field, strings.Join(allowed, ", "))
}

func executeDesiredState(input desiredStateInput, state appinit.RuntimeState) ([]string, []string, error) {
	changes := []string{}
	warnings := []string{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if input.Subscriptions != nil && len(input.Subscriptions.URLs) > 0 {
		sources, err := sourcesFromURLs(input.Subscriptions.URLs)
		if err != nil {
			return changes, warnings, err
		}
		replace := true
		if _, err := subscriptions.Configure(subscriptions.ConfigureOptions{ConfigPath: state.Paths.SubscriptionConfig, Sources: sources, Replace: &replace}); err != nil {
			return changes, warnings, err
		}
		changes = append(changes, "subscription_sources_replaced")
	}
	if input.Subscriptions != nil && input.Subscriptions.Refresh != nil && *input.Subscriptions.Refresh {
		result, err := subscriptions.Refresh(ctx, subscriptions.RefreshOptions{
			ConfigPath: state.Paths.SubscriptionConfig,
			RuntimeDir: state.Paths.SubscriptionRuntime,
			MergedPath: state.Paths.SubscriptionPath,
			Force:      true,
			UserAgent:  subscriptions.DefaultUserAgent,
		})
		if err != nil {
			return changes, warnings, err
		}
		warnings = append(warnings, result.Warnings...)
		changes = append(changes, "subscriptions_refreshed")
	}
	if input.Components != nil {
		if input.Components.LocalClash == "installed_or_latest" {
			warnings = append(warnings, "localClash core update is owned by the LuCI helper/bootstrap layer in V1.")
		}
		if input.Components.Mihomo == "installed_or_latest" {
			if _, err := coredownload.Download(ctx, coredownload.Options{Version: "latest", Flavor: coredownload.FlavorAll, Target: coredownload.TargetRouter, TargetOS: "linux", TargetArch: runtime.GOARCH, OutputDir: "bin", Repo: "MetaCubeX/mihomo", Force: true}); err != nil {
				return changes, warnings, err
			}
			changes = append(changes, "mihomo_updated")
		}
		if input.Components.Dashboard == "installed_or_latest" {
			if _, err := dashboard.Download(ctx, dashboard.Options{Version: "latest", AssetName: "dist.zip", OutputDir: filepath.Join(".runtime", "mihomo", "ui", "zashboard"), Repo: "Zephyruso/zashboard", Force: true}); err != nil {
				return changes, warnings, err
			}
			changes = append(changes, "dashboard_updated")
		}
	}
	configChanged, err := executeDesiredConfig(input.Config, state)
	if err != nil {
		return changes, warnings, err
	}
	if configChanged {
		changes = append(changes, "config_updated")
	}
	if configChanged || contains(changes, "subscriptions_refreshed") {
		_, renderWarnings, err := renderProductConfig(state)
		if err != nil {
			return changes, warnings, err
		}
		warnings = append(warnings, renderWarnings...)
		changes = append(changes, "config_rendered")
	}
	runtimeChanges, runtimeWarnings, err := executeDesiredRuntime(input.Runtime, state)
	if err != nil {
		return changes, warnings, err
	}
	changes = append(changes, runtimeChanges...)
	warnings = append(warnings, runtimeWarnings...)
	return changes, warnings, nil
}

func executeDesiredConfig(input *desiredConfig, state appinit.RuntimeState) (bool, error) {
	if input == nil {
		return false, nil
	}
	template := emptyAsLeave(input.Template)
	mode := emptyAsLeave(input.RuntimeProfile)
	core := emptyAsLeave(input.Core)
	if template == "leave" && mode == "leave" && core == "leave" {
		return false, nil
	}
	if mode == "leave" || core == "leave" {
		profile, err := runtimeprofile.StatusFor(state.Paths.RuntimeProfilePath)
		if err != nil {
			return false, err
		}
		if mode == "leave" {
			mode = profile.Mode
		}
		if core == "leave" {
			core = profile.Core
		}
	}
	if template != "leave" {
		allow := false
		if input.AllowOverwriteModified != nil {
			allow = *input.AllowOverwriteModified
		}
		_, err := applyTemplateInput(configInput{Version: 1, Template: template, RuntimeProfile: mode, Core: core, AllowOverwriteModified: allow}, state)
		return true, err
	}
	_, err := runtimeprofile.Configure(state.Paths.RuntimeProfilePath, mode, core)
	return err == nil, err
}

func executeDesiredRuntime(input *desiredRuntime, state appinit.RuntimeState) ([]string, []string, error) {
	if input == nil {
		return nil, nil, nil
	}
	changes := []string{}
	warnings := []string{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	service := emptyAsLeave(input.Service)
	switch service {
	case "start":
		result, err := corerun.Start(ctx, runtimeStartOptions(state))
		if err != nil {
			return changes, warnings, err
		}
		warnings = append(warnings, result.Warnings...)
		if result.Started {
			changes = append(changes, "runtime_started")
		}
	case "restart":
		result, err := corerun.Restart(ctx, runtimeRestartOptions(state))
		if err != nil {
			return changes, warnings, err
		}
		warnings = append(warnings, result.Warnings...)
		if result.Restarted {
			changes = append(changes, "runtime_restarted")
		}
	case "restart_if_needed":
		status := corerun.Status(runtimeStatusOptions(state))
		if status.Running {
			result, err := corerun.Restart(ctx, runtimeRestartOptions(state))
			if err != nil {
				return changes, warnings, err
			}
			warnings = append(warnings, result.Warnings...)
			if result.Restarted {
				changes = append(changes, "runtime_restarted")
			}
		} else {
			result, err := corerun.Start(ctx, runtimeStartOptions(state))
			if err != nil {
				return changes, warnings, err
			}
			warnings = append(warnings, result.Warnings...)
			if result.Started {
				changes = append(changes, "runtime_started")
			}
		}
	case "stop":
		result, err := corerun.Stop(corerun.StopOptions{WorkDir: state.Paths.MihomoRuntimeDir, Timeout: 5 * time.Second})
		if err != nil {
			return changes, warnings, err
		}
		warnings = append(warnings, result.Warnings...)
		if result.Stopped || result.RemovedPIDFile {
			changes = append(changes, "runtime_stopped")
		}
	}
	takeover := emptyAsLeave(input.RouterTakeover)
	switch takeover {
	case "enabled":
		result, err := routertakeover.Apply(ctx, routerTakeoverOptions(state))
		if err != nil {
			return changes, warnings, err
		}
		warnings = append(warnings, result.Warnings...)
		if result.Applied {
			changes = append(changes, "takeover_applied")
		}
	case "disabled":
		result, err := routertakeover.Stop(ctx, routerTakeoverOptions(state))
		if err != nil {
			return changes, warnings, err
		}
		warnings = append(warnings, result.Warnings...)
		if result.Stopped {
			changes = append(changes, "takeover_stopped")
		}
	}
	return changes, warnings, nil
}

func desiredChanges(input desiredStateInput) []string {
	changes := []string{}
	if input.Subscriptions != nil {
		if len(input.Subscriptions.URLs) > 0 {
			changes = append(changes, "subscription_sources_replace")
		}
		if input.Subscriptions.Refresh != nil && *input.Subscriptions.Refresh {
			changes = append(changes, "subscriptions_refresh")
		}
	}
	if input.Components != nil {
		if input.Components.LocalClash == "installed_or_latest" {
			changes = append(changes, "localclash_install_or_update")
		}
		if input.Components.Mihomo == "installed_or_latest" {
			changes = append(changes, "mihomo_install_or_update")
		}
		if input.Components.Dashboard == "installed_or_latest" {
			changes = append(changes, "dashboard_install_or_update")
		}
	}
	if input.Config != nil && (emptyAsLeave(input.Config.Template) != "leave" || emptyAsLeave(input.Config.RuntimeProfile) != "leave" || emptyAsLeave(input.Config.Core) != "leave") {
		changes = append(changes, "config_update")
	}
	if input.Runtime != nil {
		if emptyAsLeave(input.Runtime.Service) != "leave" {
			changes = append(changes, "runtime_"+input.Runtime.Service)
		}
		if emptyAsLeave(input.Runtime.RouterTakeover) != "leave" {
			changes = append(changes, "takeover_"+input.Runtime.RouterTakeover)
		}
	}
	return changes
}

func runProductReset(args []string) error {
	if err := parseJSONOnly("reset", args); err != nil {
		return err
	}
	result, err := reset.Run(reset.Options{Yes: true, Out: io.Discard})
	if err != nil {
		return err
	}
	return printProductOK(productEnvelope{OK: true, Changed: len(result.Deleted) > 0, Summary: "Reset completed.", Status: result, Changes: changedIf(len(result.Deleted) > 0, "reset_completed"), Warnings: []string{}})
}

type subscriptionInput struct {
	Version int      `json:"version"`
	URLs    []string `json:"urls"`
}

type configInput struct {
	Version                int    `json:"version"`
	Template               string `json:"template"`
	RuntimeProfile         string `json:"runtime_profile"`
	Core                   string `json:"core"`
	AllowOverwriteModified bool   `json:"allow_overwrite_modified"`
}

func parseSubscriptionInput(args []string) (subscriptionInput, error) {
	var input subscriptionInput
	if err := parseInputJSON("subscription set", args, &input); err != nil {
		return input, err
	}
	if input.Version != 1 {
		return input, fmt.Errorf("version must be 1")
	}
	if len(input.URLs) == 0 {
		return input, fmt.Errorf("urls must contain at least one URL")
	}
	return input, nil
}

func parseConfigInput(args []string) (configInput, error) {
	var input configInput
	if err := parseInputJSON("config apply-template", args, &input); err != nil {
		return input, err
	}
	if input.Version != 1 {
		return input, fmt.Errorf("version must be 1")
	}
	if input.Template != policytemplate.TemplateMinimal && input.Template != policytemplate.TemplateLocalClashDefault {
		return input, fmt.Errorf("template must be minimal or localclash-default")
	}
	if input.RuntimeProfile != runtimeprofile.ModeNormal && input.RuntimeProfile != runtimeprofile.ModeRouter {
		return input, fmt.Errorf("runtime_profile must be normal or router")
	}
	if input.Core != runtimeprofile.CoreMeta && input.Core != runtimeprofile.CoreSmart {
		return input, fmt.Errorf("core must be meta or smart")
	}
	return input, nil
}

func parseInputJSON(name string, args []string, dest any) error {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	inputPath := fs.String("input", "", "input JSON path")
	asJSON := fs.Bool("json", false, "print product JSON response")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*asJSON || fs.NArg() != 0 || strings.TrimSpace(*inputPath) == "" {
		return fmt.Errorf("usage: localclash %s --input <file> --json", name)
	}
	data, err := os.ReadFile(*inputPath)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("input JSON must contain exactly one object")
	}
	return nil
}

func parseJSONOnly(name string, args []string) error {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "print product JSON response")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*asJSON || fs.NArg() != 0 {
		return fmt.Errorf("usage: localclash %s --json", name)
	}
	return nil
}

func sourcesFromURLs(rawURLs []string) ([]subscriptions.Source, error) {
	seen := map[string]bool{}
	sources := make([]subscriptions.Source, 0, len(rawURLs))
	for i, raw := range rawURLs {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return nil, fmt.Errorf("urls[%d] must not be empty", i)
		}
		parsed, err := url.Parse(trimmed)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return nil, fmt.Errorf("urls[%d] must be an absolute http or https URL", i)
		}
		if seen[trimmed] {
			return nil, fmt.Errorf("duplicate subscription URL at urls[%d]", i)
		}
		seen[trimmed] = true
		sources = append(sources, subscriptions.Source{ID: fmt.Sprintf("sub-%d", i+1), URL: trimmed})
	}
	return sources, nil
}

func productStatus(state appinit.RuntimeState) (map[string]any, []string) {
	warnings := append([]string{}, diagnosticsToWarnings(state.Diagnostics)...)
	subStatus, err := subscriptions.Status(subscriptionStatusOptions(state))
	if err != nil {
		warnings = append(warnings, err.Error())
	}
	cfgStatus, cfgWarnings := configStatus(state)
	warnings = append(warnings, cfgWarnings...)
	runtimeStatus := corerun.Status(runtimeStatusOptions(state))
	return map[string]any{
		"bootstrap":    state,
		"subscription": subStatus,
		"components":   componentStatus(state),
		"config":       cfgStatus,
		"runtime":      runtimeStatus,
	}, warnings
}

func componentStatus(state appinit.RuntimeState) map[string]any {
	exe, _ := os.Executable()
	dashboardPath := filepath.Join(state.Paths.MihomoRuntimeDir, "ui", "zashboard")
	return map[string]any{
		"localclash": map[string]any{
			"path":      exe,
			"installed": exe != "",
		},
		"mihomo": map[string]any{
			"path":      state.Core.Path,
			"installed": state.Core.Exists,
			"missing":   state.Core.Missing,
			"version":   state.Core.Version,
		},
		"dashboard": map[string]any{
			"path":      dashboardPath,
			"installed": pathExistsAny(dashboardPath),
		},
	}
}

func configStatus(state appinit.RuntimeState) (map[string]any, []string) {
	warnings := []string{}
	intent, err := configinspect.InspectIntent(configinspect.IntentOptions{
		ConfigPath:          "localclash.yaml",
		Subscription:        state.Paths.SubscriptionPath,
		SubscriptionConfig:  state.Paths.SubscriptionConfig,
		SubscriptionRuntime: state.Paths.SubscriptionRuntime,
		RulesCache:          state.Paths.RulesCacheDir,
		Limit:               8,
	})
	if err != nil {
		warnings = append(warnings, err.Error())
	}
	profile, err := runtimeprofile.StatusFor(state.Paths.RuntimeProfilePath)
	if err != nil {
		warnings = append(warnings, err.Error())
	}
	return map[string]any{
		"intent":          intent,
		"runtime_profile": profile,
		"generated": map[string]any{
			"path":   state.Paths.GeneratedConfig,
			"exists": pathExists(state.Paths.GeneratedConfig),
		},
	}, warnings
}

func applyTemplateInput(input configInput, state appinit.RuntimeState) (map[string]any, error) {
	if !input.AllowOverwriteModified {
		current, err := localconfig.Load("localclash.yaml")
		if err == nil && current.PolicyTemplate != "" && current.PolicyTemplate != input.Template {
			return nil, codedProductError{
				code:        "modified_config_requires_confirmation",
				message:     "Current localclash.yaml does not match the requested template; refusing to overwrite without allow_overwrite_modified.",
				nextActions: []string{"Set allow_overwrite_modified to true after user confirmation."},
				details: map[string]string{
					"current_policy_template": current.PolicyTemplate,
					"requested_template":      input.Template,
				},
			}
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	config, template, err := policytemplate.Build(policytemplate.DefaultDir, input.Template)
	if err != nil {
		return nil, err
	}
	if err := localconfig.Write("localclash.yaml", config); err != nil {
		return nil, err
	}
	profile, err := runtimeprofile.Configure(state.Paths.RuntimeProfilePath, input.RuntimeProfile, input.Core)
	if err != nil {
		return nil, err
	}
	return map[string]any{"template": template, "runtime_profile": profile}, nil
}

func subscriptionStatusOptions(state appinit.RuntimeState) subscriptions.StatusOptions {
	return subscriptions.StatusOptions{
		ConfigPath: state.Paths.SubscriptionConfig,
		MergedPath: state.Paths.SubscriptionPath,
		RuntimeDir: state.Paths.SubscriptionRuntime,
	}
}

func configRenderOptions(state appinit.RuntimeState) configrender.Options {
	return configrender.Options{
		SourcePath:         state.Paths.SubscriptionPath,
		PolicyPath:         state.Paths.PolicyPath,
		OutputPath:         state.Paths.GeneratedConfig,
		PacksSelectionPath: state.Paths.PacksSelectionPath,
		RulesCacheDir:      state.Paths.RulesCacheDir,
		RuntimeProfilePath: state.Paths.RuntimeProfilePath,
		Force:              true,
	}
}

func renderProductConfig(state appinit.RuntimeState) (map[string]any, []string, error) {
	opts := configRenderOptions(state)
	selectionPath := ""
	source := "base"
	warnings := []string{}

	if pathExists("localclash.yaml") {
		config, err := localconfig.Load("localclash.yaml")
		if err != nil {
			return nil, nil, err
		}
		resolved, err := localconfig.Resolve(localconfig.ResolveOptions{
			Config:              config,
			SubscriptionPath:    state.Paths.SubscriptionPath,
			SubscriptionConfig:  state.Paths.SubscriptionConfig,
			SubscriptionRuntime: state.Paths.SubscriptionRuntime,
			RulesCache:          state.Paths.RulesCacheDir,
		})
		if err != nil {
			return nil, nil, err
		}
		selectionPath = state.Paths.PacksSelectionPath
		if strings.TrimSpace(selectionPath) == "" {
			selectionPath = "localclash-packs.yaml"
		}
		if err := localconfig.WriteSelection(selectionPath, resolved.Selection); err != nil {
			return nil, nil, err
		}
		opts.PacksSelectionPath = selectionPath
		source = "durable_state"
		warnings = append(warnings, resolved.Warnings...)
	}

	result, err := configrender.Render(opts)
	if err != nil {
		return nil, nil, err
	}
	return map[string]any{
		"render":          result,
		"source":          source,
		"source_of_truth": "localclash.yaml",
		"selection":       selectionPath,
		"output":          state.Paths.GeneratedConfig,
	}, warnings, nil
}

func runtimeStatusOptions(state appinit.RuntimeState) corerun.StatusOptions {
	return corerun.StatusOptions{CorePath: state.Paths.CorePath, ConfigPath: state.Paths.GeneratedConfig, WorkDir: state.Paths.MihomoRuntimeDir}
}

func runtimeStartOptions(state appinit.RuntimeState) corerun.StartOptions {
	return corerun.StartOptions{CorePath: state.Paths.CorePath, ConfigPath: state.Paths.GeneratedConfig, WorkDir: state.Paths.MihomoRuntimeDir}
}

func runtimeRestartOptions(state appinit.RuntimeState) corerun.RestartOptions {
	return corerun.RestartOptions{CorePath: state.Paths.CorePath, ConfigPath: state.Paths.GeneratedConfig, WorkDir: state.Paths.MihomoRuntimeDir, StopTimeout: 5 * time.Second}
}

func routerTakeoverOptions(state appinit.RuntimeState) routertakeover.Options {
	return routertakeover.Options{RuntimeProfile: state.Paths.RuntimeProfilePath, ConfigPath: state.Paths.GeneratedConfig, RuntimeDir: state.Paths.MihomoRuntimeDir}
}

func printProductOK(envelope productEnvelope) error {
	if envelope.Changes == nil {
		envelope.Changes = []string{}
	}
	if envelope.Warnings == nil {
		envelope.Warnings = []string{}
	}
	if envelope.NextActions == nil {
		envelope.NextActions = []string{}
	}
	return printJSON(envelope)
}

func printProductError(err error) error {
	code := "command_failed"
	message := err.Error()
	var details any
	var next []string
	var coded codedProductError
	if errors.As(err, &coded) {
		code = coded.code
		message = coded.message
		details = coded.details
		next = coded.nextActions
	}
	return printJSON(productErrorEnvelope{OK: false, Code: code, Message: message, Details: details, NextActions: next})
}

func hasFlag(args []string, name string) bool {
	want := "--" + name
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func changedIf(changed bool, name string) []string {
	if changed {
		return []string{name}
	}
	return []string{}
}

func emptyAsLeave(value string) string {
	if strings.TrimSpace(value) == "" {
		return "leave"
	}
	return value
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func diagnosticsToWarnings(diagnostics []appinit.Diagnostic) []string {
	warnings := []string{}
	for _, diagnostic := range diagnostics {
		if diagnostic.Level == "warning" || diagnostic.Level == "error" {
			warnings = append(warnings, diagnostic.Step+": "+diagnostic.Message)
		}
	}
	return warnings
}

func pathExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func pathExistsAny(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
