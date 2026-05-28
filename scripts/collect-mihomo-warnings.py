#!/usr/bin/env python3
"""Collect long-running Mihomo log evidence from the external controller."""

from __future__ import annotations

import argparse
import collections
import datetime as dt
import http.client
import json
import os
import re
import signal
import socket
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Any


DEFAULT_API = "http://192.168.6.1:9090"
DEFAULT_SECRET = "123456"
DEFAULT_OUT_ROOT = ".runtime/diagnostics"

CLASSIFIER_PATTERNS: list[tuple[str, re.Pattern[str]]] = [
    (
        "dns_upstream",
        re.compile(
            r"\b(dns|nameserver|fallback|doh|dot)\b|:53\b|:853\b|"
            r"\b8\.8\.8\.8\b|\b1\.1\.1\.1\b",
            re.IGNORECASE,
        ),
    ),
    ("telegram", re.compile(r"\btelegram\b|\b149\.154\.|\b91\.108\.", re.IGNORECASE)),
    (
        "timeout",
        re.compile(
            r"\b(timeout|i/o timeout|deadline|context deadline exceeded|timed out)\b",
            re.IGNORECASE,
        ),
    ),
    (
        "network_error",
        re.compile(
            r"network is unreachable|no route to host|connection refused|"
            r"connection reset|broken pipe|host is down",
            re.IGNORECASE,
        ),
    ),
    (
        "direct_match",
        re.compile(r"\bmatch\b.*\bdirect\b|\bdirect\b.*\bmatch\b", re.IGNORECASE),
    ),
    (
        "rule_match",
        re.compile(r"\b(rule|ruleset|rule-set|geosite|geoip|ipcidr)\b", re.IGNORECASE),
    ),
    (
        "smart_model",
        re.compile(r"\bsmart\b|lightgbm|model\.bin|model-middle", re.IGNORECASE),
    ),
    (
        "geodata",
        re.compile(r"geoip\.dat|geosite\.dat|asn\.mmdb|country\.mmdb|geodata|mmdb", re.IGNORECASE),
    ),
    ("provider", re.compile(r"\b(provider|rule-provider)\b", re.IGNORECASE)),
    ("dial", re.compile(r"\b(dial|connect|outbound|proxy)\b", re.IGNORECASE)),
]

CONFIG_KEYS = (
    "port",
    "socks-port",
    "redir-port",
    "tproxy-port",
    "mixed-port",
    "allow-lan",
    "bind-address",
    "mode",
    "log-level",
    "ipv6",
    "interface-name",
    "geodata-mode",
    "geodata-loader",
    "geosite-matcher",
    "tcp-concurrent",
    "unified-delay",
    "find-process-mode",
    "sniffing",
    "global-ua",
)


def now_iso() -> str:
    return dt.datetime.now(dt.timezone.utc).astimezone().isoformat(timespec="milliseconds")


def utc_stamp() -> str:
    return dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")


def normalize_api(value: str) -> str:
    return value.rstrip("/")


def endpoint(api: str, path: str, query: dict[str, str] | None = None) -> str:
    url = f"{normalize_api(api)}/{path.lstrip('/')}"
    if query:
        return f"{url}?{urllib.parse.urlencode(query)}"
    return url


def request_headers(secret: str) -> dict[str, str]:
    return {
        "Accept": "application/json, text/plain, */*",
        "Authorization": f"Bearer {secret}",
        "User-Agent": "localClash-log-collector/1",
    }


def json_default(value: Any) -> str:
    return str(value)


def env_value(primary: str, fallback: str, default: str) -> str:
    return os.environ.get(primary, os.environ.get(fallback, default))


def env_int(primary: str, fallback: str, default: str) -> int:
    return int(env_value(primary, fallback, default))


class JsonlWriter:
    def __init__(self, path: Path) -> None:
        self.path = path
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self._file = path.open("a", encoding="utf-8")
        self._lock = threading.Lock()

    def write(self, entry: dict[str, Any]) -> None:
        line = json.dumps(entry, ensure_ascii=False, separators=(",", ":"), default=json_default)
        with self._lock:
            self._file.write(line + "\n")
            self._file.flush()

    def close(self) -> None:
        with self._lock:
            self._file.close()


def normalize_log_level(level: Any) -> str:
    if level is None:
        return "unknown"
    return str(level).strip().lower() or "unknown"


