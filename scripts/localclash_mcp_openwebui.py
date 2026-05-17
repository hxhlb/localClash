#!/usr/bin/env python3
"""Register and serve the localClash MCP server for local Open WebUI."""

from __future__ import annotations

import argparse
import datetime as dt
import http.server
import json
import os
import signal
import socket
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any


DEFAULT_OPENWEBUI = "http://127.0.0.1:3000"
DEFAULT_HOST_ADDR = "127.0.0.1:8765"
DEFAULT_MCP_PATH = "/mcp"
DEFAULT_OPENWEBUI_MCP_URL = "http://host.docker.internal:8765/mcp"
DEFAULT_EMAIL = "ronnie@local.openwebui"
DEFAULT_LOG_NAME = "localclash-mcp-openwebui.jsonl"
HOP_BY_HOP_HEADERS = {
    "connection",
    "keep-alive",
    "proxy-authenticate",
    "proxy-authorization",
    "te",
    "trailer",
    "transfer-encoding",
    "upgrade",
}
SECRET_KEY_PARTS = (
    "authorization",
    "cookie",
    "password",
    "passwd",
    "secret",
    "token",
    "api_key",
    "apikey",
    "url",
)


def default_repo_root() -> Path:
    return Path(__file__).resolve().parents[1]


def now_iso() -> str:
    return dt.datetime.now(dt.timezone.utc).astimezone().isoformat(timespec="milliseconds")


def split_addr(addr: str) -> tuple[str, int]:
    if ":" not in addr:
        raise RuntimeError(f"addr {addr!r} does not include a port")
    host, port_text = addr.rsplit(":", 1)
    if not host:
        host = "127.0.0.1"
    try:
        port = int(port_text)
    except ValueError as exc:
        raise RuntimeError(f"addr {addr!r} does not include a numeric port") from exc
    return host, port


def public_url(addr: str, path: str) -> str:
    return f"http://{addr}{path}"


def loopback_backend_addr(public_addr: str) -> str:
    split_addr(public_addr)
    bind_host = "127.0.0.1"
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind((bind_host, 0))
        return f"{bind_host}:{sock.getsockname()[1]}"


def should_redact_key(key: str) -> bool:
    lowered = key.lower()
    return any(part in lowered for part in SECRET_KEY_PARTS)


def redact(value: Any) -> Any:
    if isinstance(value, dict):
        return {
            key: "[REDACTED]" if should_redact_key(str(key)) else redact(item)
            for key, item in value.items()
        }
    if isinstance(value, list):
        return [redact(item) for item in value]
    return value


def decode_body(body: bytes, body_limit: int, redact_logs: bool) -> Any:
    if not body:
        return None
    truncated = len(body) > body_limit
    limited = body[:body_limit]
    text = limited.decode("utf-8", errors="replace")
    try:
        decoded: Any = json.loads(text)
    except json.JSONDecodeError:
        decoded = text
    if redact_logs:
        decoded = redact(decoded)
    if truncated:
        return {"truncated": True, "limit_bytes": body_limit, "body": decoded}
    return decoded


def mcp_summary(body: bytes) -> dict[str, Any]:
    if not body:
        return {}
    try:
        decoded = json.loads(body.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError):
        return {}
    if not isinstance(decoded, dict):
        return {}
    summary: dict[str, Any] = {}
    if "id" in decoded:
        summary["jsonrpc_id"] = decoded.get("id")
    if decoded.get("method"):
        summary["jsonrpc_method"] = decoded.get("method")
    params = decoded.get("params")
    if isinstance(params, dict) and params.get("name"):
        summary["tool"] = params.get("name")
    return summary


