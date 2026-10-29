#!/usr/bin/env bash
# SPDX-FileCopyrightText: (C) 2026 Intel Corporation
# SPDX-License-Identifier: Apache-2.0
#
# build-in-container.sh: build an OS image from a template inside the ICT
# container image and receive the result in a host directory, without
# installing ICT or the programs it runs on the host.
#
# It starts one privileged `docker run` that mounts the template's directory
# (or --context) read-only at /input and OUTPUT_DIR at --mount-point, keeps
# downloaded packages and the build's work tree in named volumes, passes on the
# host's proxy variables, and runs scripts/container-build.sh in the container.
#
# SECURITY: the container runs privileged with the host's /dev mounted
# (building an OS image creates loop devices, mounts filesystems and chroots
# into them), so it is root-equivalent on the host.
#
# Guide: docs/user-guide/get-started/cli-docker-quick-start.md

set -euo pipefail

image="ict"
mount_point="/output"
context=""
cache_volume="ict-cache"
workspace_volume="ict-workspace"

usage() {
    cat <<'EOF'
Usage: build-in-container.sh [options] TEMPLATE OUTPUT_DIR [-- BUILD_FLAGS...]

Build TEMPLATE, an image template on this host, inside the ICT container image,
and copy what the build produced (the OS image, its SBOM and any other build
outputs) into OUTPUT_DIR as OUTPUT_DIR/<systemConfig name>/<file>. OUTPUT_DIR is
created if missing.

Options:
  --mount-point DIR       Path inside the container where OUTPUT_DIR is
                          mounted (default: /output).
  --context DIR           Host directory mounted read-only at /input (default:
                          the template's directory). It must contain the
                          template and every file the template references:
                          an `extends` parent, additional files, scripts.
  --image NAME            Container image to run (default: ict).
  --cache-volume NAME     Named volume for downloaded packages
                          (default: ict-cache).
  --workspace-volume NAME Named volume for the build's work tree
                          (default: ict-workspace).
  -h, --help              Show this help.

Arguments after -- go to `image-composer-tool build`, for example
`-- --log-level debug`. A file path among them must be a path inside the
container: a file under the context directory is at /input/<path>.
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

# Create the output directory as the user running this script. If it were left
# to `docker run -v`, Docker would create it owned by root.
mkdir -p "${positional[1]}"
output="$(realpath "${positional[1]}")"

# `-e NAME` without a value passes the host's value of NAME into the container.
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
