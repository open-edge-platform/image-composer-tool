# Unattended ISO Installer Tutorial

This guide builds an ISO that installs a fully configured Ubuntu system with no
operator input: the installer picks the target disk, partitions it, installs
the OS from packages carried on the ISO, configures users, SSH keys, proxy,
cloud-init, and boot-time provisioning, makes the installed disk the first boot
device, and reboots.

The worked example is the Base Platform template, available for
Ubuntu 24.04 (latest HWE kernel) and Ubuntu 26.04:

- [`image-templates/ubuntu24/ubuntu24-x86_64-base-platform-iso.yml`](../../../image-templates/ubuntu24/ubuntu24-x86_64-base-platform-iso.yml)
- [`image-templates/ubuntu26/ubuntu26-x86_64-base-platform-iso.yml`](../../../image-templates/ubuntu26/ubuntu26-x86_64-base-platform-iso.yml)

Field details are in the [Image Template Reference](../architecture/image-composer-tool-templates.md).

## How the installer works

An `imageType: iso` build resolves and downloads every package at compose time
and stores them on the ISO as a local package repository, together with the
merged template. On the target, the ISO boots into an installer environment
whose `systemConfig.initramfs.template` is `default-initrd-unattended-x86_64.yml`;
it runs ICT's `live-installer`, which installs the same system a raw build
would produce. The install needs no network access to package repositories.

## 1. Describe the system

Everything below is set in the ISO template.

### Target disk

```yaml
disk:
  path: ""                 # empty: choose by policy
  selectionPolicy:
    strategy: largest      # first | largest | fastest
    excludeRemovable: true
    requireEmpty: true     # only disks without partitions
```

Set `path` to install onto a specific disk instead. The installer refuses disks too small for the partition layout.

### Partitions that adapt to the disk size

A negative offset is measured back from the end of the selected disk, so root
fills whatever space is left while the data and swap partitions keep a fixed
size:

```yaml
  partitions:
    - id: rootfs
      type: linux-root-amd64
      start: 513MiB
      end: "-20GiB"
      fsType: ext4
      mountPoint: /
    - id: data
      type: linux
      start: "-20GiB"
      end: "-4GiB"
      fsType: ext4
      mountPoint: /data
      mountOptions: defaults,nofail
    - id: swap
      type: linux-swap
      start: "-4GiB"
      end: "0"
      fsType: linux-swap
```

Each mounted partition and each swap partition gets an `/etc/fstab` entry.
Because the partitions are created on the real target disk, no resize is needed
after installation.

### Users and SSH keys

```yaml
systemConfig:
  packages:
    - openssh-server
  users:
    - name: admin
      password: "$6$<salt>$<hash>"   # openssl passwd -6
      groups: [sudo]
      sudo: true
      shell: /bin/bash
      home: /home/admin
      sshAuthorizedKeysFiles:
        - additionalfiles/base-platform/admin.pub
```

Key files are read at build time; keys can also be listed inline under
`sshAuthorizedKeys`, or added per build with
`--ssh-authorized-key admin=$HOME/.ssh/id_ed25519.pub`.

### Proxy

```yaml
  proxy:
    httpProxy: http://proxy.example.com:3128
    httpsProxy: http://proxy.example.com:3128
    noProxy: localhost,127.0.0.1,.example.com
```

The proxy is written to `/etc/environment`, apt's configuration, and the
default environment of every systemd service.

### Cloud-init

```yaml
  cloudInit:
    enabled: true
    userDataFile: additionalfiles/base-platform/user-data
```

cloud-init is installed and reads the user-data from the NoCloud seed on first
boot. Meta-data with a unique instance ID is generated for each installed
system.

### Boot-time provisioning scripts

```yaml
  provisioning:
    scripts:
      - name: platform-bootstrap
        local: additionalfiles/base-platform/10-platform-bootstrap.sh
        final: /usr/local/sbin/platform-bootstrap.sh
        stage: first-boot     # or every-boot
        order: 10
```

Each script runs from its own systemd unit
(`ict-provision-<NNN>-<name>.service`) after the network is online, in `order`.
A first-boot script that fails is retried on the next boot.

### Developer source archives and other artifacts

Copy any file with `additionalFiles`. Files larger than 4 GiB are supported on
the ISO.

```yaml
  additionalFiles:
    - local: additionalfiles/base-platform/platform-src.tar.gz
      final: /opt/platform/src/platform-src.tar.gz
```

### APT upgrade policy

