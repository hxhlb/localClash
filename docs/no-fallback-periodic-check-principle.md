# No Fallback Periodic Check Principle

Date: 2026-05-31

This document reframes the No Fallback Policy as a periodic engineering audit
principle, not as a blanket day-to-day implementation rule for `AGENTS.md`.

The goal is to regularly find dangerous hidden fallback behavior without
blocking legitimate safety, resiliency, or operator-explicit recovery paths.

## Positioning

No Fallback should be used as an audit lens:

- periodically inspect the codebase for suspicious fallback behavior
- classify each finding with evidence and runtime impact
- discuss whether the fallback is wrong, acceptable, or should become explicit
- only change production behavior after an item has been reviewed

No Fallback should not be applied as an absolute rule that rejects every fallback
implementation. Some fallback behavior is valid when it is explicit,
observable, and intentional.

## Why This Should Not Be a Hard Agent Rule

A hard rule in `AGENTS.md` can create false positives and block reasonable
engineering behavior. For example:

- download retry or mirror behavior can be a safety/resiliency mechanism
- temporary-file cleanup often uses best-effort removal
- status inspection may tolerate optional files when the missing file is part of
  the expected state model
- network operations often need explicit retry or alternate endpoint handling

The problem is not that fallback exists. The problem is hidden fallback that
changes behavior or provenance without the caller, operator, or test surface
knowing.

## Check Target

During a scheduled No Fallback check, look for production code that does one or
more of the following:

- silently switches to a legacy compatibility path
- silently substitutes a default value when required input is missing
- uses placeholder or mock data in a production path
- performs best-effort migration without reporting what changed
- auto-creates missing state that should have been initialized explicitly
- catches an error and continues with weaker or different behavior
- tries a fallback algorithm without caller opt-in
- infers provenance when the source is missing or ambiguous

Mihomo DNS fields, Mihomo rule semantics, and intentional product-level routing
fallbacks are not automatically violations. They should only be included when
the implementation hides engineering state or provenance.

## Acceptable Fallback Criteria

A fallback can be acceptable when all of these are true:

- it is explicitly requested by the user, CLI flag, config field, or API input
- the fallback path is documented as part of the product contract
- the result reports which path was actually used
- failures on every attempted path are returned or recorded with enough detail
- tests prove the fallback is not silently used when it was not requested

Examples:

- `--mirror-strategy ordered` may try multiple mirrors if the result reports
  attempted URLs and final selected URL.
- `--retry 3` may retry a network operation if retry count and final error are
  visible.
- cleanup after a failed temp-file write may ignore remove errors if the primary
  operation still returns the real write error and cleanup failure cannot alter
  product state.

## Suspicious Fallback Criteria

A fallback should be treated as suspicious when any of these are true:

- the caller cannot tell that fallback happened
- the code returns success after changing source, mode, target, or provenance
- a missing required file becomes a different data source
- an invalid record is skipped instead of blocking the operation
- the result looks resolved even though required metadata could not be loaded
- stderr logs are the only record of the fallback path
- tests only prove the fallback works, not that it is opt-in

## Audit Output Format

Each finding should be written as a discussion item, not as an immediate fix.

Use this structure:

```text
ID:
Code path:
Evidence:
Observed behavior:
Why it may violate No Fallback:
Failure being protected against:
Why explicit failure might be acceptable:
Why fallback might still be necessary:
Decision needed:
Test expectation if rejected:
Test expectation if accepted:
Status:
```

Status values:

- `pending discussion`
- `accepted explicit fallback`
- `reject hidden fallback`
- `needs runtime verification`
- `not a violation`

## Recommended Cadence

Run this check periodically, especially before release or after large changes in:

- initialization and bootstrap
- config rendering and config patching
- subscription loading and provenance
- runtime lifecycle and router takeover
- download/update flows
- MCP tools that mutate state
- status and diagnostic commands

Suggested cadence:

- before each release candidate
- after major router/runtime workflow changes
- after adding compatibility or migration code
- after adding retry, mirror, cache, or recovery logic

## Review Workflow

1. Search for suspicious terms such as `fallback`, `legacy`, `default`,
   `continue`, ignored errors, missing-file handling, retries, mirrors, and
   auto-created state.
2. Exclude product/config semantics that are intentionally named fallback, such
   as Mihomo DNS or routing semantics.
3. For each remaining item, read the actual code path and identify whether
   product behavior can change silently.
4. Record the item in an audit document with evidence and discussion questions.
5. Discuss items one by one.
6. Only after a decision, implement the chosen behavior and add tests that prove
   the fallback is either removed or explicit.

## Current Related Audit

The first discussion draft is:

- `docs/no-fallback-engineering-audit-2026-05-31.md`

That audit should be treated as raw findings for discussion, not as an approved
change list.
