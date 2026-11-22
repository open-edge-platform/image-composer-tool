#!/usr/bin/env bash
# SPDX-FileCopyrightText: (C) 2026 Intel Corporation
# SPDX-License-Identifier: Apache-2.0
#
# install-ict.sh - install the Image Composer Tool (ICT) Debian package and the
# host tools a build needs, on Ubuntu or Debian.
#
#   bash install-ict.sh --tag ict-2026.3-rc2   # download from a GitHub release
#   bash install-ict.sh --deb ./ict_2026.3_amd64.deb
#
# Run it as a normal user that can use sudo (or as root). The script runs only its
# apt commands through sudo, so GITHUB_TOKEN and proxy variables stay visible to it;
# starting it with sudo would drop them.
#
# With --tag the script downloads the amd64 .deb and SHA256SUMS that the release
# workflow publishes for the tag, checks the checksum, then installs the package.
# apt resolves the package's own dependencies. The host tools that a build
# installs on demand are installed up front so the first build does not need to.
set -euo pipefail

repo="${ICT_GITHUB_REPOSITORY:-open-edge-platform/image-composer-tool}"
api="${GITHUB_API_URL:-https://api.github.com}"

usage() {
    cat <<'EOF'
Usage: install-ict.sh (--tag TAG | --deb FILE) [options]

Install the Image Composer Tool Debian package and its prerequisites.

  --tag TAG             Download the .deb of the GitHub release for TAG
                        (for example ict-2026.3-rc2) and verify its SHA-256.
  --deb FILE            Install a .deb you already downloaded. If a SHA256SUMS
                        file sits next to it, the checksum is verified.
  --with-cross-arch     Also install the user-mode QEMU needed to build for
                        another CPU architecture.
  --skip-prerequisites  Install only the package and what apt requires for it.
  -h, --help            Show this help.

Set ICT_GITHUB_REPOSITORY=OWNER/REPO to download from another repository's release
(default: open-edge-platform/image-composer-tool), with GITHUB_TOKEN if it is private.

Run as a user that can use sudo, without sudo in front (the script elevates its
apt commands itself, so exported GITHUB_TOKEN and proxy variables are kept), or as
root. Needs network access to the
distribution's apt mirrors and, with --tag, to GitHub. GITHUB_TOKEN, if set, is
sent to the GitHub API. Proxies are taken from the usual environment variables.
EOF
}

die() { echo "error: $*" >&2; exit 1; }
info() { echo "==> $*"; }

tag=""
deb=""
cross_arch=false
prerequisites=true
while [[ $# -gt 0 ]]; do
    case "$1" in
        --tag|--deb)
            [[ $# -ge 2 && -n "$2" && "$2" != --* ]] || die "$1 requires a value"
            if [[ "$1" == "--tag" ]]; then tag="$2"; else deb="$2"; fi
            shift 2
            ;;
        --with-cross-arch) cross_arch=true; shift ;;
        --skip-prerequisites) prerequisites=false; shift ;;
        -h|--help) usage; exit 0 ;;
        *) usage >&2; die "unknown argument: $1" ;;
    esac
done
[[ -n "${tag}" || -n "${deb}" ]] || { usage >&2; die "give --tag or --deb"; }
[[ -z "${tag}" || -z "${deb}" ]] || die "give only one of --tag and --deb"
[[ -z "${tag}" || "${tag}" =~ ^[A-Za-z0-9._+-]+$ ]] || die "invalid tag: ${tag}"
[[ -z "${deb}" || -f "${deb}" ]] || die "no such file: ${deb}"

# Debian family only: the package is a .deb for amd64.
[[ -r /etc/os-release ]] || die "cannot read /etc/os-release"
# shellcheck disable=SC1091
. /etc/os-release
case " ${ID:-} ${ID_LIKE:-} " in
    *" debian "*|*" ubuntu "*) ;;
    *) die "unsupported distribution '${ID:-unknown}': needs Ubuntu or Debian" ;;
esac
command -v apt-get >/dev/null || die "apt-get not found"
arch="$(dpkg --print-architecture)"
[[ "${arch}" == "amd64" ]] || die "the ICT package is built for amd64; this host is ${arch}"

SUDO=()
if [[ ${EUID} -ne 0 ]]; then
    command -v sudo >/dev/null || die "run as root or install sudo"
    SUDO=(sudo)
fi
# sudo drops most of the environment, so pass the proxy settings to apt-get
# explicitly (the usual variables, in both cases, when set).
apt_env=(DEBIAN_FRONTEND=noninteractive)
for var in http_proxy https_proxy ftp_proxy no_proxy all_proxy \
           HTTP_PROXY HTTPS_PROXY FTP_PROXY NO_PROXY ALL_PROXY; do
    [[ -z "${!var:-}" ]] || apt_env+=("${var}=${!var}")
done
apt_get() { "${SUDO[@]}" env "${apt_env[@]}" apt-get "$@"; }

