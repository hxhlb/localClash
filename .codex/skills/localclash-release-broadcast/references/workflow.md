# Release Broadcast Workflow Reference

## Files

- Changelog source: `docs/changelog.md`
- Telegram fixed top Markdown: `telegram/top.md`
- Telegram generator/poster: `scripts/telegram-channel-update.py`
- Generated Telegram update body: `telegram/changelog.md`
- Default Telegram image:
  `telegram/localclash-telegram-update.png`
- Local token file: `telegram/.token`
- Fallback Syncnext token file:
  `/Volumes/Data/Github/SyncnextProjects/Syncnext/telegram/.token`

## Git-Tracked Versus Local Files

Tracked source files:

- `docs/changelog.md`
- `telegram/top.md`
- `telegram/localclash-telegram-update.png`
- `scripts/telegram-channel-update.py`
- `.codex/skills/localclash-release-broadcast/**`

Local generated files that must stay ignored:

- `telegram/changelog.md`
- `telegram/.token`
- `telegram/out/`
- `telegram/sent/`

Verify with:

```bash
git check-ignore -v \
  telegram/changelog.md \
  telegram/.token \
  telegram/out/example.md \
  telegram/sent/example.json
```

## Telegram Commands

Preview without writing:

```bash
scripts/telegram-channel-update.py --dry-run --no-write
```

Preview and write the ignored local Markdown:

```bash
scripts/telegram-channel-update.py --dry-run
```

Post to the default channel with the default image:

```bash
scripts/telegram-channel-update.py
```

Post text only:

```bash
scripts/telegram-channel-update.py --no-image
```

Override the channel:

```bash
scripts/telegram-channel-update.py --chat-id @SomeChannel
```

Token lookup order:

1. `TELEGRAM_BOT_TOKEN`
2. `telegram/.token`
3. `/Volumes/Data/Github/SyncnextProjects/Syncnext/telegram/.token`

## Changelog Style

- Use Traditional Chinese for `docs/changelog.md`.
- Keep release links explicit.
- Avoid internal command transcripts in public-facing change bullets.
- Include only current, verified facts for "latest" claims.
- Put verification evidence in a short `Verification:` list when relevant.
