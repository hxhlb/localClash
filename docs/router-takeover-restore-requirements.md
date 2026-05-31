# Router Takeover Restore Requirements

Status: implemented in LuCI package `0.1.0-19`; router deployment pending

This document defines the clean recovery model for localClash router takeover
after the 2026-05-31 real-router incidents. It supersedes the tactical
`luci-app-localclash 0.1.0-18` behavior where a normal takeover apply could
create a durable restore intent.

Related incident records:

- `2026-05-31 LuCI Reboot Restore Gap` in `docs/router-incident-register.md`
- `2026-05-31 WAN Firewall Reload Takeover Drift` in
  `docs/router-incident-register.md`

## Problem

localClash router takeover is intentionally runtime-only OpenWrt state:
nft hooks, DNS hijack rules, fwmark routes, and TUN forwarding are installed at
runtime and must not be persisted into OpenWrt firewall configuration.

Two similar but different recovery failures exist:

- Reboot restore gap: after router reboot, LuCI starts only the MCP service and
  does not explicitly restore Mihomo runtime plus router takeover.
- Same-boot takeover drift: during one boot, WAN link instability or other
  OpenWrt network churn can trigger `fw4` reload, which can remove localClash
  runtime-only nft hooks while Mihomo keeps running.

The tactical `0.1.0-18` fix repaired same-boot drift, but it also persisted a
generic takeover intent. That mixes "takeover was active in this boot" with
"the user wants takeover restored after reboot", which weakens the safety
expectation that reboot clears operational takeover unless the user explicitly
opts into boot restore.

## Product Principle

Do not infer boot-time takeover intent from a direct runtime takeover action.

The product must split recovery into two separate mechanisms:

- Same-boot repair: ephemeral, stored in `/tmp`, used only to repair takeover
  drift during the current boot.
- Boot auto-restore: persistent, stored under the localClash state directory,
  enabled only through an explicit LuCI status/action control.

## State Model

### Runtime Effective State

Source of truth:

- `router_takeover_status.effective`

Meaning:

- Whether localClash-owned nft hooks, DNS hijack, fwmark route, and TUN routing
  are effective right now.

This is observational state and is not a restore policy.

### Same-Boot Repair Ticket

Recommended path:

- `/tmp/localclash/router-takeover/repair-ticket`

Allowed compatibility input:

- `/tmp/localclash/router-takeover/status` containing `applied`, while migrating
  away from the tactical implementation.

Meaning:

- During the current boot only, localClash successfully applied router takeover.
- If OpenWrt reloads firewall/network state and takeover becomes ineffective,
  hotplug repair may re-apply takeover.

Required behavior:

- `router_takeover_apply` and LuCI `takeover_apply` write the repair ticket only
  after takeover apply succeeds.
- `runtime_start_takeover` writes the repair ticket only after runtime start and
  takeover apply both succeed.
- `router_takeover_stop` and LuCI `takeover_stop` must remove the repair ticket.
- `runtime_stop` must remove the repair ticket when it stops a runtime that
  could otherwise be used by router takeover.
- Reboot clears this ticket naturally because it lives under `/tmp`.

Forbidden behavior:

- The same-boot repair ticket must not be stored under `/root/localclash`.
- The same-boot repair ticket must not be interpreted as boot auto-restore
  policy.

### Boot Auto-Restore Setting

Recommended path:

- `/root/localclash/boot-auto-restore-enabled`

Meaning:

- The user explicitly wants localClash to restore runtime and router takeover
  after router reboot.

Required behavior:

- This setting is changed only by an explicit LuCI control or an explicit helper
  method dedicated to boot auto-restore.
- A plain `router_takeover_apply`, LuCI `takeover_apply`, or
  `runtime_start_takeover` must not create or enable this setting.
- When enabled, boot restore starts the configured localClash runtime and applies
  router takeover only after the runtime is ready.
- When disabled or absent, reboot must leave operational takeover cleared unless
  the user manually starts runtime/takeover again.

Forbidden behavior:

- Do not use `/root/localclash/takeover-enabled` as a generic restore intent.
  That name conflates manual takeover with boot auto-restore policy.

## LuCI UX Requirement

The LuCI surface is not a traditional form plus "Apply" page. Runtime and
takeover controls are direct action buttons, so boot restore must follow the
same product style.

Add a new status/action control:

- Label: `开机自动恢复`
- State text: `已启用` / `未启用`
- Action: toggle or explicit enable/disable direct action.
- Description text:
  `启用后，路由器重启时会自动启动 localClash runtime 并恢复网络接管。关闭时，重启会清除接管；同次开机内的 firewall reload 仍会依本次接管记录自动修复。`