class LogSink:
    def __init__(self, path: Path, *, body_limit: int, redact_logs: bool) -> None:
        self.path = path
        self.body_limit = body_limit
        self.redact_logs = redact_logs
        self._lock = threading.Lock()
        path.parent.mkdir(parents=True, exist_ok=True)
        self._file = path.open("w", encoding="utf-8")

    def close(self) -> None:
        with self._lock:
            self._file.close()

    def event(self, kind: str, **fields: Any) -> None:
        entry = {"ts": now_iso(), "kind": kind, **fields}
        line = json.dumps(entry, ensure_ascii=False, separators=(",", ":"))
        with self._lock:
            self._file.write(line + "\n")
            self._file.flush()
        self.print_event(entry)

    def print_event(self, entry: dict[str, Any]) -> None:
        kind = entry.get("kind")
        request_id = entry.get("request_id", "-")
        if kind == "mcp_request":
            suffix = self.summary_suffix(entry)
            print(f"[{entry['ts']}] MCP request #{request_id} {entry.get('method')} {entry.get('path')}{suffix}")
            self.print_body(entry.get("body"))
            return
        if kind == "mcp_response":
            suffix = self.summary_suffix(entry)
            print(
                f"[{entry['ts']}] MCP response #{request_id} "
                f"status={entry.get('status')} duration_ms={entry.get('duration_ms')}{suffix}"
            )
            self.print_body(entry.get("body"))
            return
        if kind == "backend_log":
            print(f"[{entry['ts']}] backend: {entry.get('message')}")
            return
        if kind == "proxy_error":
            print(f"[{entry['ts']}] proxy error #{request_id}: {entry.get('error')}", file=sys.stderr)
            return
        if kind == "proxy_start":
            print(
                f"[{entry['ts']}] proxy listening on {entry.get('public_url')} "
                f"-> backend {entry.get('backend_url')}"
            )
            return
        print(f"[{entry['ts']}] {kind}: {json.dumps(entry, ensure_ascii=False)}")

    @staticmethod
    def summary_suffix(entry: dict[str, Any]) -> str:
        parts = []
        if entry.get("jsonrpc_method"):
            parts.append(f"jsonrpc={entry['jsonrpc_method']}")
        if entry.get("tool"):
            parts.append(f"tool={entry['tool']}")
        if entry.get("jsonrpc_id") is not None:
            parts.append(f"id={entry['jsonrpc_id']}")
        return " " + " ".join(parts) if parts else ""

    @staticmethod
    def print_body(body: Any) -> None:
        if body is None:
            return
        if isinstance(body, (dict, list)):
            print(json.dumps(body, ensure_ascii=False, indent=2))
            return
        print(str(body))


def request_json(method: str, url: str, token: str | None = None, payload: dict | None = None) -> dict:
    data = None
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")

    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=20) as resp:
            body = resp.read().decode("utf-8")
            return json.loads(body) if body else {}
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"{method} {url} failed with HTTP {exc.code}: {detail}") from exc


def sign_in(base_url: str, email: str, password: str) -> str:
    data = request_json(
        "POST",
        f"{base_url.rstrip('/')}/api/v1/auths/signin",
        payload={"email": email, "password": password},
    )
    token = data.get("token")
    if not token:
        raise RuntimeError("Open WebUI sign-in did not return a token")
    return token


def register(args: argparse.Namespace) -> None:
    password = args.password or os.environ.get("OPENWEBUI_PASSWORD")
    if not password:
        raise SystemExit("Set OPENWEBUI_PASSWORD or pass --password.")

    base_url = args.openwebui.rstrip("/")
    token = sign_in(base_url, args.email, password)

    current = request_json("GET", f"{base_url}/api/v1/configs/tool_servers", token=token)
    connections = current.get("TOOL_SERVER_CONNECTIONS") or []
    connection = {
        "url": args.mcp_url,
        "path": "",
        "type": "mcp",
        "auth_type": "none",
        "headers": {},
        "key": "",
        "config": {"enable": True},
        "info": {
            "id": args.server_id,
            "name": args.server_name,
            "description": "localClash HTTP MCP server on this Mac",
        },
    }

    kept = [
        item
        for item in connections
        if (item.get("info") or {}).get("id") != args.server_id and item.get("url") != args.mcp_url
    ]
    kept.append(connection)
    result = request_json(
        "POST",
        f"{base_url}/api/v1/configs/tool_servers",
        token=token,
        payload={"TOOL_SERVER_CONNECTIONS": kept},
    )
    count = len(result.get("TOOL_SERVER_CONNECTIONS") or [])
    print(f"registered {args.server_name} at {args.mcp_url}")
    print(f"tool_server_connections={count}")

    if args.verify:
        verified = request_json(
            "POST",
            f"{base_url}/api/v1/configs/tool_servers/verify",
            token=token,
            payload=connection,
        )
        specs = verified.get("specs") or []
        print(f"verify_status={verified.get('status')}")
        print(f"verify_tool_count={len(specs)}")


def port_from_addr(addr: str) -> str:
    return str(split_addr(addr)[1])


def listener_pids(port: str) -> list[int]:
    result = subprocess.run(
        ["lsof", f"-tiTCP:{port}", "-sTCP:LISTEN"],
        check=False,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
    )
    return [int(line) for line in result.stdout.splitlines() if line.strip().isdigit()]


def command_for_pid(pid: int) -> str:
    result = subprocess.run(
        ["ps", "-p", str(pid), "-o", "command="],
        check=False,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
    )
    return result.stdout.strip()