```yaml
packageRepositories:
  - codename: platform-stack
    url: https://ppa.example.com/platform/ubuntu
    pkey: https://ppa.example.com/platform/key.gpg

systemConfig:
  aptPolicy:
    upgradeAllowedRepos:
      - platform-stack
```

Only the listed repositories (plus the Ubuntu archives) may upgrade installed
packages. The Ubuntu archives, including security updates, are only blocked
when `systemConfig.immutability.enabled` is `true`, in which case nothing
upgrades.

### Boot entry

```yaml
  bootloader:
    bootEntryPolicy: preserve   # or exclusive
```

The installer adds an `ICT` firmware boot entry and puts it first in
`BootOrder`. `preserve` keeps the existing `BootOrder` after it. Existing boot
entries are kept, except entries labelled exactly `ICT` from an earlier install,
which are replaced once the new entry is in place.

## 2. Build

```bash
go build -buildmode=pie -o ./build/live-installer ./cmd/live-installer
go build -o ./build/image-composer-tool ./cmd/image-composer-tool   # or: earthly +build

./build/image-composer-tool validate --merged image-templates/ubuntu24/ubuntu24-x86_64-base-platform-iso.yml
sudo -E ./build/image-composer-tool build \
  --ssh-authorized-key "admin=$HOME/.ssh/id_ed25519.pub" \
  image-templates/ubuntu24/ubuntu24-x86_64-base-platform-iso.yml
```

The shipped `additionalfiles/base-platform/admin.pub` contains only comments and
the template sets no password, so the `--ssh-authorized-key` flag (or a real key
in that file) is what lets you log in to the installed system. Create a key
first with `ssh-keygen -t ed25519` if you do not have one.

The build output directory
(`<work_dir>/ubuntu-ubuntu24-x86_64/imagebuild/<system-config-name>/`)
contains:

| File | Content |
|------|---------|
| `base-platform-ubuntu24-24.04.iso` | The installer ISO |
| `spdx_manifest_deb_*.json` | SPDX SBOM of the system the ISO installs |
| `base-platform-ubuntu24-24.04.composition.json` | Composition manifest: base OS, base and template repositories, packages with versions and checksums, kernel, provisioning inputs, and every additional file with its SHA-256 |
| `template-dump.yaml` | The merged template the installer uses |

The installed system also carries its own SBOM in `/usr/share/sbom/`.

The shipped templates create a sudo `admin` user with no password, so a build
needs a credential: set `password` in the template, or pass a public key with
`--ssh-authorized-key admin=$HOME/.ssh/id_ed25519.pub` on the `build` command.
Without one, the build stops before creating the ISO instead of producing an
installer that cannot finish.

## 3. Verify in QEMU

```bash
qemu-img create -f raw /tmp/target.raw 60G
cp /usr/share/OVMF/OVMF_VARS_4M.fd /tmp/vars.fd
sudo qemu-system-x86_64 -m 4096 -enable-kvm -cpu host \
  -drive if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd \
  -drive if=pflash,format=raw,file=/tmp/vars.fd \
  -drive file=/tmp/target.raw,format=raw,if=virtio \
  -cdrom <built.iso> -nic user,model=virtio-net-pci,hostfwd=tcp::2222-:22 \
  -nographic -no-reboot
```

The installer logs `Unattended install completed successfully` and QEMU exits
on the reboot. Boot the installed disk (same command without `-cdrom` and
`-no-reboot`), then check:

```bash
ssh -p 2222 admin@localhost                       # key-based login
id admin; getent passwd admin                     # groups, shell, home
grep -i proxy /etc/environment; apt-config dump | grep -i proxy
systemctl status 'ict-provision-*'                # provisioning ran
cloud-init status --long                          # status: done
lsblk; findmnt /data; swapon --show; df -h /      # layout fills the disk
efibootmgr                                        # ICT first in BootOrder
apt-cache policy; apt-get -s upgrade              # upgrade policy
```

## Troubleshooting

- The installer log is `/tmp/unattended-installer.log` in the installer
  environment. On failure the installer drops to a shell instead of rebooting.
- A failed first-boot script: `journalctl -u ict-provision-<NNN>-<name>`. Fix
  the cause and reboot, or remove the stamp in
  `/var/lib/image-composer-tool/provisioned/` to run it again.
- cloud-init did not run: check `/var/lib/cloud/seed/nocloud/` and
  `/etc/cloud/cloud.cfg.d/90_ict_datasource.cfg` on the installed system.
