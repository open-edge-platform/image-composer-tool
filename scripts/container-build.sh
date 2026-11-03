#!/usr/bin/env bash
# SPDX-FileCopyrightText: (C) 2026 Intel Corporation
# SPDX-License-Identifier: Apache-2.0
#
# container-build.sh: build one template inside the ICT container image, then
# copy what the build produced into a directory mounted from the host.
#
# Runs inside the container; on the host, scripts/build-in-container.sh starts
# the container and passes it the template's path and the output mount point.
# The build itself works in the image's work directory (/opt/ict/workspace,
# unless --work-dir is passed through), which holds the root-owned chroot trees
# of every build. Only the files this run wrote under
# <work-dir>/<os>-<dist>-<arch>/imagebuild/ are copied, keeping the path they
# had there, so the host receives <output>/<systemConfig name>/<file> and none
# of those trees.

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
  --output DIR      Directory, mounted from the host, that receives the
                    build's outputs as DIR/<systemConfig name>/<file>.
  --owner UID:GID   Give the copied files to this numeric user and group, so
                    they belong to the host user instead of root.
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

# Refuse an output directory on the container's own filesystem (findmnt then
# reports / as the mount it is on): the files would be deleted with the
# container instead of reaching the host.
mkdir -p "$output"
if [[ "$(findmnt --noheadings --output TARGET --target "$output")" == "/" ]]; then
    die "$output is not on a mounted volume; mount a host directory there (docker run -v HOST_DIR:$output)"
fi

# If the build was told to use another work directory, collect from there.
for ((i = 0; i < ${#build_flags[@]}; i++)); do
    case "${build_flags[$i]}" in
        --work-dir) work_dir="${build_flags[$((i + 1))]:-$work_dir}" ;;
        --work-dir=*) work_dir="${build_flags[$i]#--work-dir=}" ;;
    esac
done

# Files newer than this marker were written by this run, not by an earlier
# build that left its outputs in the same work directory.
marker="$(mktemp)"
trap 'rm -f "$marker"' EXIT

# Run the build in the background and pass SIGINT and SIGTERM (Ctrl+C, docker
# stop) on to it, so ICT can unmount its filesystems and detach its loop
# devices before it exits. `wait` returns early when a signal arrives, so keep
# waiting until the build has really exited.
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
