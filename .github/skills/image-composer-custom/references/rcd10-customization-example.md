# RCD10 (Rocky Linux) Customization Example

## What This Is

A complete end-to-end build of an RCD10 (RHEL-compatible distro, el10) minimal
raw image with nano and iperf3 added — plus a default login user. This is the
canonical reference for RHEL-family customizations using the image-composer-tool.

## Base Template

```
rcd10-x86_64-minimal-raw.yml
```

Available templates: `rcd10-mah.yml`, `rcd10-rockylinux.yml`,
`rcd10-x86_64-dlstreamer.yml`, `rcd10-x86_64-minimal-raw.yml`.

List them with:

```bash
python3 .github/skills/image-composer-build/scripts/list-templates.py rcd10
```

## Customization

Customize with the script, then add the user section separately:

### Step 1 — Customize packages

```bash
python3 .github/skills/image-composer-custom/scripts/customize-template.py \
    rcd10-x86_64-minimal-raw.yml \
    --name rcd10-x86_64-minimal-nano-iperf \
    --desc "RCD10 minimal OS with nano and iperf3" \
    --add-packages "nano,iperf3"
```

The script:
- Copies the base template to `user-templates/<name>.yml`
- Adds `nano` and `iperf3` to `systemConfig.packages`
- Updates `image.name` and `metadata.description`

### Step 2 — Add default login user

Edit the generated YAML to add a `users` section under `systemConfig`:

```yaml
systemConfig:
  name: rcd10-x86_64-minimal-nano-iperf
  description: Default yml configuration for raw image
  users:
    - name: user
      password: "user"
      groups:
        - wheel
        - sudo
  immutability:
    enabled: false
  packages:
    - nano
    - iperf3
```

Alternatively, use the post-processing snippet from the skill's "Default Login User"
section in `SKILL.md`.

### Step 3 — Verify the template

```bash
python3 -c "import yaml; yaml.safe_load(open('user-templates/rcd10-x86_64-minimal-nano-iperf.yml'))"
echo "OK"
```

### Step 4 — Build

```bash
cd /data/os-image-composer && sudo -E ./image-composer-tool build \
  user-templates/rcd10-x86_64-minimal-nano-iperf.yml
```

## Build Results (from this session)

| Metric | Value |
|--------|-------|
| Build time | 3m 23s |
| Base image size | 4 GiB |
| User | `user` / `user`, groups: `wheel`, `sudo` |
| Extra packages | nano, iperf3 |
| RPMs resolved | 242 total (38 requested + deps) |
| Installed | 227 (SBOM final: 226) |
| Kernel | 6.12.0-233.el10.x86_64 |

### Output artifacts

```
/data/os-image-composer/workspace/redhat-compatible-distro-el10-x86_64/imagebuild/rcd10-x86_64-minimal-nano-iperf/
  rcd10-x86_64-minimal-nano-iperf-10.0.raw.gz    434 MB
  rcd10-x86_64-minimal-nano-iperf-10.0.vhdx      1.1 GB
  spdx_manifest_rpm_...json                      224 KB
```

### Key build log entries

```
Configuring User...
Creating user: user
User user created successfully
```

```
Installing package 38/39: nano
Installing package 39/39: iperf3
```

```
├── Configuring User...  ──  user created with wheel,sudo groups
├── Image build (raw)    ──  1m33s
├── VHDX conversion      ──  45s
├── GZ compression       ──  43s
└── Total                ──  3m23s
```

## RPM Distro Notes

RCD10 resolves packages from CentOS Stream 10 repos:

| Repo | URL |
|------|-----|
| BaseOS | `https://mirror.stream.centos.org/10-stream/BaseOS/x86_64/os` |
| AppStream | `https://mirror.stream.centos.org/10-stream/AppStream/x86_64/os` |

The tool auto-detects `os: redhat-compatible-distro`, `dist: el10`,
`arch: x86_64` and uses the provider config from
`config/osv/redhat-compatible-distro/el10/providerconfigs/x86_64_repo.yml`.

## Differences from Ubuntu/Debian Customization

| Aspect | Ubuntu/Debian | RCD10 (RHEL) |
|--------|---------------|--------------|
| Package manager | apt/deb | dnf/rpm |
| Repo format | deb repos with GPG keys | RPM repos with GPG keys |
| User creation | `adduser` in chroot | `useradd` in chroot |
| Default groups | `sudo` group via `systemConfig.users` | `wheel,sudo` groups |
| Provisioning | YAML-based user config supported | YAML-based user config supported |
| Kernel naming | `linux-image-*` | `kernel`, `kernel-core`, `kernel-modules` |
