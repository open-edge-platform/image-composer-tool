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

> To run the UI without installing Go, Node.js, the programs a build runs, or
> sudoers rules, run `docker compose up --build` from the repository root
> instead. The container runs privileged, which makes it root-equivalent on the
> host; see the
> [Web UI Quick Start](../docs/user-guide/get-started/web-ui-quick-start.md).

### 1. Build the binary first

The sudoers rules below reference the **absolute path** of the ICT binary, so
build it before granting them (`sudo` matches the rule literally — a relative or
wrong path silently falls through to a password prompt and every privileged
action fails). See step 2 for the full frontend+binary build; the short version:

```bash
go build -o ./build/image-composer-tool ./cmd/image-composer-tool/
```

> **ISO images require the `live-installer` binary.** Build it before starting
> an ISO build from the UI, otherwise ISO composes fail immediately with a
> missing-prerequisite error:
>
> ```bash
> go build -buildmode=pie -o ./build/live-installer ./cmd/live-installer
> ```
>
> If you use `earthly +build`, both binaries are built automatically.

### 2. Grant scoped, passwordless sudo rules (automated)

The server performs exactly three privileged operations via `sudo -n`: **build**
(chroot/mount), **kill** (SIGTERM a build's process group to cancel it), and
**cat** (stream root-owned artifacts back to the browser). It needs one scoped
NOPASSWD rule per operation — and nothing more. **Do not hand-write these**; a
wrong path or a missing rule is the most common reason cancellation and downloads
fail on a fresh host. Instead, let the binary generate the exact rules for this
machine and install them with validation:

```bash
# One-shot: generate for this user + binary + workspace, visudo-validate, install.
sudo ./scripts/install-sudoers.sh --ict-binary "$(pwd)/build/image-composer-tool"

# Verify the rules resolve passwordless:
sudo -l -U "$(whoami)" | grep image-composer-tool
```

Or generate and install by hand if you prefer to review first:

```bash
./build/image-composer-tool serve --print-sudoers \
  --ict-binary "$(pwd)/build/image-composer-tool" \
  | sudo tee /etc/sudoers.d/image-composer-tool-webui
sudo chmod 440 /etc/sudoers.d/image-composer-tool-webui
sudo visudo -cf /etc/sudoers.d/image-composer-tool-webui   # validate
```

The generator resolves the current user, the absolute binary path, and the
`kill`/`cat` helper paths the way `sudo` will at runtime (respecting
`secure_path`), and scopes the `cat` rule to the `<work-dir>/builds` subtree
(where all artifacts live) — so the service user can read build artifacts but not
arbitrary root-owned files. Pass
`--work-dir` to both the generator and `serve` if you don't use the default
`webui-workspace`.

<details>
<summary>Equivalent rules, for reference (what the generator emits)</summary>

```
Cmnd_Alias ICT_BUILD = /abs/path/image-composer-tool build *
Defaults!ICT_BUILD env_keep += "NO_PROXY no_proxy HTTP_PROXY http_proxy HTTPS_PROXY https_proxy FTP_PROXY ftp_proxy"
<svc-user> ALL=(root) NOPASSWD: ICT_BUILD
<svc-user> ALL=(root) NOPASSWD: /usr/bin/kill -TERM -[0-9]*
<svc-user> ALL=(root) NOPASSWD: /usr/bin/cat /abs/path/webui-workspace/builds/*
```

The build rule is named via a `Cmnd_Alias` so the scoped `env_keep` (needed to
forward the server's own proxy environment into privileged builds, see the
release notes) applies to that command only — not to `kill` or `cat`, and not
sudo's default `env_keep` for anything else.

Every build the web UI runs also picks up `HTTP_PROXY`/`HTTPS_PROXY`/
`FTP_PROXY`/`NO_PROXY` (or their lowercase forms) from the `serve` process's
own environment and forwards them as `--http-proxy`/`--https-proxy`/
`--ftp-proxy`/`--no-proxy`, so the built image's `systemConfig.proxy` matches
whatever proxy `serve` itself runs behind — without editing any template.
These flags **override** a template's own `systemConfig.proxy` block when
set; start `serve` with a clean environment (no proxy vars) if a template's
explicit proxy configuration must be preserved as-is.

</details>

> **Cancellation & security posture.** The build runs as a root-owned process
> group (so ICT can tear down its own mounts and loop devices on SIGTERM). The
> server is non-root and cannot signal that group directly across the `sudo`
> boundary, so **Cancel** delivers the signal as root via `sudo -n kill -TERM
> -<pgid>`. The kill rule above authorizes that. Omit it and cancellation fails
> with a *cancellation-failure* (the signal can't be delivered); the UI surfaces
> that distinctly from a *cleanup-failure* (ICT ran but left residue).
>
> **Read this before granting the kill rule.** The target process group id isn't
> known until a build starts, so sudoers cannot constrain *which* group is
> signalled — `-[0-9]*` matches any pgid. Understand what you are granting the
> service user:
>
> - `kill -TERM -1` as root — SIGTERM to **every process on the host**, i.e. a
>   full-system shutdown-equivalent, available to anyone who can run commands as
>   the service user.
> - SIGTERM to any other process group on the box, including the server's own.
>
> The signal is restricted to `-TERM` (no `-KILL`, no arbitrary signal), and the
> service is expected to run as a dedicated, non-login user on a build host — but
> this is a real privilege escalation from "may run one build command" and should
> be accepted deliberately, not by default. If that is too broad for your
> environment, either run the server as root on an isolated build host (no `kill`
> rule needed — the `--sudo` path is skipped and the group is signalled directly)
> or omit the rule and accept that Cancel reports a cancellation-failure.
>
> **Run `serve` in its own session, not directly in your interactive shell.** The
> server hardens against signalling its own process group (a cancel can never take
> down the server or your login session), but the cleanest isolation is to give
> the server its own session so a stray group signal can't reach your shell at
> all. Launch it under `setsid` (`setsid ./build/image-composer-tool serve --sudo
> &`) or, better, a systemd unit — not as a foreground job in the SSH session you
> also use to run `kill`/cancel commands.

> **The artifact column always serves the complete, merged template — from the
> moment a build starts, through every outcome.** The server archives the
> fully-resolved template (the Advanced-mode selections merged into their
> curated parent) before the build even begins running, so the download never
> serves the raw per-build override delta — not while composing, and not for
> success, ordinary failure, cancellation, or a stalled cancel. If a build never
> exits within 5 minutes of a cancel request, the server re-archives at that
> point too (without deleting the delta on disk, in case the unresponsive
> process is still reading it) so the UI still has something usable even though
> the underlying process may still be winding down in the background.
>
> **Container/orchestrator shutdown cancels an in-flight build and waits for its
> teardown.** On `SIGTERM` (e.g. `docker stop`, a Kubernetes pod eviction, or a
> rolling update) `serve` stops admitting new builds, stops accepting new
> connections, lets in-flight HTTP/SSE requests drain for up to 10s, then
> **cancels any build still running** and waits up to 20s for it to reach a
> terminal state and archive its merged template before the process exits.
>
> New builds are refused as soon as shutdown begins — a compose submitted from
> this point on gets `503 SERVER_SHUTTING_DOWN` rather than being started on a
> process that is about to exit and could not tear it down. Point the client at
> the replacement instance and retry there.
>
> The cancel is the point: a real compose takes minutes, so it would never
> finish inside that window, and the build child — which runs in its own process
> group and knows nothing about the server's shutdown — would otherwise keep
> working until the orchestrator's `SIGKILL` cut it down mid-chroot, leaving
> **mounted filesystems and attached loop devices behind on the host**.
> Cancelling spends the window on ICT's own teardown instead. A build that is
> still tearing down when the window closes is recorded as `cancelling`, the
> same way a build orphaned by a restart is — and that record, including any
> reason the cancel signal failed, is written to disk before the process exits,
> so it survives into the next instance's compose history.
>
> This is best-effort: it only has as much time as the deployment's own
> termination grace period allows (`terminationGracePeriodSeconds` in
> Kubernetes, `docker stop -t`) before a `SIGKILL` cuts it short regardless.
> Set that grace period comfortably above 35s if you want teardown and archiving
> to reliably survive a rolling deploy.

### 3. Build the frontend, embed it, and rebuild the binary

Step 1 built a plain binary for the sudoers path. For the actual server you need
the frontend embedded (`//go:embed internal/webui/dist`):

```bash
export PATH="$HOME/.local/node/bin:$PATH"          # ensure npm is on PATH
(cd web && npm ci && npm run build)                 # build the UI → web/dist/
rm -rf internal/webui/dist && cp -r web/dist internal/webui/dist  # stage for //go:embed
go build -o ./build/image-composer-tool ./cmd/image-composer-tool/
```

### 4. Start the server

```bash
setsid ./build/image-composer-tool serve --sudo &   # own session; see the posture note above
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

#### Changing what the Basic tab offers

The Basic tab's selections come from `internal/api/service/data/manifest.yaml`,
which is embedded into the binary at build time — editing it needs a `go build`
to take effect. To iterate without rebuilding, copy it somewhere and pass
`--manifest`:

```bash
cp internal/api/service/data/manifest.yaml /tmp/my-manifest.yaml
./build/image-composer-tool serve --sudo --manifest /tmp/my-manifest.yaml
```

That file is read from disk on startup, so editing it and restarting `serve` is
enough. Template paths inside a manifest are relative to `--templates-dir`
(default `image-templates/`), so both flags must point at the same tree.

### 5. Open the UI

- **Local machine:** browse to <http://localhost:8080>.
- **Remote build host (port forwarding):** the server listens only on the host's
  loopback, so forward the port over SSH from your workstation:

  ```bash
  ssh -L 8080:localhost:8080 <user>@<build-host>
  ```

  Keep that SSH session open, then browse to <http://localhost:8080> on your
  workstation. (Change the left-hand `8080` if that port is busy locally, e.g.
  `-L 9090:localhost:8080` → browse to `http://localhost:9090`.)

> Redeploying after a UI change: repeat step 3 (rebuild + re-stage + `go build`),
> restart `serve`, and hard-refresh the browser (Ctrl/Cmd+Shift+R) to bypass the
> cached bundle.

### Package metadata caching (and the 404 it used to cause)

Each build downloads packages into its own `--cache-dir`, but the **package index**
is shared across builds, under `<temp_dir>/builds/<repo>_<arch>_<component>/`
(`temp_dir` defaults to `./tmp`). That index is re-validated against the
repository's `Release` file on every online build, so a compose that fails with a
lone 404 on one `.deb` — the symptom of an index cached before a security update —
should recover on its own. You should not need to clear `tmp/builds/*` by hand.

> `--no-cache` isolates `--cache-dir`/`--work-dir` only; it does not relocate the
> shared metadata directory above.

See [Repository Metadata Caching](../docs/user-guide/architecture/image-composer-tool-caching.md#repository-metadata-caching)
for the validation rules and the offline fallback.

---

## Development (hot-reload)

For iterating on the frontend, run the Vite dev server (hot module reload) and
the Go backend separately. Vite proxies `/api/v1` to the backend on `:8080`.

```bash
# Terminal 1 — backend API. Start it FROM THE REPO ROOT: serve resolves
# --templates-dir and the config file relative to its working directory, so
# from anywhere else every selection fails with "matched template file not
# found on disk".
go run ./cmd/image-composer-tool serve --sudo

# Terminal 2 — Vite dev server
cd web && npm ci && npm run dev
# UI with hot-reload at http://localhost:5174
```

Port-forward `5174` (and `8080`) the same way if the backend is on a remote host.
The dev-server port is pinned to `5174` with `strictPort` in `vite.config.ts`, so
it fails fast rather than hopping to another port and breaking the forward.

If the UI shows *"Failed to load configuration: 500 Internal Server Error"*, the
backend is not up: Vite reports a refused proxy connection as a 500. Check
terminal 1, not the browser.

### Package search finds nothing in the Intel repositories

On a network whose only route out is an HTTP proxy, a `no_proxy` containing
`.intel.com` makes the package picker skip several repositories and take about
30 seconds to say so. The catalog's Intel-hosted repositories —
`eci.intel.com`, `amrdocs.intel.com`, `apt.repos.intel.com`,
`librealsense.intel.com` — are served from public CDNs rather than the internal
network, so a blanket `.intel.com` bypass sends them direct, where there is no
route, and each one waits out its full connect timeout before failing.

Drop the blanket entry, keeping genuinely internal hosts direct:

```bash
export no_proxy="localhost,127.0.0.0/8,192.168.0.0/16,172.16.0.0/12,10.0.0.0/8,gnai.intel.com"
export NO_PROXY="$no_proxy"
sudo -E ./build/image-composer-tool serve --sudo   # -E keeps the environment
```

`sudo` drops the environment without `-E`, which is the usual reason an
otherwise-correct proxy setup still fails.

## Type checking

```bash
cd web && npx tsc --noEmit
```