# Install the packages in "$@" that this release of the distribution offers, one
# by one, and report the ones it does not, so one renamed package does not stop
# the rest.
apt_install_available() {
    local pkg candidate
    local -a want=()
    for pkg in "$@"; do
        candidate="$(LC_ALL=C apt-cache policy "${pkg}" 2>/dev/null | sed -n 's/^  Candidate: //p')"
        if [[ -n "${candidate}" && "${candidate}" != "(none)" ]]; then
            want+=("${pkg}")
        else
            echo "warning: ${pkg} is not available on ${PRETTY_NAME:-this host}; skipping" >&2
        fi
    done
    [[ ${#want[@]} -eq 0 ]] || apt_get install -y --no-install-recommends "${want[@]}"
}

info "Updating the package index"
apt_get update

workdir=""
cleanup() { [[ -z "${workdir}" ]] || rm -rf "${workdir}"; }
trap cleanup EXIT

if [[ -n "${tag}" ]]; then
    apt_get install -y --no-install-recommends curl ca-certificates jq
    workdir="$(mktemp -d)"
    chmod 0755 "${workdir}"
    info "Looking up release ${tag} of ${repo}"
    auth=()
    [[ -z "${GITHUB_TOKEN:-}" ]] || auth=(-H "Authorization: Bearer ${GITHUB_TOKEN}")
    curl -fsSL "${auth[@]}" -H "Accept: application/vnd.github+json" \
        -o "${workdir}/release.json" "${api}/repos/${repo}/releases/tags/${tag//+/%2B}" \
        || die "could not read release ${tag} of ${repo}"
    # The package is published as ict_<version>_amd64.deb by this repository's release
    # workflow, and as image-composer-tool_<version>_amd64.deb by some other builds of it.
    deb_name="$(jq -r '[.assets[].name | select(test("^(ict|image-composer-tool)_.*_amd64\\.deb$"))][0] // empty' "${workdir}/release.json")"
    [[ -n "${deb_name}" ]] || die "release ${tag} has no ict_*_amd64.deb or image-composer-tool_*_amd64.deb asset"
    jq -e '.assets[] | select(.name == "SHA256SUMS")' "${workdir}/release.json" >/dev/null \
        || die "release ${tag} has no SHA256SUMS asset"
    # Download through the API asset URLs: they work for private repositories too,
    # unlike browser_download_url, which does not accept the API token.
    fetch_asset() {
        local name="$1" url
        url="$(jq -r --arg n "${name}" '.assets[] | select(.name == $n) | .url' "${workdir}/release.json")"
        curl -fSL "${auth[@]}" -H "Accept: application/octet-stream" -o "${workdir}/${name}" "${url}"
    }
    info "Downloading ${deb_name}"
    fetch_asset "${deb_name}"
    fetch_asset SHA256SUMS
    deb="${workdir}/${deb_name}"
fi

# Verify when a checksum file is available (always with --tag).
sums="$(dirname "${deb}")/SHA256SUMS"
if [[ -f "${sums}" ]]; then
    info "Verifying the checksum"
    # Check the entry whose file name is exactly this package's (a longer name that
    # merely contains it must not count), and fail if there is none.
    deb_name="$(basename "${deb}")"
    # The whole text after the hash must equal the file name, so an entry such as
    # "<hash>  <name> backup" is not taken for <name>.
    hash="$(awk -v n="${deb_name}" '
        { rest = substr($0, length($1) + 1); sub(/^ [ *]/, "", rest)
          if (rest == n) { print $1; exit } }' "${sums}")"
    [[ "${hash}" =~ ^[0-9a-fA-F]{64}$ ]] || die "no entry for ${deb_name} in ${sums}"
    (cd "$(dirname "${deb}")" && sha256sum -c - <<<"${hash}  ${deb_name}") \
        || die "checksum verification failed for ${deb}"
elif [[ -n "${tag}" ]]; then
    die "no SHA256SUMS to verify ${deb}"
else
    echo "warning: no SHA256SUMS next to ${deb}; checksum not verified" >&2
fi

if [[ "${prerequisites}" == true ]]; then
    info "Installing the host tools builds use"
    # The same tools the providers install on demand (see installHostDependency in
    # internal/provider/*), plus sudo, which builds run their privileged commands
    # through even as root, and the disk, filesystem and signing tools a minimal
    # image or container may lack. A package the distribution release does not have is
    # reported and skipped.
    apt_install_available sudo mmdebstrap dosfstools mtools xorriso qemu-utils \
        systemd-ukify grub-common cryptsetup sbsigntool debian-archive-keyring \
        systemd-boot-efi dpkg-dev \
        fdisk parted gdisk e2fsprogs xfsprogs cloud-guest-utils cpio gnupg openssl curl wget xz-utils zstd
    command -v mmdebstrap >/dev/null || apt_install_available debootstrap
    if [[ "${cross_arch}" == true ]]; then
        # Cross-architecture Debian builds check for arch-test and update-binfmts
        # (binfmt-support) as well as the user-mode QEMU, which Ubuntu 26.04
        # provides as qemu-user-binfmt and older releases as qemu-user-static.
        apt_install_available arch-test binfmt-support
        qemu_candidate="$(LC_ALL=C apt-cache policy qemu-user-static 2>/dev/null | sed -n 's/^  Candidate: //p')"
        if [[ -n "${qemu_candidate}" && "${qemu_candidate}" != "(none)" ]]; then
            apt_install_available qemu-user-static
        else
            apt_install_available qemu-user-binfmt
        fi
    fi
fi

info "Installing the ICT package"
# Copy so the unprivileged apt download user can read it; apt resolves the
# package's dependencies.
if [[ -z "${workdir}" ]]; then
    workdir="$(mktemp -d)"
    chmod 0755 "${workdir}"
fi
install_deb="${workdir}/$(basename "${deb}")"
[[ "${install_deb}" -ef "${deb}" ]] || cp "${deb}" "${install_deb}"
chmod 0644 "${install_deb}"
apt_get install -y --no-install-recommends "${install_deb}"

info "Installed"
image-composer-tool version
