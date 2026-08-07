#!/bin/bash
#
# cap-audit.sh — Phase 0 of the "reduce root privilege surface" plan
# (see docs/architecture-decision-record/adr-reduce-root-privilege.md).
#
# Runs an ICT build under a MINIMAL Linux capability set instead of unrestricted
# root, so we can discover empirically which capabilities the build actually
# needs. Any operation that fails names a capability missing from the candidate
# set; a clean build confirms the set is sufficient.
#
# Mechanism: ICT runs as root (uid 0), and a uid-0 process derives each child's
# permitted capabilities from the BOUNDING SET. So the only reliable way to
# constrain what the build (and every losetup/mount/chroot child it spawns) can
# do is to drop the complement of our keep-set from the bounding set. capsh does
# exactly that; dropping from the bounding set needs CAP_SETPCAP, which we have
# because we are launched via sudo (real root), matching how ICT already runs.
#
# Usage:
#   sudo ./scripts/cap-audit.sh <template.yml> [extra ict build args...]
#
# Environment:
#   ICT_BIN        path to the image-composer-tool binary (default: ./image-composer-tool)
#   KEEP_CAPS      comma-separated caps to KEEP (default: the candidate set below)
#   AUDIT_LOG      where to tee the build log (default: ./cap-audit-<ts>.log)
#
# This script changes NO ICT code. It only launches the existing binary under a
# reduced bounding set and captures the result for analysis.

set -uo pipefail

# --- Candidate minimal capability set (keep list). Kept in sync with the ADR. ---
# CAP_SYS_ADMIN    losetup, mount, bind, sysfs, mkswap
# CAP_SYS_CHROOT   entering the chroot
# CAP_CHOWN        own root-owned rootfs files
# CAP_FOWNER       operate on files not owned by the euid
# CAP_DAC_OVERRIDE read/traverse root-owned trees regardless of perms
# CAP_MKNOD        device nodes in the rootfs
# CAP_SETUID/SETGID accounts created inside the rootfs (useradd/passwd)
# CAP_SETFCAP      file capabilities set on binaries inside the rootfs (e.g. ping)
# CAP_DAC_READ_SEARCH read-bypass distinct from DAC_OVERRIDE (metadata/loop reads)
# CAP_FSETID       preserve setuid/setgid bits when writing rootfs files
DEFAULT_KEEP_CAPS="cap_sys_admin,cap_sys_chroot,cap_chown,cap_fowner,cap_dac_override,cap_mknod,cap_setuid,cap_setgid,cap_setfcap,cap_dac_read_search,cap_fsetid"

ICT_BIN="${ICT_BIN:-./image-composer-tool}"
KEEP_CAPS="${KEEP_CAPS:-$DEFAULT_KEEP_CAPS}"

err() { printf 'cap-audit: %s\n' "$*" >&2; }

