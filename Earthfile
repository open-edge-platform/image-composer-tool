VERSION 0.8

LOCALLY
ARG http_proxy=$(echo $http_proxy)
ARG https_proxy=$(echo $https_proxy)
ARG no_proxy=$(echo $no_proxy)
ARG HTTP_PROXY=$(echo $HTTP_PROXY)
ARG HTTPS_PROXY=$(echo $HTTPS_PROXY)
ARG NO_PROXY=$(echo $NO_PROXY)
ARG REGISTRY
ARG VERSION="__auto__"

# Use pre-built Go image that already has most tools
FROM ${REGISTRY}golang:1.24.1-bullseye

ENV http_proxy=$http_proxy
ENV https_proxy=$https_proxy
ENV no_proxy=$no_proxy
ENV HTTP_PROXY=$HTTP_PROXY
ENV HTTPS_PROXY=$HTTPS_PROXY
ENV NO_PROXY=$NO_PROXY

# Set Go environment variables (already set in golang image, but ensure they're correct)
ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/go"
ENV GOBIN="/go/bin"
ENV PATH="${GOBIN}:${PATH}"

# The golang image already includes:
# - wget, curl, git, build-essential
# - Most basic tools
# - Go 1.24.1

# Only install absolutely essential packages that might be missing
# Use --no-install-recommends and || true to continue even if some fail
RUN apt-get update && apt-get install -y --no-install-recommends \
    bc bash rpm mmdebstrap dosfstools sbsigntool xorriso grub-common cryptsetup \
    || echo "Some packages failed to install, continuing..."

RUN ln -s /bin/uname /usr/bin/uname

