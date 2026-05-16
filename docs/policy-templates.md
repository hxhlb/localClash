# Policy Templates

`docs/rule-model.md` is the authoritative development contract for rule
layering, customization, and the Loyalsoldier boundary. This document only
describes the current starter policy template.

## Initial Choice

The first policy template uses `Loyalsoldier/clash-rules` as the base ruleset.

Reasons:

- The rule categories are small enough for a user to understand.
- The files map cleanly into Mihomo `rule-providers`.
- The template can stay localClash-owned while rule content remains upstream.
- It includes the recommended whitelist and blacklist rule orders from Loyalsoldier.
- Whitelist mode sends unmatched traffic to proxy.
- Blacklist mode sends unmatched traffic direct.
- Rendered configs prepend a local safety baseline before the upstream policy rules.
- The local safety baseline keeps loopback, private LAN ranges, link-local ranges, and local hostnames direct.
- Rendered configs keep `.local`, `.lan`, `.home.arpa`, and `localhost` DNS resolution on the system resolver instead of remote DoH.

Loyalsoldier is the default base routing preset. It is not the immutable local
safety baseline and it should not be modeled as an optional rule pack.

## Boundary

Do not commit upstream rule content into this repository. A policy template should store:

- upstream references
- group mappings
- rule order
- local override slots

Optional rule packs should be modeled separately from policy templates. Rule
packs are user-selectable add-ons; policy templates define the broad public
internet routing mode such as whitelist-first or blacklist-first.

The renderer should turn the policy into a generated Mihomo runtime config under `generated/`, which is local data and ignored by git.

## Starter Template

See `policies/loyalsoldier.yaml`.
