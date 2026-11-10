#!/usr/bin/env bash
# SPDX-FileCopyrightText: (C) 2026 Intel Corporation
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version=""
output="${root}/dist"
while [[ $# -gt 0 ]]; do
    case "$1" in
        --version|--output)
            if [[ $# -lt 2 || -z "$2" || "$2" == --* ]]; then
                echo "error: $1 requires a value" >&2
                exit 2
            fi
            case "$1" in
                --version) version="$2" ;;
                --output) output="$2" ;;
            esac
            shift 2
            ;;
        -h|--help)
            echo "Usage: bash scripts/build-deb.sh [--version VERSION] [--output DIRECTORY]"
            exit 0
            ;;
        *) echo "error: unknown argument: $1" >&2; exit 2 ;;
    esac
done

for tool in go git dpkg-deb; do
    command -v "${tool}" >/dev/null || {
        echo "error: ${tool} is required" >&2
        exit 1
    }
done
commit="$(git -C "${root}" rev-parse HEAD)"
if [[ -z "${version}" ]]; then
    base="$(sed -nE 's/^## Version ([0-9][0-9.]*)[[:space:]]*$/\1/p' \
        "${root}/docs/user-guide/release-notes.md" | head -1)"
    version="${base}~snapshot.${GITHUB_RUN_NUMBER:-0}.${commit:0:8}"
fi
if [[ ! "${version}" =~ ^[0-9][0-9A-Za-z.+~]*$ ]]; then
    echo "error: invalid Debian version: ${version}" >&2
    exit 2
fi
goversion="$(cd "${root}" && go env GOVERSION)"
if ! printf '%s\n%s\n' 1.26.1 "${goversion#go}" | sort -V -C; then
    echo "error: Go 1.26.1 or newer is required; found ${goversion}" >&2
    exit 1
fi

stage="$(mktemp -d)"
trap 'rm -rf "${stage}"' EXIT
version_pkg="github.com/open-edge-platform/image-composer-tool/internal/config/version"
build_date="$(date -u -d "@${SOURCE_DATE_EPOCH:-$(date +%s)}" +%Y-%m-%d)"
ldflags="-s -w -X ${version_pkg}.Version=${version} \
 -X ${version_pkg}.Toolname=Image-Composer-Tool \
 -X '${version_pkg}.Organization=Open Edge Platform' \
 -X ${version_pkg}.BuildDate=${build_date} \
 -X ${version_pkg}.CommitSHA=${commit:0:8}"
mkdir -p "${stage}/usr/local/bin"
(
    cd "${root}"
    for binary in image-composer-tool live-installer; do
        CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
            go build -trimpath -buildmode=pie -ldflags "${ldflags}" \
                -o "${stage}/usr/local/bin/${binary}" "./cmd/${binary}"
    done
)
mkdir -p "${stage}/etc/image-composer-tool/config" \
    "${stage}/usr/share/image-composer-tool/examples" \
    "${stage}/usr/share/doc/ict" \
    "${stage}/var/cache/image-composer-tool" "${stage}/DEBIAN"
printf '%s\n' 'workers: 8' \
    'config_dir: "/etc/image-composer-tool/config"' \
    'cache_dir: "/var/cache/image-composer-tool"' \
    'work_dir: "/tmp/image-composer-tool"' 'temp_dir: "/tmp"' \
    'logging:' '  level: "info"' > "${stage}/etc/image-composer-tool/config.yml"
cp -r "${root}/config/." "${stage}/etc/image-composer-tool/config/"
cp -r "${root}/image-templates/." "${stage}/usr/share/image-composer-tool/examples/"
install -m 0644 "${root}/README.md" "${root}/LICENSE" \
    "${root}/docs/user-guide/architecture/image-composer-tool-cli-specification.md" \
    "${stage}/usr/share/doc/ict/"
printf 'Version: %s\nCommit: %s\nBuild date: %s\n' \
    "${version}" "${commit}" "${build_date}" > "${stage}/usr/share/doc/ict/build-info.txt"
cat > "${stage}/DEBIAN/control" <<EOF
Package: ict
Version: ${version}
Section: utils
Priority: optional
Architecture: amd64
Maintainer: Intel Edge Software Team <edge.platform@intel.com>
Depends: bash, coreutils, unzip, dosfstools, xorriso, grub-common
Recommends: mmdebstrap, debootstrap
Description: Image Composer Tool (ICT)
 Compose custom bootable OS images from templates.
EOF
(cd "${stage}" && find etc -type f | sort | sed 's|^|/|' > DEBIAN/conffiles)
chmod 0644 "${stage}/DEBIAN/"* "${stage}/etc/image-composer-tool/config.yml" \
    "${stage}/usr/share/doc/ict/build-info.txt"
mkdir -p "${output}"
filename="ict_${version}_amd64.deb"
dpkg-deb --root-owner-group --build "${stage}" "${output}/${filename}"
printf '%s\n' "${version}" > "${output}/image-composer-tool.version"
(cd "${output}" && sha256sum "${filename}" > SHA256SUMS)
echo "Built ${output}/${filename}"