UX rules:

- The control must clearly describe that it affects reboot behavior.
- The control must not be hidden behind ordinary takeover apply.
- Stopping current network takeover should clear same-boot repair state, but it
  should not silently disable boot auto-restore. If boot auto-restore remains
  enabled, LuCI should make that visible in status text.
- A separate future action may be named "停止并关闭开机自动恢复" if the product
  needs a one-click combined stop/disable path.

## Helper And Service API

The LuCI helper should expose distinct methods for the two restore layers.

Suggested methods:

- `takeover_restore`: same-boot repair only; checks the `/tmp` repair ticket.
- `boot_restore_status`: reports persistent boot auto-restore setting.
- `boot_restore_enable`: creates the persistent setting.
- `boot_restore_disable`: removes the persistent setting.
- `boot_restore_run`: boot-service entrypoint; when enabled, starts runtime and
  applies takeover.

The existing action methods keep their direct-action semantics:

- `takeover_apply`: applies current takeover and writes only the same-boot
  repair ticket.
- `takeover_stop`: stops current takeover and removes the same-boot repair
  ticket.
- `runtime_start_takeover`: starts runtime, applies current takeover, and writes
  only the same-boot repair ticket.
- `runtime_stop`: stops runtime and removes the same-boot repair ticket.

## Hotplug Repair Behavior

The iface hotplug hook handles same-boot drift only.

Trigger:

- OpenWrt `ifup` / `ifupdate` events that can be followed by `fw4` reload.

Required flow:

1. Debounce repeated hotplug events.
2. Delay briefly so OpenWrt network/firewall state settles.
3. Check the same-boot repair ticket under `/tmp`.
4. If the ticket is absent, exit without changing runtime or firewall state.
5. If the ticket is present, call `takeover_restore`.
6. `takeover_restore` checks `router_takeover_status`.
7. If takeover is already effective, log and exit.
8. If runtime is running and profile mode is `router`, re-apply takeover.
9. If runtime is not running, do not start it from same-boot repair alone unless
   the current boot repair ticket explicitly records that runtime was also
   managed by localClash and the product has a documented reason to do so.

## Boot Restore Behavior

Boot restore is separate from hotplug repair.

Trigger:

- localClash/OpenWrt boot service start after the MCP helper is available.

Required flow:

1. Check the persistent boot auto-restore setting.
2. If disabled or absent, do not start runtime and do not apply takeover.
3. If enabled, ensure localClash core, runtime profile, generated config, and
   selected Mihomo core are available.
4. Start or restart the localClash-managed runtime.
5. Apply router takeover after the runtime is ready.
6. Write a same-boot repair ticket only after takeover succeeds.
7. Log each stage with enough detail for LuCI status and future incident review.

## Migration From Tactical 0.1.0-18

The current tactical file `/root/localclash/takeover-enabled` must not continue
to act as a generic boot restore policy.

Migration requirement:

- Treat `/root/localclash/takeover-enabled` as legacy ambiguous state.
- Do not automatically migrate it to `boot-auto-restore-enabled`.
- `boot_restore_enable` and `boot_restore_disable` may remove the legacy marker
  after the user has made an explicit choice.
- LuCI may surface a one-time warning such as:
  `检测到旧版接管恢复标记。请明确启用或关闭「开机自动恢复」。`
- A user choosing enable creates `boot-auto-restore-enabled`.
- A user choosing disable removes both the legacy marker and the new setting.

## Observability

Logs and status output should distinguish:

- `manual_apply`
- `manual_stop`
- `same_boot_repair`
- `boot_auto_restore`
- `legacy_ambiguous_marker`

Each restore attempt should record:

- trigger source
- whether same-boot ticket existed
- whether boot auto-restore was enabled
- runtime running state
- takeover effective state before and after
- skipped reason or failure code

## Acceptance Criteria

- Calling takeover apply creates only `/tmp/localclash/router-takeover/repair-ticket`.
- Calling takeover stop removes the `/tmp` repair ticket and prevents later
  hotplug repair in the same boot.
- Reboot without `boot-auto-restore-enabled` does not restore runtime or
  takeover.
- Reboot with `boot-auto-restore-enabled` restores runtime and takeover after
  service startup.
- Same-boot firewall reload or WAN ifupdate restores takeover only when the
  `/tmp` repair ticket exists.
- Plain takeover apply never creates persistent boot restore policy.
- LuCI exposes `开机自动恢复` with visible enabled/disabled status and
  explanatory text.
- Tests cover helper behavior for no ticket, ticket with effective takeover,
  ticket with missing takeover, stop clearing ticket, boot restore disabled, and
  boot restore enabled.
