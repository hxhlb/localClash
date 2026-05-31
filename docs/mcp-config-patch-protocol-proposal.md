# MCP Config Patch Protocol Proposal - 2026-05-31

This proposal refines the localClash MCP config patch workflow after a real
router maintenance session exposed a protocol shape gap: adding or updating
routing intent fits the current patch workflow, but removing a previously
created strategy does not.

The proposed MCP tool shape is still compact and sound, but this is a breaking
protocol rewrite. The old flattened-candidate implementation model should be
replaced, not wrapped with compatibility aliases: patches should be the durable
product-level resources, and `localclash-intent.json` should become the
compiled final intent produced by applying those patches.

## Problem

Current flattened-candidate flow:

- `config_status`: inspect durable config, generated config, render readiness,
  and pending patch artifacts.
- `config_patch_create`: create a candidate by merging an overlay into the
  flattened config. It currently accepts an overlay with upsert semantics.
- `config_patch_apply`: apply a reviewed candidate by writing durable config,
  deriving pack selection, and regenerating `generated/mihomo.yaml`.

The current protocol shape is ambiguous because there are two different
patch-like things:

- A preview/review computation that should not become durable state.
- A durable product strategy that should be manageable as a patch.

This ambiguity matters when a user asks to delete a strategy. The correct target
is the durable patch, not a guessed set of `policy_groups`, `packs`,
`proxy_groups`, or `custom_rules` from the merged effective config.

Current implementation constraint:

- `localclash-intent.json` is treated as the flattened durable source of truth.
- Existing review artifacts are not active strategy ownership.
- `config_patch_create` merges an overlay into the flattened config with
  upsert semantics.
- Therefore there is currently no durable patch resource for
  `config_patch_get`, `remove_patch`, disable, enable, tombstone, or reorder to
  address.

The fix is not to add object-level delete fields to every overlay collection.
The fix is to make the patch registry the durable source of truth.

Because this project has not published the old flattened-candidate protocol as
a stable user-facing contract, the rewrite should not preserve
`config_patch_create` or the old overlay-only input shape as deprecated aliases.

## Design Principles

- Keep one review boundary and one apply/build boundary.
- Treat active patches as first-class product resources.
- Treat `localclash-intent.json` as a compiled final intent artifact, not the
  routine mutation target.
- Do not make agents reverse-engineer patch ownership from flattened effective
  config.
- Do not create a second persisted draft state. `config_patch_draft` owns one
  in-memory current draft slot per workspace/config target; only
  `config_patch_apply` persists.
- Let patch registry state model enable, disable, remove, reorder, and tombstone
  operations naturally.
- Make build and apply deterministic: patch ordering, overlay composition,
  validation, provenance, and filesystem commits must be specified as product
  contracts.
- Preserve a compact MCP surface so agents do not choose among many CRUD tools.
- Treat this as a breaking protocol rewrite: no deprecated alias, no fallback
  path for the old flattened candidate input shape.

## Implementation Model

The implementation should become:

```text
initialize
-> policy-templates/
-> active patch registry
-> localclash-intent.json
-> localclash-packs.gob
-> generated/mihomo.yaml
```

The active patch registry is the source of truth. `localclash-intent.json` is
the final intent built from the registry. `generated/mihomo.yaml` is the runtime
config built from that final intent.

Recommended file roles:

```text
policy-templates/              recoverable default patch sources
patches/                        active patch registry files
localclash-intent.json          compiled final intent artifact
localclash-packs.gob            compiled pack-selection artifact
generated/mihomo.yaml           compiled Mihomo runtime artifact
.runtime/backups/               apply/build backup history
.runtime/logs/                  operational logs
.runtime/diagnostics/           diagnostic artifacts
```

`policy-templates/` does not need to be mutated through MCP. It is a recoverable
default source. Initialization imports the selected policy template patches into
the active patch registry. Restoring defaults can rebuild registry entries from
`policy-templates/`.

`patches/` is the active patch registry. Each active patch is one JSON file:

```text
patches/
  default.region-exits.v1_region-exits.json
  default.direct-baseline.v1_direct-baseline.json
  default.ai-dev-speedtest.v1_ai-dev-speedtest.json
  user.ip-194.221.250.50-proxy_proxy-ip.json
```

The filename is for human scanning and stable diffs; the authoritative identity
is still the `patch_id` inside the file. Implementations must reject duplicate
`patch_id` values even if filenames differ.

