#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $0 #<session-id-prefix>" >&2
}

if [[ $# -ne 1 ]]; then
  usage
  exit 2
fi

session_prefix="${1#\#}"
if [[ -z "${session_prefix}" ]]; then
  usage
  exit 2
fi

claude_home="${CLAUDE_HOME:-${HOME}/.claude}"
projects_dir="${claude_home}/projects"

if [[ ! -d "${projects_dir}" ]]; then
  echo "error: Claude projects directory not found: ${projects_dir}" >&2
  exit 1
fi

shopt -s nullglob
matches=("${projects_dir}"/*/"${session_prefix}"*.jsonl)
shopt -u nullglob

case "${#matches[@]}" in
  0)
    echo "error: no Claude Code JSONL found for ${session_prefix}" >&2
    exit 1
    ;;
  1)
    printf '%s\n' "${matches[0]}"
    ;;
  *)
    echo "error: multiple Claude Code JSONL files matched ${session_prefix}" >&2
    printf '%s\n' "${matches[@]}" >&2
    exit 1
    ;;
esac
