#!/usr/bin/env bash
# SPDX-FileCopyrightText: (C) 2026 Intel Corporation
# SPDX-License-Identifier: Apache-2.0
#
# build-in-container.sh — build a template with the ICT image instead of a
# host toolchain, and receive the image in a host directory.
#
# The template's directory (or --context) is mounted read-only at /input, the
# output directory at --mount-point, and the container runs
# scripts/container-build.sh with both paths as arguments. Named volumes keep
# the package cache and the build workspace between runs.
#
# SECURITY: the container runs privileged with the host's /dev mounted
# (builds create loop devices, mount filesystems and chroot), so it is
# root-equivalent on the host.

set -euo pipefail

image="ict"
mount_point="/output"
context=""
cache_volume="ict-cache"
workspace_volume="ict-workspace"

usage() {
    cat <<'EOF'
Usage: build-in-container.sh [options] TEMPLATE OUTPUT_DIR [-- BUILD_FLAGS...]

Build TEMPLATE inside the ICT image and copy the artifacts into OUTPUT_DIR
(created if missing) as OUTPUT_DIR/<system-config-name>/<file>.

Options:
  --mount-point DIR       Where OUTPUT_DIR is mounted inside the container
                          (default: /output).
  --context DIR           Directory mounted read-only at /input (default: the
                          template's directory). It must contain the template
                          and everything it references: extends parents,
                          additional files, scripts.
  --image NAME            ICT image to run (default: ict).
  --cache-volume NAME     Named volume for the package cache (default: ict-cache).
  --workspace-volume NAME Named volume for the build workspace (default: ict-workspace).
  -h, --help              Show this help.

Arguments after -- are passed to `image-composer-tool build`.
EOF
}

die() {
    echo "ERROR: $*" >&2
    exit 2
}

positional=()
while [[ $# -gt 0 ]]; do
    case "$1" in
        --mount-point) [[ $# -ge 2 ]] || die "--mount-point requires a directory"; mount_point="$2"; shift 2 ;;
        --context) [[ $# -ge 2 ]] || die "--context requires a directory"; context="$2"; shift 2 ;;
        --image) [[ $# -ge 2 ]] || die "--image requires a name"; image="$2"; shift 2 ;;
        --cache-volume) [[ $# -ge 2 ]] || die "--cache-volume requires a name"; cache_volume="$2"; shift 2 ;;
        --workspace-volume) [[ $# -ge 2 ]] || die "--workspace-volume requires a name"; workspace_volume="$2"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        --) shift; break ;;
        -*) usage >&2; die "unknown option: $1" ;;
        *) positional+=("$1"); shift ;;
    esac
done
[[ ${#positional[@]} -eq 2 ]] || { usage >&2; die "expected TEMPLATE and OUTPUT_DIR"; }

[[ -f "${positional[0]}" ]] || die "template not found: ${positional[0]}"
template="$(realpath "${positional[0]}")"
context="$(realpath "${context:-$(dirname "$template")}")"
[[ "$template" == "$context"/* ]] || die "the template must be inside --context ($context)"
[[ "$mount_point" == /* ]] || die "--mount-point must be an absolute path inside the container"
case "$mount_point" in
    /|/input|/input/*|/dev|/dev/*|/opt/ict|/opt/ict/*) die "--mount-point $mount_point would shadow a path the build needs" ;;
esac

if ! docker image inspect "$image" >/dev/null 2>&1; then
    die "image '$image' not found; build it with" \
        "'docker build -f build/ict/Dockerfile --target ict -t ict .' or 'earthly +ict-image'," \
        "or pass the one 'docker compose build' made with --image"
fi

# Create the output directory as the invoking user; left to docker, it would
# be created owned by root.
mkdir -p "${positional[1]}"
output="$(realpath "${positional[1]}")"

proxy_env=()
for name in http_proxy https_proxy no_proxy HTTP_PROXY HTTPS_PROXY NO_PROXY; do
    [[ -z "${!name:-}" ]] || proxy_env+=(-e "$name")
done

exec docker run --rm --init --privileged \
    -v /dev:/dev \
    -v "$context:/input:ro" \
    -v "$output:$mount_point" \
    -v "$cache_volume:/opt/ict/cache" \
    -v "$workspace_volume:/opt/ict/workspace" \
    "${proxy_env[@]}" \
    "$image" \
    /opt/ict/scripts/container-build.sh \
    --template "/input/${template#"$context"/}" \
    --output "$mount_point" \
    --owner "$(id -u):$(id -g)" \
    -- "$@"