golang-base:
    # Create Go workspace
    RUN mkdir -p /go/src /go/bin /go/pkg && chmod -R 777 /go
    
    # Install golangci-lint
    RUN go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.7
    
    WORKDIR /work
    COPY go.mod .
    COPY go.sum .
    RUN go mod download # for caching
    COPY cmd/ ./cmd
    COPY internal/ ./internal
    COPY image-templates/ ./image-templates
    COPY scripts/ ./scripts
    RUN chmod +x ./scripts/*.sh || true

version-info:
    FROM +golang-base
    ARG VERSION="__auto__"
    ARG version="__auto__"
    # Copy .git directory to inspect tags for versioning metadata
    COPY .git .git
    RUN --no-cache RAW_VERSION="$VERSION" && \
        if [ -n "$version" ] && [ "$version" != "__auto__" ]; then \
            RAW_VERSION="$version"; \
        fi && \
        if [ -z "$RAW_VERSION" ] || [ "$RAW_VERSION" = "__auto__" ]; then \
            RAW_VERSION=$(git tag --sort=-creatordate | head -n1 2>/dev/null || echo "dev"); \
        fi && \
        ./scripts/sanitize-version.sh "$RAW_VERSION" > /tmp/version.txt
    SAVE ARTIFACT /tmp/version.txt

all:
    BUILD +build

clean-dist:
    LOCALLY
        RUN rm -rf dist
        RUN mkdir -p dist

build:
    FROM +golang-base
    ARG VERSION="__auto__"
    ARG version="__auto__"
    
    # Copy git metadata for commit stamping
    COPY .git .git
    BUILD +version-info --VERSION=$VERSION --version=$version
    # Reuse canonical version metadata emitted by +version-info
    COPY +version-info/version.txt /tmp/version.txt
    
    # Get git commit SHA
    RUN COMMIT_SHA=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown") && \
        echo "$COMMIT_SHA" > /tmp/commit_sha
    
    # Get build date in UTC
    RUN BUILD_DATE=$(date -u '+%Y-%m-%d') && \
        echo "$BUILD_DATE" > /tmp/build_date

    # Build with variables instead of cat substitution
    RUN VERSION=$(cat /tmp/version.txt) && \
        COMMIT_SHA=$(cat /tmp/commit_sha) && \
        BUILD_DATE=$(cat /tmp/build_date) && \
        CGO_ENABLED=0 GOARCH=amd64 GOOS=linux \
        go build -trimpath -buildmode=pie -o build/live-installer \
            -ldflags "-s -w \
                     -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.Version=$VERSION' \
                     -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.Toolname=Image-Composer-Tool' \
                     -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.Organization=Open Edge Platform' \
                     -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.BuildDate=$BUILD_DATE' \
                     -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.CommitSHA=$COMMIT_SHA'" \
            ./cmd/live-installer

    SAVE ARTIFACT build/live-installer AS LOCAL ./build/live-installer
    
    # Build with variables instead of cat substitution
    RUN VERSION=$(cat /tmp/version.txt) && \
        COMMIT_SHA=$(cat /tmp/commit_sha) && \
        BUILD_DATE=$(cat /tmp/build_date) && \
        CGO_ENABLED=0 GOARCH=amd64 GOOS=linux \
        go build -trimpath -buildmode=pie -o build/image-composer-tool \
            -ldflags "-s -w \
                     -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.Version=$VERSION' \
                     -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.Toolname=Image-Composer-Tool' \
                     -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.Organization=Open Edge Platform' \
                     -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.BuildDate=$BUILD_DATE' \
                     -X 'github.com/open-edge-platform/image-composer-tool/internal/config/version.CommitSHA=$COMMIT_SHA'" \
            ./cmd/image-composer-tool
            
    SAVE ARTIFACT build/image-composer-tool AS LOCAL ./build/image-composer-tool
    SAVE ARTIFACT /tmp/version.txt AS LOCAL ./build/image-composer-tool.version

lint:
    FROM +golang-base
    WORKDIR /work
    COPY . /work
    RUN --mount=type=cache,target=/root/.cache \
        golangci-lint run ./...

test:
    FROM +golang-base
    ARG COV_THRESHOLD=""
    ARG PRINT_TS=""
    ARG FAIL_ON_NO_TESTS=false
    
    # Copy the entire project (including scripts directory)
    COPY . /work
    
    # Make the coverage script executable
    RUN chmod +x /work/scripts/run_coverage_tests.sh
    
    # Run the comprehensive coverage tests using our script
    # Args: COV_THRESHOLD PRINT_TS FAIL_ON_NO_TESTS DEBUG
    # If COV_THRESHOLD not provided or empty, read from .coverage-threshold file
    RUN cd /work && \
        FILE_THRESH=$(cat .coverage-threshold 2>/dev/null | tr -d '[:space:]') && \
        FILE_THRESH="${FILE_THRESH:-65.0}" && \
        THRESHOLD="${COV_THRESHOLD:-$FILE_THRESH}" && \
        THRESHOLD="${THRESHOLD:-65.0}" && \
        echo "Using coverage threshold: ${THRESHOLD}%" && \
        ./scripts/run_coverage_tests.sh "${THRESHOLD}" "${PRINT_TS}" "${FAIL_ON_NO_TESTS}"
    
    # Save coverage artifacts locally
    SAVE ARTIFACT coverage.out AS LOCAL ./coverage.out
    SAVE ARTIFACT coverage_report.txt AS LOCAL ./coverage_report.txt

test-debug:
    FROM +golang-base
    ARG COV_THRESHOLD=""
    ARG PRINT_TS=""
    ARG FAIL_ON_NO_TESTS=false
    
    # Copy the entire project (including scripts directory)
    COPY . /work
    
    # Make the coverage script executable
    RUN chmod +x /work/scripts/run_coverage_tests.sh
    
    # Run the coverage tests with debug output (keeps temp files for inspection)
    # Args: COV_THRESHOLD PRINT_TS FAIL_ON_NO_TESTS DEBUG
    # If COV_THRESHOLD not provided or empty, read from .coverage-threshold file
    RUN cd /work && \
        FILE_THRESH=$(cat .coverage-threshold 2>/dev/null | tr -d '[:space:]') && \
        FILE_THRESH="${FILE_THRESH:-65.0}" && \
        THRESHOLD="${COV_THRESHOLD:-$FILE_THRESH}" && \
        THRESHOLD="${THRESHOLD:-65.0}" && \
        echo "Using coverage threshold: ${THRESHOLD}%" && \
        ./scripts/run_coverage_tests.sh "${THRESHOLD}" "${PRINT_TS}" "${FAIL_ON_NO_TESTS}" "true"
    
    # Save coverage artifacts locally
    SAVE ARTIFACT coverage.out AS LOCAL ./coverage.out
    SAVE ARTIFACT coverage_report.txt AS LOCAL ./coverage_report.txt

test-quick:
    FROM +golang-base
    RUN go test ./...

deb:
    FROM debian:bookworm-slim
    ARG ARCH=amd64
    ARG VERSION="__auto__"
    ARG version="__auto__"

    BUILD +clean-dist

    WORKDIR /pkg
    BUILD +version-info --VERSION=$VERSION --version=$version
    COPY +version-info/version.txt /tmp/version.txt
    RUN cp /tmp/version.txt /tmp/pkg_version
    
    # Create directory structure following FHS (Filesystem Hierarchy Standard)
    RUN mkdir -p usr/local/bin \
                 etc/ict/config \
                 usr/share/ict/examples \
                 usr/share/doc/ict \
                 var/cache/ict \
                 DEBIAN
    
    # Copy the built binary from the build target
    COPY +build/image-composer-tool usr/local/bin/image-composer-tool
    
    # Make the binary executable
    RUN chmod +x usr/local/bin/image-composer-tool
    
    # Create default global configuration with system paths (user-editable)
    # Note: Must be named config.yml to match the default search paths in the code
    RUN echo "# ICT - Global Configuration" > etc/ict/config.yml && \
        echo "# This file contains tool-level settings that apply across all image builds." >> etc/ict/config.yml && \
        echo "# Image-specific parameters should be defined in the image specification." >> etc/ict/config.yml && \
        echo "" >> etc/ict/config.yml && \
        echo "# Core tool settings" >> etc/ict/config.yml && \
        echo "workers: 8" >> etc/ict/config.yml && \
        echo "# Number of concurrent download workers (1-100, default: 8)" >> etc/ict/config.yml && \
        echo "" >> etc/ict/config.yml && \
        echo "config_dir: \"/etc/image-composer-tool/config\"" >> etc/ict/config.yml && \
        echo "# Directory containing configuration files for different target OSs" >> etc/ict/config.yml && \
        echo "" >> etc/ict/config.yml && \
        echo "cache_dir: \"/var/cache/image-composer-tool\"" >> etc/ict/config.yml && \
        echo "# Package cache directory where downloaded RPMs/DEBs are stored" >> etc/ict/config.yml && \
        echo "" >> etc/ict/config.yml && \
        echo "work_dir: \"/tmp/image-composer-tool\"" >> etc/ict/config.yml && \
        echo "# Working directory for build operations and image assembly" >> etc/ict/config.yml && \
        echo "" >> etc/ict/config.yml && \
        echo "temp_dir: \"/tmp\"" >> etc/ict/config.yml && \
        echo "# Temporary directory for short-lived files" >> etc/ict/config.yml && \
        echo "" >> etc/ict/config.yml && \
        echo "# Logging configuration" >> etc/ict/config.yml && \
        echo "logging:" >> etc/ict/config.yml && \
        echo "  level: \"info\"" >> etc/ict/config.yml && \
        echo "  # Log verbosity level: debug, info, warn, error" >> etc/ict/config.yml
    
    # Copy OS variant configuration files (user-editable)
    COPY config etc/ict/config
    
    # Copy image templates as examples (read-only, for reference)
    COPY image-templates usr/share/ict/examples
    
    # Copy documentation
    COPY README.md usr/share/doc/ict/
    COPY LICENSE usr/share/doc/ict/
    COPY docs/architecture/ict-cli-specification.md usr/share/doc/ict/
    
    # Create the DEBIAN control file with proper metadata
    RUN VERSION=$(cat /tmp/pkg_version) && \
        echo "Package: ict" > DEBIAN/control && \
        echo "Version: ${VERSION}" >> DEBIAN/control && \
        echo "Section: utils" >> DEBIAN/control && \
        echo "Priority: optional" >> DEBIAN/control && \
        echo "Architecture: ${ARCH}" >> DEBIAN/control && \
        echo "Maintainer: Intel Edge Software Team <edge.platform@intel.com>" >> DEBIAN/control && \
        echo "Depends: bash, coreutils, unzip, dosfstools, xorriso, grub-common" >> DEBIAN/control && \
        echo "Recommends: mmdebstrap, debootstrap" >> DEBIAN/control && \
        echo "License: MIT" >> DEBIAN/control && \
        echo "Description: ICT (OIC)" >> DEBIAN/control && \
        echo " OIC enables users to compose custom bootable OS images based on a" >> DEBIAN/control && \
        echo " user-provided template that specifies package lists, configurations," >> DEBIAN/control && \
        echo " and output formats for supported distributions." >> DEBIAN/control
    
    # Build the debian package and stage in a stable location
    RUN VERSION=$(cat /tmp/pkg_version) && \
        mkdir -p /tmp/dist && \
        dpkg-deb --build . /tmp/dist/ict_${VERSION}_${ARCH}.deb

    # Save the debian package artifact and resolved version information to dist/
    RUN VERSION=$(cat /tmp/pkg_version) && cp /tmp/pkg_version /tmp/dist/image-composer-tool.version
    SAVE ARTIFACT /tmp/dist/ict_*_${ARCH}.deb AS LOCAL dist/
    SAVE ARTIFACT /tmp/dist/image-composer-tool.version AS LOCAL dist/image-composer-tool.version
