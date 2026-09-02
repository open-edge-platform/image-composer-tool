#!/bin/bash
# Benchmarks fresh (no-cache) vs. cached (subsequent) build times for a fixed
# set of ubuntu24/debian13 image templates.
#
# For each template:
#   1. `image-composer-tool cache clean --all --provider-id <id>` wipes the
#      package cache and chroot/workspace cache for that template's provider,
#      guaranteeing the next build starts from a genuinely cold state.
#   2. The template is built (fresh/no-cache timing).
#   3. The SAME template is built again immediately, without touching the
#      cache (cached/subsequent timing) — packages and the chrootenv from
#      step 2 are reused.
#
# Requires: sudo (a background keep-alive loop re-authenticates every 60s so
# a stale credential never blocks mid-run on an invisible password prompt),
# network access for package/baseline downloads, and enough disk space for 2x
# image builds per template (see `df -h .` output printed at startup).
#
# Usage: ./scripts/benchmark_build_times.sh
#
# Results: CSV summary at benchmark-logs/build-benchmark-<timestamp>.csv;
# per-build logs under benchmark-logs/<timestamp>/.
#
# NOTE: overlay-mode templates (the two "*-overlay-*" entries below) resolve
# packages into cache/pkgCache/<os>-<dist>-<pkgarch>/overlay, keyed by the
# packaging system's arch name (e.g. "amd64" for Debian/Ubuntu on x86_64) —
# NOT the generic <os>-<dist>-<arch> provider-id used elsewhere. Both forms
# are cleaned below so overlay templates actually start from a cold cache.
# Overlay builds also don't reuse a persistent chrootenv (there is none in
# overlay mode), and the baseline image + repo Release-file freshness check
# are re-fetched from the network on every run by design.
#
# Their second run is therefore NOT a cache-warm comparison: ResolveOverlayPackages
# purges every downloaded artifact before each non-empty resolve
# (internal/image/overlay/resolve.go), so that run re-downloads the full package
# closure too — at best only repository metadata stays warm. The script reports
# this second run as a "repeat" build (run_type=repeat) rather than "cached", and
# excludes it from the cache-speedup summary below.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

BINARY="./image-composer-tool"
TIMESTAMP="$(date +%Y%m%d-%H%M%S)"
LOG_DIR="benchmark-logs/${TIMESTAMP}"
RESULTS_CSV="benchmark-logs/build-benchmark-${TIMESTAMP}.csv"

mkdir -p "$LOG_DIR"

# name|template path|provider-id (provider-id = {os}-{dist}-{arch})|run_type (cached|repeat)
# run_type=repeat marks overlay-mode templates, whose second run re-downloads the
# package closure (see NOTE above) and is not a true cache-warm comparison.
TEMPLATES=(
  "ubuntu24-minimal-raw|image-templates/ubuntu24/ubuntu24-x86_64-minimal-raw.yml|ubuntu-ubuntu24-x86_64|cached"
  "ubuntu24-robotics-jazzy-overlay-extends|image-templates/ubuntu24/ubuntu24-x86_64-robotics-jazzy-overlay-extends.yml|ubuntu-ubuntu24-x86_64|repeat"
  "ubuntu24-robotics-jazzy-iso|image-templates/ubuntu24-x86_64-robotics-jazzy-iso.yml|ubuntu-ubuntu24-x86_64|cached"
  "debian13-minimal-raw|image-templates/debian13/debian13-x86_64-minimal-raw.yml|debian-debian13-x86_64|cached"
  "debian13-bb-overlay-initrd-raw|image-templates/debian13/debian13-x86_64-bb-overlay-initrd-raw.yml|debian-debian13-x86_64|repeat"
  "debian13-bb-dracut-raw|image-templates/debian13-x86_64-bb-dracut-raw.yml|debian-debian13-x86_64|cached"
)

log() { printf '%s %s\n' "$(date '+%H:%M:%S')" "$*"; }

format_duration() {
  local total=$1
  printf '%02d:%02d:%02d' $((total / 3600)) $(((total % 3600) / 60)) $((total % 60))
}

# Sets BUILD_STATUS (OK|FAILED) and BUILD_SECONDS as a side effect.
run_build() {
  local template="$1" logfile="$2"
  local start end
  start=$(date +%s)
  if sudo -E "$BINARY" build "$template" >"$logfile" 2>&1; then
    BUILD_STATUS="OK"
  else
    BUILD_STATUS="FAILED"
  fi
  end=$(date +%s)
  BUILD_SECONDS=$((end - start))
}

log "== Recompiling image-composer-tool =="
go build -buildmode=pie -ldflags "-s -w" ./cmd/image-composer-tool

