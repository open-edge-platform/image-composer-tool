#!/usr/bin/env bash
# SPDX-FileCopyrightText: (C) 2026 Intel Corporation
# SPDX-License-Identifier: Apache-2.0
#
# container-build.sh — build one template inside the ICT image and copy the
# resulting artifacts into a directory mounted from the host.
#
# Runs inside the container (scripts/build-in-container.sh starts it from the
# host). The build itself uses the image's work and cache directories; only
# the files this run produced under <work-dir>/*/imagebuild/ are copied, into
# <output>/<system-config-name>/, so the host directory never receives the
# root-owned chroot trees a build leaves behind.

set -euo pipefail

template=""
output=""
owner=""
work_dir="workspace"

usage() {
    cat <<'EOF'
Usage: container-build.sh --template FILE --output DIR [--owner UID:GID] [-- BUILD_FLAGS...]

Options:
  --template FILE   Template to build (a path inside the container).
  --output DIR      Directory, on a volume mounted from the host, that receives
                    the artifacts as DIR/<system-config-name>/<file>.
  --owner UID:GID   Give the copied artifacts to this user and group, so the
                    host user owns them instead of root.
  -h, --help        Show this help.

Arguments after -- are passed to `image-composer-tool build`.
EOF
}

die() {
    echo "ERROR: $*" >&2
    exit 2
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --template) [[ $# -ge 2 ]] || die "--template requires a path"; template="$2"; shift 2 ;;
        --output) [[ $# -ge 2 ]] || die "--output requires a directory"; output="$2"; shift 2 ;;
        --owner) [[ $# -ge 2 ]] || die "--owner requires UID:GID"; owner="$2"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        --) shift; break ;;
        *) usage >&2; die "unknown argument: $1" ;;
    esac
done
build_flags=("$@")

[[ -n "$template" ]] || { usage >&2; die "--template is required"; }
[[ -n "$output" ]] || { usage >&2; die "--output is required"; }
[[ -f "$template" ]] || die "template not found in the container: $template"
[[ -z "$owner" || "$owner" =~ ^[0-9]+:[0-9]+$ ]] || die "--owner must be numeric UID:GID, got: $owner"

# Refuse an output directory on the container's own filesystem: the artifacts
# would be deleted with the container instead of reaching the host.
mkdir -p "$output"
if [[ "$(findmnt --noheadings --output TARGET --target "$output")" == "/" ]]; then
    die "$output is not on a mounted volume; mount a host directory there (docker run -v HOST_DIR:$output)"
fi

# Honour a --work-dir passed through to the build when collecting artifacts.
for ((i = 0; i < ${#build_flags[@]}; i++)); do
    case "${build_flags[$i]}" in
        --work-dir) work_dir="${build_flags[$((i + 1))]:-$work_dir}" ;;
        --work-dir=*) work_dir="${build_flags[$i]#--work-dir=}" ;;
    esac
done

marker="$(mktemp)"
trap 'rm -f "$marker"' EXIT

# Run the build as a child and forward SIGINT/SIGTERM to it, so stopping the
# container lets ICT unmount its filesystems and detach its loop devices.
image-composer-tool build "${build_flags[@]}" "$template" &
build_pid=$!
trap 'kill -TERM "$build_pid" 2>/dev/null || true' INT TERM
status=0
wait "$build_pid" || status=$?
while kill -0 "$build_pid" 2>/dev/null; do
    wait "$build_pid" || status=$?
done
trap - INT TERM
if [[ $status -ne 0 ]]; then
    echo "ERROR: image-composer-tool build exited with status $status; nothing copied to $output" >&2
    exit "$status"
fi

echo "Artifacts:"
copied=0
while IFS= read -r -d '' file; do
    relative="${file#*/imagebuild/}"
    install -D -m 0644 "$file" "$output/$relative"
    [[ -z "$owner" ]] || chown "$owner" "$output/$relative" "$(dirname "$output/$relative")"
    echo "  $output/$relative"
    copied=$((copied + 1))
done < <(find "$work_dir" -path '*/imagebuild/*' -type f -newer "$marker" -print0)

if [[ $copied -eq 0 ]]; then
    echo "ERROR: the build succeeded but produced no files under $work_dir/*/imagebuild/" >&2
    exit 1
fi
echo "Copied $copied artifact(s) to $output"