Patch filenames must be canonical:

```text
<safe_patch_id>_<safe_title>.json
```

- `safe_patch_id` is derived from `patch_id` by allowing only letters, digits,
  dot, underscore, and hyphen; any other character is rejected before write.
- `safe_title` is a lowercase slug derived from `title`, limited to
  `[a-z0-9-]`; it is optional display context and never authoritative.
- Path separators, `..`, empty ids, case-only duplicates, and filenames whose
  internal `patch_id` does not match the canonical id are invalid.
- If two files contain the same `patch_id`, registry loading fails before build.

No `.runtime/drafts` directory is needed. If draft output is persisted, it
duplicates the patch status lifecycle already represented by `patches/*.json`.
If draft output is not persisted, a directory has no purpose.

Instead, the MCP server should maintain a single in-memory current draft slot
for the active workspace/config target:

- `config_patch_draft` computes review data and stores it in that one slot.
- A second `config_patch_draft` call replaces the current slot; there is no
  draft history and no draft id.
- `config_patch_apply` applies the current slot by default, or applies explicit
  `operations` plus `base_hashes` supplied by the caller.
- A successful apply clears the current slot.
- A stale-hash apply failure marks the current slot stale. The agent can inspect
  the failure, but the stale slot cannot be applied again; a new
  `config_patch_draft` call must replace it.
- Process restart clears the current slot; this is acceptable because
  `patches/*.json` is the only durable patch state.

This keeps the review/apply workflow ergonomic without adding another lifecycle
beside `patches/*.json`.

## Patch Registry

Each patch file should be self-contained:

```json
{
  "version": 1,
  "patch_id": "user.ip-194.221.250.50-proxy",
  "title": "Proxy IP 194.221.250.50",
  "source": "user",
  "status": "enabled",
  "order_id": "1200.500000",
  "summary": "Proxy 194.221.250.50",
  "overlay": {
    "custom_rules": [
      {
        "id": "ip-194.221.250.50-proxy",
        "target": "AUTO",
        "rules": [
          { "type": "ip_cidr", "value": "194.221.250.50/32" }
        ]
      }
    ]
  }
}
```

A policy-template imported patch uses the same file shape, with provenance:

```json
{
  "version": 1,
  "patch_id": "default.ai-dev-speedtest.v1",
  "title": "AI Development Speed Test",
  "source": "policy_template",
  "source_ref": "policy-templates/localclash-default.d/30-ai-dev-speedtest.json",
  "status": "enabled",
  "order_id": "0500.000000",
  "summary": "Add ChatGPT, AI, GitHub, and speed-test business groups.",
  "overlay": {
    "proxy_groups": [],
    "policy_groups": [],
    "packs": [],
    "transport_rules": [],
    "custom_rules": [],
    "enabled_rule_packs": [],
    "rule_providers": []
  }
}
```

Patch order is part of the product contract because Mihomo rules are
order-sensitive. The registry uses `order_id`, stored as a fixed-width decimal
string, as the build order key:

```text
format:      DDDD.DDDDDD
example:     1200.500000
build order: lexicographic order_id ascending, then patch_id ascending
```

`order_id` must not be parsed as a JSON number or Go `float64`; that would make
configuration order depend on floating-point precision. The fixed-width format
means ordinary string comparison is sufficient after validation. `order_id`
must be unique among all non-tombstoned patches.

Use broad ranges for template defaults and user patches, for example:

```text
0000.xxxxxx  reserved safety baseline
0100.xxxxxx  default region exits
0200.xxxxxx  default direct baseline
0300.xxxxxx  default QUIC / transport
0400.xxxxxx  default communication and social
0500.xxxxxx  default AI, development, and speed test
0600.xxxxxx  default platform and media
0700.xxxxxx  default games
0800.xxxxxx  default tail fallback
1000.xxxxxx  user and integration patches
```

Insert operations can allocate a midpoint order id:

```text
before: 1200.000000
after:  1201.000000
new:    1200.500000
```

If repeated insertions consume too much decimal space between neighbors,
`normalize_order_ids` may rewrite `order_id` values into wider gaps while
preserving the effective order. It is an explicit reviewed operation, not an
implicit side effect. Patch identity is `patch_id`, not `order_id`, so
normalization must not change patch ownership or provenance.

Patch status values:

