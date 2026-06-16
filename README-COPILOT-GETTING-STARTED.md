# AI Skill Tutorial for Beginners

Welcome! This guide shows you how to use GitHub Copilot to work with the **image-composer-tool**, a tool that builds custom Linux OS images from templates.

## What is This Project?

**image-composer-tool** lets you:
- Pick a pre-built Linux OS template (Ubuntu, Debian, CentOS, etc.)
- Customize it with extra packages and configurations
- Build a ready-to-run disk image (`.raw`, `.iso`, `.img` formats)

You'll use **GitHub Copilot** (the AI assistant built into VS Code) with three **skills** (pre-written recipes) to do this interactively.

## What You'll Need

- **VS Code** with the **GitHub Copilot** extension installed
- **Go** compiler (check with: `go version`)
- **Docker** or **sudo** access (image building requires elevated privileges)
- **~20 GB free disk space** (images can be large)
- This repository cloned on your machine

## Quick Start (5 minutes)

### Step 1: Open VS Code in the repository
```bash
cd /data/os-image-composer
code .
```

### Step 2: Open Copilot Chat
Press `Ctrl+Shift+I` (or `Cmd+Shift+I` on Mac) to open the Copilot Chat panel on the right side.

### Step 3: Ask Copilot to list available OS templates
In the chat box, type:
```
What OS templates are available to build?
```

Copilot will respond with a list of buildable operating systems (Ubuntu, Debian, etc.) with architectures and image types.

**That's it!** You've just used your first Copilot skill.

---

## The Three Copilot Skills

This repo includes three pre-built "skills" (smart prompts) that Copilot understands:

| Skill | What it does | When to use |
|-------|---|---|
| **image-composer-list-os** | Shows available OS templates | First: browse what you can build |
| **image-composer-custom** | Customizes a template with packages | Second: add tools/apps you want |
| **image-composer-build** | Builds the final image | Last: compile the image |

### Understanding Templates

A **template** is a YAML recipe that describes an OS image:
- **OS/Distro**: "Ubuntu 24.04", "Debian 13", "CentOS"
- **Architecture**: "x86_64" (Intel/AMD) or "aarch64" (ARM)
- **Image type**: "raw" (disk image), "iso" (bootable CD), "img" (initial RAM disk)
- **Packages**: Pre-installed apps like Docker, Python, etc.

Example template file: `image-templates/ubuntu24-x86_64-minimal-raw.yml`

---

## Three Ways to Use Copilot Skills

You can trigger skills in three ways. Pick whichever feels most natural:

### Method A: Direct Chat (Easiest for Beginners)
Just describe what you want in natural language:
```
Show me all Ubuntu templates I can build
```
Copilot automatically detects the skill and runs it.

### Method B: Explicit Skill Request
Reference the skill name directly:
```
Use image-composer-list-os to show all available OS targets
```

### Method C: Terminal Commands (Advanced)
Manually run the Python scripts (no Copilot needed):
```bash
python3 .github/skills/image-composer-list-os/scripts/list-os-templates.py
```

---

## Typical Workflow (Start to Finish)

Here's a complete example: **"Build a custom Ubuntu image with Docker"**

### 1. List available templates
**Chat:**
```
What Ubuntu templates can I build?
```

**Response:** Lists 25 Ubuntu templates. You pick `ubuntu24-x86_64-minimal-raw.yml`.

### 2. Customize with Docker
**Chat:**
```
Use image-composer-custom to create a custom image based on ubuntu24-x86_64-minimal-raw.yml 
with the name ubuntu24-docker and add these packages: docker-ce, docker-ce-cli, containerd.io
```

**Result:** Copilot creates a new template file at `user-templates/ubuntu24-docker.yml`.

### 3. Build the image
**Chat:**
```
Build the custom image at user-templates/ubuntu24-docker.yml
```

**Result:** 
- Copilot recompiles the build tool
- Checks your disk space
- Builds the image (takes 10-30 minutes)
- Reports the output location (e.g., `workspace/ubuntu24-x86_64/imagebuild/docker/ubuntu24-docker.raw`)

---

## Detailed Skill Reference

All examples assume you are in the repository root directory.

### Skill 1: image-composer-list-os

**What it does:** Shows all available OS templates you can build, with metadata (OS name, architecture, image type).

**Why use it:** Start here to see what's available and pick a base template for your build.

#### Simple chat examples

Just ask Copilot naturally:

```
What operating systems can I build?
```

```
Show me all Ubuntu x86_64 raw images
```