def is_warning_level(level: Any) -> bool:
    return normalize_log_level(level) in {
        "warning",
        "warn",
        "error",
        "fatal",
        "panic",
    }


class CollectorState:
    def __init__(self, sample_limit: int) -> None:
        self.started_at = now_iso()
        self.first_log_at: str | None = None
        self.last_log_at: str | None = None
        self.log_count = 0
        self.first_warning_at: str | None = None
        self.last_warning_at: str | None = None
        self.warning_count = 0
        self.level_counts: collections.Counter[str] = collections.Counter()
        self.class_counts: collections.Counter[str] = collections.Counter()
        self.warning_class_counts: collections.Counter[str] = collections.Counter()
        self.tag_counts: collections.Counter[str] = collections.Counter()
        self.samples_by_class: dict[str, list[str]] = collections.defaultdict(list)
        self.sample_limit = sample_limit
        self.snapshots = 0
        self.snapshot_errors = 0
        self.stream_connects = 0
        self.stream_errors = 0
        self._lock = threading.Lock()

    def record_log(
        self,
        timestamp: str,
        level: Any,
        primary: str,
        tags: list[str],
        message: str,
    ) -> None:
        with self._lock:
            normalized_level = normalize_log_level(level)
            self.log_count += 1
            if self.first_log_at is None:
                self.first_log_at = timestamp
            self.last_log_at = timestamp
            self.level_counts[normalized_level] += 1
            self.class_counts[primary] += 1
            if is_warning_level(normalized_level):
                self.warning_count += 1
                if self.first_warning_at is None:
                    self.first_warning_at = timestamp
                self.last_warning_at = timestamp
                self.warning_class_counts[primary] += 1
            for tag in tags:
                self.tag_counts[tag] += 1
            samples = self.samples_by_class[primary]
            if len(samples) < self.sample_limit:
                samples.append(message)

    def record_snapshot(self) -> None:
        with self._lock:
            self.snapshots += 1

    def record_snapshot_error(self) -> None:
        with self._lock:
            self.snapshot_errors += 1

    def record_stream_connect(self) -> None:
        with self._lock:
            self.stream_connects += 1

    def record_stream_error(self) -> None:
        with self._lock:
            self.stream_errors += 1

    def summary(self, *, api: str, level: str, out_dir: Path) -> dict[str, Any]:
        with self._lock:
            return {
                "generated_at": now_iso(),
                "started_at": self.started_at,
                "api": api,
                "level": level,
                "out_dir": str(out_dir),
                "log_count": self.log_count,
                "first_log_at": self.first_log_at,
                "last_log_at": self.last_log_at,
                "level_counts": dict(self.level_counts.most_common()),
                "warning_count": self.warning_count,
                "first_warning_at": self.first_warning_at,
                "last_warning_at": self.last_warning_at,
                "class_counts": dict(self.class_counts.most_common()),
                "warning_class_counts": dict(self.warning_class_counts.most_common()),
                "tag_counts": dict(self.tag_counts.most_common()),
                "samples_by_class": dict(self.samples_by_class),
                "snapshots": self.snapshots,
                "snapshot_errors": self.snapshot_errors,
                "stream_connects": self.stream_connects,
                "stream_errors": self.stream_errors,
            }


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Stream Mihomo logs and collect periodic controller snapshots "
            "for router-template diagnosis."
        )
    )
    parser.add_argument("--api", default=os.environ.get("MIHOMO_API", DEFAULT_API))
    parser.add_argument("--secret", default=os.environ.get("MIHOMO_SECRET", DEFAULT_SECRET))
    parser.add_argument("--level", default=os.environ.get("MIHOMO_LOG_LEVEL", "info"))
    parser.add_argument(
        "--duration",
        type=int,
        default=env_int("MIHOMO_LOG_DURATION", "MIHOMO_WARNING_DURATION", "0"),
        help="Run duration in seconds. 0 means until interrupted.",
    )
    parser.add_argument(
        "--out-dir",
        default=env_value("MIHOMO_LOG_OUT_DIR", "MIHOMO_WARNING_OUT_DIR", ""),
        help="Output directory. Defaults to .runtime/diagnostics/mihomo-logs-<utc>.",
    )
    parser.add_argument(
        "--snapshot-interval",
        type=int,
        default=env_int(
            "MIHOMO_LOG_SNAPSHOT_INTERVAL",
            "MIHOMO_WARNING_SNAPSHOT_INTERVAL",
            "300",
        ),
    )
    parser.add_argument(
        "--summary-interval",
        type=int,
        default=env_int(
            "MIHOMO_LOG_SUMMARY_INTERVAL",
            "MIHOMO_WARNING_SUMMARY_INTERVAL",
            "60",
        ),
    )
    parser.add_argument("--reconnect-delay", type=float, default=5.0)
    parser.add_argument("--http-timeout", type=float, default=20.0)
    parser.add_argument("--stream-timeout", type=float, default=90.0)
    parser.add_argument("--max-body-bytes", type=int, default=20 * 1024 * 1024)
    parser.add_argument("--max-message-bytes", type=int, default=4096)
    parser.add_argument("--sample-limit", type=int, default=5)
    parser.add_argument("--rule-sample-limit", type=int, default=20)
    parser.add_argument(
        "--include-node-names",
        action="store_true",
        help="Keep selected node names in proxy-group snapshots. Defaults to redacting node selections.",
    )
    parser.add_argument(
        "--ssh-host",
        default=env_value("MIHOMO_LOG_SSH_HOST", "MIHOMO_WARNING_SSH_HOST", ""),
        help="Optional read-only process snapshot target, for example root@192.168.6.1.",
    )
    parser.add_argument("--ssh-timeout", type=float, default=10.0)
    parser.add_argument("--print-logs", action="store_true")
    parser.add_argument("--print-warnings", action="store_true")
    return parser.parse_args()