- `enabled`: participates in the build.
- `disabled`: keeps content and `order_id` in the registry but excludes it from
  the build.
- `tombstoned`: records an intentional removal, especially for patches that can
  be restored from `policy-templates/`. Tombstones should also keep `order_id`
  so default reimport does not silently reintroduce a patch at a different
  position.

`remove_patch` behavior is fixed:

- `source=policy_template`: mark the patch `tombstoned` so default reimport does
  not silently restore it.
- `source=user`: remove the patch file by default.
- If a user patch should be retained as an audit marker, callers must use
  `set_patch_status(status=tombstoned)` explicitly instead of `remove_patch`.

Tombstones distinguish "the default exists on disk" from "the user explicitly
does not want this default active right now".

## Build Composition

The build algorithm is normative:

1. Load every `patches/*.json` file.
2. Reject invalid schema versions, invalid filenames, duplicate `patch_id`
   values, duplicate non-tombstoned `order_id` values, and unsupported patch
   statuses.
3. Compute `registry_hash` from the canonical JSON representation of all patch
   ids, statuses, order ids, content hashes, selected `policy_template`, and
   builder schema version.
4. Select patches with `status=enabled`.
5. Sort by `order_id`, then `patch_id`.
6. Merge overlays in order into an empty effective localClash intent.
7. Validate references, duplicate provided objects, rule/provider targets,
   selected packs, and generated Mihomo config before committing.

Overlay identity and collision rules:

- `proxy_groups`, `policy_groups`: keyed by object id.
- `packs`: keyed by `{source, pack}`.
- `transport_rules`, `custom_rules`, `enabled_rule_packs`, `rule_providers`:
  keyed by `id`.
- If two enabled patches provide the same key with identical canonical content,
  the duplicate is accepted and recorded in provenance.
- If two enabled patches provide the same key with different content, build
  fails with a conflict. Agents must disable, tombstone, remove, or edit one of
  the patches explicitly.
- Lists inside one object keep the order written in that patch.

The compiled `localclash-intent.json` should include generated metadata showing
it was built from `patches/*.json`, including `registry_hash` and build time.
Code paths that attempt to directly mutate compiled intent should reject the
file or force callers through patch operations.

## Apply Means Build

`config_patch_apply` should no longer mean "merge this overlay into
`localclash-intent.json`". It should mean:

```text
apply reviewed registry operation
-> write active patch files under patches/
-> build localclash-intent.json
-> build localclash-packs.gob
-> build generated/mihomo.yaml
```

That makes apply the durable mutation boundary and the build boundary.

The apply transaction should cover:

- active patch registry
- compiled `localclash-intent.json`
- compiled `localclash-packs.gob`
- compiled `generated/mihomo.yaml`

Apply must use a workspace-level lock and a staged commit:

1. Acquire the config apply lock.
2. Recompute `registry_hash` and verify supplied hashes/generation.
3. Write changed patch files and compiled artifacts into a staging directory on
   the same filesystem.
4. Run mandatory validation against the staged registry and artifacts.
5. Backup affected active patch files and currently active compiled artifacts.
6. Atomically rename staged files into place and fsync containing directories.
7. On failure before commit, leave active state unchanged. On failure during
   commit, roll back from backups and report whether rollback verification
   passed.

Failure reporting should preserve the existing config-patch guarantees: active
state unchanged or rolled back, affected patch backups preserved, backup paths
reported, and next actions explicit.

## Proposed Tool Surface

Keep the config workflow compact:

| Tool | Role |
| --- | --- |
| `config_status` | Existing overview tool. Extend it with patch registry summaries when requested. |
| `config_patch_get` | Read one active patch with full overlay content. |
| `config_patch_draft` | Preview patch registry operations in one in-memory current draft slot. It returns impact and apply arguments, but writes no files. |
| `config_patch_apply` | Apply reviewed operations: mutate registry, then build final artifacts. |

The proposal intentionally does not add separate
`config_patch_list/delete/enable/disable/reorder` MCP tools. Those operations
should be represented as patch operations. `config_patch_draft` previews them;
`config_patch_apply` persists them.

The old flattened-candidate tool shape is removed. There is no
`config_patch_create` compatibility alias, and `config_patch_draft` only accepts
operation-style input.

CLI sugar such as `localclash patch rm <patch_id>` can exist, but it should use
the same apply/build implementation underneath.

## Resource Names

Use distinct identifiers:

