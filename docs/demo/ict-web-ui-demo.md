<!--
SPDX-FileCopyrightText: (C) 2026 Intel Corporation
SPDX-License-Identifier: Apache-2.0
-->

# ICT Web UI — Demo Script

**Audience:** Leadership / non-technical
**Length:** 3–5 minutes
**Goal:** Show that anyone can produce a validated, bootable OS image from a
browser — no CLI, no YAML, no Linux build expertise.

---

## The one-line pitch

> "Today, building a custom OS image means the command line, hand-edited YAML,
> and Linux build know-how. The Image Composer Tool web UI turns that into a
> few clicks — pick a validated configuration, compose, download. Self-service
> images for every team."

---

## Pre-demo setup (do this *before* you're in the room)

A real image build takes many minutes (it downloads hundreds of packages and
assembles a root filesystem). **Do not** wait for a live build in front of the
audience. Instead, seed the history with a completed build ahead of time and use
the "here's one I prepared earlier" move during the demo.

### 1. Build and start the server

From the repo root on the build host:

```bash
export PATH="$HOME/.local/node/bin:$PATH"          # ensure npm is on PATH
(cd web && npm ci && npm run build)                 # build the UI
rm -rf internal/webui/dist && cp -r web/dist internal/webui/dist
go build -o ./build/image-composer-tool ./cmd/image-composer-tool/
./build/image-composer-tool serve --sudo            # starts on 127.0.0.1:8080
```

### 2. Confirm the scoped sudo rule (needed for artifact download)

```bash
sudo tee /etc/sudoers.d/ict-webui > /dev/null <<'EOF'
user ALL=(root) NOPASSWD: /home/user/arodage/image-composer-tool/build/image-composer-tool build *
user ALL=(root) NOPASSWD: /usr/bin/cat /home/user/arodage/image-composer-tool/webui-workspace/builds/*
EOF
sudo chmod 440 /etc/sudoers.d/ict-webui
```

### 3. Seed the history with a completed build

Open the UI, go to **Basic**, pick a fully-backed vertical (**Retail Edge** or
**Robotics**), click **Compose Image**, and let it run to completion. This gives
you a **green** entry in the History sidebar with real downloadable artifacts to
show. Do this well ahead of time — it takes a while.

### 4. Open access for the room

- Same machine: browse to **http://localhost:8080**
- Remote: tunnel first — `ssh -L 8080:localhost:8080 user@<build-host>` — then
  browse to **http://localhost:8080**

### 5. Final pre-flight (30 seconds before you start)

- [ ] UI loads; Intel logo and three tabs (**Basic**, **Advanced**, **Compose Image**) visible
- [ ] History sidebar shows at least one **green** (completed) build
- [ ] Clicking that build shows its **Artifacts** table with a working **Download**
- [ ] Browser zoom set so the room can read it (Ctrl/Cmd + a couple times)

---

## The demo (≈4 minutes)

> Format below: **SAY** = what you narrate · **DO** = what you click.

### 1. Hook — the problem (30s)

**DO:** Start on the landing page (**Basic** tab).

**SAY:**
> "This is the Image Composer Tool. Normally, creating a custom Linux image for a
> device — a retail kiosk, a robot, an edge box — is a developer task: command
> line, config files, and a lot of Linux plumbing. We've made it a web
> experience. Watch how simple it gets."

### 2. Compose a configuration (60s)

**DO:** Open **Targeted Vertical** → pick **Retail Edge** (or **Robotics**).
Point out that **SKU**, **Platform**, and **Operating System** fill in
automatically.

**SAY:**
> "I choose what the image is *for* — here, retail edge. Everything else — the
> hardware SKU, the platform, the OS — is pre-configured from validated,
> tested combinations. There's no way to pick something that doesn't work
> together. No guesswork, no invalid builds."

### 3. Review before you commit (30s)

**DO:** Tick **Review Image Configuration**. The two-panel summary appears.

**SAY:**
> "Before building anything, I get a plain-English summary — what image this is,
> which architecture, how many packages, the disk layout. Full transparency,
> nothing hidden in a config file."

### 4. Compose + live progress (45s)

**DO:** Click **Compose Image**. The view switches to the **Compose Image** tab;
the nav shows a **pulsing yellow** "Compose in progress" indicator; log lines
stream in live.

**SAY:**
> "One click starts the build. It's running on the server right now — you can
> watch the live output, and the status indicator up here tracks it. A real
> build takes several minutes, so let me show you one that already finished."

### 5. "Here's one I prepared earlier" (60s)

**DO:** In the **History** sidebar, click the **green** (completed) build. Expand
**Compose details** briefly, then scroll to the **Artifacts** table and click
**Download** on the image (and the SBOM).

**SAY:**
> "Here's a completed image. Every compose is kept in history — you can see
> exactly what was built and reproduce it. And here's the payoff: the finished,
> bootable image, ready to download — plus a software bill of materials for
> security and compliance. From a few clicks to a deployable image."

### 6. Close (30s)

**SAY:**
> "So — anyone on the team, without touching a command line, can produce a
> validated, auditable, downloadable OS image in a couple of minutes. That's
> self-service image composition. And this is just the guided path; an advanced
> mode for power users is on the way."

---

## Backup / fallback notes

- **Live build errors on stage:** don't dwell on it — say "builds occasionally
  hit a flaky mirror; here's a completed one," and go straight to the green
  history build. The story doesn't depend on the live build finishing.
- **Tunnel/connection drops:** fall back to running the browser on the build
  host itself at `http://localhost:8080`.
- **"What's the Advanced tab?"** It's greyed out / "Coming soon" — a future
  free-form mode for power users. Not part of this demo.
- **"What's BKC?"** (if visible in the vertical list) — Best Known Configuration
  images used for platform validation. Mention only if asked; demo Retail or
  Robotics for a familiar story.

---

## Talking-points cheat sheet

- **No CLI, no YAML, no Linux expertise** — image composition as a web workflow.
- **Curated & validated** — only known-good combinations; invalid builds can't
  be selected.
- **Transparent** — review the full configuration before you build.
- **Auditable & reproducible** — every compose is kept in history with its exact
  configuration and command.
- **Deployable output** — a bootable image plus an SBOM, downloadable in a click.
