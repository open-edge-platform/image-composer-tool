#!/usr/bin/env bash
# PreToolUse hook: nudges toward splitting oversized pushes into smaller PRs/feature branches.
# Only emits a decision ("ask") on oversized pushes; otherwise exits silently so
# the normal permission flow still applies — "allow" would auto-approve.
set -euo pipefail

LOC_THRESHOLD=500

input="$(cat)"
if ! command -v jq >/dev/null 2>&1; then
  if [[ "$input" == *"git push"* ]]; then
    echo '{"systemMessage":"PR size check skipped: jq is not installed."}'
  fi
  exit 0
fi
command_text="$(jq -r '.tool_input.command // .command // empty' <<<"$input" 2>/dev/null || true)"

if [[ -z "$command_text" ]]; then
  exit 0
fi

# Match `git push` per command of a chain, allowing global options before the
# subcommand (`git -C dir push`, `git -c k=v push`) but not `git stash push`.
segments="${command_text//&&/$'\n'}"
segments="${segments//||/$'\n'}"
segments="${segments//;/$'\n'}"
segments="${segments//|/$'\n'}"
push_re='(^|[[:space:]])git([[:space:]]+(-[cC][[:space:]]+[^[:space:]]+|-[^[:space:]]+))*[[:space:]]+push([[:space:]]|$)'
is_push=false
while IFS= read -r seg; do
  if [[ "$seg" =~ $push_re ]]; then
    is_push=true
  fi
done <<<"$segments"
if [[ "$is_push" != true ]]; then
  exit 0
fi

merge_base="$(git merge-base HEAD origin/main 2>/dev/null || true)"
if [[ -z "$merge_base" ]]; then
  exit 0
fi

# An empty diff makes grep exit 1; don't let pipefail turn that into a hook
# error, which Copilot treats as a deny.
loc="$(git diff --shortstat "$merge_base" HEAD 2>/dev/null \
  | grep -oE '[0-9]+ (insertion|deletion)' \
  | awk '{sum+=$1} END {print sum+0}' || true)"

if [[ "${loc:-0}" -gt "$LOC_THRESHOLD" ]]; then
  jq -n --arg reason "This branch has ~${loc} LOC changed since main (threshold: ${LOC_THRESHOLD}). Consider splitting into a feature/<name> branch with smaller incremental PRs — see CONTRIBUTING.md 'Large Features & Feature Branches'." \
    '{hookSpecificOutput:{hookEventName:"PreToolUse",permissionDecision:"ask",permissionDecisionReason:$reason}}'
fi