- `patch_id`: active patch registry id.

Avoid using `patch_id` for review artifacts in new APIs. New APIs should not
need a persisted review-artifact id.

## `config_status` Patch Inventory

`config_status` should remain the list-like entrypoint. A parameter such as
`patches: true` or `detail: "patches"` can include compact active patch
inventory:

```json
{
  "patches": [
    {
      "patch_id": "user.chatgpt-us-jp",
      "source": "user",
      "status": "enabled",
      "order_id": "1200.500000",
      "summary": "ChatGPT via US and JP exits",
      "provides": [
        "policy_groups[ChatGPT]",
        "packs[blackmatrix7/OpenAI -> ChatGPT]"
      ]
    }
  ],
  "registry_hash": "registry-content-sha256",
  "artifacts": {
    "patch_registry": "patches/",
    "final_intent": "localclash-intent.json",
    "generated": "generated/mihomo.yaml"
  }
}
```

This avoids a separate `config_patch_list` MCP tool while still giving agents a
safe way to discover patch ids.

## `config_patch_get`

`config_patch_get` reads one active patch:

```json
{
  "patch_id": "user.chatgpt-us-jp"
}
```

Expected output:

```json
{
  "patch_id": "user.chatgpt-us-jp",
  "source": "user",
  "status": "enabled",
  "order_id": "1200.500000",
  "sha256": "patch-content-sha256",
  "registry_hash": "registry-content-sha256",
  "overlay": {
    "proxy_groups": [],
    "policy_groups": [],
    "packs": [],
    "transport_rules": [],
    "custom_rules": [],
    "enabled_rule_packs": [],
    "rule_providers": []
  },
  "provides": [],
  "referenced_by": []
}
```

Use this only when the agent needs to modify, explain, or review the full patch
content. Routine discovery should stay in `config_status`.

## `config_patch_draft`

`config_patch_draft` is a preview tool backed by a single in-memory current
draft slot. It does not modify active patch files and does not write review
files. It supports operation-style input so add, update, remove, disable,
enable, tombstone, and reorder can all share the same review and apply argument
shape.

Draft lifecycle:

- There is exactly one current draft per workspace/config target.
- A new draft replaces the previous current draft instead of creating another
  draft resource.
- The current draft contains normalized operations, base hashes, impact summary,
  base registry hash, generation, and optional validation result.
- The current draft is volatile process memory. It is not a recovery mechanism.
- Apply either uses the current draft or explicit apply args with the same
  shape. No `draft_id` is ever needed.
- Apply with `use_current_draft=true` must include the reviewed `generation`;
  a mismatched generation is rejected.

Operation semantics:

- `upsert_patch` creates or fully replaces one active patch. Omitted overlay
  fields are empty, not "retain the old value".
- When modifying an existing patch, agents should call `config_patch_get` first,
  edit the complete returned overlay, and then draft `upsert_patch`.
- Existing patch updates should include an optimistic concurrency guard such as
  `base_patch_sha256` so stale agent context cannot overwrite a newer strategy.
- `remove_patch` removes `source=user` patch files and tombstones
  `source=policy_template` patches. It does not delete guessed individual
  objects from the compiled final intent. If a user patch should be retained as
  an audit marker, callers must use `set_patch_status(status=tombstoned)`.
- `set_patch_status` changes patch status, for example `enabled`, `disabled`,
  or `tombstoned`.
- `reorder_patch` changes `order_id` only; it must not modify overlay content.
  It may accept an explicit `order_id` or allocate a midpoint from
  `before_patch_id` / `after_patch_id`.

`draft_name` is optional display text for the current in-memory draft. It is not
an identifier, is not persisted, and cannot be used to apply or recover a draft.

Upsert example:

```json
{
  "test": true,
  "draft_name": "chatgpt-us-jp",
  "operations": [
    {
      "op": "upsert_patch",
      "patch_id": "user.chatgpt-us-jp",
      "base_patch_sha256": "optional-existing-patch-sha256",
      "order_id": "1200.500000",
      "summary": "ChatGPT via US and JP exits",
      "overlay": {
        "proxy_groups": [],
        "policy_groups": [],
        "packs": [],
        "transport_rules": [],
        "custom_rules": [],
        "enabled_rule_packs": [],
        "rule_providers": []
      }
    }
  ]
}
```

Remove example:

