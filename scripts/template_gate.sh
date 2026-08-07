#!/bin/bash
# Static gate for every checked-in image template.
#
# For each tracked template under image-templates/ this runs:
#   1. validate      - YAML well-formedness + JSON-schema conformance
#   2. resolve --full - the extends/overlay merge, plus the OS-defaults merge
#
# resolve is the load-bearing check: it is the only thing that exercises the
# extends chains and overlay baselines of the real repo templates. The merge
# algorithm in internal/config/merge.go is unit-tested against synthetic YAML,
# but no Go test resolves a checked-in template, so a template whose parent is
# moved or renamed breaks with nothing to catch it.
#
# Both commands are offline: no package mirror is contacted, so this cannot
# flake on an upstream outage.
#
# MUST be run from the repository root. internal/config/global.go defaults
# ConfigDir to "./config"; from any other directory the OS defaults silently
# fail to load, resolve still exits 0, and the gate would pass vacuously. That
# is why the warning below is treated as a failure rather than ignored.

# -e catches unexpected failures in the scaffolding (git, mapfile, the build
# check). It does not short-circuit the template loop: every per-template check
# runs inside an `if !` condition, where -e is suspended, so all failures are
# still collected and reported together.
set -euo pipefail

BIN="${ICT_BIN:-./build/image-composer-tool}"
DEGRADED_MARKER="Could not load default configuration"

if [ ! -x "$BIN" ]; then
  echo "::error::$BIN not found or not executable. Build it first: go build -buildmode=pie -ldflags \"-s -w\" -o ./build/image-composer-tool ./cmd/image-composer-tool" >&2
  exit 1
fi

if [ ! -d ./config ]; then
  echo "::error::./config not found - this script must run from the repository root, otherwise OS defaults do not load and the checks pass vacuously." >&2
  exit 1
fi

# git ls-files rather than a glob: it covers templates added by future PRs and
# ignores untracked scratch files in a developer's working tree.
mapfile -t TEMPLATES < <(git ls-files 'image-templates/*.yml')

if [ "${#TEMPLATES[@]}" -eq 0 ]; then
  echo "::error::no templates found under image-templates/ - refusing to report success." >&2
  exit 1
fi

echo "Checking ${#TEMPLATES[@]} templates"

failures=0

for template in "${TEMPLATES[@]}"; do
  for check in "validate" "resolve --full"; do
    # shellcheck disable=SC2086 # $check intentionally splits into subcommand + flag
    if ! output=$("$BIN" $check "$template" 2>&1); then
      # Trim the cobra usage block: keep the first line that names an error,
      # falling back to the first line of output when none matches.
      reason=$(printf '%s\n' "$output" | grep -m1 -E '^(Error|.*ERROR)' || printf '%s\n' "$output" | head -1)
      echo "::error file=${template}::${check} failed: ${reason}"
      failures=$((failures + 1))
      continue
    fi

    # Exit code 0 is not sufficient: a degraded run warns and still succeeds.
    if printf '%s\n' "$output" | grep -qF "$DEGRADED_MARKER"; then
      echo "::error file=${template}::${check} ran without OS defaults (\"${DEGRADED_MARKER}\") - the check was vacuous."
      failures=$((failures + 1))
    fi
  done
done

if [ "$failures" -ne 0 ]; then
  echo
  echo "::error::${failures} template check(s) failed."
  exit 1
fi

echo "All ${#TEMPLATES[@]} templates passed validate and resolve --full."