def fetch_json(args: argparse.Namespace, path: str) -> Any:
    req = urllib.request.Request(endpoint(args.api, path), headers=request_headers(args.secret))
    with urllib.request.urlopen(req, timeout=args.http_timeout) as resp:
        body = resp.read(args.max_body_bytes + 1)
    if len(body) > args.max_body_bytes:
        raise RuntimeError(f"{path} response exceeded {args.max_body_bytes} bytes")
    text = body.decode("utf-8", errors="replace")
    return json.loads(text)


def classify_message(message: str) -> tuple[str, list[str]]:
    tags = [name for name, pattern in CLASSIFIER_PATTERNS if pattern.search(message)]
    if not tags:
        tags = ["unknown"]
    return tags[0], tags


def trim_message(message: str, max_bytes: int) -> tuple[str, bool]:
    encoded = message.encode("utf-8")
    if len(encoded) <= max_bytes:
        return message, False
    trimmed = encoded[:max_bytes].decode("utf-8", errors="ignore")
    return trimmed, True


def parse_log_line(text: str) -> dict[str, Any] | None:
    stripped = text.strip()
    if not stripped:
        return None
    if stripped.startswith(":"):
        return None
    if stripped.startswith("event:"):
        return None
    if stripped.startswith("data:"):
        stripped = stripped[5:].strip()
    try:
        decoded = json.loads(stripped)
    except json.JSONDecodeError:
        return {"level": None, "message": stripped, "raw": stripped}
    if not isinstance(decoded, dict):
        return {"level": None, "message": str(decoded), "raw": decoded}
    level = decoded.get("type") or decoded.get("level")
    message = decoded.get("payload") or decoded.get("message") or decoded.get("text")
    if message is None:
        message = json.dumps(decoded, ensure_ascii=False, separators=(",", ":"))
    return {"level": level, "message": str(message), "raw": decoded}


def summarize_config(config: Any) -> dict[str, Any]:
    if not isinstance(config, dict):
        return {"type": type(config).__name__}
    summary = {key: config.get(key) for key in CONFIG_KEYS if key in config}
    dns = config.get("dns")
    if isinstance(dns, dict):
        summary["dns"] = {
            key: dns.get(key)
            for key in (
                "enable",
                "listen",
                "ipv6",
                "enhanced-mode",
                "respect-rules",
                "fake-ip-filter-mode",
            )
            if key in dns
        }
        for key in ("nameserver", "proxy-server-nameserver", "direct-nameserver", "default-nameserver", "fallback"):
            value = dns.get(key)
            if isinstance(value, list):
                summary["dns"][key] = value
                summary["dns"][f"{key}_count"] = len(value)
        policy = dns.get("nameserver-policy")
        if isinstance(policy, dict):
            summary["dns"]["nameserver-policy_keys"] = sorted(str(key) for key in policy.keys())
    tun = config.get("tun")
    if isinstance(tun, dict):
        summary["tun"] = {
            key: tun.get(key)
            for key in (
                "enable",
                "device",
                "stack",
                "dns-hijack",
                "auto-route",
                "auto-detect-interface",
                "auto-redirect",
                "strict-route",
            )
            if key in tun
        }
    geox_url = config.get("geox-url")
    if isinstance(geox_url, dict):
        summary["geox-url_keys"] = sorted(str(key) for key in geox_url.keys())
    return summary