def stop_existing_server(port: str, binary: Path, force: bool) -> None:
    pids = listener_pids(port)
    if not pids:
        return

    for pid in pids:
        command = command_for_pid(pid)
        if not force and "localclash" not in command.lower() and str(binary) not in command:
            raise RuntimeError(f"port {port} is already used by pid {pid}: {command}")
        print(f"stopping existing localClash MCP listener pid={pid}")
        os.kill(pid, signal.SIGTERM)

    deadline = time.time() + 5
    while time.time() < deadline:
        if not listener_pids(port):
            return
        time.sleep(0.2)

    for pid in listener_pids(port):
        print(f"killing unresponsive localClash MCP listener pid={pid}", file=sys.stderr)
        os.kill(pid, signal.SIGKILL)


def build_binary(root: Path, binary: Path) -> None:
    binary.parent.mkdir(parents=True, exist_ok=True)
    print("building localClash MCP binary...", file=sys.stderr)
    subprocess.run(["go", "build", "-o", str(binary), "."], cwd=root, check=True)


class MCPProxyServer(http.server.ThreadingHTTPServer):
    daemon_threads = True

    def __init__(self, server_address: tuple[str, int], backend_addr: str, log_sink: LogSink) -> None:
        super().__init__(server_address, MCPProxyHandler)
        self.backend_addr = backend_addr
        self.log_sink = log_sink
        self._request_id = 0
        self._request_id_lock = threading.Lock()

    def next_request_id(self) -> int:
        with self._request_id_lock:
            self._request_id += 1
            return self._request_id


class MCPProxyHandler(http.server.BaseHTTPRequestHandler):
    server: MCPProxyServer
    protocol_version = "HTTP/1.1"

    def log_message(self, _format: str, *_args: Any) -> None:
        return

    def do_GET(self) -> None:
        self.forward()

    def do_POST(self) -> None:
        self.forward()

    def do_OPTIONS(self) -> None:
        self.forward()

    def forward(self) -> None:
        request_id = self.server.next_request_id()
        content_length = int(self.headers.get("Content-Length", "0") or "0")
        body = self.rfile.read(content_length) if content_length > 0 else b""
        started = time.monotonic()

        request_body = decode_body(body, self.server.log_sink.body_limit, self.server.log_sink.redact_logs)
        request_summary = mcp_summary(body)
        self.server.log_sink.event(
            "mcp_request",
            request_id=request_id,
            method=self.command,
            path=self.path,
            headers=(
                redact(dict(self.headers.items()))
                if self.server.log_sink.redact_logs
                else dict(self.headers.items())
            ),
            body=request_body,
            **request_summary,
        )

        backend_url = f"http://{self.server.backend_addr}{self.path}"
        headers = {
            key: value
            for key, value in self.headers.items()
            if key.lower() not in HOP_BY_HOP_HEADERS and key.lower() != "host"
        }
        req = urllib.request.Request(
            backend_url,
            data=body if body or self.command in ("POST", "PUT", "PATCH") else None,
            headers=headers,
            method=self.command,
        )

        try:
            with urllib.request.urlopen(req, timeout=60) as resp:
                status = resp.status
                response_headers = dict(resp.headers.items())
                response_body = resp.read()
        except urllib.error.HTTPError as exc:
            status = exc.code
            response_headers = dict(exc.headers.items())
            response_body = exc.read()
        except urllib.error.URLError as exc:
            response_body = json.dumps({"error": f"backend request failed: {exc.reason}"}).encode("utf-8")
            response_headers = {"Content-Type": "application/json"}
            status = http.HTTPStatus.BAD_GATEWAY
            self.server.log_sink.event("proxy_error", request_id=request_id, error=str(exc.reason))

        duration_ms = round((time.monotonic() - started) * 1000, 2)
        response_summary = mcp_summary(response_body)
        self.server.log_sink.event(
            "mcp_response",
            request_id=request_id,
            status=int(status),
            duration_ms=duration_ms,
            headers=redact(response_headers) if self.server.log_sink.redact_logs else response_headers,
            body=decode_body(response_body, self.server.log_sink.body_limit, self.server.log_sink.redact_logs),
            **response_summary,
        )

        self.send_response(int(status))
        for key, value in response_headers.items():
            lowered = key.lower()
            if lowered in HOP_BY_HOP_HEADERS or lowered == "content-length":
                continue
            self.send_header(key, value)
        self.send_header("Content-Length", str(len(response_body)))
        self.end_headers()
        self.wfile.write(response_body)


def stream_process_output(proc: subprocess.Popen[str], log_sink: LogSink) -> None:
    if proc.stdout is None:
        return
    for line in proc.stdout:
        message = line.rstrip("\n")
        if message:
            log_sink.event("backend_log", message=message)


def wait_for_backend(addr: str, proc: subprocess.Popen[str], timeout: float = 20.0) -> None:
    deadline = time.time() + timeout
    url = f"http://{addr}/health"
    while time.time() < deadline:
        if proc.poll() is not None:
            raise RuntimeError(f"localClash MCP backend exited with code {proc.returncode}")
        try:
            with urllib.request.urlopen(url, timeout=1) as resp:
                if resp.status == http.HTTPStatus.OK:
                    return
        except (urllib.error.URLError, TimeoutError):
            pass
        time.sleep(0.2)
    raise RuntimeError(f"timed out waiting for localClash MCP backend health check: {url}")


