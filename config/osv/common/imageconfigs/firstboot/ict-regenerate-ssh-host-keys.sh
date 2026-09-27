#!/bin/sh
set -eu

STATE_DIR=/var/lib/image-composer-tool
STAMP_FILE=${STATE_DIR}/ssh-host-keys-regenerated

log() {
  echo "[ict-regenerate-ssh-host-keys] $*"
}

[ -f "${STAMP_FILE}" ] && {
  log "stamp file exists, skipping"
  exit 0
}

if ! command -v ssh-keygen >/dev/null 2>&1; then
  log "ssh-keygen not found, skipping"
  exit 0
fi

# ssh-keygen -A only generates the host key types that are missing, so this is
# a no-op if keys are already present (e.g. this ran once already, or the
# image was not built from an installerPayload whose deploy path strips them).
log "regenerating any missing SSH host keys"
ssh-keygen -A

mkdir -p "${STATE_DIR}"
touch "${STAMP_FILE}"
log "regeneration flow finished, disabling one-shot service"
systemctl disable ict-regenerate-ssh-host-keys.service >/dev/null 2>&1 || true