def summarize_proxies(payload: Any, *, include_node_names: bool) -> dict[str, Any]:
    if not isinstance(payload, dict) or not isinstance(payload.get("proxies"), dict):
        return {"type": type(payload).__name__}
    proxies = payload["proxies"]
    type_counts: collections.Counter[str] = collections.Counter()
    groups: list[dict[str, Any]] = []
    alive_counts: collections.Counter[str] = collections.Counter()
    group_names: set[str] = {"COMPATIBLE", "DIRECT", "GLOBAL", "PASS", "REJECT", "REJECT-DROP"}
    for name, item in proxies.items():
        if not isinstance(item, dict):
            continue
        candidates = item.get("all")
        if "now" in item or isinstance(candidates, list):
            group_names.add(str(name))
    for name, item in proxies.items():
        if not isinstance(item, dict):
            continue
        proxy_type = str(item.get("type", "unknown"))
        type_counts[proxy_type] += 1
        alive_counts[str(bool(item.get("alive", False))).lower()] += 1
        candidates = item.get("all")
        is_group = "now" in item or isinstance(candidates, list)
        if is_group:
            selected = item.get("now")
            if (
                not include_node_names
                and isinstance(selected, str)
                and selected not in group_names
                and not selected.startswith("Smart")
            ):
                selected = "[NODE]"
            groups.append(
                {
                    "name": name,
                    "type": proxy_type,
                    "now": selected,
                    "alive": item.get("alive"),
                    "candidate_count": len(candidates) if isinstance(candidates, list) else 0,
                }
            )
    groups.sort(key=lambda item: (str(item.get("type")), str(item.get("name"))))
    return {
        "count": len(proxies),
        "type_counts": dict(type_counts.most_common()),
        "alive_counts": dict(alive_counts),
        "groups": groups,
    }


def summarize_rule(rule: dict[str, Any]) -> dict[str, Any]:
    extra = rule.get("extra") if isinstance(rule.get("extra"), dict) else {}
    return {
        "index": rule.get("index"),
        "type": rule.get("type"),
        "payload": rule.get("payload"),
        "proxy": rule.get("proxy"),
        "size": rule.get("size"),
        "hitCount": extra.get("hitCount"),
        "hitAt": extra.get("hitAt"),
        "missCount": extra.get("missCount"),
        "missAt": extra.get("missAt"),
    }


def summarize_rules(payload: Any, limit: int) -> dict[str, Any]:
    if not isinstance(payload, dict) or not isinstance(payload.get("rules"), list):
        return {"type": type(payload).__name__}
    rules = [item for item in payload["rules"] if isinstance(item, dict)]
    type_counts = collections.Counter(str(rule.get("type", "unknown")) for rule in rules)
    return {
        "count": len(rules),
        "type_counts": dict(type_counts.most_common()),
        "first": [summarize_rule(rule) for rule in rules[:limit]],
        "last": [summarize_rule(rule) for rule in rules[-limit:]] if rules else [],
    }


def sample_processes(args: argparse.Namespace) -> dict[str, Any] | None:
    if not args.ssh_host:
        return None
    command = (
        "date -Iseconds 2>/dev/null || date; "
        "(ps w 2>/dev/null || ps 2>/dev/null) | "
        "grep -E '[m]ihomo|[l]ocalclash' || true"
    )
    started = time.monotonic()
    try:
        result = subprocess.run(
            ["ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", args.ssh_host, command],
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=args.ssh_timeout,
            check=False,
        )
    except (OSError, subprocess.SubprocessError) as exc:
        return {
            "ok": False,
            "duration_ms": round((time.monotonic() - started) * 1000),
            "error": str(exc),
        }
    return {
        "ok": result.returncode == 0,
        "duration_ms": round((time.monotonic() - started) * 1000),
        "returncode": result.returncode,
        "stdout": result.stdout,
        "stderr": result.stderr,
    }