def serve(args: argparse.Namespace) -> None:
    root = Path(args.localclash_root).expanduser().resolve()
    binary = root / ".runtime" / "localclash-mcp"
    port = port_from_addr(args.addr)
    public_host, public_port = split_addr(args.addr)
    log_file = Path(args.log_file).expanduser() if args.log_file else root / ".runtime" / "logs" / DEFAULT_LOG_NAME
    backend_addr = args.backend_addr or loopback_backend_addr(args.addr)

    build_binary(root, binary)
    if args.restart_existing:
        stop_existing_server(port, binary, args.force_stop)

    log_sink = LogSink(log_file, body_limit=args.log_body_limit, redact_logs=not args.no_log_redaction)
    cmd = [str(binary), "mcp", "--addr", backend_addr, "--path", args.path]
    print(f"starting localClash MCP backend: {public_url(backend_addr, args.path)}")
    print(f"starting localClash MCP proxy: {public_url(args.addr, args.path)}")
    print(f"MCP proxy log: {log_file}")
    print("press Ctrl+C to stop")
    proc = subprocess.Popen(
        cmd,
        cwd=root,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        bufsize=1,
    )
    output_thread = threading.Thread(target=stream_process_output, args=(proc, log_sink), daemon=True)
    output_thread.start()
    httpd: MCPProxyServer | None = None
    interrupted = False
    try:
        wait_for_backend(backend_addr, proc)
        httpd = MCPProxyServer((public_host, public_port), backend_addr, log_sink)
        log_sink.event(
            "proxy_start",
            public_url=public_url(args.addr, args.path),
            backend_url=public_url(backend_addr, args.path),
            log_file=str(log_file),
            redact_logs=not args.no_log_redaction,
            body_limit=args.log_body_limit,
        )
        print(f"localClash MCP HTTP listening on {public_url(args.addr, args.path)}")
        print(f"health check: http://{args.addr}/health")

        def stop(_signum: int, _frame: object) -> None:
            raise KeyboardInterrupt

        signal.signal(signal.SIGINT, stop)
        signal.signal(signal.SIGTERM, stop)

        monitor_thread = threading.Thread(target=lambda: (proc.wait(), httpd.shutdown()), daemon=True)
        monitor_thread.start()
        httpd.serve_forever(poll_interval=0.2)
    except KeyboardInterrupt:
        interrupted = True
    finally:
        if httpd is not None:
            httpd.server_close()
        if proc.poll() is None:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()
        output_thread.join(timeout=1)
        log_sink.close()
    if interrupted and proc.returncode is not None and proc.returncode < 0:
        raise SystemExit(0)
    raise SystemExit(proc.returncode or 0)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)

    register_parser = sub.add_parser("register", help="register localClash MCP in Open WebUI")
    register_parser.add_argument("--openwebui", default=DEFAULT_OPENWEBUI)
    register_parser.add_argument("--email", default=DEFAULT_EMAIL)
    register_parser.add_argument("--password")
    register_parser.add_argument("--mcp-url", default=DEFAULT_OPENWEBUI_MCP_URL)
    register_parser.add_argument("--server-id", default="localclash")
    register_parser.add_argument("--server-name", default="localClash")
    register_parser.add_argument("--verify", action="store_true")
    register_parser.set_defaults(func=register)

    serve_parser = sub.add_parser("serve", help="build, restart, and run the localClash MCP server")
    serve_parser.add_argument("--localclash-root", default=str(default_repo_root()))
    serve_parser.add_argument("--addr", default=DEFAULT_HOST_ADDR)
    serve_parser.add_argument("--path", default=DEFAULT_MCP_PATH)
    serve_parser.add_argument("--backend-addr", help="internal backend listen address; defaults to a free 127.0.0.1 port")
    serve_parser.add_argument("--log-file", help=f"JSONL request/response log path; default is .runtime/logs/{DEFAULT_LOG_NAME}")
    serve_parser.add_argument("--log-body-limit", type=int, default=65536, help="maximum request/response body bytes to record per event")
    serve_parser.add_argument(
        "--no-log-redaction",
        action="store_true",
        help="write raw MCP bodies and headers without redacting common secret-bearing fields",
    )
    serve_parser.add_argument("--restart-existing", action=argparse.BooleanOptionalAction, default=True)
    serve_parser.add_argument("--force-stop", action="store_true", help="allow stopping a non-localClash listener on the target port")
    serve_parser.set_defaults(func=serve)

    args = parser.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
