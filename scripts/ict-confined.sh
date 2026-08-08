#!/bin/bash
#
# ict-confined.sh — run image-composer-tool with only the minimal Linux
# capabilities a build needs, instead of unrestricted root.
# See docs/architecture-decision-record/adr-reduce-root-privilege.md (Phase 1).
#
# ICT must run as root (uid 0) for loop-device attach, host mounts, and chroot.
# This wrapper keeps uid 0 but shrinks the CAPABILITY BOUNDING SET to the
# minimal, audited set (ICT_MIN_CAPS in scripts/ict-capabilities.env), dropping
# every other capability. Because a uid-0 process — and every child it spawns
# (mmdebstrap, mount, losetup, chroot, apt, …) — derives its capabilities from
# the bounding set, this bounds the whole build to the minimal set and makes the
# dropped capabilities unrecoverable. This is the exact mechanism validated by
# the Phase 0 audit, so production behaviour matches what was tested.
#
# It changes NO ICT behaviour or command construction; it only lowers the
# privilege the process is granted. Dropping from the bounding set needs
# CAP_SETPCAP, so this wrapper itself must be entered as root (via sudo -E),
# exactly as ICT is invoked today.
#
# Usage:
#   sudo -E ./scripts/ict-confined.sh build <template.yml> [args...]
#   sudo -E ./scripts/ict-confined.sh serve  [args...]
#   ICT_BIN=/usr/bin/image-composer-tool sudo -E ./scripts/ict-confined.sh build t.yml
#
# Environment:
#   ICT_BIN   path to the binary (default: ./image-composer-tool, else the first
#             image-composer-tool found on PATH)

set -uo pipefail

err() { printf 'ict-confined: %s\n' "$*" >&2; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
if ! source "$SCRIPT_DIR/ict-capabilities.env"; then
    err "cannot source $SCRIPT_DIR/ict-capabilities.env"
    exit 2
fi
if [[ -z "${ICT_MIN_CAPS:-}" ]]; then
    err "ICT_MIN_CAPS is empty; check ict-capabilities.env"
    exit 2
fi

if [[ "$(id -u)" -ne 0 ]]; then
    err "must be entered as root (e.g. 'sudo -E $0 ...') so the bounding set can be shrunk"
    exit 2
fi

command -v capsh >/dev/null 2>&1 || { err "'capsh' not found (install libcap2-bin)"; exit 2; }

# Resolve the binary: explicit ICT_BIN, else ./image-composer-tool, else PATH.
if [[ -n "${ICT_BIN:-}" ]]; then
    :
elif [[ -x "./image-composer-tool" ]]; then
    ICT_BIN="./image-composer-tool"
else
    ICT_BIN="$(command -v image-composer-tool 2>/dev/null || true)"
fi
if [[ -z "${ICT_BIN:-}" || ! -x "$ICT_BIN" ]]; then
    err "image-composer-tool binary not found (set ICT_BIN=/path/to/image-composer-tool)"
    exit 2
fi
ICT_BIN="$(readlink -f "$ICT_BIN")"

if [[ $# -eq 0 ]]; then
    err "usage: sudo -E $0 <build|serve|...> [args...]"
    exit 2
fi

# Compute the complement to drop: every host capability NOT in the keep set.
# Reading the live bounding set keeps this correct across kernels that add or
# omit capabilities.
mapfile -t ALL_CAPS < <(capsh --print | sed -n 's/^Bounding set =//p' | tr ',' '\n' | sed '/^$/d')
if [[ ${#ALL_CAPS[@]} -eq 0 ]]; then
    err "could not read the bounding set from capsh --print"
    exit 2
fi

declare -A KEEP_SET=()
IFS=',' read -ra _keep <<< "$ICT_MIN_CAPS"
for c in "${_keep[@]}"; do
    c="$(echo "$c" | tr '[:upper:]' '[:lower:]' | xargs)"
    [[ -n "$c" ]] && KEEP_SET["$c"]=1
done

DROP_LIST=()
for c in "${ALL_CAPS[@]}"; do
    [[ -z "${KEEP_SET[$c]:-}" ]] && DROP_LIST+=("$c")
done

if [[ ${#DROP_LIST[@]} -eq 0 ]]; then
    err "nothing to drop — refusing to run without confinement (check ICT_MIN_CAPS)"
    exit 2
fi
DROP_CSV="$(IFS=,; echo "${DROP_LIST[*]}")"

# Preserve SUDO_UID/SUDO_GID so ICT's post-build ownership restore still targets
# the invoking user, and pass through the environment (matching `sudo -E`).
# Each incoming argument is single-quoted so paths with spaces survive the
# `capsh -- -c` string.
quoted=""
for a in "$@"; do
    quoted+=" '${a//\'/\'\\\'\'}'"
done

exec capsh --drop="$DROP_CSV" -- -c \
    "exec env SUDO_UID='${SUDO_UID:-}' SUDO_GID='${SUDO_GID:-}' '$ICT_BIN'$quoted"