if [[ $# -lt 1 ]]; then
    err "usage: sudo $0 <template.yml> [extra ict build args...]"
    exit 2
fi

if [[ "$(id -u)" -ne 0 ]]; then
    err "must run as root (via sudo) so CAP_SETPCAP is available to shrink the bounding set"
    exit 2
fi

for tool in capsh getcap; do
    command -v "$tool" >/dev/null 2>&1 || { err "required tool '$tool' not found (install libcap2-bin)"; exit 2; }
done

if [[ ! -x "$ICT_BIN" ]]; then
    err "ICT binary not found or not executable: $ICT_BIN (build it, or set ICT_BIN=)"
    exit 2
fi
ICT_BIN="$(readlink -f "$ICT_BIN")"

TEMPLATE="$1"
shift
EXTRA_ARGS=("$@")

# --- Compute the complement to drop: every host capability NOT in the keep set. ---
# We read the live bounding set so this stays correct across kernels that add or
# omit capabilities (the keep set may name a cap absent here; that is harmless).
mapfile -t ALL_CAPS < <(capsh --print | sed -n 's/^Bounding set =//p' | tr ',' '\n' | sed '/^$/d')
if [[ ${#ALL_CAPS[@]} -eq 0 ]]; then
    err "could not read the bounding set from capsh --print"
    exit 2
fi

declare -A KEEP_SET=()
IFS=',' read -ra _keep <<< "$KEEP_CAPS"
for c in "${_keep[@]}"; do
    c="$(echo "$c" | tr '[:upper:]' '[:lower:]' | xargs)"   # normalize
    [[ -n "$c" ]] && KEEP_SET["$c"]=1
done

DROP_LIST=()
for c in "${ALL_CAPS[@]}"; do
    if [[ -z "${KEEP_SET[$c]:-}" ]]; then
        DROP_LIST+=("$c")
    fi
done
DROP_CSV="$(IFS=,; echo "${DROP_LIST[*]}")"

# Warn about keep-set entries not present on this host (typo or newer cap name).
for c in "${!KEEP_SET[@]}"; do
    present=0
    for h in "${ALL_CAPS[@]}"; do [[ "$h" == "$c" ]] && { present=1; break; }; done
    [[ $present -eq 0 ]] && err "note: kept cap '$c' is not in this host's bounding set (ignored)"
done

ts="$(date +%Y%m%d-%H%M%S 2>/dev/null || echo run)"
AUDIT_LOG="${AUDIT_LOG:-./cap-audit-${ts}.log}"

{
    echo "=========================================================="
    echo "ICT capability-confinement audit (Phase 0)"
    echo "=========================================================="
    echo "binary   : $ICT_BIN"
    echo "template : $TEMPLATE"
    echo "extra    : ${EXTRA_ARGS[*]:-(none)}"
    echo "keep caps: $KEEP_CAPS"
    echo "drop caps: $DROP_CSV"
    echo "----------------------------------------------------------"
    echo "Reading a failure: a build step that dies with EPERM / 'Operation not"
    echo "permitted' / 'must be run as root' names a capability we dropped and"
    echo "must add to the keep set. A clean build confirms the set is sufficient."
    echo "=========================================================="
} | tee "$AUDIT_LOG"

# --- Launch the build under the reduced bounding set. ---
# --drop shrinks the bounding set (bounds every uid-0 child too).
# SUDO_UID/SUDO_GID are preserved so ICT's Tier-1 ownership restore still runs.
# We pass through the current environment (matching `sudo -E`) via `env`.
# Capture the build's own output separately (BUILD_LOG) so classification below
# scans only what the build emitted — never this script's instructional header,
# which mentions "EPERM"/"Operation not permitted" as guidance and would
# otherwise false-positive as a capability failure.
BUILD_LOG="$(mktemp)"
trap 'rm -f "$BUILD_LOG"' EXIT
set -x
capsh --drop="$DROP_CSV" -- -c \
    "exec env SUDO_UID='${SUDO_UID:-}' SUDO_GID='${SUDO_GID:-}' '$ICT_BIN' build '$TEMPLATE' ${EXTRA_ARGS[*]}" \
    2>&1 | tee -a "$AUDIT_LOG" "$BUILD_LOG"
rc="${PIPESTATUS[0]}"
set +x

# Classify a non-zero exit: only blame capabilities when the log actually shows
# a permission signal AND a privileged op was reached. A build that dies during
# package resolution (before any losetup/mount/chroot/bootstrap) has nothing to
# do with the dropped capabilities, and saying otherwise sends the reader
# chasing a non-existent cap.
CAP_SIGNAL_RE='Operation not permitted|operation not permitted|EPERM|[Pp]ermission denied|must be run as root'
PRIV_OP_RE='Losetup .* created|Mounted |chroot|mmdebstrap|bootstrap'

{
    echo "----------------------------------------------------------"
    if [[ "$rc" -eq 0 ]]; then
        echo "RESULT: build exited 0 under the minimal capability set."
        echo "        The candidate keep set appears SUFFICIENT for this template."
    elif grep -qEi "$CAP_SIGNAL_RE" "$BUILD_LOG"; then
        echo "RESULT: build exited $rc AND the log shows a permission signal."
        echo "        LIKELY a capability failure. The matching lines:"
        grep -nEi "$CAP_SIGNAL_RE" "$BUILD_LOG" | grep -viE 'INFO' | head -10 | sed 's/^/          /'
        echo "        Map the failing op to its capability and add it to KEEP_CAPS."
    elif ! grep -qEi "$PRIV_OP_RE" "$BUILD_LOG"; then
        echo "RESULT: build exited $rc, but FAILED BEFORE ANY PRIVILEGED OPERATION"
        echo "        (no losetup/mount/chroot/bootstrap ran). This is UNRELATED to"
        echo "        capability confinement — e.g. package resolution, config, or"
        echo "        network. Re-run with a template that resolves to test the caps."
    else
        echo "RESULT: build exited $rc with no permission signal in the log."
        echo "        A privileged op ran, so this is probably NOT a capability gap"
        echo "        (a dropped cap normally surfaces as EPERM). Inspect the error"
        echo "        above; re-run as full root to confirm it is not cap-related."
    fi
    echo "log: $AUDIT_LOG"
} | tee -a "$AUDIT_LOG"

exit "$rc"
