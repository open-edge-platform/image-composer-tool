# Configure a Custom Script in the Initrd (Debian 13, GRUB)

## Overview

**Goal:** Run your own shell script **on the device during early boot** (inside the initramfs), for **Debian 13** `**imageType: raw`** images that use **GRUB**.

This tutorial supports **two routes** for Debian 13 raw + GRUB images:

1. **Route 1: initramfs-tools flow** using `image-templates/debian13-x86_64-bb-raw.yml`
2. **Route 2: dracut module flow** using `image-templates/debian13-x86_64-bb-dracut-raw.yml`

Both routes achieve the same goal (run your custom logic during early boot), but they use different initrd tooling and file layouts.

**Start from your image template:** add `systemConfig.additionalFiles` (and related entries) that point at files in the repo. This page shows the YAML first, then the file contents and boot stages.

**Full working examples:**

- `image-templates/debian13-x86_64-bb-raw.yml` with `image-templates/additionalfiles/debian13-bb/` (initramfs-tools flow)
- `image-templates/debian13-x86_64-bb-dracut-raw.yml` with `image-templates/additionalfiles/debian13-bb-dracut/` (dracut module flow)

## Choose your route first

Use exactly one route for your template customization.

| Route | Pick this when | Start from this template |
| ----- | -------------- | ------------------------ |
| Route 1: initramfs-tools hook + script | You want the classic Debian `update-initramfs` hook model (`hooks/` + `scripts/init-bottom/`). | `image-templates/debian13-x86_64-bb-raw.yml` |
| Route 2: dracut module | You prefer dracut module structure under `modules.d/` and dracut module enablement config. | `image-templates/debian13-x86_64-bb-dracut-raw.yml` |

### Package switch required for each route

- Route 1 (`bb-raw`, initramfs-tools): **add** `initramfs-tools` and `cloud-initramfs-growroot`; **remove** `dracut` and `dracut-core` if present.
- Route 2 (`bb-dracut-raw`, dracut): **add** `dracut` and `dracut-core`; **remove** `initramfs-tools` and `cloud-initramfs-growroot` if present.
- For both routes: keep your GRUB and kernel packages (for example `grub-cloud-amd64` and `linux-image-amd64`).

### Remove the current initrd stack with `configurations` commands

If your base package set already includes the wrong initrd tooling, remove it explicitly in `systemConfig.configurations`.

Use one of these snippets based on your route:

#### If you are switching to Route 1 (`initramfs-tools`)

Remove dracut packages and dracut module config:

```yaml
  configurations:
    - cmd: "apt-get purge -y dracut dracut-core || true"
    - cmd: "rm -f /etc/dracut.conf.d/*.conf"
```

#### If you are switching to Route 2 (`dracut`)

Remove initramfs-tools packages and old hook/script paths:

```yaml
  configurations:
    - cmd: "apt-get purge -y initramfs-tools cloud-initramfs-growroot || true"
    - cmd: "rm -f /etc/initramfs-tools/hooks/hello /etc/initramfs-tools/scripts/init-bottom/hello"
```

Notes:

- Keep only one initrd framework in the final image to avoid mixed behavior.
- Keep your selected framework in `packages` (`initramfs-tools` for Route 1, or `dracut` + `dracut-core` for Route 2).
- `|| true` makes the command safe when a package is not installed.

---

## Route 1: initramfs-tools hook + boot script (`bb-raw`)

