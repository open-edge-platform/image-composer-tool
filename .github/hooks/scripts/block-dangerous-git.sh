#!/usr/bin/env bash
# PreToolUse hook: denies git/shell commands that bypass repo safety gates.
set -euo pipefail

input="$(cat)"
command_text="$(jq -r '.tool_input.command // .command // empty' <<<"$input" 2>/dev/null || true)"

if [[ -z "$command_text" ]]; then
  echo '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}'
  exit 0
fi

deny_reason=""
if [[ "$command_text" =~ push[[:space:]]+(--force|-f)([[:space:]]|$) || "$command_text" =~ push[[:space:]]+.*--force-with-lease ]]; then
  deny_reason="git push --force is not allowed; see copilot-instructions.md Git Commits & PRs"
elif [[ "$command_text" == *"--no-verify"* ]]; then
  deny_reason="--no-verify bypasses commit/push hooks and is not allowed"
elif [[ "$command_text" =~ reset[[:space:]]+--hard ]]; then
  deny_reason="git reset --hard is destructive; confirm with the user before running manually"
fi

if [[ -n "$deny_reason" ]]; then
  jq -n --arg reason "$deny_reason" \
    '{hookSpecificOutput:{hookEventName:"PreToolUse",permissionDecision:"deny",permissionDecisionReason:$reason}}'
  exit 0
fi

echo '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}'
