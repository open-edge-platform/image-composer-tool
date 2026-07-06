#!/bin/sh
. /lib/dracut-lib.sh

MARKER_LOG="/run/initramfs/wait-root.log"
ONCE_FLAG="/run/initramfs/.wait-root-ran"
ROUND_FILE="/run/initramfs/.wait-root-round"
MAX_ROUNDS=3

log_marker() {
    msg="$1"

    # Send markers to all common early-boot sinks so at least one is visible.
    info "$msg"
    echo "$msg" >/dev/kmsg 2>/dev/null || true
    echo "$msg" >/dev/console 2>/dev/null || true
    printf '%s\n' "$msg" >>"$MARKER_LOG" 2>/dev/null || true
}

is_cmdline_hook_run() {
    case "$0" in
        *"/hooks/cmdline/"*)
            return 0
            ;;
        *)
            return 1
            ;;
    esac
}

ROOTSPEC="$(getarg root=)"
CURRENT_ROUND=1

if [ -f "$ROUND_FILE" ]; then
    CURRENT_ROUND="$(cat "$ROUND_FILE" 2>/dev/null || echo 1)"
fi

log_marker "WAIT_ROOT_EXECUTED round=$CURRENT_ROUND rootspec=${ROOTSPEC:-<empty>}"

if is_cmdline_hook_run; then
    log_marker "WAIT_ROOT_CMDLINE_SEED"
    initqueue --onetime --name wait-root-cmdline-seed /bin/sh /sbin/wait-root.sh
    exit 0
fi

# Ensure this script is executed at least once as an initqueue job each boot.
if [ ! -e "$ONCE_FLAG" ]; then
    : >"$ONCE_FLAG" 2>/dev/null || true
    initqueue --onetime --name wait-root-always /bin/sh "$0"
fi

parts="$(blkid 2>/dev/null | awk -F':' '{print $1}' | tr '\n' ' ')"
log_marker "WAIT_ROOT_ROUND_${CURRENT_ROUND}_PARTS ${parts:-<none>}"

if [ -z "$ROOTSPEC" ]; then
    log_marker "no root= found in cmdline; skipping wait-root check"
    exit 0
fi

case "$ROOTSPEC" in
    /dev/*)
        ROOTDEV="$ROOTSPEC"
        ;;
    UUID=*)
        ROOTDEV="/dev/disk/by-uuid/${ROOTSPEC#UUID=}"
        ;;
    LABEL=*)
        ROOTDEV="/dev/disk/by-label/${ROOTSPEC#LABEL=}"
        ;;
    PARTUUID=*)
        ROOTDEV="/dev/disk/by-partuuid/${ROOTSPEC#PARTUUID=}"
        ;;
    PARTLABEL=*)
        ROOTDEV="/dev/disk/by-partlabel/${ROOTSPEC#PARTLABEL=}"
        ;;
    *)
        ROOTDEV="$ROOTSPEC"
        ;;
esac

if [ -b "$ROOTDEV" ]; then
    log_marker "root device exists: $ROOTDEV"
    exit 0
fi

NEXT_ROUND=$((CURRENT_ROUND + 1))
echo "$NEXT_ROUND" >"$ROUND_FILE" 2>/dev/null || true

if [ "$CURRENT_ROUND" -lt "$MAX_ROUNDS" ]; then
    log_marker "WAIT_ROOT_REQUEUE round=$CURRENT_ROUND rootdev=$ROOTDEV"
    initqueue --onetime --name wait-root-round /bin/sh "$0"
else
    log_marker "WAIT_ROOT_MAX_ROUNDS_REACHED rootdev=$ROOTDEV"
fi

# Keep dracut's settled check for real root readiness in parallel.
initqueue --settled /bin/sh -c "test -b \"$ROOTDEV\""
