# Copilot Skills Quick Start

This guide is for fast onboarding. It puts the most useful part first:
exact prompts you can paste into Copilot Chat for every repo skill.

## Prerequisites

- VS Code with GitHub Copilot installed
- This repo cloned locally
- Go installed (`go version`)
- `sudo` access (required for image builds)
- At least 20 GB free disk (`df -h .`)

## Start in 60 Seconds

1. Open the repo in VS Code.
2. Open Copilot Chat (`Ctrl+Shift+I`).
3. Paste one of the prompts from the next section.

---

## Skill Prompt Cookbook (Use This First)

Use these prompts directly in Copilot Chat.

### 1) image-composer-list-os

Use this to discover what you can build.

**Starter prompt**

```text
Use image-composer-list-os and show me all available OS targets.
```

**More examples**

```text
Use image-composer-list-os and list only Ubuntu templates.
```

```text
Use image-composer-list-os and show only x86_64 raw templates.
```

```text
Use image-composer-list-os and summarize counts by OS family.
```

### 2) image-composer-custom

Use this to create custom templates in `user-templates/` without touching
canonical templates in `image-templates/`.

**Starter prompt**

```text
Use image-composer-custom to clone ubuntu24-x86_64-minimal-raw.yml as ubuntu24-dev and add packages: git, curl, vim.
```

**More examples**

```text
Use image-composer-custom to create ubuntu24-docker from ubuntu24-x86_64-minimal-raw.yml and add packages: docker-ce, docker-ce-cli, containerd.io.
```

```text
Use image-composer-custom to create ubuntu24-ros2 from ubuntu24-x86_64-minimal-raw.yml and add ROS-related packages.
```

```text
Use image-composer-custom to create ubuntu24-docker-repo from ubuntu24-x86_64-minimal-raw.yml and add Docker repo URL and repo GPG key.
```

### 3) image-composer-build

Use this to build an image from a template.

**Starter prompt**

```text
Use image-composer-build to build image-templates/debian13-x86_64-minimal-raw.yml.
```

**More examples**

```text
Use image-composer-build to build user-templates/ubuntu24-dev.yml and tell me where the artifact is saved.
```

```text
Use image-composer-build to build image-templates/emt3-x86_64-minimal-iso.yml and verify output files.
```

```text
Use image-composer-build to list candidate templates for edge use cases, then build one.
```

---

## Recommended Flow

1. Run discovery with `image-composer-list-os`.
2. Create a custom template with `image-composer-custom`.
3. Build with `image-composer-build`.

## Terminal Equivalents (Optional)

If you prefer direct scripts instead of chat-triggered skills:

```bash
# List buildable templates
python3 .github/skills/image-composer-list-os/scripts/list-os-templates.py --summary-only

# Create custom template
python3 .github/skills/image-composer-custom/scripts/customize-template.py \
  ubuntu24-x86_64-minimal-raw.yml \
  --name ubuntu24-dev \
  --add-packages "git,curl,vim"

# Build template
go build -buildmode=pie -ldflags "-s -w" ./cmd/image-composer-tool
sudo -E ./image-composer-tool build user-templates/ubuntu24-dev.yml
```

## Quick Troubleshooting

- Copilot did not trigger a skill:
  use explicit phrasing like "Use image-composer-build to ...".
- Build fails with validation mismatch:
  rebuild binary first with `go build -buildmode=pie -ldflags "-s -w" ./cmd/image-composer-tool`.
- Disk space errors:
  run `df -h .` and clear old artifacts in `workspace/`.
- Cannot find output image:
  run `find workspace/ -name "*.raw*" -o -name "*.iso*" 2>/dev/null`.

## Output Location

Artifacts are typically written under:

```text
workspace/<os>-<dist>-<arch>/imagebuild/<system-config-name>/
```

## Next References

- Main repo overview: `README.md`
- Full CLI details: `docs/user-guide/architecture/image-composer-tool-cli-specification.md`
- Template system: `docs/user-guide/architecture/image-composer-tool-templates.md`
- Skill definitions: `.github/skills/`
