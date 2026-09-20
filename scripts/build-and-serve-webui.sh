#!/usr/bin/env bash
# SPDX-FileCopyrightText: (C) 2026 Intel Corporation
# SPDX-License-Identifier: Apache-2.0
#
# build-and-serve-webui.sh — build the web UI, embed it into the ICT binary,
# and launch `serve` on a local port. Follows web/README.md's "Quick start"
# steps 3-4 (frontend build -> stage into internal/webui/dist -> go build ->
# serve).
#
# Usage:
#   ./scripts/build-and-serve-webui.sh [--port PORT] [--host HOST] [--no-sudo]
#
# Options:
#   --port PORT   Listen port (default: 8080)
#   --host HOST   Bind address (default: 127.0.0.1)
#   --no-sudo     Skip --sudo (server will not be able to run real composes)
#
# Any server left running from a previous invocation (tracked via
# build/webui-serve.pid) is stopped right before the new one starts, so
# re-running this script doesn't fail with "address already in use".
#
# The server is launched in its own session via `setsid` so a build's
# cancellation signal can never reach this shell (see web/README.md's
# "Cancellation & security posture" note). Logs go to build/webui-serve.log;
# the PID is printed and written to build/webui-serve.pid.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

PORT="8080"
HOST="127.0.0.1"
USE_SUDO=true

while [[ $# -gt 0 ]]; do
    case "$1" in
        --port)
            PORT="$2"
            shift 2
            ;;
        --host)
            HOST="$2"
            shift 2
            ;;
        --no-sudo)
            USE_SUDO=false
            shift
            ;;
        -h|--help)
            sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *)
            echo "unknown argument: $1" >&2
            exit 1
            ;;
    esac
done

echo "==> Building frontend (web/dist)"
(cd web && npm ci && npm run build)

echo "==> Staging frontend for //go:embed (internal/webui/dist)"
rm -rf internal/webui/dist
cp -r web/dist internal/webui/dist

echo "==> Building image-composer-tool binary"
mkdir -p build
go build -o ./build/image-composer-tool ./cmd/image-composer-tool/

SERVE_ARGS=(serve --host "$HOST" --port "$PORT")
if $USE_SUDO; then
    SERVE_ARGS+=(--sudo)
fi

if [[ -f build/webui-serve.pid ]]; then
    OLD_PID="$(cat build/webui-serve.pid)"
    if [[ -n "$OLD_PID" ]] && kill -0 "$OLD_PID" 2>/dev/null; then
        echo "==> Stopping previous server (pid $OLD_PID)"
        kill "$OLD_PID"
        for _ in $(seq 1 20); do
            kill -0 "$OLD_PID" 2>/dev/null || break
            sleep 0.2
        done
        if kill -0 "$OLD_PID" 2>/dev/null; then
            echo "    still running after 4s, sending SIGKILL" >&2
            kill -9 "$OLD_PID" 2>/dev/null || true
        fi
    fi
    rm -f build/webui-serve.pid
fi

echo "==> Starting server: ./build/image-composer-tool ${SERVE_ARGS[*]}"
setsid ./build/image-composer-tool "${SERVE_ARGS[@]}" >build/webui-serve.log 2>&1 &
PID=$!
echo "$PID" >build/webui-serve.pid

sleep 1
if ! kill -0 "$PID" 2>/dev/null; then
    echo "server exited immediately — check build/webui-serve.log" >&2
    cat build/webui-serve.log >&2
    exit 1
fi

echo "==> Server running (pid $PID), logs at build/webui-serve.log"
echo "==> Open http://${HOST}:${PORT}"
echo "==> Stop with: kill \$(cat build/webui-serve.pid)"
