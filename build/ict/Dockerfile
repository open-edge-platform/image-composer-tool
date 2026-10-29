# SPDX-FileCopyrightText: (C) 2026 Intel Corporation
# SPDX-License-Identifier: Apache-2.0
#
# ICT image: the image-composer-tool binary (the CLI, with the web UI embedded
# in `serve`), live-installer, config/, image-templates/ and the complete build
# toolchain on Ubuntu 24.04. Everything sits under /opt/ict in the repository
# layout, so the tool resolves config/ and image-templates/ exactly as it does
# from a checkout.
#
# Stages:
#   web        node:22       builds the web UI bundle (web/dist)
#   bin        golang:1.26   embeds the bundle, builds both binaries
#   toolchain  ubuntu:24.04  build toolchain; cached until the package list changes
#   ict        final         toolchain + binaries + runtime files
#
# Build it with any of:
#   docker compose build                     (docker-compose.yml at the repo root)
#   earthly +ict-image                       (stamps VERSION and COMMIT_SHA)
#   docker build -f build/ict/Dockerfile --target ict -t ict .
#
# Behind a proxy, pass --build-arg http_proxy=... (and https_proxy, no_proxy).
# These are Docker's predefined proxy args: RUN steps see them, but they are
# not recorded in the image.

# ── web: frontend bundle ─────────────────────────────────────────────────────
FROM node:22 AS web

WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# ── bin: image-composer-tool + live-installer ────────────────────────────────
FROM golang:1.26 AS bin

ARG VERSION=dev
ARG COMMIT_SHA=unknown

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/

# Replace the committed placeholder page with the real UI bundle, so `serve`
# embeds the SPA instead of deferring to a dev server.
RUN rm -rf internal/webui/dist
COPY --from=web /src/web/dist internal/webui/dist

# Same -ldflags as the Earthfile's +build target.
RUN BUILD_DATE=$(date -u '+%Y-%m-%d') && \
    LDFLAGS="-s -w \
        -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.Version=${VERSION}' \
        -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.Toolname=Image-Composer-Tool' \
        -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.Organization=Open Edge Platform' \
        -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.BuildDate=${BUILD_DATE}' \
        -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.CommitSHA=${COMMIT_SHA}'" && \
    CGO_ENABLED=0 go build -trimpath -buildmode=pie -ldflags "$LDFLAGS" \
        -o build/live-installer ./cmd/live-installer && \
    CGO_ENABLED=0 go build -trimpath -buildmode=pie -ldflags "$LDFLAGS" \
        -o build/image-composer-tool ./cmd/image-composer-tool

# ── toolchain: Ubuntu 24.04 + every tool ICT shells out to ───────────────────
# Every command in internal/utils/shell/shell.go commandMap must resolve here;
# scripts/preflight.sh checks that on every container start.
FROM ubuntu:24.04 AS toolchain

ARG DEBIAN_FRONTEND=noninteractive

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
    # crypto / integrity (ca-certificates: TLS trust for ICT's own downloads)
    ca-certificates \
    cryptsetup-bin \
    openssl \
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
    # DKMS kernel module build/verify tooling
    dkms \
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
    # provider host dependencies the toolchain above does not cover. Each
    # provider's installHostDependency() apt-installs missing ones at build
    # time, which fails here because the image ships no apt lists.
    arch-test \
    debian-archive-keyring \
    qemu-user-static \
    \
    # misc
    lsof \
    \
    && rm -rf /var/lib/apt/lists/*

# tdnf/yum/opkg/snap are distro-specific and not installable on Ubuntu.
# run is a legacy allowlist entry and may be absent in Ubuntu.
# Create explicit stubs at the allowlist path (/usr/bin/<cmd>) so commandMap
# entries resolve and fail clearly when invoked.
RUN for cmd in tdnf yum opkg snap run; do \
      if [ ! -e /usr/bin/$cmd ]; then \
        printf '#!/bin/sh\necho "ERROR: %s is not available in the ICT image (distro-specific tool)" >&2\nexit 127\n' "$cmd" \
          > /usr/bin/$cmd && \
        chmod +x /usr/bin/$cmd; \
      fi; \
    done

# Compatibility aliases for distro naming differences referenced by commandMap.
RUN if [ ! -e /usr/sbin/grub2-mkconfig ] && [ -e /usr/sbin/grub-mkconfig ]; then \
      ln -s /usr/sbin/grub-mkconfig /usr/sbin/grub2-mkconfig; \
    fi

# ── ict: final image ─────────────────────────────────────────────────────────
FROM toolchain AS ict

ARG VERSION=dev
ARG COMMIT_SHA=unknown

LABEL org.opencontainers.image.title="Image Composer Tool" \
      org.opencontainers.image.description="Image Composer Tool CLI, web UI (serve) and build toolchain" \
      org.opencontainers.image.source="https://github.com/open-edge-platform/image-composer-tool" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT_SHA}"

WORKDIR /opt/ict

# The binaries go in build/, where `earthly +build` puts them: the ISO initrd
# configs reference live-installer as <repo>/build/live-installer, and `serve`
# finds ./build/image-composer-tool on its own.
COPY --from=bin /src/build/image-composer-tool /src/build/live-installer build/
COPY image-composer-tool.yml ./
COPY config/ config/
COPY image-templates/ image-templates/
COPY scripts/preflight.sh scripts/preflight.sh
# Builds one template from the CLI into a host-mounted directory; see
# scripts/build-in-container.sh.
COPY scripts/container-build.sh scripts/container-build.sh
# preflight.sh reads the required tool list from shell.go's commandMap.
COPY internal/utils/shell/shell.go internal/utils/shell/shell.go
RUN chmod +x scripts/preflight.sh scripts/container-build.sh

ENV PATH=/opt/ict/build:$PATH

ENTRYPOINT ["/opt/ict/scripts/preflight.sh", "--command-map-file", "/opt/ict/internal/utils/shell/shell.go", "--"]
CMD ["image-composer-tool", "--help"]
