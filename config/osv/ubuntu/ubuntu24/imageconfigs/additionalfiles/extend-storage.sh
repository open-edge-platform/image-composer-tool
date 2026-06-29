#!/bin/bash
# dash/sh do not support "set -o pipefail"; re-exec with bash if needed.
case "${BASH_VERSION:-}" in
'') exec /bin/bash "$0" "$@" ;;
esac

# Grow the last GPT partition (assumed rootfs) and resize its filesystem to fill the disk.
# Use after flashing/cloning a raw image to a larger physical disk (dd, bmaptool, clone),
# so the rootfs uses the full device instead of the original image size.
#
# Target layout: GPT disk where the highest-numbered partition is mounted at "/" (ext4).
# Matches image-templates/ubuntu24-x86_64-agent.yml (ESP + rootfs as last partition).
#
# Usage (must run under bash — not "sh extend-storage.sh"):
#   sudo /opt/agent/extend-storage.sh
#   sudo EXTEND_STORAGE_DRY_RUN=1 /opt/agent/extend-storage.sh   # show plan, no changes
#
# Optional env (defaults):
#   EXTEND_STORAGE_DISK=            # e.g. /dev/nvme0n1 — skip auto-detect of disk
#   EXTEND_STORAGE_PART=            # e.g. 2 — skip auto-detect of partition number
#   EXTEND_STORAGE_DRY_RUN=0        # 1 = print growpart/resize plan, make no changes
#   EXTEND_STORAGE_ALLOW_NON_ROOT=0 # 1 = grow last partition even if not mounted at /
#
# Idempotent: exits 0 with a log line when the partition already fills the disk.
# Requires: root, GPT disk, last partition = root ext4. Installs cloud-guest-utils if growpart missing.

set -euo pipefail

readonly SCRIPT_NAME="${0##*/}"
readonly SCRIPT_REV="2026-06-29-extend-storage-v1"
readonly LOG_TAG="extend-storage"
readonly LOG_FILE="/var/log/extend-storage.log"

EXTEND_STORAGE_DISK="${EXTEND_STORAGE_DISK:-}"
EXTEND_STORAGE_PART="${EXTEND_STORAGE_PART:-}"
EXTEND_STORAGE_DRY_RUN="${EXTEND_STORAGE_DRY_RUN:-0}"
EXTEND_STORAGE_ALLOW_NON_ROOT="${EXTEND_STORAGE_ALLOW_NON_ROOT:-0}"

log() {
	echo "[${LOG_TAG}] $(date -u +%Y-%m-%dT%H:%M:%SZ) $*" | tee -a "${LOG_FILE}" >&2
}

require_root() {
	if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
		echo "${SCRIPT_NAME}: run as root (e.g. sudo ${SCRIPT_NAME})" >&2
		exit 1
	fi
}

# Split a partition device into base disk + partition number, handling nvme/mmcblk pN naming.
split_part_device() {
	local part="$1"
	local disk="" num=""

	if [[ "${part}" =~ ^(/dev/(nvme[0-9]+n[0-9]+|mmcblk[0-9]+|loop[0-9]+))p([0-9]+)$ ]]; then
		disk="/dev/${BASH_REMATCH[2]}"
		num="${BASH_REMATCH[3]}"
	elif [[ "${part}" =~ ^(/dev/[a-z]+)([0-9]+)$ ]]; then
		disk="${BASH_REMATCH[1]}"
		num="${BASH_REMATCH[2]}"
	else
		return 1
	fi

	echo "${disk} ${num}"
}

# Reassemble disk + partition number into a partition device, handling pN naming.
part_device_for() {
	local disk="$1"
	local num="$2"

	if [[ "${disk}" =~ (nvme[0-9]+n[0-9]+|mmcblk[0-9]+|loop[0-9]+)$ ]]; then
		echo "${disk}p${num}"
	else
		echo "${disk}${num}"
	fi
}

highest_partition_number() {
	local disk="$1"
	lsblk -n -r -o NAME,TYPE "${disk}" 2>/dev/null \
		| awk '$2 == "part" {print $1}' \
		| grep -Eo '[0-9]+$' \
		| sort -n \
		| tail -1
}

device_size_bytes() {
	blockdev --getsize64 "$1" 2>/dev/null
}

detect_root_partition() {
	local src
	src="$(findmnt -n -o SOURCE / 2>/dev/null || true)"
	if [[ -z "${src}" ]]; then
		log "ERROR: could not determine root device via findmnt -n -o SOURCE /"
		exit 1
	fi
	echo "${src}"
}

ensure_deps() {
	local need_growpart=0 need_resize=0
	command -v growpart >/dev/null 2>&1 || need_growpart=1
	command -v resize2fs >/dev/null 2>&1 || need_resize=1

	if [[ "${need_growpart}" -eq 0 && "${need_resize}" -eq 0 ]]; then
		return 0
	fi

	if [[ "${EXTEND_STORAGE_DRY_RUN}" == "1" ]]; then
		log "DRY-RUN: would apt-get install cloud-guest-utils e2fsprogs (growpart/resize2fs missing)"
		return 0
	fi

	log "Installing missing tools (cloud-guest-utils, e2fsprogs)"
	export DEBIAN_FRONTEND=noninteractive
	apt-get update -y
	apt-get install -y --no-install-recommends cloud-guest-utils e2fsprogs
}

