#!/bin/sh
set -eu

STATE_DIR=/var/lib/image-composer-tool
STAMP_FILE=${STATE_DIR}/last-partition-expanded

log() {
  echo "[ict-auto-expand-last-partition] $*"
}

missing_cmds=""
missing_pkgs=""

append_unique_pkg() {
  pkg=$1
  case ",${missing_pkgs}," in
    *,"${pkg}",*) ;;
    *) missing_pkgs="${missing_pkgs}${missing_pkgs:+, }${pkg}" ;;
  esac
}

require_cmd() {
  cmd=$1
  pkg_hint=$2
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    missing_cmds="${missing_cmds}${missing_cmds:+, }${cmd}"
    append_unique_pkg "${pkg_hint}"
  fi
}

fail_if_missing_prereqs() {
  [ -z "${missing_cmds}" ] && return 0

  log "ERROR: missing required command(s): ${missing_cmds}"
  log "ERROR: missing package(s): ${missing_pkgs}"
  exit 1
}

require_base_prereqs() {
  # Base commands required by all execution paths.
  require_cmd findmnt "util-linux"
  require_cmd lsblk "util-linux"
  require_cmd sfdisk "fdisk/util-linux"
  require_cmd partprobe "parted"
  require_cmd udevadm "udev/systemd"
  require_cmd readlink "coreutils"
  require_cmd systemctl "systemd"
}

require_fs_prereqs() {
  fs_type=$1
  case "${fs_type}" in
    ext2|ext3|ext4)
      require_cmd resize2fs "e2fsprogs"
      ;;
    xfs)
      require_cmd xfs_growfs "xfsprogs"
      ;;
    btrfs)
      require_cmd btrfs "btrfs-progs"
      ;;
    linux-swap|swap)
      require_cmd swapoff "util-linux"
      require_cmd mkswap "util-linux"
      require_cmd swapon "util-linux"
      ;;
    *)
      ;;
  esac
}

require_base_prereqs
fail_if_missing_prereqs

[ -f "${STAMP_FILE}" ] && {
  log "stamp file exists, skipping"
  exit 0
}

ROOT_SRC=$(findmnt -n -o SOURCE / || true)
[ -n "${ROOT_SRC}" ] || {
  log "could not determine root source"
  exit 0
}
ROOT_SRC_REAL=$(readlink -f "${ROOT_SRC}" 2>/dev/null || echo "${ROOT_SRC}")

DISK_NAME=$(lsblk -no PKNAME "${ROOT_SRC_REAL}" 2>/dev/null | head -n1 || true)
[ -n "${DISK_NAME}" ] || {
  log "could not determine parent disk for root source: ${ROOT_SRC_REAL}"
  exit 0
}
DISK_DEV=/dev/${DISK_NAME}

LAST_PART_NAME=$(lsblk -ln -o NAME,TYPE "${DISK_DEV}" | awk '$2=="part"{print $1}' | tail -n1 || true)
[ -n "${LAST_PART_NAME}" ] || {
  log "could not determine last partition on disk: ${DISK_DEV}"
  exit 0
}
LAST_PART_DEV=/dev/${LAST_PART_NAME}
LAST_PART_NUM=$(lsblk -no PARTN "${LAST_PART_DEV}" 2>/dev/null | head -n1 || true)
[ -n "${LAST_PART_NUM}" ] || LAST_PART_NUM=$(printf '%s' "${LAST_PART_NAME}" | sed -E 's/.*[^0-9]([0-9]+)$/\1/' || true)
[ -n "${LAST_PART_NUM}" ] || {
  log "could not determine partition number for: ${LAST_PART_DEV}"
  exit 0
}
LAST_PART_DEV_REAL=$(readlink -f "${LAST_PART_DEV}" 2>/dev/null || echo "${LAST_PART_DEV}")

# Verify that the last partition is actually the rootfs partition
if [ "${LAST_PART_DEV_REAL}" != "${ROOT_SRC_REAL}" ]; then
  log "skipping: last partition is not rootfs (last=${LAST_PART_DEV_REAL}, root=${ROOT_SRC_REAL})"
  exit 0
fi

log "expanding last partition ${LAST_PART_DEV} on ${DISK_DEV}"
echo ', +' | sfdisk --no-reread --force -N "${LAST_PART_NUM}" "${DISK_DEV}"

partprobe "${DISK_DEV}" || true
udevadm settle || true

FS_TYPE=$(lsblk -no FSTYPE "${LAST_PART_DEV}" 2>/dev/null | head -n1 || true)
MOUNT_POINT=$(findmnt -n -o TARGET "${LAST_PART_DEV}" 2>/dev/null || true)

require_fs_prereqs "${FS_TYPE}"
fail_if_missing_prereqs

case "${FS_TYPE}" in
  ext2|ext3|ext4)
    resize2fs "${LAST_PART_DEV}" || true
    ;;
  xfs)
    [ -n "${MOUNT_POINT}" ] && xfs_growfs "${MOUNT_POINT}" || true
    ;;
  btrfs)
    [ -n "${MOUNT_POINT}" ] && btrfs filesystem resize max "${MOUNT_POINT}" || true
    ;;
  linux-swap|swap)
    swapoff "${LAST_PART_DEV}" || true
    mkswap -f "${LAST_PART_DEV}" || true
    swapon "${LAST_PART_DEV}" || true
    ;;
  *)
    :
    ;;
esac

mkdir -p "${STATE_DIR}"
touch "${STAMP_FILE}"
log "expansion flow finished, disabling one-shot service"
systemctl disable ict-auto-expand-last-partition.service >/dev/null 2>&1 || true
