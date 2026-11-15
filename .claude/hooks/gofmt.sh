#!/usr/bin/env bash
# PostToolUse (Edit|Write): format the Go file Claude just changed.
set -euo pipefail

file=$(jq -r '.tool_input.file_path // empty')
[[ "$file" == *.go && -f "$file" ]] || exit 0

if ! out=$(gofmt -w "$file" 2>&1); then
  # Exit 2 shows the syntax error to Claude so it fixes the file.
  echo "gofmt failed on $file:" >&2
  echo "$out" >&2
  exit 2
fi