```
List all minimal ISO templates
```

#### Advanced chat examples (with filters)

```
Show me all 32-bit ARM (aarch64) images
```

```
What Debian 13 templates do you have?
```

#### Direct terminal commands

If you prefer to run it manually:

```bash
# See all 64 templates at a glance
python3 .github/skills/image-composer-list-os/scripts/list-os-templates.py --summary-only
```

**Example output:**
```
Total templates: 64

OS families:
  - azure-linux: 7
  - debian: 5
  - ubuntu: 25
  - ...

Architectures:
  - x86_64: 56
  - aarch64: 8
```

```bash
# Filter by OS
python3 .github/skills/image-composer-list-os/scripts/list-os-templates.py --os ubuntu
```

```bash
# Filter by architecture
python3 .github/skills/image-composer-list-os/scripts/list-os-templates.py --arch x86_64
```

```bash
# Filter by image type (raw, iso, img)
python3 .github/skills/image-composer-list-os/scripts/list-os-templates.py --image-type raw
```

```bash
# Combine filters
python3 .github/skills/image-composer-list-os/scripts/list-os-templates.py --os ubuntu --arch aarch64 --image-type iso
```

### Skill 2: image-composer-build

**What it does:** Builds a disk image from a template file. Compiles the build tool, checks your disk space, and creates the final image.

**Why use it:** After you've picked or customized a template, use this to generate the actual bootable/deployable image.

**Important:** Builds require elevated privileges (`sudo`) and take 10–30 minutes depending on template size.

#### Simple chat examples

Just tell Copilot what to build:

```
Build an Ubuntu 24.04 minimal image
```

```
Build the debian13-x86_64-minimal-raw.yml template
```

```
Create a minimal ISO installer for Debian
```

#### Advanced chat examples

```
Build image-templates/emt3-x86_64-edge-raw.yml and verify the output
```

```
Build a custom ubuntu24-docker image and show me where the file is saved
```

#### What happens under the hood

When you ask Copilot to build, it runs this workflow:

1. **Recompile** the build engine (`go build ...`) — ensures compatibility
2. **Check disk space** — images need 5–50 GB depending on size
3. **Run the build** — create the image with `./image-composer-tool build`
4. **Verify artifacts** — confirm the `.raw`, `.iso`, or `.img` file was created
5. **Optional: Integrity check** — verify compressed images aren't corrupted

#### Manual terminal commands

If you prefer to run it step-by-step:

```bash
# Step 1: Rebuild the tool (required for template compatibility)
go build -buildmode=pie -ldflags "-s -w" ./cmd/image-composer-tool

# Step 2: Check available disk space
df -h .

# Step 3: Build a template (runs in background, check logs)
sudo -E ./image-composer-tool --log-file /tmp/image-composer-tool.log build image-templates/ubuntu24-x86_64-minimal-raw.yml

# Step 4: Watch the build progress
tail -f /tmp/image-composer-tool.log

# Step 5: Find the output image
find workspace/ -name "*.raw*" -o -name "*.iso*" 2>/dev/null

# Step 6: Verify compressed image integrity (if .gz file exists)
gunzip -t workspace/ubuntu24-x86_64/imagebuild/minimal/ubuntu24-minimal-raw.raw.gz
```

#### Expected output location

Built images land in: `workspace/<os-arch>/imagebuild/<template-name>/`

Examples:
- `workspace/ubuntu24-x86_64/imagebuild/minimal/ubuntu24-minimal-raw.raw` (~2 GB)
- `workspace/debian13-x86_64/imagebuild/minimal/debian13-minimal-iso.iso` (~500 MB)
- `workspace/emt3-x86_64/imagebuild/edge/emt3-edge-raw.raw.gz` (~1.5 GB compressed)

#### Build examples with different templates

```
Build image-templates/debian13-x86_64-minimal-raw.yml
```

```
Build image-templates/emt3-x86_64-desktop-virtualization-iso.yml
```

```
Build image-templates/elxr12-aarch64-minimal-raw.yml
```

### Skill 3: image-composer-custom

**What it does:** Creates a customized copy of a template in `user-templates/` without modifying the original. You can add packages, repositories, and configurations.

**Why use it:** The canonical templates in `image-templates/` are read-only. Use this skill to add Docker, Python, ROS, or any other software you need.

**Key advantage:** Your custom templates stay in `user-templates/` — the canonical templates in `image-templates/` remain pristine.

#### Simple chat examples

Add packages to a template:

