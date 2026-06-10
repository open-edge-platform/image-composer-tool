# AI Skill Tutorial

This guide shows practical usage examples for the three Copilot skills in this repo:

- `image-composer-build`
- `image-composer-custom`
- `image-composer-list-os`

All examples assume you are in the repository root.

## Skill 1: image-composer-list-os

Use this skill to discover what OS templates are available under `image-templates/`.

### Chat trigger examples

```text
/image-composer-list-os
```

```text
/image-composer-list-os --os ubuntu --arch x86_64 --image-type raw
```

```text
Use image-composer-list-os to list all buildable OS targets from image-templates, including os/dist/arch/imageType and template names.
```

### Direct script commands

```bash
python3 .github/skills/image-composer-list-os/scripts/list-os-templates.py
```

```bash
python3 .github/skills/image-composer-list-os/scripts/list-os-templates.py --summary-only
```

```bash
python3 .github/skills/image-composer-list-os/scripts/list-os-templates.py --dist ubuntu24 --arch x86_64 --image-type raw
```

## Skill 2: image-composer-build

Use this skill to build an OS image from a template file.

### Chat trigger examples

```text
/image-composer-build build a minimal ubuntu raw image using template ubuntu24-x86_64-minimal-raw.yml
```

```text
Use image-composer-build skill and run this workflow:
- recompile image-composer-tool
- check disk space
- build image-templates/ubuntu24-x86_64-minimal-raw.yml
- report artifact path under workspace/
- verify gzip integrity if raw.gz exists
```

### Command workflow (manual equivalent)

```bash
# 1) Recompile build engine (required in this repo's skill workflow)
go build -buildmode=pie -ldflags "-s -w" ./cmd/image-composer-tool

# 2) Check disk space
df -h .

# 3) Build the image
sudo -E ./image-composer-tool --log-file /tmp/image-composer-tool.log build image-templates/ubuntu24-x86_64-minimal-raw.yml

# 4) Verify artifacts
find workspace/ -name "*.raw*" -o -name "*.vhdx*" -o -name "*.iso*" 2>/dev/null

# 5) Optional integrity check for compressed artifact
gunzip -t workspace/<os-dist>-<arch>/imagebuild/<name>/<artifact>.raw.gz
```

### Other build examples

```text
/image-composer-build build image-templates/debian13-x86_64-minimal-raw.yml
```

```text
/image-composer-build build image-templates/emt3-x86_64-desktop-virtualization-iso.yml
```

## Skill 3: image-composer-custom

Use this skill to create a custom template in `user-templates/` without editing canonical templates.

### Chat trigger examples

```text
/image-composer-custom ubuntu24-x86_64-minimal-raw.yml --name ubuntu24-dev-minimal --add-packages "nano,htop,curl"
```

```text
Use image-composer-custom to clone ubuntu24-x86_64-minimal-raw.yml, add nano and htop, save to user-templates, then show the build command.
```

```text
Use image-composer-custom:
- base template: ubuntu24-x86_64-minimal-raw.yml
- output name: ubuntu24-docker-minimal
- add packages: docker-ce,docker-ce-cli,containerd.io
- add repo: https://download.docker.com/linux/ubuntu noble stable
- add repo key: https://download.docker.com/linux/ubuntu/gpg
- print the resulting build command
```

### Direct script commands

```bash
# List base templates
python3 .github/skills/image-composer-custom/scripts/customize-template.py --list-base
```

```bash
# Create custom template
python3 .github/skills/image-composer-custom/scripts/customize-template.py \
  ubuntu24-x86_64-minimal-raw.yml \
  --name ubuntu24-dev-minimal \
  --add-packages "nano,htop,curl"
```

```bash
# List custom templates
python3 .github/skills/image-composer-custom/scripts/customize-template.py --list
```

```bash
# Build generated custom template
sudo -E ./image-composer-tool build user-templates/ubuntu24-dev-minimal.yml
```

## Typical End-to-End Flow

```text
1) /image-composer-list-os
2) Pick a template name from the output
3) /image-composer-custom <base-template> --name <custom-name> --add-packages "..."
4) /image-composer-build build image-templates/<template>.yml
   or build user-templates/<custom-name>.yml
```

## Troubleshooting

- If skill does not trigger, use explicit phrasing: "Use image-composer-build skill ..."
- If build fails with template validation on old binary, recompile first:
  `go build -buildmode=pie -ldflags "-s -w" ./cmd/image-composer-tool`
- If compressed artifact check is slow, use `gzip -l <file>.gz` for quick metadata check.
