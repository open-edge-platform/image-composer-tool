<!--
SPDX-FileCopyrightText: (C) 2026 Intel Corporation
SPDX-License-Identifier: Apache-2.0
-->

# ICT Web UI

React 19 + TypeScript + Vite frontend for the Image Composer Tool (ICT) web UI.
It lets you compose validated Linux images from the browser — pick a vertical,
review the configuration, compose, and download the image + SBOM — with no CLI
or YAML editing.

The UI is served as a **single Go binary**: the built frontend is embedded into
the `image-composer-tool` binary via `//go:embed`, and the `serve` subcommand
hosts both the static UI and the JSON/SSE API on one port.

---

## Quick start (build + run the single binary)

Real image composition needs root (chroot/mount), so the server runs builds
under `sudo`. Run everything from the **repository root**.

### 1. Grant a scoped, passwordless sudo rule

The server invokes the ICT binary (and `cat`, to stream root-owned artifacts) via
`sudo -n`. Grant a NOPASSWD rule scoped to exactly those commands — do **not**
give the service blanket sudo. The path must be the **absolute** path to the
binary you build in step 2 (the server resolves it to an absolute path, and
`sudo` matches the rule literally).

```bash
echo "$(whoami) ALL=(root) NOPASSWD: $(pwd)/build/image-composer-tool build *" \
  | sudo tee /etc/sudoers.d/ict-webui
echo "$(whoami) ALL=(root) NOPASSWD: /usr/bin/cat $(pwd)/webui-workspace/builds/*" \
  | sudo tee -a /etc/sudoers.d/ict-webui
sudo chmod 440 /etc/sudoers.d/ict-webui

# Verify (should print "sudo OK"):
sudo -n "$(pwd)/build/image-composer-tool" build --help >/dev/null && echo "sudo OK"
```

### 2. Build the frontend, embed it, and build the binary

```bash
export PATH="$HOME/.local/node/bin:$PATH"          # ensure npm is on PATH
(cd web && npm ci && npm run build)                 # build the UI → web/dist/
rm -rf internal/webui/dist && cp -r web/dist internal/webui/dist  # stage for //go:embed
go build -o ./build/image-composer-tool ./cmd/image-composer-tool/
```

### 3. Start the server

```bash
./build/image-composer-tool serve --sudo
# INFO  ICT web UI API listening on 127.0.0.1:8080
```

The server binds `127.0.0.1` by default (localhost only). Useful flags:

| Flag | Default | Purpose |
| --- | --- | --- |
| `--sudo` | off | Run composes under `sudo -n` (required for real builds) |
| `--host` | `127.0.0.1` | Bind address (`0.0.0.0` exposes on all interfaces — not recommended) |
| `--port`, `-p` | `8080` | Listen port |
| `--ict-binary` | auto | ICT binary to invoke; auto-detects `./build/…`, `./…`, then `$PATH` |
| `--manifest` | embedded | Path to a manifest YAML to read from disk (live-editable, no rebuild) |
| `--work-dir` | `webui-workspace` | Base dir for per-compose work/output |

### 4. Open the UI

- **Local machine:** browse to <http://localhost:8080>.
- **Remote build host (port forwarding):** the server listens only on the host's
  loopback, so forward the port over SSH from your workstation:

  ```bash
  ssh -L 8080:localhost:8080 <user>@<build-host>
  ```

  Keep that SSH session open, then browse to <http://localhost:8080> on your
  workstation. (Change the left-hand `8080` if that port is busy locally, e.g.
  `-L 9090:localhost:8080` → browse to `http://localhost:9090`.)

> Redeploying after a UI change: repeat step 2 (rebuild + re-stage + `go build`),
> restart `serve`, and hard-refresh the browser (Ctrl/Cmd+Shift+R) to bypass the
> cached bundle.

---

## Development (hot-reload)

For iterating on the frontend, run the Vite dev server (hot module reload) and
the Go backend separately. Vite proxies `/api/v1` to the backend on `:8080`.

```bash
# Terminal 1 — backend API
go run ./cmd/image-composer-tool serve --sudo

# Terminal 2 — Vite dev server
cd web && npm ci && npm run dev
# UI with hot-reload at http://localhost:5173
```

Port-forward `5173` (and `8080`) the same way if the backend is on a remote host.

## Type checking

```bash
cd web && npx tsc --noEmit
```