Use [debian13-x86_64-bb-raw.yml](https://github.com/open-edge-platform/image-composer-tool/blob/main/image-templates/debian13-x86_64-bb-raw.yml) as an example.

Paths in `**local`** are relative to the **directory that contains your template YAML** (`image-templates/…` → `additionalfiles/debian13-bb/…`).

```yaml
  ...
  bootloader:
    bootType: efi
    provider: grub

  packages:
    - initramfs-tools
    # … keep your other packages (see bb example)

  additionalFiles:
    - local: additionalfiles/debian13-bb/hello.sh
      final: /usr/local/sbin/hello.sh
    - local: additionalfiles/debian13-bb/hooks/hello
      final: /etc/initramfs-tools/hooks/hello
    - local: additionalfiles/debian13-bb/scripts/init-bottom/hello
      final: /etc/initramfs-tools/scripts/init-bottom/hello

  configurations:
    - cmd: "chmod 755 /usr/local/sbin/hello.sh /etc/initramfs-tools/hooks/hello /etc/initramfs-tools/scripts/init-bottom/hello"
```

### What each `additionalFiles` entry does


| `local` (your repo)           | `final` (inside the built image)                 | Role                                                                  |
| ----------------------------- | ------------------------------------------------ | --------------------------------------------------------------------- |
| `…/hello.sh`                  | `/usr/local/sbin/hello.sh`                       | Your script on the rootfs; the **hook** copies it into the initramfs. |
| `…/hooks/hello`               | `/etc/initramfs-tools/hooks/hello`               | Runs when `**update-initramfs`** builds the initramfs (pack step).    |
| `…/scripts/init-bottom/hello` | `/etc/initramfs-tools/scripts/init-bottom/hello` | Runs on the **device** during early boot (execute step).              |


- `**local`:** file on the build host (must exist before compose).
- `**final`:** path on the image; ICT copies files here before GRUB install runs `**update-initramfs`**.

Rename `debian13-bb` in `local` paths to match your folder name. Keep the `**final**` paths as shown.

### Field summary


| Template field        | Purpose                                                                           |
| --------------------- | --------------------------------------------------------------------------------- |
| `additionalFiles`     | **Required.** Installs hook, boot script, and your `hello.sh`.                    |
| `packages`            | Include `**initramfs-tools`** (and `**grub-cloud-amd64**` or your GRUB packages). |
| `bootloader.provider` | `**grub**` for this guide (initramfs is rebuilt via `update-initramfs`).          |

---

## Route 2: dracut module (`bb-dracut-raw`)

If you want the same early-boot marker behavior using dracut modules instead of initramfs-tools hooks, use
[debian13-x86_64-bb-dracut-raw.yml](https://github.com/open-edge-platform/image-composer-tool/blob/main/image-templates/debian13-x86_64-bb-dracut-raw.yml).

This variant keeps the same Debian 13 + GRUB + raw image target but changes how content is added to initrd:

| Area | `debian13-x86_64-bb-raw.yml` (initramfs-tools) | `debian13-x86_64-bb-dracut-raw.yml` (dracut) |
| ---- | ----------------------------------------------- | --------------------------------------------- |
| Package focus | `initramfs-tools` | `dracut` and `dracut-core` |
| Files copied by template | `hello.sh`, `hooks/hello`, `scripts/init-bottom/hello` | `modules.d/91hello/module-setup.sh`, `modules.d/91hello/hello.sh`, `modules.d/91hello/initqueue-sample.sh` |
| Destination inside image | `/etc/initramfs-tools/...` and `/usr/local/sbin/hello.sh` | `/usr/lib/dracut/modules.d/91hello/...` |
| Enable step | Hook/script are discovered by initramfs-tools layout | Add `/etc/dracut.conf.d/91hello.conf` with `add_dracutmodules+=" hello "` |

### dracut template snippet

```yaml
  packages:
    - dracut
    - dracut-core
    # ... keep your GRUB/kernel packages

  additionalFiles:
    - local: additionalfiles/debian13-bb-dracut/modules.d/91hello/module-setup.sh
      final: /usr/lib/dracut/modules.d/91hello/module-setup.sh
    - local: additionalfiles/debian13-bb-dracut/modules.d/91hello/hello.sh
      final: /usr/lib/dracut/modules.d/91hello/hello.sh
    - local: additionalfiles/debian13-bb-dracut/modules.d/91hello/initqueue-sample.sh
      final: /usr/lib/dracut/modules.d/91hello/initqueue-sample.sh

  configurations:
    - cmd: 'mkdir -p /etc/dracut.conf.d && echo ''add_dracutmodules+=" hello "'' > /etc/dracut.conf.d/91hello.conf'
    - cmd: "chmod 755 /usr/lib/dracut/modules.d/91hello/module-setup.sh /usr/lib/dracut/modules.d/91hello/hello.sh /usr/lib/dracut/modules.d/91hello/initqueue-sample.sh"
```

### dracut module files

Create the module under:

```text
image-templates/
  additionalfiles/debian13-bb-dracut/
    modules.d/91hello/
      module-setup.sh
      hello.sh
      initqueue-sample.sh
```

`module-setup.sh` declares install logic for the module; `hello.sh` is the script executed from initrd.

Use the provided example files in `image-templates/additionalfiles/debian13-bb-dracut/modules.d/91hello/` as the
reference implementation.

### dracut `initqueue` stage example (`initqueue-sample.sh`)

Use this when you want a script to run in dracut's `initqueue` phase while root-device discovery is still in progress.

In this example module:

- `module-setup.sh` installs `initqueue-sample.sh` into initrd as `/sbin/initqueue-sample.sh`.
- The script is registered in two hook points:
  - `inst_hook cmdline 5 ...` to seed an initial initqueue job early.
  - `inst_hook initqueue 90 ...` to run in initqueue rounds.

Key behavior of `initqueue-sample.sh`:

- Logs markers such as `WAIT_ROOT_EXECUTED`, `WAIT_ROOT_REQUEUE`, and `WAIT_ROOT_MAX_ROUNDS_REACHED`.
- Reads `root=` from kernel cmdline and resolves common forms (`/dev/...`, `UUID=...`, `LABEL=...`, `PARTUUID=...`).
- Requeues itself with `initqueue --onetime` for up to 3 rounds if the root block device is not yet present.
- Adds a settled readiness check with `initqueue --settled /bin/sh -c "test -b \"$ROOTDEV\""`.

Add this file in your template when using the example module:

```yaml
  additionalFiles:
    - local: additionalfiles/debian13-bb-dracut/modules.d/91hello/initqueue-sample.sh
      final: /usr/lib/dracut/modules.d/91hello/initqueue-sample.sh
```

How to verify on boot:

- Check `dmesg` for `WAIT_ROOT_` markers.
- If available in the initramfs runtime, inspect `/run/initramfs/wait-root.log`.

### Why this is different from the ad-hoc `install_items` initqueue approach

You may have seen another pattern where files are copied into initramfs paths and then listed in dracut
`install_items` so they get packed. That method can work, but it is different from this tutorial's approach.

This tutorial uses a **dracut module + template-declared files** approach, which gives you:

- **Reproducibility in ICT**: all inputs are declared in the image template (`additionalFiles`) and tracked as part of
  the image definition, instead of relying on one-off build-host actions.
- **Cleaner dracut lifecycle integration**: `module-setup.sh` defines exactly which hooks are used (`cmdline`,
  `initqueue`, `pre-mount`) and in what order, so behavior is explicit and reviewable.
- **Correct behavior for event-driven initqueue**: dracut `initqueue` is event/timing driven, so a sample script must
  be registered as a hook and be able to requeue/wait for readiness signals; only listing files with `install_items`
  packages content into initrd but does not model this runtime event flow by itself.
- **Better portability across build environments**: module files live in the repo and are copied into the target image
  layout; the flow does not depend on host-specific runtime state.
- **Easier maintenance**: script logic (for example, requeue rounds and root-device checks in
  `initqueue-sample.sh`) stays in one place, rather than being split across ad-hoc file install and pack lists.

In short: both methods can place a script in initrd, but the module-based method used here is preferred for
declarative, version-controlled image builds in ICT.


---

## Build and check

**Build the tool, install prerequisites, validate, and compose the image** using the [README.md](https://github.com/open-edge-platform/image-composer-tool/blob/main/README.md) (Quick Start and *Compose an Image*).

Pick one template based on your chosen route:

- `image-templates/debian13-x86_64-bb-raw.yml`
- `image-templates/debian13-x86_64-bb-dracut-raw.yml`

Run validate if you use it (see [Usage Guide](../get-started/usage-guide.md)). If validate warns about a missing `local` file, fix the path or add the file under [Where to put files in the repo](#where-to-put-files-in-the-repo).

**On the device:**

1. Boot the flashed **raw** image.
2. Use serial console if your template sets `console=ttyS0,...` on the kernel cmdline.
3. Look for your message during early boot, or run: `dmesg | grep -i hello`

Optional checks on a machine with the image mounted:

- initramfs-tools flow: `lsinitramfs /boot/initrd.img-* | grep hello`
- dracut flow: `lsinitrd /boot/initrd.img-* | grep 91hello`

---

## Where to put files in the repo


| Layout                          | `local` in template                                                                     |
| ------------------------------- | --------------------------------------------------------------------------------------- |
| Next to templates (recommended) | `image-templates/additionalfiles/<your-name>/...`                                       |
| Debian OS defaults tree         | `../additionalfiles/...` from `config/osv/debian/debian13/imageconfigs/defaultconfigs/` |


Example tree:

```text
image-templates/
  debian13-x86_64-bb-raw.yml
  additionalfiles/debian13-bb/
    hello.sh
    hooks/hello
    scripts/init-bottom/hello
```

---

## Supporting files (content to create)

### `hello.sh`

```sh
#!/bin/sh
echo "hello from initrd (debian13-bb)" >/dev/kmsg
```

Use `/dev/kmsg` or `logger` so output appears on serial or in `dmesg`.

### `hooks/hello`

Runs during `**update-initramfs**` on the build machine; copies `hello.sh` into the initramfs image.

```sh
#!/bin/sh
PREREQ=""
prereqs() { echo "$PREREQ"; exit 0; }

case "$1" in
prereqs) prereqs; exit 0 ;;
esac

. /usr/share/initramfs-tools/hook-functions
copy_exec /usr/local/sbin/hello.sh /usr/local/sbin/hello.sh
```

### `scripts/init-bottom/hello`

Runs on the **device** in the initrd (default: late in initramfs, before switch to the installed system).

```sh
#!/bin/sh
PREREQ=""
prereqs() { echo "$PREREQ"; exit 0; }

case "$1" in
prereqs) prereqs; exit 0 ;;
esac

if [ -x /usr/local/sbin/hello.sh ]; then
	/usr/local/sbin/hello.sh
fi
```

Make all three executable (`chmod 755`) or use the `configurations` line in the template.

Every initramfs-tools hook and script must start with the `**PREREQ` / `prereqs**` block shown above.

---

## Choosing an initramfs-tools boot stage

The **hook** always runs at **image build** time when the initramfs is generated. To change **when your script runs on the device**, move the runner to a different directory under `scripts/` (and update the template `final:` path).


| `final` path under `/etc/initramfs-tools/scripts/` | When it runs (plain language)                                                     | Good for                                              |
| -------------------------------------------------- | --------------------------------------------------------------------------------- | ----------------------------------------------------- |
| `**init-bottom/hello`**                            | Late initrd, after root handling, before switch_root. **Default in the example.** | Logging, checks before the real OS starts.            |
| `init-premount`                                    | Early, before mounting root                                                       | Very early setup                                      |
| `local-premount`                                   | Before local root mount                                                           | Block device ready, root not mounted yet              |
| `local-bottom`                                     | After local root mount steps                                                      | Work that needs the root filesystem mounted in initrd |


There is no `99` prefix naming rule; the file name (`hello`) is arbitrary.

---

## Troubleshooting


| Problem                            | Check                                                                                                |
| ---------------------------------- | ---------------------------------------------------------------------------------------------------- |
| No output on boot                  | All three `additionalFiles` entries; `chmod 755` on hook and scripts; `initramfs-tools` in packages. |
| Validate / build skips a file      | Wrong `local` path relative to the template YAML.                                                    |
| Script on disk but not in initrd   | Missing `hooks/hello` or hook not executable.                                                        |
| Script in initramfs but never runs | Missing or wrong `scripts/.../hello` path; wrong boot stage directory.                               |
| Wrong image type or bootloader     | This guide targets **Debian 13 raw** with `**bootloader: grub`**.                                    |


---

## Related documentation

- [Custom commands at image build time](configure-additional-actions-for-build.md) — not initrd execution on the device.
- [Image templates](../architecture/image-composer-tool-templates.md) — `additionalFiles` fields and merge behavior.
- [README.md](https://github.com/open-edge-platform/image-composer-tool/blob/main/README.md) — build and compose commands.

