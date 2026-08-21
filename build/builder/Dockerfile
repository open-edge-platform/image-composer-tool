# SPDX-FileCopyrightText: (C) 2026 Intel Corporation
# SPDX-License-Identifier: Apache-2.0
#
# Builder image: Ubuntu 24.04 base with the complete ICT toolchain bundled.
# Every command in internal/utils/shell/shell.go commandMap must resolve here.
#
# Layer order (do not reorder — toolchain layer must cache across binary updates):
#   Layer 1: base OS + apt sources
#   Layer 2: toolchain packages       <-- cached; changes only when packages change
#   Layer 3: ICT binary               <-- invalidated on every build

FROM ubuntu:24.04

ARG DEBIAN_FRONTEND=noninteractive
ARG http_proxy
ARG https_proxy
ARG no_proxy
ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG NO_PROXY

ENV http_proxy=${http_proxy} \
    https_proxy=${https_proxy} \
    no_proxy=${no_proxy} \
    HTTP_PROXY=${HTTP_PROXY} \
    HTTPS_PROXY=${HTTPS_PROXY} \
    NO_PROXY=${NO_PROXY}

# ── Layer 2: full ICT toolchain ───────────────────────────────────────────────
# Packages are grouped by the functional area they cover.
# Special tools: tdnf (Azure-Linux-only), yum/opkg (non-Ubuntu package managers),
# snap (needs snapd), and run (legacy allowlist entry) are handled after the main
# install block.
RUN apt-get update && apt-get install -y --no-install-recommends \
    \
    # core OS utilities (coreutils, util-linux, procps, etc.)
    bash \
    coreutils \
    util-linux \
    procps \
    psmisc \
    findutils \
    grep \
    gawk \
    sed \
    tar \
    gzip \
    bzip2 \
    xz-utils \
    zstd \
    curl \
    wget \
    sudo \
    lsb-release \
    gnupg \
    gpg \
    cron \
    \
    # filesystem tools
    e2fsprogs \
    xfsprogs \
    dosfstools \
    mtools \
    parted \
    gdisk \
    cloud-guest-utils \
    lvm2 \
    \
    # disk / block device tools
    fdisk \
    eject \
    \
    # crypto / integrity
    cryptsetup-bin \
    \
    # bootloader
    grub-common \
    grub2-common \
    efibootmgr \
    \
    # initramfs / UKI / systemd
    initramfs-tools \
    dracut-core \
    systemd \
    systemd-boot \
    systemd-ukify \
    binfmt-support \
    \
    # image / disk creation
    qemu-utils \
    qemu-system-x86 \
    xorriso \
    sbsigntool \
    \
    # Debian toolchain (mmdebstrap, dpkg-dev, etc.)
    mmdebstrap \
    debootstrap \
    dpkg-dev \
    apt-utils \
    \
    # RPM toolchain (cross-family package management)
    rpm \
    createrepo-c \
    dnf \
    \
    # misc
    lsof \
    \
    && rm -rf /var/lib/apt/lists/*

# ── Special tools ─────────────────────────────────────────────────────────────
# tdnf/yum/opkg/snap are distro-specific and not installable on Ubuntu.
# run is a legacy allowlist entry and may be absent in Ubuntu.
# Create explicit stubs at the allowlist path (/usr/bin/<cmd>) so commandMap
# entries resolve and fail clearly when invoked.
RUN for cmd in tdnf yum opkg snap run; do \
      if [ ! -e /usr/bin/$cmd ]; then \
        printf '#!/bin/sh\necho "ERROR: %s is not available in this builder image (distro-specific tool)" >&2\nexit 127\n' "$cmd" \
          > /usr/bin/$cmd && \
        chmod +x /usr/bin/$cmd; \
      fi; \
    done

# Compatibility aliases for distro naming differences referenced by commandMap.
RUN if [ ! -e /usr/sbin/grub2-mkconfig ] && [ -e /usr/sbin/grub-mkconfig ]; then \
      ln -s /usr/sbin/grub-mkconfig /usr/sbin/grub2-mkconfig; \
    fi

# ── Layer 3: runtime support files ────────────────────────────────────────────
# The ICT binary is injected by Earthly after this Dockerfile build so the
# toolchain layer stays cached and the image does not depend on a host-local
# build artifact being present in the Docker build context.
COPY scripts/preflight.sh /usr/local/bin/preflight.sh
COPY internal/utils/shell/shell.go /usr/share/image-composer-tool/shell.go
RUN chmod +x /usr/local/bin/preflight.sh

ENTRYPOINT ["/usr/local/bin/preflight.sh", "--command-map-file", "/usr/share/image-composer-tool/shell.go"]
CMD ["image-composer-tool", "--help"]