```json
{
  "test": true,
  "draft_name": "remove-chatgpt-us-jp",
  "operations": [
    {
      "op": "remove_patch",
      "patch_id": "user.chatgpt-us-jp"
    }
  ]
}
```

Disable example:

```json
{
  "test": true,
  "draft_name": "disable-chatgpt-us-jp",
  "operations": [
    {
      "op": "set_patch_status",
      "patch_id": "user.chatgpt-us-jp",
      "status": "disabled"
    }
  ]
}
```

Reorder example:

```json
{
  "test": true,
  "draft_name": "move-chatgpt-before-ai-defaults",
  "operations": [
    {
      "op": "reorder_patch",
      "patch_id": "user.chatgpt-us-jp",
      "before_patch_id": "default.ai-dev-speedtest.v1",
      "after_patch_id": "default.communication-social.v1"
    }
  ]
}
```

The draft output should include:

- current draft metadata such as a monotonic in-memory `generation`
- normalized `operations`
- `base_hashes` for affected patch files
- `base_registry_hash` for patch set membership, status, order, schema, and
  builder-version protection
- `apply_args` for the reviewed current draft, directly passable to
  `config_patch_apply`
- compact impact summary
- validation status if `test=true`
- next action: call `config_patch_apply` with `apply_args`

Example output shape:

```json
{
  "valid": true,
  "current_draft": {
    "generation": 7
  },
  "impact": {
    "patches_changed": ["user.chatgpt-us-jp"]
  },
  "operations": [
    {
      "op": "remove_patch",
      "patch_id": "user.chatgpt-us-jp"
    }
  ],
  "base_hashes": {
    "user.chatgpt-us-jp": "patch-content-sha256"
  },
  "base_registry_hash": "registry-content-sha256",
  "apply_args": {
    "use_current_draft": true,
    "generation": 7
  },
  "explicit_apply_args": {
    "operations": [
      {
        "op": "remove_patch",
        "patch_id": "user.chatgpt-us-jp"
      }
    ],
    "base_hashes": {
      "user.chatgpt-us-jp": "patch-content-sha256"
    },
    "base_registry_hash": "registry-content-sha256"
  }
}
```

## `config_patch_apply`

`config_patch_apply` remains the durable mutation and build boundary. It can
apply the current in-memory draft:

```json
{
  "use_current_draft": true,
  "generation": 7
}
```

It can also accept the same operation shape previewed by `config_patch_draft`,
plus base hashes:

```json
{
  "operations": [
    {
      "op": "remove_patch",
      "patch_id": "user.chatgpt-us-jp"
    }
  ],
  "base_hashes": {
    "user.chatgpt-us-jp": "patch-content-sha256"
  },
  "base_registry_hash": "registry-content-sha256",
  "generation": 7
}
```

Before writing, apply must verify that every affected patch file still matches
the supplied base hash and that the registry still matches
`base_registry_hash`. If any hash is stale, apply refuses and asks the agent to
re-run `config_patch_get` or `config_patch_draft`. If the failed request used
the current in-memory draft, that draft is marked stale and cannot be retried.

When `use_current_draft=true`, apply uses the current in-memory draft's
operations and hashes. The request must include the reviewed `generation`. If
there is no current draft or the generation does not match, apply refuses and
asks the agent to call `config_patch_draft`.

## Routing Integration

`routing_explain` should not replace patch inventory or patch reads. It is a
route-result query tool. It can help bridge from behavior to config by returning
patch provenance:

```json
{
  "query": "ChatGPT",
  "provided_by_patches": ["user.chatgpt-us-jp"],
  "next_actions": [
    "call config_status with patches=true to inspect patch inventory",
    "call config_patch_get with patch_id=user.chatgpt-us-jp before modifying it"
  ]
}
```

This keeps `routing_explain` focused on behavior while still making patch-based
follow-up discoverable.

Provenance is not inferred from generated YAML text after the fact. The build
pipeline must carry patch provenance while compiling intent:

- every effective proxy group, policy group, transport rule, custom rule, pack,
  enabled rule pack, and rule provider records the contributing `patch_id`;
- generated summaries can collapse this into `provided_by_patches`;
- when duplicate identical content is accepted, provenance lists all providers;
- when conflicting content exists, build fails before generated artifacts are
  committed.

The inventory `provides` field is a compact direct-overlay summary only. It
should not be treated as full behavior provenance; behavior provenance belongs
in `routing_explain`.

