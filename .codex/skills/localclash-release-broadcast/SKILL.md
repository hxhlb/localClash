---
name: localclash-release-broadcast
description: Write and publish localClash release changelogs, X.com changelog cards, and Telegram channel update announcements. Use when Codex needs to update docs/changelog.md, summarize Core and localclash-luci releases, generate the X.com update image, prepare or send Telegram update posts to @RonnieAppsChannel, verify generated release-broadcast files stay out of Git tracking, or maintain the localClash release broadcast workflow.
---

# localClash Release Broadcast

Use this skill inside the `localClash` workspace to produce user-facing release
notes and Telegram channel announcements.

## Workflow

1. Work from the workspace root.
2. Inspect `git status --short --branch` first. Do not stage, revert, or edit
   unrelated user changes.
3. Confirm current release facts from GitHub before writing latest-version
   claims:
   - `gh release list --limit 8` in this repo for Core.
   - `gh release list --limit 8` in `../localclash-luci` for LuCI.
   - Use `gh release view <tag>` when asset lists, publish time, or release URL
     matters.
4. Update `docs/changelog.md`:
   - Keep Core and LuCI as separate release channels.
   - Update the latest-version table.
   - Add a dated section only for channels released on that date.
   - Write user/maintainer-facing impact, not raw commit logs.
   - Include verification evidence when install, update, manifest, OpenWrt
     package, or router behavior changed.
5. Generate the X.com changelog image from the latest dated changelog section:
   ```bash
   scripts/x-release-card.py
   ```
   Inspect `telegram/out/localclash-x-release-card.png` before using it
   in an X.com post. The card must contain changelog content only: no Telegram
   fixed top, no product feature introduction, no right-bottom filler text.
6. Generate and inspect the Telegram announcement:
   ```bash
   scripts/telegram-channel-update.py --dry-run --no-write
   ```
7. For live Telegram posting:
   - If the user explicitly asks to send/post/publish the Telegram notice, run
     `scripts/telegram-channel-update.py`.
   - Otherwise stop after dry-run and ask for approval before posting.
   - The default channel is `@RonnieAppsChannel`.
   - The default image is
     `telegram/out/localclash-telegram-update-handdrawn-16x9.png`.
8. Verify local generated release-broadcast files are ignored:
   ```bash
   git check-ignore -v telegram/changelog.md telegram/.token telegram/out/example.md telegram/sent/example.json telegram/out/localclash-x-release-card.html telegram/out/localclash-x-release-card.png
   ```
9. Run `git diff --check` before claiming the docs/tooling are ready.

## Boundaries

- Never commit or stage `telegram/changelog.md`, `telegram/.token`,
  `telegram/out/`, or `telegram/sent/`.
- Never print Telegram bot tokens.
- Do not treat Core and LuCI releases as the same artifact. Core releases carry
  binaries, base assets, and `localclash-release-manifest.json`; LuCI releases
  carry IPK/APK package artifacts and checksums.
- Do not overwrite public release history. If a LuCI package changed after a
  release, bump `PKG_RELEASE` and publish a new tag instead.

## Reference

For exact paths, commands, and output ownership rules, read
`references/workflow.md`.
