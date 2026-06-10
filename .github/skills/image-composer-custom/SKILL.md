---
name: image-composer-custom
description: "Customize image-composer templates with extra packages and repos, saving results to user-templates/ so canonical templates stay pristine. Use when users ask for package/repo injection without editing image-templates/."
argument-hint: "base-template --name output-name [options]"
user-invocable: true
---

# image-composer-custom

## Overview

Create custom disk image templates by extending canonical base templates with
additional packages, repositories, and metadata — without touching the original
`image-templates/` directory.

The customized template goes to `user-templates/` so it persists across
sessions and can be reused, edited, or archived independently.

## When to Use

- User needs an existing base template **plus** a few extra packages
- User wants to add a third-party repo (ROS2, Docker, etc.) to a standard image
- User wants a reusable custom variant without modifying canonical templates
- User wants to quickly iterate: customize → build → test → tweak

## Usage

```bash
python3 .github/skills/image-composer-custom/scripts/customize-template.py \
    <base_template> --name <output_name> [options]
```

### Options

| Flag | Description |
|------|-------------|
| `--name <name>` | **Required.** Output template name (without .yml) |
| `--desc <text>` | Custom description for the new template |
| `--add-packages "pkg1,pkg2"` | Comma-separated extra packages to install |
| `--add-repo "url [codename [component]]"` | Add a package repo (can use multiple times) |
| `--add-repo-key <url>` | GPG key URL for the added repos |
| `--repo-codename <name>` | Default codename for repos (default: noble) |
| `--repo-component <name>` | Default component for repos (default: main) |
| `--list-base` | List all available base templates |
| `--list` | List existing user-customized templates |
| `--build` | Build the image immediately after customization |

## External Package Repositories (Generic Mechanism)

The `--add-repo` / `--add-repo-key` flags and the underlying `packageRepositories`
YAML field are a **generic mechanism** — they work for **any** external package
repository, regardless of OS family. They are not Docker-specific, ROS2-specific,
or tied to any particular vendor.

### How it works

The tool appends entries to the `packageRepositories` list in the template YAML.
At build time, the image-composer-tool:

1. Downloads the GPG key (if provided) and installs it in the image's trust store
2. Adds the repo to the image's package manager sources (`/etc/apt/sources.list.d/`
   for Debian/Ubuntu, or equivalent for RPM-based distros)
3. Resolves packages from all configured repos (built-in + external) and installs
   them into the image

The format is the same regardless of what software you're adding:

```yaml
packageRepositories:
  - url: "https://repo.example.com/linux/<distro>"
    codename: "<distro-codename>"       # e.g. noble, bookworm, el10
    component: "<component>"            # e.g. stable, main, 10-stream
    pkey: "https://repo.example.com/gpg"  # GPG key URL, or "[trusted=yes]"
```

### Valid for both apt-based and RPM-based images

| OS family | Works? | Example codename | Example component |
|-----------|--------|------------------|-------------------|
| Ubuntu (apt) | ✓ | `noble` (24.04) | `stable`, `main` |
| Debian (apt) | ✓ | `bookworm` (13) | `stable`, `main` |
| RCD10 / Rocky (RPM) | ✓ | `CentOS-Stream-10-AppStream` | *(optional)* |
| ELXR12 (RPM) | ✓ | *(varies)* | *(optional)* |
| EMT3 (RPM) | ✓ | *(varies)* | *(optional)* |

The RPM-based images handle repos slightly differently (codename is a label,
not a distro version), but the `packageRepositories` YAML field works the same way.

### Concrete examples (generic mechanism in action)

These are all just different data plugged into the same `packageRepositories`
mechanism:

| What you want | Repo URL | Codename | Component |
|---------------|----------|----------|-----------|
| Docker CE on Ubuntu | `https://download.docker.com/linux/ubuntu` | `noble` | `stable` |
| Docker CE on Debian | `https://download.docker.com/linux/debian` | `bookworm` | `stable` |
| ROS2 on Ubuntu | `http://packages.ros.org/ros2/ubuntu` | `noble` | `main` |
| EPEL on RCD10/Rocky | `https://dl.fedoraproject.org/pub/epel/10/Everything/x86_64` | *(label)* | *(label)* |
| NodeSource Node.js | `https://deb.nodesource.com/node_22.x` | `noble` | `main` |
| Microsoft (dotnet, mssql) | `https://packages.microsoft.com/ubuntu/24.04/prod` | `noble` | `main` |
| Jenkins | `https://pkg.jenkins.io/debian-stable` | `binary/` | *(varies)* |
| Any custom internal repo | `https://your-internal-mirror.example.com/apt` | `noble` | `main` |

