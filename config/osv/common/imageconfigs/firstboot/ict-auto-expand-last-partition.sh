#!/bin/sh
set -eu

STATE_DIR=/var/lib/image-composer-tool
STAMP_FILE=${STATE_DIR}/last-partition-expanded

[ -f "${STAMP_FILE}" ] && exit 0

ROOT_SRC=$(findmnt -n -o SOURCE / || true)
[ -n "${ROOT_SRC}" ] || exit 0

DISK_NAME=$(lsblk -no PKNAME "${ROOT_SRC}" 2>/dev/null | head -n1 || true)
[ -n "${DISK_NAME}" ] || exit 0
DISK_DEV=/dev/${DISK_NAME}

LAST_PART_NAME=$(lsblk -ln -o NAME,TYPE "${DISK_DEV}" | awk '$2=="part"{print $1}' | tail -n1 || true)
[ -n "${LAST_PART_NAME}" ] || exit 0
LAST_PART_DEV=/dev/${LAST_PART_NAME}
LAST_PART_NUM=$(lsblk -no PARTN "${LAST_PART_DEV}" 2>/dev/null | head -n1 || true)
[ -n "${LAST_PART_NUM}" ] || exit 0

# Verify that the last partition is actually the rootfs partition
if [ "${LAST_PART_DEV}" != "${ROOT_SRC}" ]; then
  exit 0
fi

echo ', +' | sfdisk --no-reread --force -N "${LAST_PART_NUM}" "${DISK_DEV}"

partprobe "${DISK_DEV}" || true
udevadm settle || true

FS_TYPE=$(lsblk -no FSTYPE "${LAST_PART_DEV}" 2>/dev/null | head -n1 || true)
MOUNT_POINT=$(findmnt -n -o TARGET "${LAST_PART_DEV}" 2>/dev/null || true)

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
systemctl disable ict-auto-expand-last-partition.service >/dev/null 2>&1 || true
