#!/usr/bin/env bash
set -euo pipefail

IMAGE="${1:-image.raw}"
OUT="${2:-ict-rootfs.tar}"
PART_NUM="${3:-}"
MOUNT_DIR="${MOUNT_DIR:-/tmp/ict-rootfs}"
ADD_WSL_CONF="${ADD_WSL_CONF:-true}"

usage() {
  echo "Usage: $0 [image.raw] [output.tar|output.tar.gz] [partition_number]"
  echo "Env vars: MOUNT_DIR=/tmp/ict-rootfs ADD_WSL_CONF=true|false"
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

for cmd in sudo fdisk losetup mount umount tar gzip mktemp; do
  require_cmd "$cmd"
done

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

if [[ ! -f "$IMAGE" ]]; then
  echo "Image not found: $IMAGE" >&2
  exit 1
fi

if [[ -e "$OUT" ]]; then
  echo "Output already exists: $OUT" >&2
  echo "Remove it or choose a different output path." >&2
  exit 1
fi

LOOP=""
ROOT_PART=""
WSL_TMP_DIR=""

cleanup() {
  set +e
  if mountpoint -q "$MOUNT_DIR" 2>/dev/null; then
    sudo umount "$MOUNT_DIR" 2>/dev/null || true
  fi
  if [[ -n "$LOOP" ]]; then
    sudo losetup -d "$LOOP" 2>/dev/null || true
  fi
  if [[ -n "$WSL_TMP_DIR" && -d "$WSL_TMP_DIR" ]]; then
    rm -rf "$WSL_TMP_DIR"
  fi
}
trap cleanup EXIT

echo "Inspecting image:"
sudo fdisk -l "$IMAGE"

if [[ -z "$PART_NUM" ]]; then
  echo
  read -rp "Enter rootfs partition number (e.g. 2 for p2): " PART_NUM
fi

if [[ ! "$PART_NUM" =~ ^[0-9]+$ ]]; then
  echo "Invalid partition number: $PART_NUM" >&2
  exit 1
fi

LOOP=$(sudo losetup --find --show --partscan --read-only "$IMAGE")
echo "Attached $IMAGE as $LOOP"

ROOT_PART="${LOOP}p${PART_NUM}"
if [[ ! -b "$ROOT_PART" ]]; then
  echo "Partition device not found: $ROOT_PART" >&2
  exit 1
fi

sudo mkdir -p "$MOUNT_DIR"

echo "Mounting $ROOT_PART at $MOUNT_DIR (read-only)"
sudo mount -o ro "$ROOT_PART" "$MOUNT_DIR"

echo "Rootfs OS info:"
sudo cat "$MOUNT_DIR/etc/os-release" || true

create_archive() {
  local out_file="$1"
  local -a tar_args=(
    --numeric-owner
    --acls
    --xattrs
    --selinux
    --one-file-system
    -cpf -
    -C "$MOUNT_DIR" .
  )

  if [[ "$ADD_WSL_CONF" == "true" ]]; then
    WSL_TMP_DIR=$(mktemp -d)
    mkdir -p "$WSL_TMP_DIR/etc"
    cat > "$WSL_TMP_DIR/etc/wsl.conf" <<'EOF'
[boot]
systemd=true
EOF
    tar_args+=( -C "$WSL_TMP_DIR" etc/wsl.conf )
    echo "Injecting /etc/wsl.conf into archive (without modifying source image)"
  fi

  case "$out_file" in
    *.tar)
      sudo tar "${tar_args[@]}" > "$out_file"
      ;;
    *.tar.gz|*.tgz)
      sudo tar "${tar_args[@]}" | gzip -n > "$out_file"
      ;;
    *)
      echo "Unsupported output extension: $out_file" >&2
      echo "Use .tar or .tar.gz" >&2
      return 1
      ;;
  esac
}

echo "Creating rootfs archive: $OUT"
create_archive "$OUT"

echo "Done: $OUT"
echo
echo "On Windows, import with:"
echo "wsl --import test-aiagent C:\\WSL\\test-aiagent C:\\path\\to\\$(basename "$OUT") --version 2"