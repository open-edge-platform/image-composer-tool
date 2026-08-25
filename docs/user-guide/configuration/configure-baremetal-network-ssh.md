# Enable Networking and SSH on Bare Metal (Debian 13 overlay)

## Overview

**Goal:** get a Debian 13 **overlay** image that boots on **bare metal** to acquire a
DHCP address and start `sshd`, when the image is a cloud image with **no cloud-init
datasource**.

**Why this is needed:** the Debian 13 generic cloud image relies on cloud-init to
configure networking and SSH from a datasource at first boot. Flashed to real
hardware there is usually no datasource, so cloud-init stalls boot probing the
network and never brings up DHCP or `sshd` — the device gets **no IP** and **SSH
never starts**. (QEMU hides this: its instant built-in DHCP lets cloud-init
succeed, so the failure only shows on hardware.)

**Approach:** deliver a small, idempotent, mode-agnostic bring-up script plus a
systemd unit through `systemConfig.additionalFiles`, and pick *who runs it* with a
single-line MODE toggle in `systemConfig.configurations`.

**Full working example:**
[image-templates/debian13/debian13-x86_64-bb-overlay-initrd-raw.yml](https://github.com/open-edge-platform/image-composer-tool/blob/main/image-templates/debian13/debian13-x86_64-bb-overlay-initrd-raw.yml)
with `image-templates/additionalfiles/debian13-bb-baremetal/`.

## What the bring-up script does

`bare-metal-bringup.sh` runs on the device at boot and performs the two steps the
flashed cloud image is missing:

1. **Networking** — write a `systemd-networkd` `.network` drop-in that requests DHCP
   on every wired NIC (matched by device type, `Type=ether`, not by name prefix),
   then enable and (re)start `systemd-networkd`. Matching by type covers NICs whose
   predictable names do not start with `en` (e.g. `eth0` under `net.ifnames=0`) while
   still excluding loopback and WiFi. `systemd-networkd` ships with systemd, so this
   needs no `ifupdown` / `netplan` / `NetworkManager`.
2. **SSH** — `ssh-keygen -A` generates any missing host keys, ensure `/run/sshd`,
   enable password auth via a `00-` `sshd_config.d` drop-in, then enable and start
   `ssh` (Debian's unit is `ssh`, not `sshd`). Keys are **per-device** only because
   MODE A removes any host keys baked by the `openssh-server` install at build time,
   so `ssh-keygen -A` fills them in freshly on first boot.

The script never touches cloud-init — the cloud-init policy is the template's job
(see the MODE toggle below), so the same script is correct under both invokers.

## The DHCP client identifier matters on bare metal

The single most common bare-metal failure is getting **no IPv4 lease** or an
**unexpected address**, even though the link is up. The cause is the DHCP *client
identifier*:

- By default `systemd-networkd` sends an **RFC-4361 DUID** as the client id.
- Many bare-metal DHCP servers lease by **MAC address** (reservations or per-MAC
  pools) and ignore the DUID, so they hand out no address for that identity or a
  different one than you expect.

The drop-in therefore forces MAC-based identification and disables IPv6 RA so the
NIC does not come "up" with only an RA-derived IPv6 address and no usable IPv4:

```ini
[Match]
Type=ether

[Network]
DHCP=yes
IPv6AcceptRA=no

[DHCPv4]
SendRelease=false
ClientIdentifier=mac
```

> QEMU's slirp DHCP leases regardless of client id, so this only matters on
> hardware. If networking works in QEMU but not on a device, check this first.

## Template wiring

Deliver the script and its systemd unit with `additionalFiles`. Default
(end-of-build) stage is correct — neither belongs in the initramfs. The script is
`0755` in the repo and `additionalFiles` preserves mode (`cp -p`), so it lands
executable.

```yaml
  packages:
    - openssh-server           # so sshd exists (only openssh-client ships by default)

  additionalFiles:
    - local: additionalfiles/debian13-bb-baremetal/usr/local/sbin/bare-metal-bringup.sh
      final: /usr/local/sbin/bare-metal-bringup.sh
    - local: additionalfiles/debian13-bb-baremetal/etc/systemd/system/bare-metal-bringup.service
      final: /etc/systemd/system/bare-metal-bringup.service
```

## Run a script on first boot only

`bare-metal-bringup.service` is an idempotent oneshot that runs on **every** boot
(re-applying the same config is harmless and self-heals a wiped `/run`). If instead
you want a unit to run its script only on the **first** boot, guard it with a
sentinel file and have systemd create that file after a successful run:

```ini
[Unit]
Description=First-boot only setup
# Skip the unit once the sentinel exists, so the script runs exactly once.
ConditionPathExists=!/var/lib/bare-metal-firstboot.done
Wants=network.target
Before=network.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/local/sbin/my-firstboot.sh
# Only mark "done" if the script succeeded, so a failed first boot retries next boot.
ExecStartPost=/usr/bin/touch /var/lib/bare-metal-firstboot.done

[Install]
WantedBy=multi-user.target
```

`ConditionPathExists=!<file>` makes systemd skip the unit once the sentinel exists,
so the script runs once and is a no-op on later boots. (systemd also offers
`ConditionFirstBoot=yes`, which fires only while `/etc/machine-id` is unpopulated —
handy for images that ship without a committed machine-id.)

## Choose a MODE (the switch)

Exactly one of the two `configurations` lines below is active. Switch by moving the
`#` — comment the active line and uncomment the other. Nothing else changes.

| MODE | Use when | What it does |
| ---- | -------- | ------------ |
| **A** (bare metal, default) | The device has **no** datasource | Disable cloud-init at build so it cannot stall boot, and enable the systemd oneshot that runs the script. |
| **B** (cloud) | The image also runs where a **real** datasource exists | Leave cloud-init **enabled** so it provisions networking/SSH from the cloud metadata. The bring-up script/unit are still shipped but stay unenabled. |

```yaml
  configurations:
    # MODE A (default): disable cloud-init, drop baked SSH host keys, enable the oneshot.
    - cmd: mkdir -p /etc/cloud && touch /etc/cloud/cloud-init.disabled && rm -f /etc/ssh/ssh_host_* && mkdir -p /etc/systemd/system/multi-user.target.wants && ln -sf /etc/systemd/system/bare-metal-bringup.service /etc/systemd/system/multi-user.target.wants/bare-metal-bringup.service
    # MODE B: leave cloud-init enabled; it provisions from the cloud datasource.
    # - cmd: rm -f /etc/cloud/cloud-init.disabled
```

Notes:

- MODE A creates the `multi-user.target.wants` symlink **directly** because
  `systemctl enable` cannot run in the `configurations` stage (it executes before
  `additionalFiles` are copied in); the symlink resolves once the unit lands at
  end-of-build.
- MODE A chains its steps with `&&` (not `;`) so the build fails fast if disabling
  cloud-init or enabling the oneshot did not actually happen — a `;` chain would
  mask an early failure behind the final command's exit status.
- MODE A removes host keys baked by the `openssh-server` install (`rm -f
  /etc/ssh/ssh_host_*`) so first boot generates per-device keys; without this every
  flashed unit would share the same build-time keys.
- MODE A disables cloud-init, which forfeits its first-boot rootfs `growpart`.
- Do **not** bake a local NoCloud seed for MODE B — a seed under
  `/var/lib/cloud/seed/nocloud/` can outrank and shadow the real cloud datasource.

## Provide a login user

Overlay mode applies `systemConfig.users`, but this feature does not create an
account itself. Password SSH is useless without one, so add a login user — see
[Configure Users](configure-image-user.md). For example:

```yaml
  users:
    - name: user
      password: user1234
      groups: ["sudo"]
```

> **Insecure bring-up.** This flow enables **password** SSH auth with a known
> credential purely to get a first shell. For any real deployment switch to
> key-based auth (drop an `authorized_keys` via `additionalFiles`) and remove
> `/etc/ssh/sshd_config.d/00-baremetal.conf`.

## Build and verify

Build and compose the image per the
[README](https://github.com/open-edge-platform/image-composer-tool/blob/main/README.md),
then flash the **raw** artifact to the device.

On the booted device:

- `ip -4 a` — the wired NIC should show the expected DHCP address.
- `networkctl status <iface>` — confirms the DHCP client id is the **link/MAC**.
- `systemctl is-active ssh` and `ssh user@<ip>` — sshd is up with per-device keys.
- `journalctl -u bare-metal-bringup.service` — the script's `bare-metal-bringup:`
  log lines (MODE A only).

## Troubleshooting

| Problem | Check |
| ------- | ----- |
| Works in QEMU, no IPv4 on hardware | `ClientIdentifier=mac` in the `.network` drop-in — the DHCP server likely leases by MAC. |
| Link up but only an IPv6/`fe80:` address | `IPv6AcceptRA=no`; confirm the DHCP server offers IPv4. |
| No IP and no SSH at all (MODE A) | The oneshot did not run — verify the `multi-user.target.wants/bare-metal-bringup.service` symlink and `journalctl -u bare-metal-bringup.service`. |
| Boot stalls / hangs probing network | cloud-init is still enabled — MODE A must `touch /etc/cloud/cloud-init.disabled`. |
| `sshd` fails to start | Missing host keys (`ssh-keygen -A`) or `/run/sshd`; both are handled by the script. |
| Can reach host but cannot log in | No login user — add one via `systemConfig.users`. |

An offline diagnostic, `scripts/inspect-image-login.sh`, mounts a built image
read-only via `qemu-nbd` and reports the account + sshd state without booting.

## Related documentation

- [Configure Users](configure-image-user.md) — add the login account this feature needs.
- [Custom Build Actions](configure-additional-actions-for-build.md) — `configurations` commands run in the chroot.
- [Image templates](../architecture/image-composer-tool-templates.md) — `additionalFiles` fields and merge behavior.
