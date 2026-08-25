#!/bin/bash
# SPDX-FileCopyrightText: (C) 2026 Intel Corporation
# SPDX-License-Identifier: Apache-2.0
#
# install-sudoers.sh — install the scoped sudoers drop-in that lets a non-root
# `image-composer-tool serve --sudo` process build, cancel, and read build
# artifacts as root (and nothing else).
#
# Run this ONCE per host, as (or via) root. It:
#   1. asks the ICT binary to generate the rules for THIS user + binary + workdir
#      (`serve --print-sudoers`),
#   2. validates them with `visudo -cf` (so a typo can never lock you out of sudo),
#   3. installs them atomically to /etc/sudoers.d/image-composer-tool-webui (0440).
#
# Usage:
#   sudo ./scripts/install-sudoers.sh [--ict-binary PATH] [--work-dir DIR] [--user NAME]
#   sudo ICT_BINARY=/opt/ict/image-composer-tool ./scripts/install-sudoers.sh
#
# The generated rules are user-specific. By default they target the user who
# invoked sudo (SUDO_USER); pass --user to target a dedicated service account.
set -euo pipefail

DROPIN_NAME="image-composer-tool-webui"
DROPIN_PATH="/etc/sudoers.d/${DROPIN_NAME}"

ICT_BINARY="${ICT_BINARY:-}"
WORK_DIR="${WORK_DIR:-}"
TARGET_USER="${SUDO_USER:-$(id -un)}"

while [[ $# -gt 0 ]]; do
	case "$1" in
		--ict-binary) ICT_BINARY="$2"; shift 2 ;;
		--work-dir)   WORK_DIR="$2";   shift 2 ;;
		--user)       TARGET_USER="$2"; shift 2 ;;
		-h|--help)
			grep '^#' "$0" | sed 's/^# \{0,1\}//'
			exit 0 ;;
		*) echo "unknown argument: $1" >&2; exit 2 ;;
	esac
done

if [[ "$(id -u)" -ne 0 ]]; then
	echo "error: must run as root (use sudo) to write ${DROPIN_PATH}" >&2
	exit 1
fi

# Locate the ICT binary if not given: prefer ./build, then ./, then PATH.
if [[ -z "$ICT_BINARY" ]]; then
	for cand in ./build/image-composer-tool ./image-composer-tool; do
		if [[ -x "$cand" ]]; then ICT_BINARY="$cand"; break; fi
	done
fi
if [[ -z "$ICT_BINARY" ]]; then
	ICT_BINARY="$(command -v image-composer-tool || true)"
fi
if [[ -z "$ICT_BINARY" || ! -x "$ICT_BINARY" ]]; then
	echo "error: could not find the image-composer-tool binary; pass --ict-binary PATH" >&2
	exit 1
fi

# Generate the rules AS THE TARGET USER so `serve --print-sudoers` records that
# user's login name (running as root would refuse — root needs no rules). We also
# pass the resolved binary/workdir so the printed paths match runtime exactly.
gen_args=(serve --print-sudoers --ict-binary "$ICT_BINARY")
[[ -n "$WORK_DIR" ]] && gen_args+=(--work-dir "$WORK_DIR")

echo "Generating scoped sudoers rules for user '${TARGET_USER}' using '${ICT_BINARY}'..." >&2
RULES="$(runuser -u "$TARGET_USER" -- "$ICT_BINARY" "${gen_args[@]}")"

if [[ -z "$RULES" ]]; then
	echo "error: generator produced no rules" >&2
	exit 1
fi

# Validate before installing: a syntactically invalid file in /etc/sudoers.d can
# break sudo for everyone. `visudo -cf -` checks stdin without installing.
TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT
printf '%s' "$RULES" > "$TMP"

if ! visudo -cf "$TMP" >/dev/null; then
	echo "error: generated sudoers rules failed validation; NOT installing. Rules were:" >&2
	echo "$RULES" >&2
	exit 1
fi

# Atomic install with the mode sudo requires (0440, root:root).
install -m 0440 -o root -g root "$TMP" "$DROPIN_PATH"

echo "Installed ${DROPIN_PATH}:" >&2
echo "-----------------------------------------------------------------------" >&2
cat "$DROPIN_PATH" >&2
echo "-----------------------------------------------------------------------" >&2
echo "Done. Verify passwordless resolution as '${TARGET_USER}', e.g.:" >&2
echo "  sudo -l -U ${TARGET_USER} | grep image-composer-tool" >&2