```
Customize ubuntu24-x86_64-minimal-raw.yml with the name ubuntu24-docker 
and add packages: docker-ce, docker-ce-cli, containerd.io
```

```
Create a custom Ubuntu 24.04 image called ubuntu24-dev with nano, vim, and python3-pip
```

```
Make a ROS 2 variant of the Ubuntu 24.04 minimal template with the name ubuntu24-ros2
```

#### Advanced chat examples (with repositories)

```
Create ubuntu24-docker-custom with Docker packages AND the Docker APT repository:
- repo URL: https://download.docker.com/linux/ubuntu
- repo GPG key: https://download.docker.com/linux/ubuntu/gpg
```

```
Customize ubuntu24-x86_64-minimal-raw.yml:
- name: ubuntu24-ai
- add packages: python3-pip, pytorch, tensorflow
- add repo: https://ppa.launchpadcontent.net/deadsnakes/ppa/ubuntu
```

#### Terminal commands

If you prefer manual steps:

```bash
# Step 1: List available base templates to customize
python3 .github/skills/image-composer-custom/scripts/customize-template.py --list-base
```

**Example output:**
```
Available base templates:
  - ubuntu24-x86_64-minimal-raw.yml
  - debian13-x86_64-minimal-raw.yml
  - emt3-x86_64-edge-raw.yml
  - ... (more)
```

```bash
# Step 2: Create a custom template with additional packages
python3 .github/skills/image-composer-custom/scripts/customize-template.py \
  ubuntu24-x86_64-minimal-raw.yml \
  --name ubuntu24-dev-minimal \
  --add-packages "nano,htop,curl,git"
```

**Result:** A new file at `user-templates/ubuntu24-dev-minimal.yml`

```bash
# Step 3: List your custom templates
python3 .github/skills/image-composer-custom/scripts/customize-template.py --list
```

```bash
# Step 4: Build your custom template
sudo -E ./image-composer-tool build user-templates/ubuntu24-dev-minimal.yml
```

#### Advanced: Custom templates with repositories

