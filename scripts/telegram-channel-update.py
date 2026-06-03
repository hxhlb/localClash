#!/usr/bin/env python3
"""Generate and optionally post a localClash Telegram channel update.

The script reads docs/changelog.md, extracts the latest dated release section,
writes a Telegram Markdown announcement to telegram/changelog.md, and can post
it to the same Telegram channel used by Syncnext.
"""

from __future__ import annotations

import argparse
import json
import mimetypes
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
import uuid
from pathlib import Path

CAPTION_LIMIT = 1024
MESSAGE_LIMIT = 4096
DEFAULT_CHAT_ID = "@RonnieAppsChannel"
DEFAULT_SYNCNEXT_TOKEN_FILE = Path("/Volumes/Data/Github/SyncnextProjects/Syncnext/telegram/.token")
DEFAULT_IMAGE_NAME = "localclash-telegram-update-handdrawn-16x9.png"


class ScriptError(RuntimeError):
    pass


def read_text(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def read_token_from_file(path: Path | None) -> str | None:
    if path is None or not path.exists():
        return None
    token = read_text(path).strip()
    return token or None


def latest_dated_section(markdown: str) -> tuple[str, str]:
    matches = list(re.finditer(r"^## (\d{4}-\d{2}-\d{2})\s*$", markdown, re.MULTILINE))
    if not matches:
        raise ScriptError("No dated changelog section found.")
    match = matches[0]
    start = match.end()
    next_match = re.search(r"^## \S+", markdown[start:], re.MULTILINE)
    end = start + next_match.start() if next_match else len(markdown)
    return match.group(1), markdown[start:end].strip()


def strip_markdown_link(text: str) -> str:
    return re.sub(r"\[([^\]]+)\]\(([^)]+)\)", r"\1: \2", text)


def telegram_escape_legacy(text: str) -> str:
    # Legacy Telegram Markdown is used to match the Syncnext sender. Avoid
    # accidental formatting in prose while preserving explicit code backticks.
    parts = text.split("`")
    for idx in range(0, len(parts), 2):
        parts[idx] = parts[idx].replace("*", "").replace("_", "")
    return "`".join(parts)


def normalize_bullet(line: str) -> str:
    line = line.strip()
    if line.startswith("- "):
        line = line[2:].strip()
    return "- " + telegram_escape_legacy(strip_markdown_link(line))


def extract_release_url(block: str) -> str | None:
    match = re.search(r"Release:\s*\n\s*\[[^\]]+\]\(([^)]+)\)", block)
    if match:
        return match.group(1)
    return None


def join_wrapped_line(current: str, continuation: str) -> str:
    continuation = continuation.strip()
    if not current:
        return continuation
    if not continuation:
        return current
    if current[-1].isascii() and continuation[0].isascii():
        return current + " " + continuation
    return current + continuation


def extract_changes(block: str) -> list[str]:
    match = re.search(r"^Changes:\s*$", block, re.MULTILINE)
    if not match:
        return []
    tail = block[match.end() :]
    next_section = re.search(r"^[A-Z][A-Za-z ]+:\s*$", tail, re.MULTILINE)
    if next_section:
        tail = tail[: next_section.start()]

    changes: list[str] = []
    current = ""
    for raw_line in tail.splitlines():
        line = raw_line.rstrip()
        if not line.strip():
            continue
        if line.lstrip().startswith("- "):
            if current:
                changes.append(current.strip())
            current = line.strip()
            continue
        if current:
            current = join_wrapped_line(current, line)
    if current:
        changes.append(current.strip())
    return changes


def split_release_blocks(section: str) -> list[tuple[str, str]]:
    headings = list(re.finditer(r"^### (.+?)\s*$", section, re.MULTILINE))
    blocks: list[tuple[str, str]] = []
    for idx, heading in enumerate(headings):
        start = heading.end()
        end = headings[idx + 1].start() if idx + 1 < len(headings) else len(section)
        blocks.append((heading.group(1).strip(), section[start:end].strip()))
    return blocks


def build_telegram_message(changelog: str) -> tuple[str, str]:
    date, section = latest_dated_section(changelog)
    blocks = split_release_blocks(section)
    if not blocks:
        raise ScriptError(f"No release blocks found under {date}.")

    lines = ["*localClash 更新日誌*", date]
    extracted_blocks = 0
    for title, block in blocks:
        changes = extract_changes(block)
        release_url = extract_release_url(block)
        if not changes:
            continue

        extracted_blocks += 1
        lines.append("")
        lines.append(f"*{telegram_escape_legacy(title)}*")
        for change in changes:
            lines.append(normalize_bullet(change))
        if release_url:
            lines.append(f"Release: {release_url}")

    message = "\n".join(lines).strip() + "\n"
    if extracted_blocks == 0:
        raise ScriptError(f"No user-facing changes extracted under {date}.")
    return date, message


def build_multipart(fields: dict[str, str], files: dict[str, tuple[str, bytes, str]]) -> tuple[bytes, str]:
    boundary = f"----tg-boundary-{uuid.uuid4().hex}"
    lines: list[bytes] = []

    for name, value in fields.items():
        lines.append(f"--{boundary}".encode())
        lines.append(f'Content-Disposition: form-data; name="{name}"'.encode())
        lines.append(b"")
        lines.append(value.encode())

    for name, (filename, data, mime) in files.items():
        lines.append(f"--{boundary}".encode())
        lines.append(f'Content-Disposition: form-data; name="{name}"; filename="{filename}"'.encode())
        lines.append(f"Content-Type: {mime}".encode())
        lines.append(b"")
        lines.append(data)

    lines.append(f"--{boundary}--".encode())
    lines.append(b"")
    return b"\r\n".join(lines), f"multipart/form-data; boundary={boundary}"


def http_post(url: str, fields: dict[str, str], files: dict[str, tuple[str, bytes, str]] | None = None) -> dict:
    if files:
        body, content_type = build_multipart(fields, files)
        headers = {"Content-Type": content_type}
    else:
        body = urllib.parse.urlencode(fields).encode()
        headers = {"Content-Type": "application/x-www-form-urlencoded"}

    request = urllib.request.Request(url, data=body, headers=headers, method="POST")
    try:
        with urllib.request.urlopen(request) as response:
            data = response.read().decode("utf-8")
    except urllib.error.HTTPError as exc:
        data = exc.read().decode("utf-8", errors="replace")
        raise ScriptError(f"HTTP {exc.code} {exc.reason}: {data}") from exc
    except urllib.error.URLError as exc:
        raise ScriptError(f"Request failed: {exc.reason}") from exc

    try:
        return json.loads(data)
    except json.JSONDecodeError as exc:
        raise ScriptError(f"Invalid JSON response: {data}") from exc


def chunk_text(text: str, limit: int) -> list[str]:
    chunks: list[str] = []
    remaining = text
    while len(remaining) > limit:
        split_at = remaining.rfind("\n", 0, limit)
        if split_at == -1:
            split_at = remaining.rfind(" ", 0, limit)
        if split_at == -1:
            split_at = limit
        chunks.append(remaining[:split_at].rstrip("\n"))
        remaining = remaining[split_at:].lstrip("\n")
    if remaining:
        chunks.append(remaining)
    return chunks


def send_message(api_base: str, chat_id: str, text: str, parse_mode: str | None) -> dict:
    fields = {"chat_id": chat_id, "text": text}
    if parse_mode:
        fields["parse_mode"] = parse_mode
    return http_post(f"{api_base}/sendMessage", fields)


def send_photo(api_base: str, chat_id: str, image_path: Path, caption: str | None, parse_mode: str | None) -> dict:
    mime, _ = mimetypes.guess_type(str(image_path))
    if not mime:
        mime = "application/octet-stream"
    fields = {"chat_id": chat_id}
    if caption:
        fields["caption"] = caption
    if parse_mode:
        fields["parse_mode"] = parse_mode
    files = {"photo": (image_path.name, image_path.read_bytes(), mime)}
    return http_post(f"{api_base}/sendPhoto", fields, files)


def post_to_telegram(token: str, chat_id: str, message: str, image: Path | None, parse_mode: str | None) -> None:
    api_base = f"https://api.telegram.org/bot{token}"
    if image:
        if len(message) <= CAPTION_LIMIT:
            response = send_photo(api_base, chat_id, image, message, parse_mode)
            if not response.get("ok"):
                raise ScriptError(f"sendPhoto failed: {response}")
            print("Posted Telegram photo with caption.")
            return

        response = send_photo(api_base, chat_id, image, None, None)
        if not response.get("ok"):
            raise ScriptError(f"sendPhoto failed: {response}")

    for chunk in chunk_text(message, MESSAGE_LIMIT):
        response = send_message(api_base, chat_id, chunk, parse_mode)
        if not response.get("ok"):
            raise ScriptError(f"sendMessage failed: {response}")
    print("Posted Telegram message.")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Generate or post a localClash Telegram channel update.")
    repo_root = Path(__file__).resolve().parents[1]
    parser.add_argument("--changelog", type=Path, default=repo_root / "docs/changelog.md")
    parser.add_argument("--output", type=Path, default=repo_root / "telegram/changelog.md")
    parser.add_argument("--chat-id", default=DEFAULT_CHAT_ID)
    parser.add_argument("--token", default=None, help="Telegram bot token. Prefer env or token files.")
    parser.add_argument("--token-file", type=Path, default=repo_root / "telegram/.token")
    parser.add_argument(
        "--syncnext-token-file",
        type=Path,
        default=DEFAULT_SYNCNEXT_TOKEN_FILE,
        help="Fallback token file used by the existing Syncnext Telegram workflow.",
    )
    parser.add_argument(
        "--image",
        type=Path,
        default=repo_root / "telegram/out" / DEFAULT_IMAGE_NAME,
        help="Image to send before/as the update.",
    )
    parser.add_argument("--no-image", action="store_true", help="Send text only, without the default image.")
    parser.add_argument("--parse-mode", default="Markdown", help="Telegram parse mode. Use empty string for plain text.")
    parser.add_argument("--dry-run", action="store_true", help="Generate and print the message without posting.")
    parser.add_argument("--no-write", action="store_true", help="Print/generate without writing --output.")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    date, message = build_telegram_message(read_text(args.changelog))

    if not args.no_write:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(message, encoding="utf-8")

    if args.dry_run:
        print(message, end="")
        if not args.no_write:
            print(f"\nGenerated Telegram update: {args.output}", file=sys.stderr)
        print(f"Source changelog date: {date}", file=sys.stderr)
        return 0

    token = (
        args.token
        or os.environ.get("TELEGRAM_BOT_TOKEN")
        or read_token_from_file(args.token_file)
        or read_token_from_file(args.syncnext_token_file)
    )
    if not token:
        raise ScriptError("Missing Telegram bot token. Set TELEGRAM_BOT_TOKEN or create telegram/.token.")

    image = None if args.no_image else args.image
    if image and not image.exists():
        raise ScriptError(f"Image file not found: {image}")
    parse_mode = args.parse_mode or None
    post_to_telegram(token, args.chat_id, message, image, parse_mode)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except ScriptError as exc:
        print(f"Error: {exc}", file=sys.stderr)
        raise SystemExit(1)