If the package manager on the host can install from it, the image builder can
use it — the mechanism is the same for every repo.

### Examples

**Add ROS2 Jazzy to the minimal Ubuntu 24.04 raw image:**

```bash
python3 customize-template.py ubuntu24-x86_64-minimal-raw.yml \
    --name my-ros2-image \
    --desc "Ubuntu 24.04 minimal + ROS2 Jazzy" \
    --add-packages "ros-jazzy-ros-base,ros-jazzy-demo-nodes-py" \
    --add-repo "http://packages.ros.org/ros2/ubuntu noble main" \
    --add-repo-key "https://raw.githubusercontent.com/ros/rosdistro/master/ros.key"
```

**Add dev tools to the DL Streamer image and build:**

```bash
python3 customize-template.py ubuntu24-x86_64-dlstreamer.yml \
    --name my-dlstreamer-dev \
    --desc "DL Streamer with dev tools" \
    --add-packages "vim,git,htop,build-essential,cmake" \
    --build
```

**Add Docker repo to a Debian 13 minimal image:**

```bash
python3 customize-template.py debian13-x86_64-minimal-raw.yml \
    --name debian-docker \
    --add-packages "docker-ce,docker-ce-cli,containerd.io" \
    --add-repo "https://download.docker.com/linux/debian bookworm stable" \
    --add-repo-key "https://download.docker.com/linux/debian/gpg"
```

**Add Docker to Ubuntu 24.04 minimal (tested working):**

```bash
python3 customize-template.py ubuntu24-x86_64-minimal-raw.yml \
    --name ubuntu-docker \
    --desc "Ubuntu 24.04 minimal with Docker CE from official repo" \
    --add-packages "docker-ce,docker-ce-cli,containerd.io,docker-buildx-plugin,docker-compose-plugin" \
    --add-repo "https://download.docker.com/linux/ubuntu noble stable" \
    --add-repo-key "https://download.docker.com/linux/ubuntu/gpg"
```

Then add the `users` section (see "Default Login User" below), then build.

## Pre-validating Packages

The `customize-template.py` script has **no package name validation** — it
writes whatever names you give it into the template YAML. The build tool
(`image-composer-tool`) catches bad names at dependency resolution **before**
any disk image work begins.

Error you'll see on a bad package name:

```
requested package '"this-package-does-not-exist"' not found in repo
found 50 packages in request of 51
Error: pre-processing failed: failed to download image packages:
one or more requested packages not found.
```

A JSON file listing all missing packages is written to
`builds/Missing_Requested_Packages_<timestamp>.json`.

To catch typos before the build runs, pre-check package names against the
repo metadata on the host:

```bash
# Exact name match
apt-cache search ^iperf3$

# Fuzzy search
apt-cache search iperf

# Full package details
apt-cache show nano
```

For packages from a third-party repo (ROS2, Docker, etc.), first add the repo
to the host, then `apt update`, then search:

```bash
sudo apt update
apt-cache search ros-jazzy
```

## Workflow

### 1. Find a Base Template

Use the discoverability script from the `image-composer-build` skill:

```bash
python3 .github/skills/image-composer-build/scripts/list-templates.py
```

Or list base templates directly:

```bash
python3 customize-template.py --list-base
```

### 2. Customize

Choose a base template and specify your additions:

```bash
python3 customize-template.py <base> --name <custom> --add-packages "pkg1,pkg2"
```

The script:
1. Copies the base template to `user-templates/<name>.yml`
2. Adds extra packages (deduplicated)
3. Adds extra repos with optional GPG key
4. Updates the image name and metadata description

### 3. Review the Custom Template

```bash
cat user-templates/<name>.yml
```

### 4. Build

Build from the user template directory:

```bash
cd /data/os-image-composer && sudo -E ./image-composer-tool build user-templates/<name>.yml
```

Or automatically with `--build`.

### 5. List and Manage User Templates

```bash
# List all
python3 customize-template.py --list

# Delete a template
rm user-templates/<name>.yml
```

## How It Works

The script performs four operations on the base template YAML:

1. **Metadata update** — sets `image.name` and `metadata.description` to the
   custom name
2. **Package injection** — appends to `systemConfig.packages`, skipping
   duplicates
3. **Repo injection** — appends to `packageRepositories`, with optional GPG
   key URL, skipping duplicates by URL
4. **Output** — writes the modified template to `user-templates/`

The canonical `image-templates/` directory is **never modified**.

