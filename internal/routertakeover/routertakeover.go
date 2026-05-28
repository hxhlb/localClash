package routertakeover

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"localclash/internal/corerun"
	"localclash/internal/runtimeprofile"
)

const (
	defaultFWMark     = "0x162"
	defaultRouteTable = "0x162"
	defaultRulePref   = "1888"
	defaultStateDir   = "/tmp/localclash/router-takeover"
	commandTimeout    = 75 * time.Second
)

type Options struct {
	RuntimeProfile string
	ConfigPath     string
	RuntimeDir     string
	LogPath        string
	StateDir       string
	DNSPort        int
	RedirPort      int
	TunDevice      string
	IPv6           bool
	DryRun         bool
	OnStage        func(StageEvent) `json:"-"`
}

type StageEvent struct {
	Stage      string         `json:"stage"`
	Event      string         `json:"event"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	Error      string         `json:"error,omitempty"`
	Fields     map[string]any `json:"fields,omitempty"`
}

type Check struct {
	ID      string `json:"id"`
	OK      bool   `json:"ok"`
	Summary string `json:"summary"`
	Error   string `json:"error,omitempty"`
}

type Result struct {
	ProfileMode    string   `json:"profile_mode"`
	RuntimeRunning bool     `json:"runtime_running"`
	Effective      bool     `json:"effective"`
	Applied        bool     `json:"applied,omitempty"`
	Stopped        bool     `json:"stopped,omitempty"`
	DryRun         bool     `json:"dry_run,omitempty"`
	StateDir       string   `json:"state_dir"`
	DNSPort        int      `json:"dns_port"`
	RedirPort      int      `json:"redir_port"`
	TunDevice      string   `json:"tun_device"`
	Checks         []Check  `json:"checks"`
	Warnings       []string `json:"warnings,omitempty"`
	NextActions    []string `json:"next_actions,omitempty"`
	Script         string   `json:"script,omitempty"`
}

type commandRunner func(context.Context, string) (string, error)

func Status(ctx context.Context, opts Options) (Result, error) {
	opts = normalizeOptions(opts)
	stage := routerTakeoverStageEmitter(opts.OnStage)
	finish := stage("read_runtime_profile", map[string]any{"runtime_profile": opts.RuntimeProfile})
	status, err := runtimeprofile.StatusFor(opts.RuntimeProfile)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, map[string]any{"profile_mode": status.Mode})
	opts = mergeProfileDefaults(opts, status)
	result := baseResult(opts, status)
	finish = stage("runtime_status", map[string]any{"runtime_dir": opts.RuntimeDir, "config": opts.ConfigPath})
	runtimeStatus := corerun.Status(corerun.StatusOptions{
		ConfigPath: opts.ConfigPath,
		WorkDir:    opts.RuntimeDir,
		LogPath:    opts.LogPath,
	})
	result.RuntimeRunning = runtimeStatus.Running
	finish(nil, map[string]any{"running": runtimeStatus.Running, "pid": runtimeStatus.PID})
	result.Checks = append(result.Checks, check("profile_router", status.Mode == runtimeprofile.ModeRouter, fmt.Sprintf("active profile mode is %s", status.Mode), "router_takeover_* is only meaningful when runtime profile mode is router"))
	result.Checks = append(result.Checks, check("runtime_running", runtimeStatus.Running, "localClash Mihomo runtime is running", "call run_runtime before router_takeover_apply"))
	runner := defaultRunner
	checks := []struct {
		id      string
		command string
		ok      string
		fail    string
	}{
		{"fw4_available", "command -v fw4 >/dev/null 2>&1", "Firewall4/fw4 is available", "fw4 is unavailable"},
		{"nft_available", "command -v nft >/dev/null 2>&1", "nft is available", "nft is unavailable"},
		{"fw4_table", "nft list table inet fw4 >/dev/null 2>&1", "Firewall4 nft table inet fw4 is active", "Firewall4 nft table inet fw4 is not active"},
		{"fw4_base_chains", "for chain in dstnat mangle_prerouting forward input srcnat; do nft list chain inet fw4 \"$chain\" >/dev/null 2>&1 || exit 1; done", "Firewall4 base chains are available", "Firewall4 base chains are missing"},
		{"tun_interface", fmt.Sprintf("ip link show %s >/dev/null 2>&1", shellQuote(opts.TunDevice)), fmt.Sprintf("TUN device %s exists", opts.TunDevice), fmt.Sprintf("TUN device %s is missing", opts.TunDevice)},
		{"fwmark_route_v4", fmt.Sprintf("ip rule show | grep -q 'fwmark %s' && ip route show table %s | grep -q %s", defaultFWMark, defaultRouteTable, shellQuote(opts.TunDevice)), "IPv4 fwmark route points to TUN", "IPv4 fwmark route is missing"},
		{"nft_chains", "nft list chain inet fw4 localclash >/dev/null 2>&1 && nft list chain inet fw4 localclash_mangle >/dev/null 2>&1", "localClash nft takeover chains are installed", "localClash nft takeover chains are missing"},
		{"tcp_redirect", "nft list chain inet fw4 dstnat 2>/dev/null | grep -q 'localClash TCP redirect'", "localClash TCP redir-host redirect is installed", "localClash TCP redir-host redirect is missing"},
		{"udp_tun_mark", "nft list chain inet fw4 mangle_prerouting 2>/dev/null | grep -q 'localClash TUN mark'", "localClash UDP/ICMP TUN mark is installed", "localClash UDP/ICMP TUN mark is missing"},
		{"dns_hijack", "nft list ruleset 2>/dev/null | grep -q 'localClash DNS hijack'", "localClash DNS hijack rule is installed", "localClash DNS hijack rule is missing"},
	}
	for _, item := range checks {
		finish = stage("check_"+item.id, nil)
		checkResult := commandCheck(ctx, runner, item.id, item.command, item.ok, item.fail)
		result.Checks = append(result.Checks, checkResult)
		finish(nil, map[string]any{"ok": checkResult.OK})
	}
	result.Effective = allChecksOK(result.Checks)
	result.NextActions = nextActions(result)
	return result, nil
}

func Apply(ctx context.Context, opts Options) (Result, error) {
	opts = normalizeOptions(opts)
	stage := routerTakeoverStageEmitter(opts.OnStage)
	finish := stage("read_runtime_profile", map[string]any{"runtime_profile": opts.RuntimeProfile})
	status, err := runtimeprofile.StatusFor(opts.RuntimeProfile)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, map[string]any{"profile_mode": status.Mode})
	opts = mergeProfileDefaults(opts, status)
	result := baseResult(opts, status)
	if status.Mode != runtimeprofile.ModeRouter {
		result.Checks = append(result.Checks, check("profile_router", false, fmt.Sprintf("active profile mode is %s", status.Mode), "call config_configure with runtime_profile=router before router_takeover_apply"))
		result.NextActions = []string{"call config_configure with runtime_profile=router", "call config_render", "call run_runtime", "call router_takeover_apply again"}
		return result, nil
	}
	finish = stage("runtime_status", map[string]any{"runtime_dir": opts.RuntimeDir, "config": opts.ConfigPath})
	runtimeStatus := corerun.Status(corerun.StatusOptions{ConfigPath: opts.ConfigPath, WorkDir: opts.RuntimeDir, LogPath: opts.LogPath})
	result.RuntimeRunning = runtimeStatus.Running
	finish(nil, map[string]any{"running": runtimeStatus.Running, "pid": runtimeStatus.PID})
	script := applyScript(opts)
	result.Script = script
	if opts.DryRun {
		result.DryRun = true
		result.Checks = append(result.Checks, check("runtime_running", runtimeStatus.Running, "localClash Mihomo runtime is running", "call run_runtime before applying router takeover"))
		if runtimeStatus.Running {
			result.NextActions = []string{"review the script", "call router_takeover_apply without dry_run after user confirmation"}
		} else {
			result.NextActions = []string{"review the script", "call run_runtime after user confirmation", "call router_takeover_apply without dry_run after user confirmation"}
		}
		return result, nil
	}
	if !runtimeStatus.Running {
		result.Checks = append(result.Checks, check("runtime_running", false, "localClash Mihomo runtime is not running", "call run_runtime before router_takeover_apply"))
		result.NextActions = []string{"call run_runtime after user confirmation", "call router_takeover_apply again"}
		return result, nil
	}
	finish = stage("apply_script", map[string]any{"state_dir": opts.StateDir, "tun_device": opts.TunDevice})
	if _, err := defaultRunner(ctx, script); err != nil {
		finish(err, map[string]any{"recovery": "runtime takeover state is non-persistent; reboot clears localClash-owned rules"})
		result.Checks = append(result.Checks, check("apply_script", false, "router takeover script applied", err.Error()))
		result.NextActions = takeoverFailureNextActions("apply", err)
		return result, nil
	}
	finish(nil, nil)
	finish = stage("verify_takeover_status", nil)
	statusOpts := opts
	statusOpts.OnStage = nil
	result, err = Status(ctx, statusOpts)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, map[string]any{"effective": result.Effective})
	result.Applied = true
	return result, nil
}

func Stop(ctx context.Context, opts Options) (Result, error) {
	opts = normalizeOptions(opts)
	stage := routerTakeoverStageEmitter(opts.OnStage)
	finish := stage("read_runtime_profile", map[string]any{"runtime_profile": opts.RuntimeProfile})
	status, err := runtimeprofile.StatusFor(opts.RuntimeProfile)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, map[string]any{"profile_mode": status.Mode})
	opts = mergeProfileDefaults(opts, status)
	result := baseResult(opts, status)
	script := stopScript(opts)
	result.Script = script
	if opts.DryRun {
		result.DryRun = true
		result.NextActions = []string{"review the cleanup script", "call router_takeover_stop without dry_run after user confirmation"}
		return result, nil
	}
	finish = stage("stop_script", map[string]any{"state_dir": opts.StateDir, "tun_device": opts.TunDevice})
	if _, err := defaultRunner(ctx, script); err != nil {
		finish(err, map[string]any{"recovery": "runtime takeover state is non-persistent; reboot clears localClash-owned rules"})
		result.Checks = append(result.Checks, check("stop_script", false, "router takeover cleanup script ran", err.Error()))
		result.NextActions = takeoverFailureNextActions("stop", err)
		return result, nil
	}
	finish(nil, nil)
	finish = stage("verify_takeover_status", nil)
	statusOpts := opts
	statusOpts.OnStage = nil
	result, err = Status(ctx, statusOpts)
	if err != nil {
		finish(err, nil)
		return Result{}, err
	}
	finish(nil, map[string]any{"effective": result.Effective})
	result.Stopped = true
	return result, nil
}

func routerTakeoverStageEmitter(callback func(StageEvent)) func(string, map[string]any) func(error, map[string]any) {
	return func(stage string, fields map[string]any) func(error, map[string]any) {
		if callback == nil {
			return func(error, map[string]any) {}
		}
		started := time.Now()
		callback(StageEvent{Stage: stage, Event: "started", Fields: fields})
		return func(err error, doneFields map[string]any) {
			event := StageEvent{
				Stage:      stage,
				Event:      "done",
				DurationMS: time.Since(started).Milliseconds(),
				Fields:     doneFields,
			}
			if err != nil {
				event.Event = "error"
				event.Error = err.Error()
			}
			callback(event)
		}
	}
}

func normalizeOptions(opts Options) Options {
	if strings.TrimSpace(opts.RuntimeProfile) == "" {
		opts.RuntimeProfile = runtimeprofile.DefaultPath
	}
	if strings.TrimSpace(opts.ConfigPath) == "" {
		opts.ConfigPath = "generated/mihomo.yaml"
	}
	if strings.TrimSpace(opts.RuntimeDir) == "" {
		opts.RuntimeDir = filepath.Join(".runtime", "mihomo")
	}
	if strings.TrimSpace(opts.LogPath) == "" {
		opts.LogPath = filepath.Join(opts.RuntimeDir, "mihomo.log")
	}
	if strings.TrimSpace(opts.StateDir) == "" {
		opts.StateDir = defaultStateDir
	}
	if opts.DNSPort == 0 {
		opts.DNSPort = 7874
	}
	if opts.RedirPort == 0 {
		opts.RedirPort = 7892
	}
	if strings.TrimSpace(opts.TunDevice) == "" {
		opts.TunDevice = "utun"
	}
	return opts
}

func mergeProfileDefaults(opts Options, status runtimeprofile.Status) Options {
	if status.Mode != runtimeprofile.ModeRouter {
		return opts
	}
	if value, ok := intFromSummary(status.Summary, "redir-port"); ok && opts.RedirPort == 7892 {
		opts.RedirPort = value
	}
	if dns, ok := status.Summary["dns"].(map[string]any); ok && opts.DNSPort == 7874 {
		if listen, ok := dns["listen"].(string); ok {
			if port := portFromListen(listen); port != 0 {
				opts.DNSPort = port
			}
		}
	}
	if tun, ok := status.Summary["tun"].(map[string]any); ok {
		if device, ok := tun["device"].(string); ok && strings.TrimSpace(device) != "" && opts.TunDevice == "utun" {
			opts.TunDevice = strings.TrimSpace(device)
		}
	}
	if ipv6, ok := status.Summary["ipv6"].(bool); ok {
		opts.IPv6 = ipv6
	}
	return opts
}

func intFromSummary(summary map[string]any, key string) (int, bool) {
	switch value := summary[key].(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	case string:
		parsed, err := strconv.Atoi(value)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func portFromListen(listen string) int {
	parts := strings.Split(listen, ":")
	if len(parts) == 0 {
		return 0
	}
	port, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return 0
	}
	return port
}

func baseResult(opts Options, status runtimeprofile.Status) Result {
	return Result{
		ProfileMode: status.Mode,
		StateDir:    opts.StateDir,
		DNSPort:     opts.DNSPort,
		RedirPort:   opts.RedirPort,
		TunDevice:   opts.TunDevice,
		Warnings: []string{
			"router_takeover_* applies runtime-only OpenWrt firewall, DNS, and policy-routing state and may interrupt network connectivity.",
			"router_takeover_* follows localClash router redir-host-mix behavior: TCP redir-host, DNS hijack, and UDP/ICMP TUN marking.",
			"router_takeover_* must not write persistent firewall configuration; reboot clears the runtime takeover state.",
			"router_takeover_* manages only localClash-owned rules and state.",
		},
	}
}

func check(id string, ok bool, summary, errText string) Check {
	item := Check{ID: id, OK: ok}
	if ok {
		item.Summary = summary
	} else {
		item.Summary = errText
		item.Error = errText
	}
	return item
}

func commandCheck(ctx context.Context, runner commandRunner, id, command, okText, errText string) Check {
	if _, err := runner(ctx, command); err != nil {
		return check(id, false, okText, errText)
	}
	return check(id, true, okText, errText)
}

func allChecksOK(checks []Check) bool {
	if len(checks) == 0 {
		return false
	}
	for _, item := range checks {
		if !item.OK {
			return false
		}
	}
	return true
}

func nextActions(result Result) []string {
	if result.Effective {
		return []string{"router takeover is installed; use router_takeover_status to verify later", "use router_takeover_stop to remove localClash-owned takeover rules"}
	}
	if result.ProfileMode != runtimeprofile.ModeRouter {
		return []string{"call config_configure with runtime_profile=router", "call config_render", "call run_runtime", "call router_takeover_apply"}
	}
	if !result.RuntimeRunning {
		return []string{"call run_runtime after user confirmation", "call router_takeover_apply"}
	}
	return []string{"call router_takeover_apply after user confirmation"}
}

func takeoverFailureNextActions(action string, err error) []string {
	actions := []string{
		"inspect the MCP task log stage_error entry for the failing command output",
		"call router_takeover_status to see which runtime-only checks are still effective",
		"retry after fixing the reported OpenWrt prerequisite or Mihomo runtime state",
		"rebooting the router clears localClash runtime takeover state because no persistent firewall config is written",
	}
	if action == "apply" {
		actions = append(actions, "call router_takeover_stop after user confirmation if status shows partially installed localClash rules")
	}
	if err != nil {
		actions = append([]string{"failure: " + err.Error()}, actions...)
	}
	return actions
}

func defaultRunner(ctx context.Context, command string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "/bin/sh", "-c", command)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		text := strings.TrimSpace(out.String())
		if text != "" {
			return text, fmt.Errorf("%w: %s", err, text)
		}
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

func applyScript(opts Options) string {
	return fmt.Sprintf(`set -eu
STATE_DIR=%s
DNS_PORT=%d
REDIR_PORT=%d
TUN_DEVICE=%s
FWMARK=%s
ROUTE_TABLE=%s
RULE_PREF=%s
TUN_WAIT_SECONDS=30

command -v fw4 >/dev/null 2>&1
command -v nft >/dev/null 2>&1
mkdir -p "$STATE_DIR"
modprobe tun >/dev/null 2>&1 || true
modprobe nft_tproxy >/dev/null 2>&1 || true

cleanup_localclash_nft() {
  for chain in dstnat nat_output mangle_prerouting mangle_output forward input srcnat; do
    nft -a list chain inet fw4 "$chain" 2>/dev/null | awk '/localClash/{print $NF}' | sort -rn | while read -r handle; do
      [ -n "$handle" ] && nft delete rule inet fw4 "$chain" handle "$handle" 2>/dev/null || true
    done
  done
  for chain in localclash localclash_output localclash_mangle localclash_mangle_output localclash_v6 localclash_mangle_v6 localclash_dns_redirect; do
    nft flush chain inet fw4 "$chain" >/dev/null 2>&1 || true
    nft delete chain inet fw4 "$chain" >/dev/null 2>&1 || true
  done
  for set_name in localclash_localnetwork localclash_localnetwork6; do
    nft flush set inet fw4 "$set_name" >/dev/null 2>&1 || true
    nft delete set inet fw4 "$set_name" >/dev/null 2>&1 || true
  done
}

cleanup_localclash_state() {
  cleanup_localclash_nft
  while ip rule del fwmark "$FWMARK" table "$ROUTE_TABLE" >/dev/null 2>&1; do :; done
  ip route del default table "$ROUTE_TABLE" >/dev/null 2>&1 || true
  while ip -6 rule del fwmark "$FWMARK" table "$ROUTE_TABLE" >/dev/null 2>&1; do :; done
  ip -6 route del default table "$ROUTE_TABLE" >/dev/null 2>&1 || true
  rm -f "$STATE_DIR/status" >/dev/null 2>&1 || true
}

check_fw4_ready() {
  if ! nft list table inet fw4 >/dev/null 2>&1; then
    echo "OpenWrt firewall table inet fw4 is not active; start or reload the firewall service, then retry router_takeover_apply" >&2
    return 1
  fi
  for chain in dstnat mangle_prerouting forward input srcnat; do
    if ! nft list chain inet fw4 "$chain" >/dev/null 2>&1; then
      echo "OpenWrt firewall chain inet fw4 $chain is missing; start or reload the firewall service, then retry router_takeover_apply" >&2
      return 1
    fi
  done
}

wait_tun_ready() {
  i=0
  while [ "$i" -lt "$TUN_WAIT_SECONDS" ]; do
    if ip link show "$TUN_DEVICE" >/dev/null 2>&1; then
      ip link set "$TUN_DEVICE" up >/dev/null 2>&1 || true
      return 0
    fi
    i=$((i + 1))
    sleep 1
  done
  echo "TUN device $TUN_DEVICE is not ready after ${TUN_WAIT_SECONDS}s; call run_runtime and retry router_takeover_apply" >&2
  return 1
}

add_dynamic_localnetwork4() {
  ip -o -4 addr show scope global 2>/dev/null | awk '{print $4}' | sort -u | while read -r addr; do
    [ -n "$addr" ] && nft add element inet fw4 localclash_localnetwork { "$addr" } >/dev/null 2>&1 || true
  done
}

add_dynamic_localnetwork6() {
  ip -o -6 addr show scope global 2>/dev/null | awk '{print $4}' | sort -u | while read -r addr; do
    [ -n "$addr" ] && nft add element inet fw4 localclash_localnetwork6 { "$addr" } >/dev/null 2>&1 || true
  done
}

check_fw4_ready
trap 'cleanup_localclash_state' ERR
cleanup_localclash_state
wait_tun_ready

while ip rule del fwmark "$FWMARK" table "$ROUTE_TABLE" >/dev/null 2>&1; do :; done
ip rule add fwmark "$FWMARK" table "$ROUTE_TABLE" pref "$RULE_PREF"
ip route replace default dev "$TUN_DEVICE" table "$ROUTE_TABLE"
while ip -6 rule del fwmark "$FWMARK" table "$ROUTE_TABLE" >/dev/null 2>&1; do :; done
ip -6 rule add fwmark "$FWMARK" table "$ROUTE_TABLE" pref "$RULE_PREF" >/dev/null 2>&1 || true
ip -6 route replace default dev "$TUN_DEVICE" table "$ROUTE_TABLE" >/dev/null 2>&1 || true

nft -f - <<EOF_NFT
add set inet fw4 localclash_localnetwork { type ipv4_addr; flags interval; auto-merge; }
add element inet fw4 localclash_localnetwork { 0.0.0.0/8, 10.0.0.0/8, 100.64.0.0/10, 127.0.0.0/8, 169.254.0.0/16, 172.16.0.0/12, 192.168.0.0/16, 224.0.0.0/4, 240.0.0.0/4 }
add chain inet fw4 localclash
add rule inet fw4 localclash ip daddr @localclash_localnetwork counter return
add rule inet fw4 localclash ct direction reply counter return
add rule inet fw4 localclash ip protocol tcp counter redirect to $REDIR_PORT
insert rule inet fw4 dstnat position 0 meta nfproto ipv4 ip protocol tcp counter jump localclash comment "localClash TCP redirect"
add chain inet fw4 localclash_mangle
add rule inet fw4 localclash_mangle meta l4proto { tcp, udp } iifname "$TUN_DEVICE" counter return
add rule inet fw4 localclash_mangle ip daddr @localclash_localnetwork counter return
add rule inet fw4 localclash_mangle ct direction reply counter return
add rule inet fw4 localclash_mangle ip protocol udp mark set $FWMARK counter accept
add rule inet fw4 localclash_mangle ip protocol icmp icmp type echo-request mark set $FWMARK counter accept comment "localClash ICMP mark"
insert rule inet fw4 mangle_prerouting position 0 meta nfproto ipv4 counter jump localclash_mangle comment "localClash TUN mark"
insert rule inet fw4 dstnat position 0 meta l4proto { tcp, udp } th dport 53 counter redirect to $DNS_PORT comment "localClash DNS hijack"
insert rule inet fw4 forward position 0 meta nfproto ipv4 oifname "$TUN_DEVICE" counter accept comment "localClash TUN forward"
insert rule inet fw4 forward position 0 meta nfproto ipv4 iifname "$TUN_DEVICE" counter accept comment "localClash TUN forward"
insert rule inet fw4 input position 0 meta nfproto ipv4 iifname "$TUN_DEVICE" counter accept comment "localClash TUN input"
insert rule inet fw4 srcnat position 0 meta nfproto ipv4 oifname "$TUN_DEVICE" counter return comment "localClash TUN postrouting"
EOF_NFT

add_dynamic_localnetwork4

nft 'add set inet fw4 localclash_localnetwork6 { type ipv6_addr; flags interval; auto-merge; }' >/dev/null 2>&1 || true
nft 'add element inet fw4 localclash_localnetwork6 { ::/128, ::1/128, ::ffff:0:0/96, 64:ff9b::/96, 100::/64, 2001:db8::/32, fe80::/10, ff00::/8 }' >/dev/null 2>&1 || true
add_dynamic_localnetwork6
nft 'add chain inet fw4 nat_output { type nat hook output priority -1; }' >/dev/null 2>&1 || true
nft "insert rule inet fw4 nat_output position 0 meta l4proto { tcp, udp } th dport 53 ip daddr 127.0.0.1 counter redirect to $DNS_PORT comment \"localClash DNS hijack\""

nft 'add chain inet fw4 localclash_v6' >/dev/null 2>&1 || true
nft 'add rule inet fw4 localclash_v6 ip6 daddr @localclash_localnetwork6 counter return' >/dev/null 2>&1 || true
nft 'add rule inet fw4 localclash_v6 ct direction reply counter return' >/dev/null 2>&1 || true
nft add rule inet fw4 localclash_v6 ip6 nexthdr tcp counter redirect to "$REDIR_PORT" >/dev/null 2>&1 || true
nft "insert rule inet fw4 dstnat position 0 meta nfproto ipv6 ip6 nexthdr tcp counter jump localclash_v6 comment \"localClash IPv6 TCP redirect\"" >/dev/null 2>&1 || true

nft 'add chain inet fw4 localclash_mangle_v6' >/dev/null 2>&1 || true
nft add rule inet fw4 localclash_mangle_v6 meta l4proto { tcp, udp } iifname "$TUN_DEVICE" counter return >/dev/null 2>&1 || true
nft 'add rule inet fw4 localclash_mangle_v6 ip6 daddr @localclash_localnetwork6 counter return' >/dev/null 2>&1 || true
nft 'add rule inet fw4 localclash_mangle_v6 ct direction reply counter return' >/dev/null 2>&1 || true
nft add rule inet fw4 localclash_mangle_v6 ip6 nexthdr udp mark set "$FWMARK" counter accept >/dev/null 2>&1 || true
nft "add rule inet fw4 localclash_mangle_v6 ip6 nexthdr ipv6-icmp icmpv6 type echo-request mark set $FWMARK counter accept comment \"localClash ICMPv6 mark\"" >/dev/null 2>&1 || true
nft "insert rule inet fw4 mangle_prerouting position 0 meta nfproto ipv6 counter jump localclash_mangle_v6 comment \"localClash IPv6 TUN mark\"" >/dev/null 2>&1 || true
nft "insert rule inet fw4 forward position 0 meta nfproto ipv6 oifname \"$TUN_DEVICE\" counter accept comment \"localClash IPv6 TUN forward\"" >/dev/null 2>&1 || true
nft "insert rule inet fw4 forward position 0 meta nfproto ipv6 iifname \"$TUN_DEVICE\" counter accept comment \"localClash IPv6 TUN forward\"" >/dev/null 2>&1 || true
nft "insert rule inet fw4 input position 0 meta nfproto ipv6 iifname \"$TUN_DEVICE\" counter accept comment \"localClash IPv6 TUN input\"" >/dev/null 2>&1 || true
nft "insert rule inet fw4 srcnat position 0 meta nfproto ipv6 oifname \"$TUN_DEVICE\" counter return comment \"localClash IPv6 TUN postrouting\"" >/dev/null 2>&1 || true

printf 'applied\n' > "$STATE_DIR/status"
trap - ERR
`, shellQuote(opts.StateDir), opts.DNSPort, opts.RedirPort, shellQuote(opts.TunDevice), shellQuote(defaultFWMark), shellQuote(defaultRouteTable), shellQuote(defaultRulePref))
}

func stopScript(opts Options) string {
	return fmt.Sprintf(`set -eu
STATE_DIR=%s
FWMARK=%s
ROUTE_TABLE=%s
for chain in dstnat nat_output mangle_prerouting mangle_output forward input srcnat; do
  nft -a list chain inet fw4 "$chain" 2>/dev/null | awk '/localClash/{print $NF}' | sort -rn | while read -r handle; do
    [ -n "$handle" ] && nft delete rule inet fw4 "$chain" handle "$handle" 2>/dev/null || true
  done
done
for chain in localclash localclash_output localclash_mangle localclash_mangle_output localclash_v6 localclash_mangle_v6 localclash_dns_redirect; do
  nft flush chain inet fw4 "$chain" >/dev/null 2>&1 || true
  nft delete chain inet fw4 "$chain" >/dev/null 2>&1 || true
done
for set_name in localclash_localnetwork localclash_localnetwork6; do
  nft flush set inet fw4 "$set_name" >/dev/null 2>&1 || true
  nft delete set inet fw4 "$set_name" >/dev/null 2>&1 || true
done
while ip rule del fwmark "$FWMARK" table "$ROUTE_TABLE" >/dev/null 2>&1; do :; done
ip route del default table "$ROUTE_TABLE" >/dev/null 2>&1 || true
while ip -6 rule del fwmark "$FWMARK" table "$ROUTE_TABLE" >/dev/null 2>&1; do :; done
ip -6 route del default table "$ROUTE_TABLE" >/dev/null 2>&1 || true
rm -f "$STATE_DIR/status" >/dev/null 2>&1 || true
`, shellQuote(opts.StateDir), shellQuote(defaultFWMark), shellQuote(defaultRouteTable))
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
