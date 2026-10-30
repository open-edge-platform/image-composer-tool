#!/usr/bin/env bash
# PreToolUse hook: denies git/shell commands that bypass repo safety gates.
# Only emits a decision on deny; otherwise exits silently so the normal
# permission flow (user approval) still applies — "allow" would auto-approve.
set -euo pipefail

input="$(cat)"
# Without jq the payload can't be parsed; ask on anything git-related rather
# than silently letting it through.
if ! command -v jq >/dev/null 2>&1; then
  if [[ "$input" == *"git "* ]]; then
    echo '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask","permissionDecisionReason":"jq is not installed, so the git safety hook cannot inspect this command. Install jq."}}'
  fi
  exit 0
fi
command_text="$(jq -r '.tool_input.command // .command // empty' <<<"$input" 2>/dev/null || true)"
if [[ -z "$command_text" ]]; then
  exit 0
fi

deny() {
  jq -n --arg reason "$1" \
    '{hookSpecificOutput:{hookEventName:"PreToolUse",permissionDecision:"deny",permissionDecisionReason:$reason}}'
  exit 0
}

# Check each command of a chain separately so a flag from one command
# (e.g. `rm -f x && git push`) isn't attributed to another.
segments="${command_text//&&/$'\n'}"
segments="${segments//||/$'\n'}"
segments="${segments//;/$'\n'}"
segments="${segments//|/$'\n'}"

while IFS= read -r seg; do
  if [[ "$seg" =~ (^|[[:space:]])git[[:space:]](.*[[:space:]])?push([[:space:]]|$) ]] &&
    [[ "$seg" =~ [[:space:]](--force|--force-with-lease|-f)([[:space:]=]|$) || "$seg" =~ [[:space:]]\+[^[:space:]] ]]; then
    deny "git push --force (or a +refspec) is not allowed; see copilot-instructions.md Git Commits & PRs"
  fi
  if [[ "$seg" == *"--no-verify"* ]] ||
    [[ "$seg" =~ (^|[[:space:]])git[[:space:]](.*[[:space:]])?commit[[:space:]](.*[[:space:]])?-n([[:space:]]|$) ]]; then
    deny "--no-verify (git commit -n) bypasses commit/push hooks and is not allowed"
  fi
  if [[ "$seg" =~ (^|[[:space:]])git[[:space:]](.*[[:space:]])?reset[[:space:]](.*[[:space:]])?--hard([[:space:]]|$) ]]; then
    deny "git reset --hard is destructive; confirm with the user before running manually"
  fi
done <<<"$segments"
