#!/bin/bash
set -euo pipefail

# cleanup_workspace
# Unmounts any lingering chroot mounts from interrupted builds and removes
# the workspace directory.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
MAX_UMOUNT_PASSES=5
FORCE_DELETE=false

if ! command -v realpath >/dev/null 2>&1; then
  echo "realpath command is required but not found. Install coreutils or ensure realpath is in PATH." >&2
  exit 1
fi

DEFAULT_WORKSPACE_DIR_INPUT="$REPO_ROOT/workspace"
if ! DEFAULT_WORKSPACE_DIR="$(realpath -m "$DEFAULT_WORKSPACE_DIR_INPUT" 2>/dev/null)"; then
  echo "Failed to resolve default workspace directory path: $DEFAULT_WORKSPACE_DIR_INPUT" >&2
  exit 1
fi
readonly DEFAULT_WORKSPACE_DIR
WORKSPACE_DIR="$DEFAULT_WORKSPACE_DIR"

run_privileged() {
  if [[ "$EUID" -eq 0 ]]; then
    "$@"
  else
    sudo "$@"
  fi
}

list_workspace_mounts() {
  if command -v findmnt >/dev/null 2>&1; then
    findmnt -rn -o TARGET | awk -v ws="$WORKSPACE_DIR" '$0 == ws || index($0, ws "/") == 1' | sort -r -u || true
    return
  fi

  # Fallback if findmnt is unavailable.
  awk '{print $2}' /proc/mounts \
    | sed 's/\\040/ /g' \
    | awk -v ws="$WORKSPACE_DIR" '$0 == ws || index($0, ws "/") == 1' \
    | sort -r -u || true
}

try_unmount_once() {
  local mount_point="$1"
  echo "  unmounting: $mount_point"

  if run_privileged umount "$mount_point" 2>/dev/null; then
    return 0
  fi

  # Busy mounts from interrupted chroots often require lazy unmount.
  if run_privileged umount -l "$mount_point" 2>/dev/null; then
    echo "    used lazy unmount for busy mount"
    return 0
  fi

  return 1
}

usage() {
  echo "Usage: $0 [--workspace-dir DIR] [--max-umount-passes N] [--force] [-h|--help]"
  echo "  --workspace-dir DIR  Override workspace directory (default: $WORKSPACE_DIR)"
  echo "  --max-umount-passes N  Number of unmount retry passes (default: $MAX_UMOUNT_PASSES)"
  echo "  --force              Allow deleting a non-default workspace path"
  echo "  -h, --help           Show this help message"
}

while [[ $# -gt 0 ]]; do
  case $1 in
    --workspace-dir)
      if [[ $# -lt 2 ]]; then
        echo "Error: --workspace-dir requires a value" >&2
        usage
        exit 1
      fi
      WORKSPACE_DIR="$2"
      shift 2
      ;;
    --max-umount-passes)
      if [[ $# -lt 2 ]]; then
        echo "Error: --max-umount-passes requires a value" >&2
        usage
        exit 1
      fi
      MAX_UMOUNT_PASSES="$2"
      shift 2
      ;;
    --force)
      FORCE_DELETE=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1"
      usage
      exit 1
      ;;
  esac
done

WORKSPACE_DIR_INPUT="$WORKSPACE_DIR"
if ! WORKSPACE_DIR="$(realpath -m "$WORKSPACE_DIR_INPUT" 2>/dev/null)"; then
  echo "Failed to canonicalize workspace directory path (check permissions or path validity): $WORKSPACE_DIR_INPUT" >&2
  exit 1
fi

if [[ "$WORKSPACE_DIR" == "/" ]]; then
  echo "Refusing to remove root directory" >&2
  exit 1
fi

if [[ "$WORKSPACE_DIR" != "$DEFAULT_WORKSPACE_DIR" && "$FORCE_DELETE" != true ]]; then
  # Both paths are canonicalized via realpath -m before this comparison.
  echo "Refusing to remove non-default workspace directory without --force: $WORKSPACE_DIR" >&2
  exit 1
fi

if [[ ! -d "$WORKSPACE_DIR" ]]; then
  echo "Workspace directory does not exist: $WORKSPACE_DIR"
  exit 0
fi

echo "Cleaning up workspace: $WORKSPACE_DIR"

if ! [[ "$MAX_UMOUNT_PASSES" =~ ^[0-9]+$ ]] || [[ "$MAX_UMOUNT_PASSES" -lt 1 ]]; then
  echo "Invalid --max-umount-passes value: $MAX_UMOUNT_PASSES"
  exit 1
fi

# Find all active mounts under the workspace, sort in reverse order (deepest first)
# so child mounts are unmounted before parents.
MOUNTS="$(list_workspace_mounts)"

if [[ -z "$MOUNTS" ]]; then
  echo "No active mounts found under $WORKSPACE_DIR"
else
  pass=1
  while [[ "$pass" -le "$MAX_UMOUNT_PASSES" ]]; do
    MOUNTS="$(list_workspace_mounts)"
    if [[ -z "$MOUNTS" ]]; then
      break
    fi

    echo "Unmount pass $pass/$MAX_UMOUNT_PASSES"
    echo "$MOUNTS"

    while IFS= read -r mount_point; do
      [[ -z "$mount_point" ]] && continue
      if ! try_unmount_once "$mount_point"; then
        echo "    WARNING: unable to unmount $mount_point in this pass"
      fi
    done <<< "$MOUNTS"

    pass=$((pass + 1))
    sleep 1
  done

  MOUNTS="$(list_workspace_mounts)"
  if [[ -n "$MOUNTS" ]]; then
    echo "ERROR: Some mounts are still active under $WORKSPACE_DIR"
    echo "$MOUNTS"
    echo "Try closing terminals/processes using this chroot and rerun."
    exit 1
  fi
fi

echo "Removing workspace directory..."
run_privileged rm -rf "$WORKSPACE_DIR"
echo "Done."
