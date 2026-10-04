#!/usr/bin/env bash
# PreToolUse hook: nudges toward splitting oversized pushes into smaller PRs/feature branches.
set -euo pipefail

LOC_THRESHOLD=500

input="$(cat)"
command_text="$(jq -r '.tool_input.command // .command // empty' <<<"$input" 2>/dev/null || true)"

allow() { echo '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}'; }

if [[ -z "$command_text" || "$command_text" != *"git push"* ]]; then
  allow
  exit 0
fi

merge_base="$(git merge-base HEAD origin/main 2>/dev/null || true)"
if [[ -z "$merge_base" ]]; then
  allow
  exit 0
fi

loc="$(git diff --shortstat "$merge_base" HEAD 2>/dev/null \
  | grep -oE '[0-9]+ (insertion|deletion)' \
  | awk '{sum+=$1} END {print sum+0}')"

if [[ "${loc:-0}" -gt "$LOC_THRESHOLD" ]]; then
  jq -n --arg reason "This branch has ~${loc} LOC changed since main (threshold: ${LOC_THRESHOLD}). Consider splitting into a feature/<name> branch with smaller incremental PRs — see CONTRIBUTING.md 'Large Features & Feature Branches'." \
    '{hookSpecificOutput:{hookEventName:"PreToolUse",permissionDecision:"ask",permissionDecisionReason:$reason}}'
  exit 0
fi

allow