Some packages require adding a custom software repository (like Docker's official repo):

```bash
python3 .github/skills/image-composer-custom/scripts/customize-template.py \
  ubuntu24-x86_64-minimal-raw.yml \
  --name ubuntu24-docker \
  --add-packages "docker-ce,docker-ce-cli" \
  --add-repo "https://download.docker.com/linux/ubuntu" \
  --add-repo-key "https://download.docker.com/linux/ubuntu/gpg"
```

#### Where custom templates go

All custom templates are saved to: `user-templates/`

Examples:
- `user-templates/ubuntu24-dev-minimal.yml` (your custom dev environment)
- `user-templates/ubuntu24-docker.yml` (custom with Docker)
- `user-templates/debian13-ros2.yml` (Debian with ROS 2)

---

## Complete Example: Build a Docker-Ready Ubuntu Image

Here's a real, step-by-step example you can follow:

### Step 1: Explore available images (2 min)
**In VS Code Copilot Chat, type:**
```
What Ubuntu 24.04 templates are available?
```

**Copilot responds:** Shows ~20 Ubuntu templates. You note `ubuntu24-x86_64-minimal-raw.yml`.

### Step 2: Create a custom version with Docker (1 min)
**In VS Code Copilot Chat, type:**
```
Create a custom image based on ubuntu24-x86_64-minimal-raw.yml 
with name ubuntu24-with-docker 
and add packages: docker-ce, docker-ce-cli, containerd.io
```

**Copilot responds:** Creates `user-templates/ubuntu24-with-docker.yml` and shows the build command.

### Step 3: Build the image (15-30 min)
**In VS Code Copilot Chat, type:**
```
Build user-templates/ubuntu24-with-docker.yml
```

**Copilot does:**
1. Recompiles the build tool
2. Checks disk space
3. Starts the build (watch logs in VS Code terminal)
4. Reports the image location when done

**When it's done**, your image is at something like:
```
workspace/ubuntu24-x86_64/imagebuild/with-docker/ubuntu24-with-docker.raw
```

### Step 4: Deploy or test (varies)
Your image is ready to:
- Upload to a cloud provider
- Flash to a USB drive
- Import into a VM (Hyper-V, KVM, VirtualBox)
- Share with your team

---

## Troubleshooting & FAQ

### "Copilot doesn't recognize my skill"

**Problem:** You ask something but Copilot doesn't use the skill.

**Solutions:**
1. Be explicit: Say "Use image-composer-list-os to..." instead of just "what templates"
2. Press `Ctrl+Shift+I` to open the chat panel if it's not visible
3. Check that you have GitHub Copilot extension installed in VS Code

### "Build failed: template validation error"

**Problem:** Build started but failed with a template error.

**Solution:** Recompile first:
```bash
go build -buildmode=pie -ldflags "-s -w" ./cmd/image-composer-tool
```
Then try building again. This ensures the build tool matches the template schema.

### "Not enough disk space"

**Problem:** Build failed because disk is full.

**Solution:** 
1. Check available space: `df -h`
2. Clean old builds: `rm -rf workspace/`
3. Free up at least 20 GB before retrying

### "Build is taking forever"

**Problem:** Build started 30+ minutes ago and still running.

**Solution:** This is normal! Large images (5+ GB) can take 30–60 minutes. You can:
- Watch progress: `tail -f /tmp/image-composer-tool.log` in a terminal
- Check disk activity: `iostat 2` to see if it's still writing
- Kill it safely: Press `Ctrl+C` in the terminal running the build (it will clean up)

### "Where is my built image?"

**Problem:** Build finished but you can't find the output.

**Solution:**
```bash
# Find all recently built images
find workspace/ -name "*.raw*" -o -name "*.iso*" 2>/dev/null | head -20

# Or search by name
find workspace/ -name "*ubuntu24*" 2>/dev/null
```

Expected location: `workspace/<os-arch>/imagebuild/<template-name>/<image-file>`

### "Can I edit canonical templates?"

**No — and don't try!** Templates in `image-templates/` are read-only and maintained by the team.

**Instead:** Always use `image-composer-custom` to create custom versions in `user-templates/`.

### "What if I want to modify a custom template?"

**Solution:** Delete it and recreate it with different parameters:
```bash
rm user-templates/ubuntu24-with-docker.yml

# Now recreate with different packages/repos
python3 .github/skills/image-composer-custom/scripts/customize-template.py \
  ubuntu24-x86_64-minimal-raw.yml \
  --name ubuntu24-with-docker \
  --add-packages "docker-ce,docker-ce-cli,containerd.io,git"
```

### "Error: 'sudo' command not found"

**Problem:** You're on a system without `sudo` (rare).

**Solution:** Ask your system administrator to run builds, or use Docker-based build tools.

### "How do I see what's in a template?"

**Solution:** Open the file in VS Code:
```
code image-templates/ubuntu24-x86_64-minimal-raw.yml
```

You'll see:
- OS name and version
- Packages to install
- Repositories to use
- Disk configuration
- Boot options

---

## Getting Help

### Within VS Code
- Open Copilot Chat (`Ctrl+Shift+I`) and ask questions naturally
- Examples: "How do I build an ARM image?" or "What packages are in the Ubuntu template?"

### In the repository
- Check `docs/` folder for detailed guides
- Read `README.md` for project overview
- Look in `.github/skills/` for skill documentation

### Ask Copilot directly
```
Explain how image-composer-tool works
```

```
What are the best practices for customizing templates?
```

```
Show me the difference between raw, iso, and img image types
```

---

## Key Concepts (Glossary)

| Term | Meaning |
|---|---|
| **Template** | A YAML file describing an OS image configuration |
| **Skill** | A pre-built Copilot recipe (list-os, build, custom) |
| **Image type** | Output format: `raw` (disk), `iso` (bootable), `img` (RAM disk) |
| **Architecture** | CPU type: `x86_64` (Intel/AMD) or `aarch64` (ARM) |
| **Distro** | Linux distribution version: `ubuntu24`, `debian13`, etc. |
| **Package** | Software to install: `docker`, `python3`, `git`, etc. |
| **Repository** | Online source for packages (Ubuntu PPA, Docker APT, etc.) |
| **Chroot** | Isolated build environment where packages are installed |
| **Artifact** | Final output: the built image file (`.raw`, `.iso`, etc.) |

---

## Typical End-to-End Flow

Quick reference for the fastest path:

```
1. Open VS Code and Copilot Chat (Ctrl+Shift+I)

2. Ask: "Show me all Ubuntu templates"
   → Copilot lists templates

3. Ask: "Create a custom Ubuntu image with Docker"
   → Copilot creates user-templates/ubuntu24-docker.yml

4. Ask: "Build user-templates/ubuntu24-docker.yml"
   → Copilot builds the image (15–30 min)

5. Your image is at: workspace/ubuntu24-x86_64/imagebuild/.../ubuntu24-docker.raw
```