def collect_snapshot(
    args: argparse.Namespace,
    state: CollectorState,
    snapshots: JsonlWriter,
    errors: JsonlWriter,
) -> None:
    started = time.monotonic()
    entry: dict[str, Any] = {"ts": now_iso(), "kind": "snapshot", "api": args.api}
    try:
        entry["configs"] = summarize_config(fetch_json(args, "/configs"))
        entry["proxies"] = summarize_proxies(fetch_json(args, "/proxies"), include_node_names=args.include_node_names)
        entry["rules"] = summarize_rules(fetch_json(args, "/rules"), args.rule_sample_limit)
        process_sample = sample_processes(args)
        if process_sample is not None:
            entry["process_sample"] = process_sample
        entry["duration_ms"] = round((time.monotonic() - started) * 1000)
        snapshots.write(entry)
        state.record_snapshot()
    except Exception as exc:  # noqa: BLE001 - diagnostic script should keep running.
        state.record_snapshot_error()
        errors.write(
            {
                "ts": now_iso(),
                "kind": "snapshot_error",
                "error": repr(exc),
                "duration_ms": round((time.monotonic() - started) * 1000),
            }
        )


def snapshot_loop(
    args: argparse.Namespace,
    state: CollectorState,
    stop: threading.Event,
    snapshots: JsonlWriter,
    errors: JsonlWriter,
) -> None:
    collect_snapshot(args, state, snapshots, errors)
    while not stop.wait(args.snapshot_interval):
        collect_snapshot(args, state, snapshots, errors)


def write_summary_files(
    args: argparse.Namespace,
    state: CollectorState,
    out_dir: Path,
    summaries: JsonlWriter,
) -> None:
    summary = state.summary(api=args.api, level=args.level, out_dir=out_dir)
    summaries.write(summary)
    tmp = out_dir / "summary.json.tmp"
    target = out_dir / "summary.json"
    tmp.write_text(json.dumps(summary, ensure_ascii=False, indent=2, default=json_default) + "\n", encoding="utf-8")
    tmp.replace(target)


def summary_loop(
    args: argparse.Namespace,
    state: CollectorState,
    stop: threading.Event,
    out_dir: Path,
    summaries: JsonlWriter,
) -> None:
    while not stop.wait(args.summary_interval):
        write_summary_files(args, state, out_dir, summaries)


def stream_once(
    args: argparse.Namespace,
    state: CollectorState,
    stop: threading.Event,
    logs: JsonlWriter,
    warnings: JsonlWriter,
    events: JsonlWriter,
    stream_timeout: float,
) -> None:
    log_url = endpoint(args.api, "/logs", {"level": args.level})
    req = urllib.request.Request(log_url, headers=request_headers(args.secret))
    started = time.monotonic()
    with urllib.request.urlopen(req, timeout=stream_timeout) as resp:
        state.record_stream_connect()
        events.write(
            {
                "ts": now_iso(),
                "kind": "stream_connected",
                "url": log_url,
                "status": getattr(resp, "status", None),
            }
        )
        while not stop.is_set():
            raw = resp.readline()
            if not raw:
                break
            parsed = parse_log_line(raw.decode("utf-8", errors="replace"))
            if not parsed:
                continue
            timestamp = now_iso()
            message, truncated = trim_message(str(parsed["message"]), args.max_message_bytes)
            primary, tags = classify_message(message)
            level = parsed.get("level") or args.level
            entry = {
                "ts": timestamp,
                "level": level,
                "class": primary,
                "tags": tags,
                "message": message,
                "truncated": truncated,
                "raw": parsed.get("raw"),
            }
            logs.write(entry)
            if is_warning_level(level):
                warnings.write(entry)
            state.record_log(timestamp, level, primary, tags, message)
            if args.print_logs:
                print(f"[{timestamp}] {normalize_log_level(level)} {primary}: {message}", flush=True)
            elif args.print_warnings and is_warning_level(level):
                print(f"[{timestamp}] {primary}: {message}", flush=True)
    events.write(
        {
            "ts": now_iso(),
            "kind": "stream_disconnected",
            "url": log_url,
            "duration_ms": round((time.monotonic() - started) * 1000),
        }
    )


