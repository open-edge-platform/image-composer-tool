# Docker External Repo Test (Ubuntu 24.04)

## Test Result: PASS

Build of `ubuntu24-x86_64-docker-test` completed successfully with Docker CE
from Docker's official public repo.

## Command Used

```bash
python3 .github/skills/image-composer-custom/scripts/customize-template.py ubuntu24-x86_64-minimal-raw.yml \
    --name ubuntu24-x86_64-docker-test \
    --desc "Ubuntu 24.04 minimal with Docker CE from Docker's official repo" \
    --add-packages "docker-ce,docker-ce-cli,containerd.io,docker-buildx-plugin,docker-compose-plugin" \
    --add-repo "https://download.docker.com/linux/ubuntu noble stable" \
    --add-repo-key "https://download.docker.com/linux/ubuntu/gpg"
```

Then added `systemConfig.users` manually (groups: `[sudo]` only — no `wheel` on Ubuntu).

## Key Observations

| Detail | Result |
|--------|--------|
| GPG key download | 3817 bytes, verified OK — no `[trusted=yes]` needed |
| Docker repo metadata | 340 packages found |
| Total repos in build | 13 (12 Ubuntu upstream + 1 Docker user repo) |
| docker-ce installed | Package 15/21 |
| docker-ce-cli installed | Package 16/21 |
| containerd.io installed | Package 17/21 |
| docker-buildx-plugin installed | Package 18/21 |
| docker-compose-plugin installed | Package 19/21 |
| User `user` created | Yes, with `sudo` group |
| Non-fatal warning | systemd-boot EFI variable in chroot (expected — same as RCD builds) |
| Total build time | 3m 55s |
| Exit code | 0 |

## Artifacts

- `ubuntu24-x86_64-docker-test-24.04.raw.gz` (1.2 GB)
- `ubuntu24-x86_64-docker-test-24.04.vhdx` (2.1 GB)
- SPDX SBOM (0.19 MB)

## What This Confirms

1. The `--add-repo` / `--add-repo-key` workflow works for Ubuntu (apt) images
2. Docker's official GPG key is accepted without the `[trusted=yes]` workaround
3. Multiple docker-* packages install cleanly in the image
4. The `packageRepositories` field in the template YAML correctly generates
   `/etc/apt/sources.list.d/package-repositories.list`, installs the GPG key
   to `/etc/apt/trusted.gpg.d/`, and creates apt preferences
5. The `users` section under `systemConfig` (with `sudo` group only) works
   correctly for Ubuntu images