main() {
	require_root
	mkdir -p "$(dirname "${LOG_FILE}")"
	: >>"${LOG_FILE}"

	log "=== ${SCRIPT_NAME} start (rev=${SCRIPT_REV}, dry_run=${EXTEND_STORAGE_DRY_RUN}) ==="

	local root_part disk partnum parsed
	if [[ -n "${EXTEND_STORAGE_DISK}" && -n "${EXTEND_STORAGE_PART}" ]]; then
		disk="${EXTEND_STORAGE_DISK}"
		partnum="${EXTEND_STORAGE_PART}"
		root_part="$(part_device_for "${disk}" "${partnum}")"
		log "Using override target: disk=${disk}, partition=${partnum} (${root_part})"
	else
		root_part="$(detect_root_partition)"
		if ! parsed="$(split_part_device "${root_part}")"; then
			log "ERROR: cannot parse disk/partition from root device '${root_part}'"
			log "Hint: LVM/LUKS/btrfs subvolume roots are unsupported; set EXTEND_STORAGE_DISK and EXTEND_STORAGE_PART"
			exit 1
		fi
		disk="${parsed%% *}"
		partnum="${parsed##* }"
		log "Detected root: ${root_part} -> disk=${disk}, partition=${partnum}"
	fi

	if [[ ! -b "${disk}" ]]; then
		log "ERROR: disk ${disk} is not a block device"
		exit 1
	fi

	local last
	last="$(highest_partition_number "${disk}")"
	if [[ -z "${last}" ]]; then
		log "ERROR: no partitions found on ${disk}"
		exit 1
	fi
	if [[ "${partnum}" != "${last}" ]]; then
		log "ERROR: target partition ${partnum} is not the last partition on ${disk} (last is ${last})"
		log "Hint: this script only grows the final partition; aborting to avoid overwriting later partitions"
		exit 1
	fi

	if [[ "${EXTEND_STORAGE_ALLOW_NON_ROOT}" != "1" ]]; then
		local mnt
		mnt="$(findmnt -n -o TARGET "${root_part}" 2>/dev/null || true)"
		if [[ "${mnt}" != "/" ]]; then
			log "ERROR: ${root_part} is not mounted at / (mountpoint='${mnt:-none}')"
			log "Hint: set EXTEND_STORAGE_ALLOW_NON_ROOT=1 to grow the last partition anyway"
			exit 1
		fi
	fi

	# Idempotency: skip when the partition already reaches (near) the end of the disk.
	local disk_bytes part_bytes free_bytes
	disk_bytes="$(device_size_bytes "${disk}" || true)"
	part_bytes="$(device_size_bytes "${root_part}" || true)"
	if [[ -n "${disk_bytes}" && -n "${part_bytes}" ]]; then
		free_bytes=$((disk_bytes - part_bytes))
		# Allow ~34 sectors of GPT backup + alignment slack (1 MiB) before considering a grow worthwhile.
		if [[ "${free_bytes}" -lt 1048576 ]]; then
			log "Partition ${root_part} already fills ${disk} (free=${free_bytes} bytes); nothing to do"
			log "=== ${SCRIPT_NAME} complete (no-op) ==="
			return 0
		fi
		log "Unallocated space after ${root_part}: ${free_bytes} bytes — will grow"
	else
		log "WARN: could not read disk/partition sizes; attempting grow anyway"
	fi

	ensure_deps

	local fstype
	fstype="$(findmnt -n -o FSTYPE "${root_part}" 2>/dev/null || true)"

	if [[ "${EXTEND_STORAGE_DRY_RUN}" == "1" ]]; then
		log "DRY-RUN: growpart ${disk} ${partnum}"
		log "DRY-RUN: partprobe ${disk} (or partx -u)"
		if [[ "${fstype}" == "ext4" || "${fstype}" == "ext3" || "${fstype}" == "ext2" ]]; then
			log "DRY-RUN: resize2fs ${root_part}"
		else
			log "DRY-RUN: filesystem '${fstype:-unknown}' not ext*; would skip resize2fs"
		fi
		log "=== ${SCRIPT_NAME} complete (dry-run) ==="
		return 0
	fi

	log "growpart ${disk} ${partnum}"
	# growpart returns 1 with "NOCHANGE" when nothing to do; treat that as success.
	local gp_out gp_rc
	set +e
	gp_out="$(growpart "${disk}" "${partnum}" 2>&1)"
	gp_rc=$?
	set -e
	log "growpart: ${gp_out}"
	if [[ "${gp_rc}" -ne 0 ]]; then
		if grep -qi 'NOCHANGE' <<<"${gp_out}"; then
			log "growpart reported NOCHANGE; partition already at maximum"
		else
			log "ERROR: growpart failed (rc=${gp_rc})"
			exit 1
		fi
	fi

	if command -v partprobe >/dev/null 2>&1; then
		partprobe "${disk}" || log "WARN: partprobe ${disk} returned non-zero"
	else
		partx -u "${disk}" || log "WARN: partx -u ${disk} returned non-zero"
	fi
	sync

	case "${fstype}" in
	ext2 | ext3 | ext4)
		log "resize2fs ${root_part}"
		resize2fs "${root_part}"
		;;
	"")
		log "WARN: could not determine filesystem type for ${root_part}; skipping filesystem resize"
		;;
	*)
		log "WARN: filesystem '${fstype}' is not ext*; partition grown but filesystem not resized"
		log "Hint: resize ${fstype} manually (e.g. xfs_growfs / for xfs, btrfs filesystem resize max /)"
		;;
	esac

	log "Final size: $(findmnt -n -o SIZE / 2>/dev/null || echo unknown) on /"
	log "=== ${SCRIPT_NAME} complete ==="
}

main "$@"
