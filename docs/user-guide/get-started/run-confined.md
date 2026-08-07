# Running with reduced privilege (capability confinement)

Image Composer Tool (ICT) builds run as root because they attach loop devices,
mount filesystems, and enter a `chroot`. Running as *unrestricted* root means a
bug or a malicious package post-install script during a build could affect the
whole host. This page shows how to run ICT with only the **minimal set of Linux
capabilities** a build actually needs, so the rest of root's power is dropped.

This does not change how ICT works or what it can build — the loop/mount/chroot
mechanism is unchanged. It only lowers the privilege the process is granted, and
it is portable: capabilities exist on every kernel ICT targets, with no
dependency on unprivileged user namespaces (which some hardened distributions,
including Ubuntu 24.04, restrict by default).

## The minimal capability set

A build needs exactly these capabilities (defined once in
`scripts/ict-capabilities.env`):

| Capability | Why |
| --- | --- |
| `CAP_SYS_ADMIN` | `losetup`, `mount`, bind mounts, sysfs, `mkswap` |
| `CAP_SYS_CHROOT` | entering the chroot |
| `CAP_CHOWN` | own root-owned rootfs files |
| `CAP_FOWNER` | operate on files not owned by the effective uid |
| `CAP_DAC_OVERRIDE` | read/traverse root-owned trees regardless of permissions |
| `CAP_DAC_READ_SEARCH` | read-bypass distinct from `DAC_OVERRIDE` (metadata/loop reads) |
| `CAP_FSETID` | preserve setuid/setgid bits when writing rootfs files |
| `CAP_MKNOD` | device nodes in the rootfs (`mmdebstrap`/`dpkg`) |
| `CAP_SETUID` / `CAP_SETGID` | accounts and groups created inside the rootfs |
| `CAP_SETFCAP` | file capabilities on binaries inside the rootfs (e.g. `ping`) |

Every other capability (for example `CAP_SYS_MODULE`, `CAP_SYS_RAWIO`,
`CAP_SYS_PTRACE`, `CAP_NET_ADMIN`, `CAP_BPF`) is dropped. This set was validated
by building both an overlay image and a from-scratch (`mmdebstrap`) raw image on
Ubuntu 24.04 with the other capabilities removed.

`CAP_SYS_ADMIN` is still broad. Confining to this set is a meaningful reduction,
not full isolation; dropping `CAP_SYS_ADMIN` entirely requires the optional
libguestfs backend described in the architecture decision record.

## CLI: run a build confined

Use the wrapper, which reduces the capability bounding set to the minimal set
and then runs ICT — every build child (`mmdebstrap`, `mount`, `losetup`,
`chroot`, `apt`, …) is bounded to the same set:

```bash
sudo -E ./scripts/ict-confined.sh build image-templates/ubuntu24-x86_64-minimal-raw.yml
```

It takes the same arguments as `image-composer-tool` and forwards them
unchanged. Point it at an installed binary with `ICT_BIN`:

```bash
ICT_BIN=/usr/bin/image-composer-tool \
  sudo -E ./scripts/ict-confined.sh build my-template.yml --workers 16
```

`sudo -E` is required: shrinking the bounding set itself needs `CAP_SETPCAP`
(i.e. entering as root), exactly as ICT is launched today. `SUDO_UID`/`SUDO_GID`
are preserved, so the post-build ownership restore still hands the image back to
your user.

### Verify the confinement

To see which capabilities were dropped for a given template — or to re-validate
the set after a toolchain change — use the audit harness, which builds under the
reduced set and reports whether any operation needed a capability that was
dropped:

```bash
sudo -E ./scripts/cap-audit.sh image-templates/ubuntu24-x86_64-minimal-raw.yml
```

## Server: run the build server confined (systemd)

`deploy/systemd/image-composer-tool.service` runs the build server with
`CapabilityBoundingSet` reduced to the minimal set.

> **Important.** The web server's `--sudo` mode runs each privileged step via
> `sudo -n <cmd>`. A `sudo` child is a new process whose capabilities come from
> the bounding set at exec time, so a unit that bounds only the server process
> does **not** confine a `sudo -n`-spawned build child. The shipped unit
> therefore runs the server **as root without `--sudo`**, so build steps are
> direct children bounded by the unit. Do not add `--sudo` to `ExecStart` unless
> you have arranged confinement for the sudo children separately.

```bash
sudo install -m0644 deploy/systemd/image-composer-tool.service \
    /etc/systemd/system/image-composer-tool.service
# edit ExecStart (binary path, listen flags) to match your deployment
sudo systemctl daemon-reload
sudo systemctl enable --now image-composer-tool
```

Confirm the running service holds only the expected set:

```bash
systemctl show image-composer-tool -p CapabilityBoundingSet
```

## Troubleshooting

If a build fails with `Operation not permitted` / `EPERM` after confinement, an
operation needs a capability not in the set. Identify it with the audit harness
(it prints the offending lines), then add the capability to **both**
`scripts/ict-capabilities.env` and the `CapabilityBoundingSet` line in the
systemd unit. Please also open an issue so the documented set can be updated —
the audited set above is expected to be sufficient for the supported templates.