> **Reference:** `references/external-repo-docker-test.md` documents a
> successful test of the external repo workflow (Docker CE on Ubuntu 24.04),
> including GPG key handling, package install order, and full build timing.

## Default Login User

Customized templates should include a `users` section under `systemConfig` to
define the default login user. Without this, the image may boot with no
configured user account or fall back to an undesirable default.

**Distro-specific groups:**

| Distro family | Admin group | Example |
|---------------|-------------|---------|
| RPM (RCD10, ELXR, EMT, Rocky) | `wheel` | `groups: [wheel]` or `groups: [wheel, sudo]` |
| Debian/Ubuntu | `sudo` | `groups: [sudo]` |

Ubuntu does **not** have a `wheel` group by default — only `sudo`. Including
`wheel` in an Ubuntu image's user groups will cause the user-creation step to
produce a non-fatal warning (group not found), and the user will only have
`sudo` group membership.

**For RPM (RCD/ELXR/EMT) images:**

```yaml
systemConfig:
  users:
    - name: user
      password: "user"               # Do not commit real plaintext passwords
      groups: ["wheel", "sudo"]
```

**For Ubuntu/Debian images:**

```yaml
systemConfig:
  users:
    - name: user
      password: "user"               # Do not commit real plaintext passwords
      groups: ["sudo"]
```

The script does **not** inject users automatically — you must add them
manually after customization, either by editing
`user-templates/<name>.yml` directly, or by using a post-processing
snippet:

```bash
python3 -c "
import yaml, sys

path = 'user-templates/$NAME.yml'
with open(path) as f:
    data = yaml.safe_load(f)

data.setdefault('systemConfig', {}).setdefault('users', []).append({
    'name': 'user',
    'password': 'user',
    'groups': ['wheel', 'sudo']
})

# Deduplicate by name
seen = set()
data['systemConfig']['users'] = [
    u for u in data['systemConfig']['users']
    if u['name'] not in seen and not seen.add(u['name'])
]

with open(path, 'w') as f:
    yaml.dump(data, f, default_flow_style=False)
print('user section added/updated')
"
```

Replace `$NAME` with your template name. Run after customization and before
building.

**Distro-specific groups in the snippet:** for Ubuntu/Debian, change
`['wheel', 'sudo']` to `['sudo']` only — the `wheel` group does not exist
on Debian-based systems.

### External repo package verification

For packages from a third-party repo (Docker, ROS2, EPEL, etc.), first add the repo
to the host, then `apt update` / `dnf check-update`, then search:

```bash
# Apt-based (Ubuntu/Debian)
sudo apt update
apt-cache search docker-ce

# RPM-based (RCD/ELXR)
dnf search docker-ce
# or
dnf repoquery docker-ce
```

This confirms the package name is correct before running the build, preventing
"requested package not found" errors at dependency resolution time.

## Common Pitfalls

1. **No package name validation** — the script accepts any package name
   without checking if it exists in any repo. See the
   "Pre-validating Packages" section above for how to spot typos before
   running a build.

2. **Repo GPG key failures** — if the image-composer-tool rejects a GPG key,
   use `pkey: "[trusted=yes]"` instead of a key URL. You can edit the generated
  file in `user-templates/` manually after creation.

3. **Package names must match the repo** — if you add a repo but the package
   name is wrong, the build fails at dependency resolution with:
   ```
   requested package '"<name>"' not found in repo
   ```
   Pre-check with `apt-cache search <name>` before building.

4. **Disk size** — extra packages need space. If the base template is 4 GiB
   and you're adding ROS2, Docker, or large SDKs, edit `disk.size` in the
   custom template before building.

4. **Build from user-templates** — the build command should reference the
  workspace-local path: `sudo -E ./image-composer-tool build user-templates/<name>.yml`.

## Verification Checklist

- [ ] `python3 customize-template.py --list` shows the new template
- [ ] Template YAML parses: `python3 -c "import yaml; yaml.safe_load(open('PATH'))"`
- [ ] Extra packages appear in `systemConfig.packages`
- [ ] Extra repos appear in `packageRepositories`
- [ ] `systemConfig.users` has the desired login user with correct groups
- [ ] Build succeeds with the user template path

## Reference Files

| File | Contains |
|------|----------|
| `references/rcd10-customization-example.md` | Complete RCD10 (Rocky Linux) minimal build with nano, iperf3, and default login user — build timings, artifact sizes, RPM repo info, and key log entries |
| `references/error-demo-non-existent-package.md` | Example error from requesting a non-existent package (`this-package-does-not-exist`)
