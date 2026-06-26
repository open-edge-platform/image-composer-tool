# Ubuntu 24 post-boot agent install scripts

Post-boot installers for Intel and NVIDIA agent stacks on **Ubuntu 22.04 / 24.04**.
They are **not** executed during image compose unless you add separate build-time actions.

| Script | Path on image | Log | Completed-step stamps |
|--------|---------------|-----|------------------------|
| Intel | `/opt/agent/agent-install.sh` | `/var/log/agent-install.log` | `/var/lib/agent-install/done/` |
| NVIDIA | `/opt/agent/agent-install-nvidia.sh` | `/var/log/agent-install-nvidia.log` | `/var/lib/agent-install-nvidia/done/` |

Wire scripts via `systemConfig.additionalFiles` (see sample
`image-templates/ubuntu24-x86_64-agent.yml` when present in the repo).

**Source of truth (keep this doc in sync when scripts change):**

- `config/osv/ubuntu/ubuntu24/imageconfigs/additionalfiles/agent-install.sh`
- `config/osv/ubuntu/ubuntu24/imageconfigs/additionalfiles/agent-install-nvidia.sh`

Run **one** GPU stack per node unless you deliberately plan overlapping drivers.

---

## Table of contents

- [Rerunnable behavior](#rerunnable-behavior)
- [What gets installed](#what-gets-installed)
- [Intel apt (OpenVINO / oneAPI / DL Streamer)](#intel-apt-openvino--oneapi--dl-streamer)
- [Hermes (automation-friendly)](#hermes-automation-friendly)
- [OpenClaw / SuperClaw / NemoClaw](#openclaw--superclaw--nemoclaw)
- [Python agent venv (pip)](#python-agent-venv-pip)
- [Environment variables](#environment-variables)
- [Logging](#logging)
- [Troubleshooting](#troubleshooting)
- [Related](#related)

---

## Rerunnable behavior

| Kind | Every run (`sudo ./agent-install.sh`) | Skip with `FORCE=0` |
|------|----------------------------------------|---------------------|
| `apt-get update` / `install` | Yes | — |
| Intel repo lists | Yes if invalid; else re-run (default FORCE=1) | Skip if OK + stamp |
| Hermes, OpenClaw, SuperClaw ctl, pip venv | Yes (default **FORCE=1**) | Yes (stamps honored) |

```bash
sudo /opt/agent/agent-install.sh
sudo FORCE=0 /opt/agent/agent-install.sh   # faster: skip completed stamp steps
```

Script revision is logged at start (`rev=` in `agent-install.sh` header).

---

## What gets installed

### At a glance

| Layer | Intel (`agent-install.sh`) | NVIDIA (`agent-install-nvidia.sh`) |
|-------|----------------------------|-------------------------------------|
| Intel OpenVINO / oneAPI / DL Streamer | Yes (apt, policy below) | No |
| NVIDIA driver / CUDA / cuDNN / NCCL | No | Yes (toggles, defaults on) |
| NVIDIA Container Toolkit | No | Default on |
| Hermes | Default on (non-interactive) | Default on (same) |
| OpenClaw | Default on (`--no-onboard`) | Default on; see NemoClaw |
| SuperClaw | Optional edge `superclaw-ctl` only | No |
| NemoClaw | No | Optional (`INSTALL_NEMOCLAW=1`) |
| Pip frameworks (venv) | Yes | Yes (separate venv path) |
| PyTorch (CUDA wheels) | No | Default on |
| vLLM | No | Optional (`INSTALL_VLLM=1`) |

### Not installed by these scripts

- TensorRT-LLM, SGLang (use NGC / vendor docs or enable vLLM on NVIDIA path only)
- Full SuperClaw Windows desktop (Intel path can install `superclaw-ctl` binary only)
- Hermes / OpenClaw **configuration** (API keys, gateway, onboarding) — manual after install
- Image compose-time packages (unless your template lists them separately)

---

## Intel apt (OpenVINO / oneAPI / DL Streamer)

Intel deb **suites** are **`ubuntu22`** / **`ubuntu24`**, not **`jammy`** / **`noble`**.

The script detects 22.04 vs 24.04, or set:

```bash
export INTEL_UBUNTU_SUITE=ubuntu24   # or ubuntu22
```

OpenVINO apt base:

```text
https://apt.repos.intel.com/openvino/${OPENVINO_REPO_TRACK}   # default OPENVINO_REPO_TRACK=2025
```

Do not duplicate those URLs in the image template `packageRepositories` and in this script —
apt fails with `Conflicting values set for option Signed-By`. Sample
`ubuntu24-x86_64-agent.yml` uses the script only.

DL Streamer:

```text
https://apt.repos.intel.com/edgeai/dlstreamer/${INTEL_UBUNTU_SUITE}
```

### Package policy

| `INTEL_PACKAGE_POLICY` | Behavior |
|------------------------|----------|
| `latest` (default) | After `apt-get update`, install **newest** matching `openvino_*`, `intel-oneapi-runtime-*`, `intel-dlstreamer_*` in cache |
| `pinned` | Require exact names in `INTEL_PINNED_PACKAGES` in the script |

Base apt set also includes Level Zero GPU libs, optional NPU / `xpu-smi` (WARN + skip if absent), `podman`.

Package discovery uses `apt-cache pkgnames` and Intel `_Packages` list fallbacks (not `apt-cache search '^openvino_'` alone).

---

## Hermes (automation-friendly)

**Goal:** Hermes **binaries and dependencies** on the system; **no** interactive setup in this script.

Defaults:

```text
HERMES_INSTALL_FLAGS=--skip-setup --non-interactive --skip-browser
```

`--skip-browser` skips Playwright/Chromium only. If no suitable Node.js is on the host, Hermes
still downloads and extracts a managed Node LTS tarball (needs **`xz-utils`** for `.tar.xz`;
both install scripts install `git` and `xz-utils` via apt before the Hermes step).

The wrapper also sets `DEBIAN_FRONTEND=noninteractive`, `NEEDRESTART_MODE=a`, and runs the
Hermes installer with **stdin from `/dev/null`** so automation and `sudo` from SSH do not use
`/dev/tty` prompts.

Stamp id: **`hermes-agent-v2`**. User steps after install:

```bash
hermes setup
# optional:
hermes gateway install
```

| Variable | Purpose |
|----------|---------|
| `INSTALL_HERMES=0` | Skip Hermes |
| `HERMES_INSTALL_AS_USER=<login>` | Install under that user's home (needs passwordless sudo for automation) |
| `HERMES_INSTALL_FLAGS` | Override flags passed to `install.sh` |

Root install (default when script runs as root): code under `/usr/local/lib/hermes-agent`,
`hermes` on PATH; data under `/root/.hermes`.

---

## OpenClaw / SuperClaw / NemoClaw

| Component | Intel | NVIDIA |
|-----------|-------|--------|
| OpenClaw | `openclaw.ai/install.sh --no-onboard` | Same |
| SuperClaw | `INSTALL_SUPERCLAW_CTL=1` → `superclaw-ctl` tarball | — |
| NemoClaw | — | `INSTALL_NEMOCLAW=1` → `nemoclaw.sh` + `docker.io`; host OpenClaw skipped unless `INSTALL_HOST_OPENCLAW_WITH_NEMOCLAW=1` |

User runs **`openclaw onboard`** when ready. NemoClaw onboarding may still need provider env
(see [NVIDIA NemoClaw docs](https://docs.nvidia.com/nemoclaw/latest/)).

---

## Python agent venv (pip)

| Script | Venv path | Packages (pip) |
|--------|-----------|----------------|
| Intel | `/opt/agent/venv` | autogen-agentchat, crewai, langgraph, openai, openai-agents |
| NVIDIA | `/opt/agent/venv-nvidia` | Same + optional PyTorch (cu124) and vLLM |

Venv creation re-runs each invocation by default (`FORCE=1`); use `FORCE=0` to skip if stamp exists.

---

## Environment variables

### Intel (`agent-install.sh`)

| Variable | Default | Notes |
|----------|---------|-------|
| `INSTALL_HERMES` | `1` | |
| `INSTALL_OPENCLAW` | `1` | |
| `INSTALL_SUPERCLAW_CTL` | `0` | |
| `INTEL_PACKAGE_POLICY` | `latest` | or `pinned` |
| `INTEL_UBUNTU_SUITE` | auto | `ubuntu22` / `ubuntu24` |
| `OPENVINO_REPO_TRACK` | `2025` | Intel openvino apt path segment |
| `INTEL_APT_ARCH` | `amd64` | |
| `HERMES_INSTALL_FLAGS` | see Hermes section | |
| `HERMES_INSTALL_AS_USER` | empty | |
| `FORCE` | `1` | `0` = skip stamped run-once steps (Hermes, venv, …) |

### NVIDIA (`agent-install-nvidia.sh`)

| Variable | Default | Notes |
|----------|---------|-------|
| `NVIDIA_DRIVER_PACKAGE` | `nvidia-driver-550-open` | |
| `CUDA_META_PACKAGE` | `cuda-toolkit-12-8` | |
| `INSTALL_CUDA_TOOLKIT` / `CUDNN` / `NCCL` / `CONTAINER_TOOLKIT` | `1` | |
| `INSTALL_PYTORCH_CUDA` | `1` | |
| `INSTALL_VLLM` | `0` | |
| `INSTALL_NEMOCLAW` | `0` | |
| `INSTALL_HERMES` / `INSTALL_OPENCLAW` | `1` | Same Hermes flags as Intel script |
| `FORCE` | `1` | `0` = skip stamped steps |

---

## Logging

- Every `log()` line goes to **stdout** and is **appended** to the script log file.
- Intel script logs **`Installed: <package> <version>`** for OpenVINO / oneAPI / DL Streamer after apt.
- **Not** fully captured: verbose output from Hermes, OpenClaw, or apt (only script milestones).

```bash
sudo tail -100 /var/log/agent-install.log
sudo grep -E 'ERROR|WARN|Installed:|complete' /var/log/agent-install.log
ls -la /var/lib/agent-install/done/
```

---

## Troubleshooting

| Symptom | Likely cause | Action |
|---------|--------------|--------|
| Intel 404 on `noble` in apt | Wrong suite in old lists | Use current script; remove obsolete stamp `intel-apt-repos`; or `FORCE=1` |
| `Skip step 'intel-apt-repos'` (old id) | Pre-v2 script / stamp | Copy current script; `FORCE=1` |
| No OpenVINO packages after update | Repos OK but wrong discovery | v6+ script; check `intel-openvino.list` shows `ubuntu24` suite |
| `Conflicting values set for option Signed-By` (openvino ubuntu24) | ICT `package-repositories.list` + script `intel-*.list` | v7+ script strips ICT Intel lines; or remove `packageRepositories` from template |
| Hermes prompts | Old install line or missing flags | Use `hermes-agent-v2` step; default `HERMES_INSTALL_FLAGS`; `FORCE=1` |
| `tar (child): xz: Cannot exec` during Hermes | Minimal image without `xz-utils` | Use current script (v8+); or `apt install -y xz-utils` and re-run with `FORCE=1` |
| Packages missing on 22.04 | Pin names / repo track | `INTEL_UBUNTU_SUITE=ubuntu22`; `INTEL_PACKAGE_POLICY=latest` |
| Run both Intel + NVIDIA scripts | Two stacks on one node | Use one script per machine |

Verify Intel lists:

```bash
cat /etc/apt/sources.list.d/intel-openvino.list
# expect: ... openvino/2025 ubuntu24 main   (on 24.04)
```

Verify Hermes non-interactive install:

```bash
grep hermes-agent-v2 /var/lib/agent-install/done/
which hermes
```

---

## Related

- [Configure additional build actions](configure-additional-actions-for-build.md) — chroot/compose-time commands
- [Image templates](../architecture/image-composer-tool-templates.md) — YAML structure
- [Multiple package repositories](configure-multiple-package-repositories.md) — Intel repos in templates (suite names)