def stream_loop(
    args: argparse.Namespace,
    state: CollectorState,
    stop: threading.Event,
    logs: JsonlWriter,
    warnings: JsonlWriter,
    events: JsonlWriter,
    errors: JsonlWriter,
) -> None:
    deadline = time.monotonic() + args.duration if args.duration > 0 else None
    while not stop.is_set():
        if deadline is not None and time.monotonic() >= deadline:
            stop.set()
            return
        try:
            stream_timeout = args.stream_timeout
            if deadline is not None:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    stop.set()
                    return
                stream_timeout = min(args.stream_timeout, max(0.25, remaining))
            stream_once(args, state, stop, logs, warnings, events, stream_timeout)
        except (TimeoutError, socket.timeout) as exc:
            events.write({"ts": now_iso(), "kind": "stream_idle_timeout", "detail": repr(exc)})
        except (urllib.error.URLError, http.client.IncompleteRead, OSError) as exc:
            state.record_stream_error()
            errors.write({"ts": now_iso(), "kind": "stream_error", "error": repr(exc)})
        if deadline is not None and time.monotonic() >= deadline:
            stop.set()
            return
        if not stop.wait(args.reconnect_delay):
            events.write(
                {
                    "ts": now_iso(),
                    "kind": "stream_reconnect",
                    "delay_seconds": args.reconnect_delay,
                }
            )


def write_metadata(args: argparse.Namespace, out_dir: Path) -> None:
    metadata = {
        "created_at": now_iso(),
        "api": args.api,
        "level": args.level,
        "duration": args.duration,
        "snapshot_interval": args.snapshot_interval,
        "summary_interval": args.summary_interval,
        "files": {
            "logs": "logs.jsonl",
            "warnings": "warnings.jsonl",
            "snapshots": "snapshots.jsonl",
            "summaries": "summary.jsonl",
            "latest_summary": "summary.json",
            "events": "events.jsonl",
            "errors": "errors.jsonl",
        },
        "notes": [
            "This collector is read-only against the Mihomo controller.",
            "The bearer token is intentionally not written to metadata.",
            (
                "logs.jsonl contains the full streamed log level; warnings.jsonl "
                "contains warning/error-level entries only."
            ),
            "Snapshot data summarizes configs, proxies, and rules for template diagnosis.",
        ],
    }
    (out_dir / "metadata.json").write_text(
        json.dumps(metadata, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )


def main() -> int:
    args = parse_args()
    args.api = normalize_api(args.api)
    out_dir = (
        Path(args.out_dir)
        if args.out_dir
        else Path(DEFAULT_OUT_ROOT) / f"mihomo-logs-{utc_stamp()}"
    )
    out_dir.mkdir(parents=True, exist_ok=True)
    write_metadata(args, out_dir)

    stop = threading.Event()

    def handle_signal(signum: int, _frame: Any) -> None:
        stop.set()
        print(f"received signal {signum}; stopping after current read", file=sys.stderr)

    signal.signal(signal.SIGINT, handle_signal)
    signal.signal(signal.SIGTERM, handle_signal)

    state = CollectorState(sample_limit=args.sample_limit)
    logs = JsonlWriter(out_dir / "logs.jsonl")
    warnings = JsonlWriter(out_dir / "warnings.jsonl")
    snapshots = JsonlWriter(out_dir / "snapshots.jsonl")
    summaries = JsonlWriter(out_dir / "summary.jsonl")
    events = JsonlWriter(out_dir / "events.jsonl")
    errors = JsonlWriter(out_dir / "errors.jsonl")

    writers = (logs, warnings, snapshots, summaries, events, errors)
    print(f"mihomo log collector output: {out_dir}")
    print(f"streaming {endpoint(args.api, '/logs', {'level': args.level})}")

    snapshot_thread = threading.Thread(
        target=snapshot_loop,
        args=(args, state, stop, snapshots, errors),
        name="snapshot-loop",
        daemon=True,
    )
    summary_thread = threading.Thread(
        target=summary_loop,
        args=(args, state, stop, out_dir, summaries),
        name="summary-loop",
        daemon=True,
    )
    snapshot_thread.start()
    summary_thread.start()

    try:
        stream_loop(args, state, stop, logs, warnings, events, errors)
    finally:
        stop.set()
        snapshot_thread.join(timeout=5)
        summary_thread.join(timeout=5)
        write_summary_files(args, state, out_dir, summaries)
        for writer in writers:
            writer.close()
        print(f"summary: {out_dir / 'summary.json'}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
