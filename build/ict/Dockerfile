# SPDX-FileCopyrightText: (C) 2026 Intel Corporation
# SPDX-License-Identifier: Apache-2.0
#
# ICT container image: Image Composer Tool (ICT) together with everything it
# needs to build OS images, so the host needs only Docker. It contains:
#   - image-composer-tool, the CLI. Its `serve` command runs the web UI, whose
#     frontend is compiled into the binary.
#   - live-installer, the program that ISO images boot into to install the OS
#     onto a disk. ISO builds copy it into the image.
#   - config/ (per-OS defaults) and image-templates/ (the shipped templates).
#   - Every external program a build runs (debootstrap, mkfs, grub and so on),
#     installed on Ubuntu 24.04.
# Everything sits under /opt/ict in the same layout as a checkout of this
# repository, and /opt/ict is the working directory, because ICT finds config/
# and image-templates/ relative to the directory it runs in.
#
# Build it with any of:
#   docker compose build          # uses docker-compose.yml at the repository root
#   earthly +ict-image            # also stamps the version and git commit
#   docker build -f build/ict/Dockerfile --target ict -t ict .
#
# Run it privileged, with the host's /dev mounted. Building an OS image creates
# loop devices, mounts filesystems and chroots into them, none of which an
# unprivileged container may do:
#   docker run --rm --privileged -v /dev:/dev ict image-composer-tool --help
# scripts/build-in-container.sh (build a template from the command line) and
# docker-compose.yml (serve the web UI) run it that way for you.
#
# Behind a proxy, add --build-arg http_proxy=... (and https_proxy, no_proxy).
# These are Docker's predefined proxy build arguments: RUN steps use them, but
# Docker does not store them in the image, so the image never carries the build
# host's proxy. Pass a proxy at run time instead (docker run -e http_proxy ...).
#
# Each FROM below starts a stage; only the last one becomes the image:
#   web        node:22       compiles the web UI frontend into web/dist
#   bin        golang:1.26   builds image-composer-tool, with the frontend
#                            embedded, and live-installer
#   toolchain  ubuntu:24.04  installs the external programs. This is the slow
#                            step; Docker reuses its cached result until the
#                            package list changes.
#   ict        final image   toolchain + binaries + config, templates, scripts

# ── web: compile the web UI frontend ─────────────────────────────────────────
# A React app built with Vite (see web/README.md); the output is static files.
FROM node:22 AS web

WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# ── bin: build image-composer-tool and live-installer ────────────────────────
FROM golang:1.26 AS bin

ARG VERSION=dev
ARG COMMIT_SHA=unknown

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/

# The binary embeds whatever internal/webui/dist holds (see embed.go there).
# The repository commits only a placeholder page in it, so that Go builds work
# without Node.js; replace it with the frontend the web stage compiled.
RUN rm -rf internal/webui/dist
COPY --from=web /src/web/dist internal/webui/dist

# -X sets the details `image-composer-tool version` prints, and -s -w strip
# debug symbols. Keep these flags the same as the Earthfile's +build target.
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

# ── toolchain: Ubuntu 24.04 + every program ICT runs ─────────────────────────
# ICT runs external programs only through an allowlist: commandMap in
# internal/utils/shell/shell.go, which names each command and the paths it may
# run from. Every command in that list must exist in this image.
# scripts/preflight.sh checks that each time the container starts, and
# `earthly +ict-image-preflight` checks it in CI. A new command added to
# commandMap therefore needs its package added here.
FROM ubuntu:24.04 AS toolchain

ARG DEBIAN_FRONTEND=noninteractive

# Packages are grouped by what they are for. Five allowlisted commands have no
# Ubuntu package (tdnf, yum, opkg, snap, run); the step after this one gives
# them stubs.
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
    # crypto / integrity. ca-certificates holds the CA bundle that ICT's own
    # HTTPS downloads (package indexes, signing keys) are verified against.
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
    # Before each build, the OS providers (internal/provider/<os>/) apt-install
    # these on the host unless they are already present. That install fails in
    # this image, which deletes its apt package lists (last line of this step),
    # so install them here instead.
    arch-test \
    debian-archive-keyring \
    qemu-user-static \
    \
    # misc
    lsof \
    \
    && rm -rf /var/lib/apt/lists/*

# tdnf, yum, opkg and snap belong to other distributions and cannot be
# installed on Ubuntu; run is a legacy allowlist entry with no package. Give
# each a stub at /usr/bin/<cmd>, the path commandMap allows, so the allowlist
# check passes and a build that does call one fails with a clear message.
RUN for cmd in tdnf yum opkg snap run; do \
      if [ ! -e /usr/bin/$cmd ]; then \
        printf '#!/bin/sh\necho "ERROR: %s is not available in the ICT image (distro-specific tool)" >&2\nexit 127\n' "$cmd" \
          > /usr/bin/$cmd && \
        chmod +x /usr/bin/$cmd; \
      fi; \
    done

# commandMap uses the RPM-family name grub2-mkconfig; Ubuntu calls it
# grub-mkconfig.
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

# The binaries go in build/, where `earthly +build` puts them in a checkout.
# ISO builds look for live-installer at <repository>/build/live-installer, and
# `serve` runs each build with ./build/image-composer-tool.
COPY --from=bin /src/build/image-composer-tool /src/build/live-installer build/
COPY image-composer-tool.yml ./
COPY config/ config/
COPY image-templates/ image-templates/
COPY scripts/preflight.sh scripts/preflight.sh
# Builds one template and copies the OS image into a directory mounted from
# the host. scripts/build-in-container.sh runs it from the host.
COPY scripts/container-build.sh scripts/container-build.sh
# preflight.sh reads the allowlist from this copy of shell.go.
COPY internal/utils/shell/shell.go internal/utils/shell/shell.go
RUN chmod +x scripts/preflight.sh scripts/container-build.sh

ENV PATH=/opt/ict/build:$PATH

# Every container start runs preflight.sh first. If an allowlisted program is
# missing, it lists what is missing and exits 1; otherwise it runs the
# container's command, which defaults to the CLI's help.
ENTRYPOINT ["/opt/ict/scripts/preflight.sh", "--command-map-file", "/opt/ict/internal/utils/shell/shell.go", "--"]
CMD ["image-composer-tool", "--help"]
