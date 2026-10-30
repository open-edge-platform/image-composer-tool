#!/usr/bin/env bash
# PostToolUse hook: validates edited image templates against the schema (non-blocking).
set -euo pipefail

input="$(cat)"
if ! command -v jq >/dev/null 2>&1; then
  echo '{"systemMessage":"template validation hook skipped: jq is not installed."}'
  exit 0
fi
file_path="$(jq -r '.tool_input.filePath // .tool_input.path // empty' <<<"$input" 2>/dev/null || true)"

if [[ -z "$file_path" || "$file_path" != *image-templates/*.yml ]]; then
  exit 0
fi
if [[ ! -f "$file_path" ]]; then
  exit 0
fi

# Hooks run in the agent's current directory, which may not be the repo root.
repo_root="${CLAUDE_PROJECT_DIR:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
binary="$repo_root/build/image-composer-tool"
if [[ ! -x "$binary" ]]; then
  exit 0
fi

if ! validate_output="$("$binary" validate "$file_path" 2>&1)"; then
  jq -n --arg msg "image-composer-tool validate failed for $file_path:
$validate_output" '{systemMessage:$msg,hookSpecificOutput:{hookEventName:"PostToolUse",additionalContext:$msg}}'
fi