# ISO builds (ubuntu24-robotics-jazzy-iso) shell out to this binary at build time.
# All benchmark templates target x86_64, so build it for linux/amd64 regardless
# of the host arch — otherwise a cross-arch host build embeds an unusable
# executable in the x86_64 ISO.
log "== Recompiling live-installer (ISO build prerequisite) =="
mkdir -p ./build
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildmode=pie -o ./build/live-installer ./cmd/live-installer

log "== Priming sudo credentials =="
sudo -v

# Keep sudo authenticated for the whole (multi-hour) run so a later build
# never blocks on an invisible password prompt, which would otherwise get
# silently counted as "build time". If the credential can no longer be
# refreshed non-interactively, abort instead of risking a silent block.
MAIN_PID=$$
(
  while sudo -n true 2>/dev/null; do
    sleep 60
  done
  echo "ERROR: sudo credential refresh failed; aborting to avoid a blocked/skewed build" >&2
  kill -TERM "$MAIN_PID"
) &
SUDO_KEEPALIVE_PID=$!
trap 'kill "$SUDO_KEEPALIVE_PID" 2>/dev/null || true' EXIT

log "== Disk space =="
df -h . workspace 2>/dev/null || df -h .

echo "template,fresh_status,fresh_seconds,fresh_duration,cached_status,cached_seconds,cached_duration,run_type" >"$RESULTS_CSV"

ANY_FAILED=0

for entry in "${TEMPLATES[@]}"; do
  IFS='|' read -r name template provider run_type <<<"$entry"

  log "=== $name ($template) — provider $provider ==="

  log "Clearing package + chroot cache for provider $provider"
  sudo -E "$BINARY" cache clean --all --provider-id "$provider" \
    >"$LOG_DIR/${name}-cacheclean.log" 2>&1

  # Overlay-mode resolution caches packages under the dpkg/rpm arch name
  # (e.g. "amd64" for x86_64), a different path than the provider-id above —
  # clear it too so overlay templates start cold.
  overlay_provider="${provider/x86_64/amd64}"
  if [[ "$overlay_provider" != "$provider" ]]; then
    sudo -E "$BINARY" cache clean --all --provider-id "$overlay_provider" \
      >>"$LOG_DIR/${name}-cacheclean.log" 2>&1
  fi

  log "Fresh build (no cache)..."
  run_build "$template" "$LOG_DIR/${name}-fresh.log"
  fresh_status=$BUILD_STATUS
  fresh_seconds=$BUILD_SECONDS
  log "Fresh build: $fresh_status in $(format_duration "$fresh_seconds")"

  cached_status="SKIPPED"
  cached_seconds=0
  if [[ "$fresh_status" == "OK" ]]; then
    if [[ "$run_type" == "repeat" ]]; then
      log "Repeat build (overlay mode re-downloads packages; not cache-warm)..."
    else
      log "Cached build (subsequent, cache warm)..."
    fi
    run_build "$template" "$LOG_DIR/${name}-cached.log"
    cached_status=$BUILD_STATUS
    cached_seconds=$BUILD_SECONDS
    log "Second run: $cached_status in $(format_duration "$cached_seconds")"
  else
    log "Skipping second build because fresh build failed — see $LOG_DIR/${name}-fresh.log"
  fi

  if [[ "$fresh_status" == "FAILED" || "$cached_status" == "FAILED" ]]; then
    ANY_FAILED=1
  fi

  echo "$name,$fresh_status,$fresh_seconds,$(format_duration "$fresh_seconds"),$cached_status,$cached_seconds,$(format_duration "$cached_seconds"),$run_type" >>"$RESULTS_CSV"
done

log "Results written to $RESULTS_CSV"
echo
column -s, -t "$RESULTS_CSV" 2>/dev/null || cat "$RESULTS_CSV"

echo
echo "Summary (speedup = (fresh-cached)/fresh; repeat builds excluded — see NOTE above):"
awk -F, 'NR>1 && $2=="OK" && $5=="OK" && $3>0 && $8=="cached" {printf "  %-45s fresh=%6ss cached=%6ss speedup=%.1f%%\n", $1, $3, $6, (($3-$6)/$3)*100}' "$RESULTS_CSV"
awk -F, 'NR>1 && $2=="OK" && $5=="OK" && $8=="repeat" {printf "  %-45s fresh=%6ss repeat=%6ss (not a cache comparison)\n", $1, $3, $6}' "$RESULTS_CSV"

if [[ "$ANY_FAILED" -eq 1 ]]; then
  log "One or more benchmark builds failed — see per-template logs above"
  exit 1
fi