## Common Call Chains

Initialize from defaults:

```text
config_configure(policy_template=localclash-default)
-> import policy-template patches into patches/*.json
-> build localclash-intent.json
-> build generated/mihomo.yaml
```

Create a ChatGPT strategy through US and JP exits:

```text
config_status
-> packs_list / pack_rules_query
-> proxy_group_build
-> policy_group_build
-> config_patch_draft(op=upsert_patch)
-> review current draft impact
-> config_patch_apply(use_current_draft=true, generation=<reviewed_generation>)
```

Delete the same strategy:

```text
config_status(patches=true) or routing_explain("ChatGPT")
-> config_patch_draft(op=remove_patch, patch_id="user.chatgpt-us-jp")
-> review current draft impact
-> config_patch_apply(use_current_draft=true, generation=<reviewed_generation>)
```

Modify an existing strategy:

```text
config_status(patches=true)
-> config_patch_get(patch_id)
-> builder tools if needed
-> config_patch_draft(op=upsert_patch, patch_id, base_patch_sha256, full overlay)
-> review current draft impact
-> config_patch_apply(use_current_draft=true, generation=<reviewed_generation>)
```

Restore default strategies:

```text
config_configure(policy_template=localclash-default, reset_patches=true)
-> reimport policy-template patches from policy-templates/
-> build localclash-intent.json
-> build generated/mihomo.yaml
```

## Bootstrap And Reset

Phase 1: introduce the patch registry and build pipeline.

- Add `patches/*.json` as the durable source of truth.
- Add a builder that compiles enabled registry patches into
  `localclash-intent.json`.
- Move current config render/apply paths to read the compiled final intent.
- Do not add a persisted draft directory; draft state is one in-memory current
  slot per workspace/config target.

Phase 2: initialize and reset from templates.

- Convert the selected policy template into registry patches during
  initialization.
- Because localClash has not published the old flattened-intent patch protocol
  as a stable user-facing storage contract, do not add a
  `config_migrate_to_patches` tool. It would expand the MCP surface for a
  legacy state that does not need to be preserved as a product contract.
- Restoring defaults should use the existing reset path:
  `config_configure(policy_template=..., reset_patches=true)` reimports
  policy-template patches from `policy-templates/` into `patches/*.json` and
  rebuilds artifacts.
- If a developer needs to rescue a local experimental flattened
  `localclash-intent.json`, treat that as a one-off manual/debug operation, not
  a product MCP protocol.
- Preserve `localclash-intent.json` as the compiled artifact consumed by the
  render/runtime pipeline, but mark it as a build artifact in status and docs.
- Add generated metadata to `localclash-intent.json` so old code paths can
  detect it is compiled and refuse direct mutation.

Phase 3: update MCP operations.

- Add `config_patch_get`.
- Add `config_patch_draft` with operation-style input and one in-memory current
  draft slot.
- Change `config_patch_apply` to accept `use_current_draft=true`, or explicit
  `operations` plus `base_hashes`.
- Implement `upsert_patch`, `remove_patch`, `set_patch_status`, and
  `reorder_patch` against the active patch registry.
- Remove the old flattened-candidate tool/input shape. Do not keep
  `config_patch_create` or an overlay-only `config_patch_draft` compatibility
  alias.
- Make validation mandatory for apply: schema, canonical filename, duplicate
  ids, duplicate order ids, overlay conflicts, reference integrity, selected
  packs, provenance, and Mihomo config validation must pass before commit.

Phase 4: retire flattened-object workarounds.

- Stop recommending `nl_file` or `sed_file` for routine strategy removal.
- Update `routing_explain` and builder guidance to point at patch registry
  operations.
- Keep low-level file tools as emergency/debug tools, not the normal MCP
  product workflow.

## Non-Goals

- Do not mutate `policy-templates/` through MCP patch operations.
- Do not add a persisted draft registry beside `patches/*.json`.
- Do not make `routing_explain` return full patch overlays.
- Do not require agents to delete individual merged config objects for routine
  strategy removal.
- Do not expand the MCP surface into separate list/get/delete/enable/disable
  tools unless the compact workflow proves insufficient.
- Do not treat partial `upsert_patch` overlays as merge patches for existing
  patches. Partial merge semantics recreate the original delete gap.
- Do not keep deprecated aliases or fallback schema for the old flattened
  candidate workflow.
