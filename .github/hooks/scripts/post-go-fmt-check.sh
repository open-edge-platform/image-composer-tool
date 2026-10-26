#!/usr/bin/env bash
# PostToolUse hook: flags gofmt drift on edited Go files (non-blocking).
set -euo pipefail

input="$(cat)"
if ! command -v jq >/dev/null 2>&1; then
  echo '{"systemMessage":"gofmt check hook skipped: jq is not installed."}'
  exit 0
fi
file_path="$(jq -r '.tool_input.filePath // .tool_input.path // empty' <<<"$input" 2>/dev/null || true)"

if [[ -z "$file_path" || "$file_path" != *.go ]]; then
  exit 0
fi
if [[ ! -f "$file_path" ]]; then
  exit 0
fi

fmt_diff="$(gofmt -l "$file_path" 2>/dev/null || true)"
if [[ -n "$fmt_diff" ]]; then
  # systemMessage is shown to the user; additionalContext is fed back to the agent.
  jq -n --arg msg "gofmt would reformat $file_path — run 'gofmt -w $file_path'." \
    '{systemMessage:$msg,hookSpecificOutput:{hookEventName:"PostToolUse",additionalContext:$msg}}'
fi
