#!/usr/bin/env python3
"""Register and serve the localClash MCP server for local Open WebUI."""

from __future__ import annotations

import argparse
import json
import os
import signal
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path


DEFAULT_OPENWEBUI = "http://127.0.0.1:3000"
DEFAULT_HOST_ADDR = "127.0.0.1:8765"
DEFAULT_MCP_PATH = "/mcp"
DEFAULT_OPENWEBUI_MCP_URL = "http://host.docker.internal:8765/mcp"
DEFAULT_EMAIL = "ronnie@local.openwebui"


def default_repo_root() -> Path:
    return Path(__file__).resolve().parents[1]


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
    if ":" not in addr:
        raise RuntimeError(f"addr {addr!r} does not include a port")
    return addr.rsplit(":", 1)[1]


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


def serve(args: argparse.Namespace) -> None:
    root = Path(args.localclash_root).expanduser().resolve()
    binary = root / ".runtime" / "localclash-mcp"
    port = port_from_addr(args.addr)

    build_binary(root, binary)
    if args.restart_existing:
        stop_existing_server(port, binary, args.force_stop)

    cmd = [str(binary), "mcp", "--addr", args.addr, "--path", args.path]
    print(f"starting localClash MCP: http://{args.addr}{args.path}")
    print("press Ctrl+C to stop")
    proc = subprocess.Popen(cmd, cwd=root)

    def stop(_signum: int, _frame: object) -> None:
        proc.terminate()

    signal.signal(signal.SIGINT, stop)
    signal.signal(signal.SIGTERM, stop)

    try:
        while proc.poll() is None:
            time.sleep(0.5)
    finally:
        if proc.poll() is None:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()
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
    serve_parser.add_argument("--restart-existing", action=argparse.BooleanOptionalAction, default=True)
    serve_parser.add_argument("--force-stop", action="store_true", help="allow stopping a non-localClash listener on the target port")
    serve_parser.set_defaults(func=serve)

    args = parser.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
