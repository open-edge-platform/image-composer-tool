#!/usr/bin/env bash
# PostToolUse hook: flags gofmt/go vet issues on edited Go files (non-blocking).
set -euo pipefail

input="$(cat)"
file_path="$(jq -r '.tool_input.filePath // .tool_input.path // empty' <<<"$input" 2>/dev/null || true)"

if [[ -z "$file_path" || "$file_path" != *.go || "$file_path" == *_test.go ]]; then
  exit 0
fi
if [[ ! -f "$file_path" ]]; then
  exit 0
fi

fmt_diff="$(gofmt -l "$file_path" 2>/dev/null || true)"
if [[ -n "$fmt_diff" ]]; then
  jq -n --arg msg "gofmt would reformat $file_path — run 'gofmt -w $file_path'." \
    '{systemMessage:$msg}'
fi
