#!/usr/bin/env bash
# PostToolUse hook: validates edited image templates against the schema (non-blocking).
set -euo pipefail

input="$(cat)"
file_path="$(jq -r '.tool_input.filePath // .tool_input.path // .tool_input.file_path // empty' <<<"$input" 2>/dev/null || true)"

if [[ -z "$file_path" || "$file_path" != *image-templates/*.yml ]]; then
  exit 0
fi
if [[ ! -f "$file_path" ]]; then
  exit 0
fi

binary="./build/image-composer-tool"
if [[ ! -x "$binary" ]]; then
  exit 0
fi

if ! validate_output="$("$binary" validate "$file_path" 2>&1)"; then
  jq -n --arg msg "image-composer-tool validate failed for $file_path:
$validate_output" '{systemMessage:$msg}'
fi
