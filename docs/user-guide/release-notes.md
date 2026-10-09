# Release Notes: Image Composer Tool

## Version 2026.3

**Release Date**: TBD

**New**:

- Intel EdgePack platform-enablement images in the Web UI Basic tab. A new **Platform Enablement** use case offers two SKUs — **EdgePack Minimal Desktop** and **EdgePack Minimal Server** — on both Ubuntu 24.04 and 26.04, and on both **Panther Lake** and **Wildcat Lake** for every SKU/OS pair, since the templates behind them support the two platforms identically. All eight combinations build a `raw` image from the EdgePack templates already in `image-templates/` (`ubuntu24|ubuntu26-x86_64-edgepack[-server]-raw.yml`), which install the `intel-edge-base-standard` metapackage — core graphics and media, NPU/IPU enablement, and the platform DKMS drivers — plus the FFmpeg, GStreamer and vPRO manageability add-ons. They are wired into the shipped Basic-tab manifest, so they are available with no extra configuration. **The matching `-unattended-iso` templates are deliberately not offered in the Basic tab**: they declare a sudo-enabled `admin` user whose `admin.pub` is an intentional placeholder with no real key, and ICT refuses to create a privileged account with an empty password — as of the `validate --merged` change noted below, at validate time rather than part-way through a build. Supplying a credential needs `--ssh-authorized-key`, which only the CLI accepts, so those four templates remain CLI-only — build them with `image-composer-tool build --ssh-authorized-key admin=/path/to/key.pub <template>`. Note the raw templates provision no user account and no cloud-init, so the images they produce are intended to be provisioned after deployment. Selecting **Ubuntu 26.04** is also new as a distinct option from the existing **Ubuntu 26.04 Server** target. The Advanced tab's repository picker gains the two repositories these templates introduce: EdgePack's Ubuntu 26.04 (`resolute`) suite, kept separate from the existing `edgepack` entry because that repository's standard base runtime also requires `intel-graphics`, which is not published for 26.04; and the pinned kobuk-team Intel Graphics PPA snapshot the Ubuntu 24.04 desktop template installs its client GPU compute runtime from, kept separate from the rolling `intel-graphics` entry so both remain searchable.

- The Web UI's Basic and Advanced tabs now prompt for a password or SSH public key when a template resolves to a user with neither, instead of letting the build fail minutes into package installation. The rule applies to **every** templated user, not only a sudo or root one: any account would otherwise be created with an empty, unusable login unless its login is a `startupScript` (the installer console account). Either a password or an SSH key satisfies the requirement; a password is hashed on the host the moment it is received and never written to disk, logged, or archived as plain text.
- **Fixed**: `image-composer-tool validate --merged` now fails when a sudo (or passwordless-root) user resolves to no password and no SSH authorized key, instead of validating cleanly and only surfacing the gap deep inside a full build. The four EdgePack unattended-ISO templates (`ubuntu24`/`ubuntu26`, desktop and server) ship `admin.pub` as an intentional placeholder with no real key, so a build against them without `--ssh-authorized-key admin=FILE` now fails at validate time rather than producing an ISO with no way to log in. Both EdgePack server unattended-ISO templates also now pin `systemctl set-default multi-user.target` and drop leftover desktop packages (`firefox`, `language-pack-gnome-en`, `language-pack-gnome-en-base`), so a headless server install stays on a console boot rather than defaulting to `graphical.target` with no display manager to serve it.
- **Fixed**: the Ubuntu 24.04 and 26.04 EdgePack desktop unattended-ISO templates (`ubuntu24-x86_64-edgepack-unattended-iso.yml`, `ubuntu26-x86_64-edgepack-unattended-iso.yml`) built images with no terminal emulator. `ubuntu-desktop-minimal` only *recommends* its terminal (`gnome-terminal` on 24.04, `ptyxis` on 26.04), and ICT does not install recommended packages, so the desktop session had no way to open a shell. The 24.04 template now lists `gnome-terminal` and the 26.04 template lists `ptyxis` explicitly. The server templates are headless and unchanged.
- **Fixed**: the Ubuntu 24.04 and 26.04 EdgePack server unattended-ISO templates now request DHCP with the NIC's MAC address instead of a machine-id-derived DUID. The installed system's `/etc/machine-id` is generated anew on every install, so netplan/systemd-networkd presented a different DHCP client identifier after each re-flash and a lab DHCP server holding a static reservation for the board gave it a pool address instead of its reserved one. Both templates now seed cloud-init with `additionalfiles/base-platform/network-config` (DHCP on `en*` interfaces with `dhcp-identifier: mac`) via `systemConfig.cloudInit.networkConfigFile`, and install `netplan.io` explicitly. `image.version` is bumped to `24.04.1` and `26.04.1` to mark the changed behavior. The desktop unattended-ISO templates are unchanged.
- **Changed**: the `admin` user in the four EdgePack unattended-ISO templates (`ubuntu24`/`ubuntu26`, desktop and server) is now also a member of the `video`, `render` and `audio` groups, in addition to `sudo`, so the account can use GPU render nodes, display/video devices and audio devices without further setup. Any of these groups missing from the image is created during user setup. `image.version` is bumped to `24.04.1` (24.04 desktop), `24.04.2` (24.04 server) and `26.04.2` (both 26.04 templates) to mark the changed behavior.

- **Fixed**: disk partition offsets in image templates are now validated before a build starts or an installation disk is wiped. Positive offsets are measured from disk start; negative offsets such as `-20GiB` are resolved back from the selected disk's end. This supports layouts such as rootfs ending at `-20GiB`, `/data` spanning `-20GiB` to `-4GiB`, and swap spanning `-4GiB` to `0`. Only the final partition may use `end: "0"`; overlapping, reversed, zero-start, and end-relative offsets beyond the selected disk are rejected. Mixed positive/negative overlap checks occur after disk selection, when its size is known, but before wiping. Policy-based disk selection includes the largest absolute and end-relative offsets plus a 1 MiB margin in its minimum-size check. The same resolver serves raw builds and the ISO live installer; an explicit disk path bypasses selection policy but not the pre-wipe layout check.
- **Fixed**: two builds in the Web UI's Compose Image **History** list were indistinguishable when they differed only by SKU. The row label was built from the use case, platform, OS and image type and left the SKU out altogether, so composing both Fed Aero host-OS blueprints — which share everything but their SKU — produced two rows reading `fed-aero / ptl / ubuntu24 / RAW`, with only the relative time to tell them apart. A row now leads with its **SKU** as the title and carries the use case, platform, OS and image type beneath it as subtext, with the relative time on its own line. The values are also resolved to the display names the Basic tab's dropdowns use (`Generic Handheld Blueprint`, `Fed Aero`, `Ubuntu 24.04`) rather than the raw manifest ids the server records, so a row reads in the same vocabulary as the selection that produced it; **a value the manifest no longer lists — a retired SKU on an older build, say — still shows its raw id** rather than disappearing. The platform is the one exception, abbreviated to its acronym (`PTL`, not `PTL (Panther Lake)`): the glossed codename is what a dropdown wants but costs a third of the row's width, enough to truncate the image type off the end. A build that recorded no SKU, which is what a use case offering a single combination does, keeps the use case as its title instead of rendering a blank one, and a build with no configuration summary at all still falls back to its template filename. Both rendered lines truncate rather than wrap, so the full selection — including the platform's full name — is available as a tooltip on the row; the history panel widens slightly to fit the longer titles. Server-side records are unchanged — the SKU was already present in every build's summary and in the Compose details panel, and was only missing from the list label.

- **Fixed**: the Web UI's Compose Status stepper turned every step green — including **Done** — while the compose was still running and the badge still read "Composing...". The phase is derived from the build log, and the `done` phase was matched on the substring `image build completed successfully`. That wording is emitted by the individual image makers too ("Raw image build completed successfully", and the ISO and initrd equivalents), and those lines are logged *before* image compression, the SBOM copy into the image filesystem, and chroot teardown — so the stepper claimed completion with minutes of work left. There is no longer any log marker for `done`: completion is taken only from the build's terminal status, so the stepper reaches **Done** exactly when the badge flips to Succeeded. `Compressing image file` was added as a **Generating image** marker instead, so the final stretch of a RAW build still advances the stepper. The Web UI also ignores a `done` phase arriving on the log stream, so a server emitting one cannot light the stepper early.

- **Fixed**: the unattended installer's "Another unattended installer instance is already running" message misled anyone watching a console other than the one running the install, such as QEMU with `-nographic`. Every console starts the installer and only the first runs it. The others now say so and follow `/tmp/unattended-installer.log` until the installer finishes or fails, so a failure is visible on every console.

- **Fixed**: the Web UI Artifacts table reported every build output as type `IMAGE`, including the SBOM and files that are not build outputs at all. Two causes, both server-side — the table itself always displayed whatever type the API sent. The API's log parser fell back to `image` for any file it did not recognise, so a chroot's leftovers in the image build directory (`bash.bashrc`, `debconf.conf`, `debian_version`, `UPLOAD-MANIFEST.txt`) were listed as images; and its directory scanner, used for history builds and for partial outputs after a failed or cancelled compose, looked for SBOMs ending in `.spdx.json` and so missed the `spdx_manifest_<deb|rpm>_<image>_<timestamp>.json` file ICT actually writes — dropping the SBOM from those views entirely. Both now share one set of naming rules (`internal/utils/artifact`), covering every image format the builder emits (`raw`, `img`, `iso`, `qcow2`, `vhd`, `vhdx`, `vmdk`, `vdi`, `tar`, with optional `gz`/`xz`/`zstd` compression) and both SBOM naming conventions (the create-mode manifest and the overlay `.delta.spdx.json` / `.complete.spdx.json` sidecars). **An output that matches no rule is now reported as `unknown` rather than guessed to be an image** — `unknown` is a new value on the `Artifact.type` enum in the API contract, so a client that handles only `image` and `sbom` should expect it. Separately, the CLI's "Generated Artifacts" summary listed every file in the image build directory; it now lists only the images and SBOMs, which is also what the web API parses.

- **Fixed**: the Web UI Artifacts table listed outputs in whatever order they came off disk, so the SBOM appeared wherever its filename happened to sort. Images are now listed first and the SBOM last. Ordering is applied when a build is read rather than when its artifact list is built, so it also holds for a past build whose recorded order predates this change, and the live completion view and a reloaded history build agree.

- **Changed**: the template a build ran against is now listed in the Web UI Artifacts table, with type `TEMPLATE` and a download action, instead of appearing in the Compose details panel. It sits after the images and before the SBOM, so the SBOM remains the table's last row. For an Advanced-mode build the row follows the template the server keeps: while the build runs it is the generated `extends` delta, and once the build concludes — whether it succeeded, failed or was cancelled — it is the self-contained resolved copy archived in the build directory. The raw `image-composer-tool build …` command line has been removed from that panel altogether — it restated information already shown as the selection and image configuration. The panel now carries only those two summaries, and is not offered at all for a build submitted as raw YAML, which has no configuration summary to show.

- **Changed**: user-facing Web UI copy now uses the British spelling "artefact" ("Artefacts", "Output Artefacts", "Add Artefact", "Download artefact"), including the Disk step's validation messages for that section. This is display text only — the `artifacts` field in the API contract, the `disk.artifacts` template key, and the YAML preview under the Disk step are unchanged, so the Output Artefacts section edits a block still spelled `artifacts:`.

- **Fixed**: the template download (`GET /builds/{id}/template`) returned 404 for any build read back from disk, which is every build after a server restart. `meta.json` never recorded the template's on-disk path, so there was nothing left to resolve once the in-memory record was gone — the download icon in the old details panel was already broken there. The path is now persisted, and a record written before this change falls back to the archived resolved copy at `<build-root>/template.yml`. `BuildDetails` also gained a `templatePath` field so the UI can show where the template lives; it is `omitempty`, and a build against a curated template whose record predates this change reports none rather than guessing.

- **Fixed**: `template-dump.yaml` — the resolved template an installer ISO carries, and the copy left in the image build directory — was thousands of lines long because the per-package SBOM metadata (supplier, checksum, licence, URL for every installed package) was folded into it as a `sbomPackageMetadata` block. It is now a plain template, matching what the web UI shows for the same build. The metadata moved to a `sbom-metadata.yaml` sidecar written beside it, both in the build directory and in the ISO, so the live-installer still emits a full SPDX document for the system it installs. `sbomPackageMetadata` remains a template field, so a build whose input already carries the block inline — a rebuild from a dump written by an earlier release, say — has it moved to the sidecar rather than written back out, and its dump is short too. **An ISO built by an earlier release is unaffected**: the installer reads the sidecar when present and falls back to the template's inline block when it is not.
- **Changed**: MBR is no longer selectable in the Advanced tab's Disk Layout step, and the chip now reads "MBR is not yet enabled." It is shown locked rather than hidden, so a template that already declares `partitionTableType: mbr` still displays its real value — it can be moved to GPT, but not back. The schema and the builder both still accept MBR, so a hand-authored template is unaffected.

- **Fixed**: a template's `metadata` block was dropped from its resolved output. The AI-searchable discovery text (`description`, `use_cases`, `keywords`) that most curated templates open with — it is optional, and a number of shipped templates declare none — never reached `resolve --full` or the Web UI's resolved view, because the template struct had no field for it. It now round-trips through load, merge and marshal. **It still does not inherit**: a child that declares no `metadata` resolves to none rather than adopting its parent's, since the block is read per file for discovery and a child is a different image than its parent describes; a child that declares its own replaces the parent's whole block rather than merging it field by field. Top-level keys are also emitted in the order the curated templates are authored in (`metadata` → `image` → `target` → … → `systemConfig`), where previously `systemConfig` was followed by `packageRepositories` and `metadata` was absent entirely, so a resolved template now reads like a hand-written one.
- Web UI Advanced tab: Edge Pack, a capability view of the package catalog. The Choose Packages to Compose step now offers two browsing surfaces behind a tab switcher — **Edge Pack** and **Repositories** — over one shared selection. A repository answers "where does this package come from"; an Edge Pack domain answers "what does this let the image do", which is usually the question someone composing an edge image actually arrives with. Edge Pack packages are grouped into domains (**Media** and **Manageability**), each domain showing its own selection count and expanding to a list where individual packages can be picked and version-pinned exactly as they can under Repositories. **Only capabilities that are genuinely a choice are domains**: `intel-edge-base-standard` installs the NPU (`intel-edge-npu`), IPU (`intel-edge-ipu`) and `edge-compute-essentials` profiles itself, so none of those is offered as a domain — presenting one would let it be unticked in the UI while the base metapackage installed it anyway. A domain's checkbox selects everything in it; a domain only partly selected shows the mixed (indeterminate) state, as does the pack-level "Domains" checkbox above the grid, so a selection is never rounded up to "all" or down to "none" in the display. Selecting a group skips packages already selected, so a version pinned by hand is not reset to floating latest by a later "select all". A **Base Runtime** selector (Standard / Real-time) sits above the domain grid, independent of it: every domain's packages need a base runtime beneath them, so **no domain — and no individual package within one — can be selected until a base runtime is chosen**, and the step says so rather than leaving a greyed-out checkbox to be puzzled over. The relationship is one-directional: choosing a runtime unlocks the domains, but clearing a domain never clears the runtime, which is an independent choice. The gate blocks adding rather than clearing, so a selection that ends up without a runtime — by clearing the runtime after picking a domain — can always be emptied again, and the step warns that those packages will not build instead of leaving them checked behind a locked control. Real-time is shown but disabled, with its reason stated — there is no shipped package or qualified SKU for it yet. That reason is rendered as visible text rather than left to a tooltip, because a disabled control cannot be focused to reveal one; and because an unavailable runtime is not something the target can build on, selecting Real-time from the Repositories tab does not satisfy the gate either. A domain the selected target OS does not publish is likewise shown-but-locked with an explanation instead of being hidden, and re-enables by itself when the target changes to one that supports it. The pack-level "Domains" checkbox respects those locks: it adds only the domains the target publishes, so it cannot become a way around a domain's own disabled checkbox, and its count is over that same addable set so that "all" stays reachable. It can still clear a locked domain's packages when a target change has left some selected, which would otherwise be unselectable and unremovable both. A domain **or a base runtime** can also declare repositories it needs on top of the pack's own, for a metapackage whose dependencies are published somewhere the pack does not carry: because the standard base carries the NPU profile, selecting that runtime enables the Intel Graphics repository alongside the pack's, and the selector says so before anything is picked — as an expanded domain does for its own prerequisites. Where such a prerequisite is not offered for the selected target, the domain — or the runtime, which locks every domain sitting on it — is shown locked for that stated reason rather than left selectable to fail later; otherwise the choice and the failure would be separated by an entire build. **The two surfaces are one selection, not two.** Selections are keyed by package name, so a package picked on the Edge Pack tab reads as selected when the same package is browsed under its repository and vice versa, it appears exactly once in the right-hand "Selected" rail no matter which surface added it, and picking it enables its repository just as picking a search hit does. Enabling is one-directional: clearing a domain, a package or the base runtime here never switches a repository back off, since it may have been enabled on the Repositories tab for something this tab cannot see. Switching tabs never disturbs what is selected. The grouping is served by a new `GET /edge-pack` endpoint over a new `data/edge-pack.yaml` catalog (overridable at runtime with `serve --edge-pack`, mirroring `--package-repos`), so the domains, their packages and their per-target availability are data rather than code. This is presentation metadata only — no template schema field changes, and an Edge Pack package reaches a build through the same `systemConfig.packages` entry in the generated `extends` delta that a repository-browsed package already does. Version metadata is best-effort: when the pack's repository index cannot be read, packages are still listed and still selectable at latest, losing only the pinnable version chips rather than the tab.

- Web UI Advanced tab: an Edge Pack selection now enables DKMS by itself. Edge Pack's metapackages ship vendor DKMS module sources (`edge-gfx-dkms`, `edge-edac-dkms`, `edge-issei-dkms`, pulled in transitively by `intel-edge-base-standard`), and those sources are only compiled against the image's target kernel if the template asks for it — so as soon as the selection contains **any** Edge Pack package, the generated `extends` delta emits `systemConfig.dkms.enabled: true`. An Edge Pack image whose modules were never built is not a configuration anyone would pick deliberately, so there is no UI control for it; whether one should be offered is left for later. A version-pinned pick (`intel-edge-npu_1.2.3-0intel1`) counts the same as an unpinned one — the pin selects a build, not a different package. `dkms.modules` is deliberately left unset, so ICT runs `dkms autoinstall` for every module the installed packages registered rather than asserting a list of module/version identifiers this catalog has no way to know; Secure Boot module signing is likewise not declared, since it needs real key material the Web UI cannot supply. A selection with no Edge Pack package emits **no** `dkms` block at all rather than `enabled: false`: `extends` merging treats the section as a wholesale overwrite whenever a child declares it, so an always-emitted block would silently disable DKMS on a curated parent template that had turned it on. See `image-templates/ubuntu24/ubuntu24-x86_64-edgepack-raw.yml` for the hand-authored equivalent.

- Intel EdgePack template for Ubuntu 26.04: `image-templates/ubuntu26/ubuntu26-x86_64-edgepack-raw.yml`, the resolute counterpart of the existing `ubuntu24-x86_64-edgepack-raw.yml`. It uses the same native building blocks — a `packageRepositories` entry for the signed EdgePack repository, EdgePack metapackages in `systemConfig.packages`, and the typed `systemConfig.dkms` section that builds the vendor DKMS modules against the image's target kernel rather than the chroot's build-host kernel. The EdgePack repository is pinned to the `resolute` suite instead of `noble`, and there is a single repository entry rather than two: NPU, IPU and edge-compute-essentials enablement is carried by `intel-edge-base-standard` itself on both 24.04 and 26.04, so neither template selects a separate `intel-edge-npu` package or its prerequisite repository. Both templates pin their kernel to `linux-image-7.0.0-34-generic`/`linux-headers-7.0.0-34-generic` rather than tracking the floating `linux-image-generic-hwe-24.04`/`-26.04` metapackage; `systemConfig.dkms` rebuilds the vendor modules against that exact kernel. The pin is a known-good workaround for a kernel API compatibility break in the GFX DKMS module against the newer `7.0.0-38` HWE kernel — EdgePack has been asked to confirm a fix or official support for `7.0.0-38` and later. Both templates are built in CI by the `Build EdgePack Images` workflow (`.github/workflows/build-edgepack.yml`); a successful build confirms EdgePack's `edge-edac-dkms`, `edge-gfx-dkms`, `edge-issei-dkms`, and `edge-mei-dkms` modules build and install against the pinned kernel inside the chroot.

- Minimal Ubuntu server + EdgePack templates for 24.04 and 26.04: `image-templates/ubuntu24/ubuntu24-x86_64-edgepack-server-raw.yml` and `image-templates/ubuntu26/ubuntu26-x86_64-edgepack-server-raw.yml`. These are headless counterparts of the two `*-edgepack-raw.yml` templates above — same EdgePack package set (`intel-edge-base-standard`, `intel-edge-media-ffmpeg`, `intel-edge-media-gst`, `intel-edge-manageability`) and the same pinned kernel, but adding `ubuntu-server-minimal` instead of `ubuntu-standard`/`ubuntu-desktop-minimal` on top of the `ubuntu-minimal` raw-image default, with no use-case-specific provisioning. Both are schema-validated; the 24.04 server template has also been built end-to-end and confirmed to produce working DKMS modules.

- **Fixed**: both Ubuntu 26.04 EdgePack templates (`ubuntu26-x86_64-edgepack-raw.yml`, `ubuntu26-x86_64-edgepack-server-raw.yml`) failed in pre-processing because they requested `python3.12-venv`, which does not exist on 26.04 (resolute ships Python 3.14). All four EdgePack templates, 24.04 and 26.04, now request the version-agnostic `python3-venv`, which resolves to `python3.12-venv` on noble and `python3.14-venv` on resolute. On 24.04 the interpreter-specific venv implementation remains `python3.12-venv`, though the generic `python3-venv` metapackage is also installed. With that fixed, the 26.04 builds then failed linking `edge-gfx-dkms`'s `xe.ko` with `No space left on device`: the finished 26.04 desktop image already uses about 4.9 GB, which leaves too little of the 6 GiB disk's root partition for the in-image DKMS build objects. Both 26.04 EdgePack templates now use a 12 GiB disk; the 24.04 templates keep 6 GiB. A new `Build EdgePack Images` workflow builds all four templates on pull requests that touch them, the Ubuntu OS config, the Ubuntu provider, DKMS/imageos code, the Debian package resolver, or the chroot code, and uploads the missing-package report when a build fails.

- **Fixed**: `internal/image/imageos/dkms.go`'s post-build DKMS verification treated the `original_module` pseudo-version directory DKMS creates when it backs up an in-tree driver it displaced (e.g. `igen6_edac`, `xe`, `virtio-gpu`, `mei*` — all upstream in-tree on current Ubuntu kernels) as if it were a real built source, and failed the whole build looking for a nonexistent `dkms.conf` under it. Any DKMS-enabled template whose target kernel already ships an in-tree module of the same name — both EdgePack templates included — could hit this. `original_module` is now skipped when walking the DKMS build tree.

- Unattended ISO provisioning settings. An `imageType: iso` template can now
  install a fully configured system hands-free, applied identically by raw
  builds and by the live installer. See the
  [Unattended ISO Installer Tutorial](./get-started/unattended-iso-provisioning.md)
  and [ADR: Provisioning Settings for the Unattended ISO Installer](../architecture-decision-record/adr-unattended-iso-provisioning.md).
  - `systemConfig.users[].sshAuthorizedKeys` / `sshAuthorizedKeysFiles` install
    SSH public keys with correct ownership and modes.
  - `systemConfig.proxy` persists `HTTP_PROXY`, `HTTPS_PROXY`, `FTP_PROXY`, and
    `NO_PROXY` into `/etc/environment`, apt, and the systemd default environment.
  - `systemConfig.provisioning.scripts[]` runs user scripts from generated
    systemd units at first boot or every boot, in a configured order.
  - `systemConfig.cloudInit` installs cloud-init and seeds it through a pinned
    NoCloud datasource.
  - `systemConfig.aptPolicy.upgradeAllowedRepos` limits which template
    repositories may upgrade installed packages. Ubuntu/Debian archives keep
    upgrading unless the image is immutable, in which case nothing upgrades.
  - `systemConfig.bootloader.bootEntryPolicy` (`preserve` or `exclusive`): the
    installer now sets the firmware `BootOrder` explicitly with the installed
    disk first.
  - Partition `start`/`end` accept offsets from the end of the disk
    (`end: "-20GiB"`), so root can fill a disk of any size while fixed-size data
    or swap partitions follow it.
  - ISO builds now keep the SPDX SBOM of the installed system and a
    `<image>-<version>.composition.json` manifest next to the ISO.
  - `build` flags `--disk-strategy`, `--hostname`,
    `--http-proxy`, `--https-proxy`, `--ftp-proxy`, `--no-proxy`, and
    `--ssh-authorized-key USER=FILE` override the matching template fields.
  - New unattended ISO templates `ubuntu24-x86_64-edgepack-unattended-iso.yml`,
    `ubuntu24-x86_64-edgepack-server-unattended-iso.yml`,
    `ubuntu26-x86_64-edgepack-unattended-iso.yml` and
    `ubuntu26-x86_64-edgepack-server-unattended-iso.yml`. Each installs the
    package set, EdgePack repository and DKMS modules of the matching
    `*-edgepack[-server]-raw.yml` template, with the kernel pinned to 7.0.0-34
    because `edge-gfx-dkms` 7.0 does not build against 7.0.0-38, and a
    `default-initrd-unattended-x86_64.yml` installer environment for Ubuntu 26.04.

- **Changed**: `systemConfig.users[].shell`, `home`, and `passwordMaxAge` are now
  applied; previously every account got `/bin/bash` and the default home. `home`
  applies only to accounts the build creates: an account already in the image,
  such as `root`, keeps its home. A
  `password` that is a crypt(3) hash is always set as a hash, even without
  `hash_algo`. `systemConfig.hostname` must be an RFC 1123 host name.
  A user without a `password` but with SSH keys now has its password locked, so
  it can log in only with those keys; if it also has sudo access (`sudo: true` or a `sudo`/`wheel` group), it is granted
  passwordless sudo, since a locked password cannot answer the sudo prompt. Such a user with neither a
  password nor an SSH key now fails the build instead of being created with an
  empty password. Four shipped templates define such a user:
  `ubuntu24-x86_64-minimal-unattended-iso.yml`,
  `generic-handheld-os-template.yml`,
  `generic-companion-os-server-template.yml` and
  `ubuntu24-x86_64-generic-handheld-os-desktop-raw.yml`. The
  four new `*-edgepack[-server]-unattended-iso.yml` templates need one as well,
  because their `admin.pub` holds only comments. For any of them, set `password`, or build
  with `--ssh-authorized-key USER=FILE`.

- **Fixed**: `packageRepositories` are now turned into apt sources and
  preferences for `debian` targets, as they already were for Ubuntu and eLxr.
  Previously the Debian provider requested this but the target check skipped
  it, so `aptPolicy` and immutable pinning never applied to Debian images.

- **Fixed**: generated `/etc/fstab` lists partitions in template order and skips
  non-swap partitions without a mount point. Previously such a partition
  produced a malformed fstab line.

- **Changed**: the live installer removes only firmware boot entries labelled
  exactly `ICT` (previously any label containing `ICT`), resolves
  `/dev/disk/by-*` target paths to the kernel device, and uses the
  architecture's removable EFI loader path (`BOOTAA64.EFI` on aarch64).

- **Changed**: immutable Ubuntu and Debian images now include APT pins that stop
  package upgrades. Build commands inherit the host's `no_proxy` and `ftp_proxy`
  in addition to `http_proxy` and `https_proxy`.

- **Changed**: ISO builds pass `-iso-level 3` to xorriso, so additional files
  larger than 4 GiB can be carried on the ISO. Additional files are stored on
  the ISO in a directory per in-image destination, under their source
  basename, so two files with the same basename no longer overwrite each other
  and a destination that is an existing directory (such as `/etc`) still
  receives `/etc/<basename>` as in a raw build. ISO prerequisite validation now also checks the
  ISO template's own `additionalFiles`, resolving relative paths exactly as the
  build does.

- Fed Aero host-OS blueprints in the Web UI Basic tab. The two generic host-OS templates from [`edge-node-infrastructure-blueprint`](https://github.com/open-edge-platform/edge-node-infrastructure-blueprint/tree/release-2026.2.0/infrastructure/host-os/ict) `release-2026.2.0` are now vendored into `image-templates/ubuntu24/` and selectable from the Basic tab: `generic-handheld-os-template.yml` (handheld/desktop) and `generic-companion-os-server-template.yml` (companion OS server). Both target Panther Lake on Ubuntu 24.04 and build a `raw` image. Both are wired into the shipped Basic-tab manifest (`internal/api/service/data/manifest.yaml`) under the Fed Aero vertical, so they are available out of the box with no extra configuration. **The `Edge Node Infrastructure Blueprint BKC` SKU has been removed from the Fed Aero vertical**, so Fed Aero now offers exactly these two blueprints on Ubuntu 24.04 (the grayed-out `Drone Image - From BKC Team` placeholder on Ubuntu 26.04 Server is unchanged). Its template, `ubuntu24-x86_64-minimal-ptl-pv-raw.yml`, remains in `image-templates/` and can still be built directly from the CLI — only the Basic-tab entry is gone. Note that `generic-handheld-os-template.yml` was **updated in place** to the `release-2026.2.0` revision — its swap partition grows from 3073MiB to 5121MiB, and it now installs the 6.18 Intel kernel and media stack from a pinned snapshot of the Intel edge overlay (`download.01.org/edge-linux-overlay`) instead of the rolling `intel-linux-overlay` suite, so images built from it differ from earlier releases. Upstream ships `<USERNAME>`/`<PASSWORD>` placeholders in both templates' `users:` block; these are filled with this repo's convention (`user`, empty password, set at deploy time) so the templates validate and build as shipped — set real credentials before deploying. The Advanced tab's repository picker gains the two repositories these blueprints introduce: the pinned Intel edge overlay snapshot and the Ubuntu MozillaTeam PPA.

- **Changed**: when package installation or DKMS module builds fail in a DKMS-enabled image, ICT preserves available `make.log` files before cleaning up the chroot. Logs are saved under `imagebuild/<systemConfigName>/dkms-logs/`, retaining their DKMS subdirectory paths.

## Version 2026.2

**Release Date**: September 9, 2026

**New**:

- Web UI Advanced tab: editable Disk Layout step. Step 3 of the Advanced wizard was a placeholder; it is now a working editor for the resolved template's `disk` block — disk name and size, GPT **or** MBR partition table, and add/remove/reorder/resize of partitions, seeded from the template the current selection resolves to. The partition table shows `name`, `fsLabel`, `fsType`, size, `mountPoint`, `start` and `end` as labelled columns, with `id`, `index`, `type`, `typeUUID`, `mountOptions` and `flags` behind a per-row details toggle — every field the schema defines round-trips, whether or not it is prominent. Partitions can be edited two ways, switchable per disk: **size-based** (the default) takes one size per partition and lays them out contiguously, calculating `start`/`end` for you — the schema stores offsets and has no size field, so this keeps them from drifting out of sync; **offset-based** takes the offset strings directly, deriving sizes, for layouts that need a deliberate gap or must match an existing table offset-for-offset. Switching converts the layout in place, and an unedited layout emits identical YAML either way. Gaps and overlaps in offset mode are reported as warnings, not blocked — the builder accepts both. The last partition can take the remainder (`end: "0"`) in either mode. The step also edits `disk.artifacts[]` — the output-format list (`raw`, `qcow2`, `vhd`, `vhdx`, `vmdk`, `vdi`, `tar`, with optional compression) — which is where ICT produces QCOW2 and the other container formats; `target.imageType` controls how the image is built and is a separate axis. Field values are checked against what the **builder** accepts, not just against the schema — the schema types nearly every Disk and Partition field as an unconstrained string, so the rules live in the implementation. Fields with a closed set are dropdowns (`fsType`, partition `type`, and the artifact format/compression); free-text fields show the expected format as a placeholder and are validated as you type. Values that would fail the build are errors, values that would degrade quietly are warnings — an unrecognised partition `type`, for instance, leaves the partition at a default type rather than failing, and is flagged as such. **Output artifacts now offer only combinations that can actually be produced for the selected image type:** RAW and overlay images get `raw`/`qcow2`/`vhd`/`vhdx`/`vmdk`/`vdi` with optional `gz`/`xz`/`zstd`; WSL2 requires exactly one `tar` + `gz` artifact; ISO and IMG do not run the artifact pipeline at all and say so instead of offering a dead control. Note this is narrower than the schema's own enums, which include `tar`, `gzip` and `bz2` — none of which the builder implements on the disk path. Constraints the template loader enforces outside the JSON schema are reported inline too (`extendLastPartitionToFillDisk` is rejected for `imageType: iso`, and for `raw` unless the last partition is the rootfs). **The edited layout is what gets built.** Once you change anything on the step, the disk block is sent as `disk` on `POST /templates/compose` and `POST /builds`, and the backend emits it into the same generated `extends` delta the image-name and package overrides already use — so the Review step's "Your changes" and "Resolved" views, and the image the build produces, all come from one file. An untouched layout is not sent: it round-trips to the template's own disk block, so sending it would generate a delta that changes nothing. Because `extends` merging replaces `disk` wholesale rather than merging it field by field, the override is always the complete block, never a diff. See [ADR: Advanced mode Disk step](../architecture-decision-record/adr-web-ui-disk-step.md).

- Kernel wildcard now installs a single kernel: a `systemConfig.kernel.packages` entry that uses a glob (for example `linux-image-generic*`) previously matched every `linux-image-generic*` metapackage in the Ubuntu 24 repositories and silently installed several kernels (6.8 GA plus 6.11/6.14/6.17/7.0 HWE tracks) in the same image. The build now installs only the **newest** matched kernel and logs a warning listing every match, so a template using a broad glob still boots with a single, up-to-date kernel; pin an exact metapackage in `systemConfig.kernel.packages` to install a different one. Explicitly listing multiple exact kernel packages is unchanged — the selection only applies to a single glob pattern that resolves to multiple kernels. The behavior applies to the Debian-family providers (Ubuntu, eLxr, Debian 13).

1. **Overlay kernel replacement** (`overlayPolicy.replaceKernel`)

   Overlay builds on a GRUB2 baseline can now **swap the kernel** rather than
   only adding one alongside the baseline's. The functionality depends on the
   following settings:

   - `overlayPolicy.replaceKernel.package: <kernel-package>` installs the named
     kernel (resolved from the configured repositories, and the value may use
     the same glob wildcards — `*`, `?`, `[...]` — as an ordinary `systemConfig`
     package, e.g. `linux-image-*-oem`) and removes the baseline kernel
     **family** — the bootable image plus its meta-package, modules, and headers
     (`linux-image-*`, `linux-image-generic`, `linux-modules-*`,
     `linux-headers-*`; rpm `kernel`/`kernel-core`/`kernel-modules`) — so the
     emitted image ships **only** the new kernel.

   - `replaceKernel.additionalPackages` (a list, same rules as `package`)
     installs further kernel-family packages alongside it — typically the
     matching `linux-headers-*` — following the same resolve/removal path.
   -`replaceKernel.enableExtraModules` (space-separated module names, mirroring
     `systemConfig.kernel.enableExtraModules`) forces driver modules into the
     replacement kernel's regenerated initramfs (`dracut --add-drivers`, or an
     `initramfs-tools` modules-file entry).
   - `replaceKernel.version` is descriptive only, surfaced in the compose API
     summary.

   The removal set is auto-detected from the baseline inventory
   (userspace packages such as `linux-libc-dev`, `linux-tools-common`, and
   rpm `kernel-headers`/`kernel-devel` are kept) and removed as one batch so
   no kernel package is left orphaned.

   The GRUB config is then regenerated so the removed kernel's menu entry is
   dropped and `GRUB_DEFAULT` points at the new kernel (auto-pinned to `"0"`
   unless `overlayPolicy.grubDefault` is set). Only the GRUB **config** on the
   writable root changes — the ESP and the bootloader binary are never touched
   (`grub-install` is never run), preserving the overlay read-only-ESP
   contract; on a Secure Boot baseline the new kernel may be unsigned
   (sign it out of band). `replaceKernel` requires
   `packageOperation: additive-and-upgrade` and, being self-authorizing for its
   kernel-family removals, does **not** require `allowPackageRemoval`; it is a
   hard error — raised at preflight, before any package is installed or
   removed — on a non-GRUB2 baseline (including a UKI baseline).

   See [`image-templates/ubuntu24/ubuntu24-x86_64-overlay-replace-kernel-raw.yml`](https://github.com/open-edge-platform/image-composer-tool/blob/main/image-templates/ubuntu24/ubuntu24-x86_64-overlay-replace-kernel-raw.yml)
   for an example. This supersedes the previous restriction (see 2026.1)
   that in-place kernel-image replacement was always blocked.

2. **Overlay feature**

   The overlay feature enables composition of a final system image by installing
   additional packages on top of an existing RAW or QCOW2 base image, rather
   than building an image entirely from scratch. It is supported for
   Ubuntu 24.04 and Debian 13 images.

   - **Key benefits**:

     - **Significantly reduced build time:** Composing a full image from scratch
       by listing all packages can take approximately 1.5 hours. Using an existing
       base image and applying only the delta packages reduces composition time
       to approximately 8 minutes.
     - **Upgrade and additive operations only:** Package additions and upgrades
       are supported. Removing packages from the base image is not supported
       in this release.
     - **Root filesystem resize:** The root filesystem is resized to accommodate
       newly installed packages based on user inputs for the maximum allowed root
       filesystem size and maximum disk size.

   - **Base Image Access for Overlay Composition**

     Base images can be accessed through the following methods:

     - **URL:** The base image is fetched directly from a remote location.
     - **Local file path:** The base image is referenced from a directory on
       the host system.

   - **Supported Base Image Types**

     | Base image type | Format |
     | --- | --- |
     | Canonical cloud images | QCOW2 |
     | Existing BKC (Best Known Configuration) images | RAW |
     | ICT-composed images | RAW |

   - **Additional benefits include**:

     - Significantly reduced overall image composition time by eliminating the
       need to resolve and install a full package list from scratch.
     - Simplified template creation by requiring only delta packages instead of a
       comprehensive list of all packages.

   ICT continues to fully support composing minimal images from scratch for all
   POR (Plan of Record) OS distributions.

3. **Post-boot root filesystem (rootfs) resize**

   Support has been added to grow the root filesystem after the first boot on
   the target device. This allows the image to remain at a minimal size during
   distribution and storage, with the filesystem expanding as needed upon boot.

4. **Template extensions: multi-level support**

   ICT now supports multi-level template extensions, enabling modular and
   layered composition of system images.

   **Benefits include**:

   - Cleaner separation of concerns through layered templates.
   - Easier collaboration across teams.
   - Reduced maintenance overhead.
   - Simplified debugging of template configurations.

5. **Debian 13 with custom initrd and graphical desktop environment**

   Debian 13 images can now use a customized initrd, providing greater
   flexibility in early boot configuration. The images can also boot into a
   graphical desktop environment with GDM over X11.

   `debian13-x86_64-bb-dracut-raw.yml` also ships a sample first-boot
   systemd oneshot unit (`first-boot-sample.service`): a script that runs
   once, on the device's first boot only, gated by a marker file, with its
   message mirrored to the journal, a log file, `dmesg`, and the serial
   console.

6. **Full Disk Encryption (FDE) for RAW images**

   This release supports selectively encrypting disk partitions in RAW images
   with user-specified passphrases in the user template for encryption and
   decryption. Sealing encryption keys in a TPM is not supported.

7. **Image composition for WSL environments**

   The tool can now compose Ubuntu images compatible with WSL environments.

8. **Intel EdgePack platform-enablement template**

   Adds `ubuntu24-x86_64-edgepack-raw.yml`, demonstrating native support for
   Intel's EdgePack platform-enablement packages (Panther Lake / Wildcat Lake)
   via `packageRepositories`, `systemConfig.packages`, and the new typed
   `systemConfig.dkms` section, which builds and verifies DKMS modules against
   the installed target kernel rather than the chroot's build-host kernel.

**Validated hardware**:

- **Target platform**: Panther Lake (PTL)

- **Reference Templates**:

  | Feature | Reference template |
  | --- | --- |
  | Overlay and root filesystem resize | `ubuntu24-x86_64-overlay-raw.yml` in `image-templates/ubuntu24/` |
  | Post-boot root filesystem resize | `ubuntu24-x86_64-minimal-raw-expand-partition.yml` in `image-templates/ubuntu24/` |
  | Template extensions | `ubuntu24-x86_64-extends-example-raw.yml` and `ubuntu24-x86_64-minimal-raw.yml` in `image-templates/ubuntu24/` |
  | Debian 13 custom initrd with overlay | `debian13-x86_64-bb-graphics-raw.yml` and `debian13-x86_64-bb-overlay-initrd-raw.yml` in `image-templates/debian13/` |
  | Debian 13 monolithic robotics | `debian13-x86_64-bb-dracut-raw.yml` in `image-templates/debian13/` |
  | Ubuntu 24 robotics templates | `ubuntu24-x86_64-robotics-hw-overlay-qcow2.yml`, `ubuntu24-x86_64-robotics-jazzy-overlay-extends.yml`, and `ubuntu24-x86_64-robotics-jazzy-iso.yml` in `image-templates/ubuntu24/` |
  | Intel EdgePack platform enablement | `ubuntu24-x86_64-edgepack-raw.yml` in `image-templates/ubuntu24/` |

**Fixed**:

- `fix(imagedisc)`: bound sfdisk calls and detach stale loop devices before reattach: `createPartitionTable`'s `sfdisk` calls had no execution timeout, so a wedged `sfdisk` (e.g. blocked behind a stale loop-device handle from a hard-killed prior build) could hang a build indefinitely. Both `sfdisk` invocations are now bounded to a 30s context so a hang fails fast into the existing retry-with-force path instead of blocking forever. Loop-device attach is also now idempotent: before `losetup`, any existing loop device already bound to the same backing file (including one whose backing file was since deleted) is detached first, removing the actual trigger that could wedge the kernel's partition-table re-read on a freshly attached device.
- `fix(debutils)`: support APT repositories that publish only a combined `InRelease` file instead of a detached `Release`/`Release.gpg` pair. Builds against such repositories (including EdgePack's public repository) previously failed to fetch metadata; the format is now auto-detected and verified either way, and offline rebuilds correctly keep using the previously detected format instead of reverting to the missing classic files.
- `fix(debutils)`: correctly resolve dependencies on a versioned virtual `Provides:` (e.g. Debian's Qt6 ABI-pinning packages) by comparing against the version the provider actually declares for that capability, not the provider's own unrelated package version. An unversioned `Provides:` no longer incorrectly satisfies a versioned dependency either.
- `fix(shell)`: `configurations` commands containing multi-line scripts or shell metacharacters (`$()`, backticks, `$var`) now reach the chroot unmodified instead of having their whitespace collapsed or being partially expanded by the outer shell before execution.

**Known Issues**:

- **Custom partition layouts with the overlay feature are not supported**:

  The tool does not support user-specified disk partition layouts in the output
  image. The base image partitions are passed through to the RAW or QCOW2 output image.

- **SBOM generation for base images without an embedded SBOM**:

  If the base image does not contain an embedded Software Bill of Materials (SBOM),
  the resulting image generates an SBOM only for the additionally installed
  packages. Packages from the base image are not included in the SBOM output.

- Web UI Advanced tab: cross-repository package search and browsing: The Choose Packages to Compose step now lists real packages instead of just the repository catalog. A search box queries every repository the target offers at once (gated to 2+ characters, matching the backend's own minimum, since an empty query means "browse the whole catalog"), and picking a result auto-enables its source repository. A two-pane browser lets a single repository be explored directly, paginated 25 rows at a time via `GET /packages/search`. Each row shows the package's real version and a `Latest` / pinned-version chip, and a running "Selected" rail groups everything added so far by repository, with per-item and clear-all removal; disabling a repository drops whatever was added from it. Selections are scoped to the current target OS — changing it (not just the SKU or platform) resets both the enabled repositories and the selected packages. See the entry below for how those selections reach the template.

- Web UI Advanced tab: "Show frequently used" curation and "Select all" for the repository browser: The two-pane browser's page size is raised from 25 to 100, and a new "Show frequently used" checkbox narrows the list to each repository's hand-picked curated packages — a new optional `curatedPackages` field in `package-repos.yaml`, with `GET /packages/search` gaining a matching `curated` query parameter. Curated picks are defined for every repository whose index is reachable: the three base-OS repos (`build-essential`, `ca-certificates`, `curl`, `network-manager`, `openssh-server`, `python3`, `sudo`, `systemd`, `vim`), the ROS 2 Jazzy and Lyrical repos (`ros-*-desktop`, `ros-*-ros-base`, `ros-*-rviz2`, `ros-dev-tools` and friends, cutting ~8400 message and driver packages down to the metapackages an image actually starts from), Gazebo, Docker CE, Mozilla, Intel Graphics, Intel Linux Overlay and RealSense. The curated names themselves stay server-side — the browser sends `curated=true` and the server filters — but `GET /package-repos` now reports a `hasCuratedPackages` flag per repository, and the browser **hides** the "Show frequently used" checkbox where a repository has no curated picks (the six Intel repositories whose indexes are not currently reachable) instead of offering a toggle that could only empty the list. A "Select all" checkbox above the list adds every currently-shown package to the selection (skipping any already selected, so a manually pinned version is left alone) and removes them again when unchecked.

- Package search no longer re-downloads indexes it already holds: The picker's index cache was capped at 24 indexes, but a catalog repository expands to one index per (suite, component, architecture) — `ubuntu-noble-base` alone needed 24, and a target's full repository set around 50. Every cross-repository search therefore evicted the indexes the next browse would ask for. Measured against the real binary, browsing a repository straight after a search re-downloaded 39 MB and took 90 seconds; it now answers from cache in 0.01s. The cap is raised to 128 indexes, with the 600,000-package budget still bounding memory. Alongside it, only the target's own architecture is fetched: a catalog entry commonly lists one architecture per publishing host (Ubuntu serves x86_64 from `archive.ubuntu.com` and aarch64 from `ports.ubuntu.com`), and the aarch64 index — 18 MB that an x86_64 target can never install from — was being downloaded on every cold search. Together these take a cold cross-repository search from 30 seconds to 5.7, with the first results shown at 0.4s, and a repeat search to 0.02s.

- Package search offers every available version of a package, not just one: A Debian-style repository publishes its release, `-updates` and `-security` suites as separate indexes, so a package routinely exists at several versions — `curl` on Ubuntu 24.04 is available as both `8.5.0-2ubuntu10` and `8.5.0-2ubuntu10.13`. Search and browse previously reported these as unrelated rows and the picker showed whichever one happened to load first. Each package is now listed once, with every version it was found at offered as a chip (newest first, the rest behind "+N more"), and `GET /packages/search` gained a `versions` array on each result carrying the version and the repository providing it. Choosing a version pins that exact string and also selects the repository it comes from, so a package available from two repositories at different versions resolves unambiguously; `Latest` instead follows whatever the repository publishes next. Ordering uses the target's own version rules rather than string comparison, so `8.5.0-2ubuntu10.13` correctly outranks `8.5.0-2ubuntu10.9` and an epoch outranks a higher upstream version. `limit` and `total` now count packages rather than versions.

- Package search returns results as they arrive, instead of waiting for the slowest repository: A cross-repository search fans out over every repository a target offers, so its latency was set by whichever mirror was slowest to answer — an unreachable one took its full TCP dial timeout while packages already found elsewhere sat unsent, leaving the search box showing "Searching…" for around 30 seconds. A new `GET /packages/search/stream` endpoint emits each repository's matches as Server-Sent Events the moment that repository responds, and the search box fills in progressively: first results now appear in about 2-4 seconds. The stream closes after a fixed budget rather than tracking the slowest repository, and the dropdown states plainly when a search could not cover everything ("Searched 14 repositories — 6 of 14 repositories unreachable"), so a partial result is never passed off as the whole answer. Repository fetch failures are now also briefly cached (2 minutes, against 6 hours for a success), so one dead mirror is not redialled on every keystroke — a repeat search over a warm cache completes in well under a second. `GET /packages/search` is unchanged and remains the endpoint for paging through one known repository; the stream is not paginated.

- Optional repository catalog rebalanced across targets: `intel-oneapi` and `mozilla-apt` are distribution-neutral and are now offered to every target (previously ubuntu24 only), and debian13 and ubuntu26-server each gained the Debian/Ubuntu-26.04 equivalents of repositories already offered for ubuntu24 (`docker-ce-ubuntu-resolute`, `gazebo-resolute`, `ros2-lyrical`). Also fixes `docker-ce`'s Debian index, which pointed at `bookworm` (Debian 12) for a debian13 target; it now points at `trixie`, Debian 13's own suite.

- Web UI Advanced tab: selected packages now reach the template and the build: What the Choose Packages to Compose step selects is no longer UI-only state. `POST /templates/compose` and `POST /builds` accept `packages` and `repos`, and the Advanced tab's generated `extends` delta gains a `systemConfig.packages` entry per selected package — using the `name_version` form the package resolvers match for a pinned version (`curl_8.5.0-2ubuntu10.13`), or the bare name to float to whatever the repository publishes — plus a `packageRepositories` entry per enabled repository, so a package picked from a repository the curated template does not already configure still resolves. Base repositories are not re-declared; they already come from the per-OS `providerconfigs/`. Each catalog repository now carries the same signing-key URL (`pkey`) the authored templates use for it, so an Advanced-mode build verifies the repository exactly as a hand-written one would, and `GET /package-repos` reports a `hasSigningKey` flag per repository. The Review step's package count and YAML update as packages are added, and the build installs them.
  The Review step now presents the template in three views — **Your changes** (the generated delta alone), **Base template** (the pre-authored template it extends) and **Resolved** (the two merged, which is what the build runs) — served by new `deltaYaml` and `baseYaml` fields on the compose response; a selection with no changes shows only the resolved template as before. Because template inheritance **combines** package lists rather than replacing them, pinning a version for a package the pre-authored template already lists unpinned leaves both entries in the resolved template; the compose response reports these on `pinConflicts` and the Review step names them, rather than presenting an ambiguous package list as settled.

**Fixed**

- Web UI package search no longer accepts a `Release` signed by an unrecognised key: the picker's GPG verification (independent of the CLI build path's own `debutils`/`rpmutils` verification) treated a signature made by a key absent from the configured keyring as a pass, logging a warning but returning no error — silently accepting a forged `Release` for the two repositories that carry a local keyring and are otherwise fetched over plaintext `http://` (`ubuntu-noble-base`, `ubuntu-resolute-base`). The keyring is now the sole trust anchor: a signature from any key it does not list is rejected, for both the armored (`InRelease`) and binary (`Release.gpg`) detached-signature forms.

- Web UI Advanced tab: base repositories no longer show a false "No signing key is configured" warning: `hasSigningKey` checked only for a `pkey` URL, which the Ubuntu base repos never carry — enabling one never emits a `packageRepositories` entry in the first place, since base repositories already come from the per-OS `providerconfigs/`. It now also reports true when the repository carries a local `gpgKeyPath`, which is what the picker's own search already verifies the repository's `Release` against, so `ubuntu-noble-base` and `ubuntu-resolute-base` no longer warn about a verification gap they don't have.

- Web UI Advanced tab: the repository browser's rail now lists base (always-enabled) repositories first, ahead of the optional ones, instead of interleaving them by priority.

## Version 2026.1

**Release Date**: June 17, 2026

**New**:

- Overlay cascade removal of orphaned reverse-dependencies: When `overlayPolicy.allowPackageRemoval` is enabled, a conflict-driven removal that orphans an unrelated baseline package (one that only `Depends:` on the removed package — for example the Debian cloud image's `cloud-initramfs-growroot` depending on `initramfs-tools`) is now resolved automatically instead of failing the build. The post-install dependency audit becomes a bounded cascade: each baseline package that is broken *after* a removal but was whole *before* it is itself removed, transitively, until the package manager's own check (`apt-get check` / `dnf check`) reports the dependency tree is whole again. The package manager's audit is the ground truth, so a dependency that an alternative still satisfies is never mistaken for breakage and nothing is over-removed. The cascade still fails **closed**: if resolving the breakage would require removing a bootloader/kernel-image package or a package the overlay is installing, the build fails. Cascade removals are folded into the preflight report's approved removals, surfaced on `InstallResult.CascadeRemoved`, and reflected in the OVERLAY PACKAGE STATISTICS summary and the complete SBOM. The behavior is entirely gated by the existing `allowPackageRemoval` flag — there is no new schema field, and with the flag off a removal that would orphan another package still fails the build.

- Overlay user provisioning with a baseline-conflict guard: Overlay builds now honor `systemConfig.users`, provisioning each account onto the baseline using the same implementation as create mode (useradd, password/hashing, groups, sudo, startup script). A requested user that **already exists in the baseline image fails the build up front** — before any resize or package install mutates the baseline — because an overlay cannot redefine a baseline account. A user's `startupScript` must reference a path present when users are created (shipped by the baseline or installed by an overlay `packages` entry), not one delivered via `additionalFiles`, which are copied later in the overlay pipeline. `systemConfig.users` is therefore no longer rejected as an unsupported overlay section.

- Robotics image composed from Canonical's cloud image via overlay + extends: the hardware base layers Intel oneAPI, Level Zero, NPU, RealSense DKMS, patched systemd/udev, and the ROS 2 Jazzy child layers OpenVINO, Gazebo Harmonic, and collaborative SLAM. The child inherits the base's baseline, policy, repositories, and disk settings. The overlay is build-verified with a 64 GiB target disk; Debian installation uses size-bounded dependency-ordered batches and retries only failed batches, while repository metadata caches are isolated per concrete package-list URL. The final Jazzy overlay emits a valid QCOW2 artifact. The resize path needs util-linux >= 2.38 because it reads partition start sectors via `lsblk -o PATH,START,TYPE`, with a `parted` fallback.

- Graceful cancellation on Ctrl+C / SIGTERM: interrupting a build (SIGINT or SIGTERM) now triggers cooperative cleanup before the tool exits. Chroot bind mounts (`/proc`, `/sys`, `/dev/{pts,shm}`, `/run`, and the cache-repo bind) are torn down in reverse order, loop devices attached to files under the work directory are detached, and every spawned child process (bash, sudo, mmdebstrap, apt, mksquashfs, losetup, mkfs.*, xorriso, dracut, ukify, sbsign, qemu-img, …) runs in its own process group so a single kill reaches the whole subtree. In-flight HTTPS downloads of DEB/RPM packages and repository metadata (Go-level `net/http` requests) also observe the same cancellation context, so a signal during the download stage aborts within one retry-backoff quantum instead of running to completion. The tool exits with the conventional exit code `130` after user-initiated cancellation. A second signal during cleanup is a hard exit (also `130`) so a wedged umount cannot pin the process forever. Internal deadlines (such as the 2-minute PostProcess cleanup budget) that exceed their limit surface as exit `1` — distinguishable from a user-initiated signal — so operators can tell "aborted by me" from "cleanup timed out". Any residual mount/loop that could not be reaped is logged with detail so the operator knows exactly what to clean up manually. No user-visible flag changes; the behavior is on by default.

- Overlay `additionalFiles` support: Overlay builds now honor `systemConfig.additionalFiles`, copying each host file into the baseline root at its `final` path (mirroring create-mode behavior for the Ubuntu and Debian overlay providers). The copy runs as the **last** build step — after both initramfs and GRUB regeneration — so a prebuilt boot artifact such as a custom `/boot/initrd.img-*` lands after regeneration instead of being overwritten by `update-initramfs`. Files that must instead be *consumed by* regeneration (for example initramfs-tools hooks under `/etc/initramfs-tools/`) still belong in a `systemConfig.configurations` command that runs the generator, which executes earlier in the pipeline. Unlike create mode, overlay does not auto-inject apt source/preferences/GPG files into `additionalFiles` (overlay installs from prepared artifacts, not live apt repositories), so only user-authored entries are copied.

- Overlay kernel command line & GRUB2 regeneration: Overlay builds on a GRUB2 baseline now apply `overlayPolicy.kernelCmdline` (a full-line replacement of `GRUB_CMDLINE_LINUX` in `/etc/default/grub`) and the optional `overlayPolicy.grubDefault` (a full-line replacement of `GRUB_DEFAULT`, to pin the default boot menu entry — e.g. an Ubuntu submenu path for an overlay-added flavored kernel), then regenerate the GRUB configuration with the baseline's native tool (`update-grub` / `grub-mkconfig`) after the initramfs is rebuilt. A kernel added by the overlay gets a boot menu entry automatically. The bootloader binary and the read-only ESP are never modified (`grub-install` is never run); regeneration failures fail the build so no image is emitted; a best-effort warning is logged when a Secure Boot baseline has no signing material. Bootloader- and kernel-image *replacement* remain blocked by overlay preflight.

- Overlay package removal & SBOM sidecars: Overlay builds gain two hardening features. `overlayPolicy.allowPackageRemoval` (default off, and only valid with `packageOperation: additive-and-upgrade`) permits removing a baseline package that an added package conflicts with (for example removing `initramfs-tools` so `dracut` can install); bootloader and bootable-kernel packages are never removed, removals are shown in the OVERLAY PACKAGE STATISTICS summary, and a removal that leaves an unrelated baseline package with an unmet dependency fails the build (later relaxed into a bounded cascade — see the cascade-removal note above). Overlay builds also emit SPDX SBOM sidecars next to the image: a **delta** SBOM (`<image>-<version>.delta.spdx.json`, always written) listing the overlay-contributed packages, and — when a base SBOM is available (an inherited `/usr/share/sbom` inventory or an external `baseline.source.sbomPath`) — a **complete** SBOM (`<image>-<version>.complete.spdx.json`) with the full baseline+overlay inventory. The initramfs generator is selected by what the baseline actually ships (dracut vs update-initramfs) rather than by package-manager family. Unsupported `systemConfig` sections in an overlay template (`hostname`, `network`, `initramfs`, `kernel`, `immutability`, `fde`, `bootloader`) now fail the build up front instead of being silently ignored, and overlay builds no longer inherit the create-mode OS default configuration (disk size/partitions, bootloader, kernel, and base packages come from the baseline image).

- Overlay `additionalFiles.stage` marker & inspection CLI change: A new per-file `additionalFiles.stage` field controls WHEN an overlay copies a file relative to boot/initramfs regeneration. The default (`stage: ""`, or omitted) keeps the historical behavior — the file is copied at the end of the build, after regeneration. Set `stage: pre-initramfs` to copy the file BEFORE initramfs/boot regeneration so the generator can consume it (e.g. a dracut module or an initramfs-tools hook that must be baked into the initramfs). The marker is overlay-only; create-mode builds ignore it. Separately, the overlay `--inspect` flag now defaults **off** (it previously defaulted on): when set, the post-build inspection report is written to a `<image>-<version>.inspect.txt` sidecar in the build artifacts directory instead of the console, and when unset nothing is written. The now-redundant `--no-inspect` flag has been removed — inspection is off by default, so scripts that passed `--no-inspect` to disable it should simply drop the flag.

- Overlay emits every build-mode output format: Overlay builds now honor `disk.artifacts` the same way create-mode builds do, so requesting `qcow2`, `vhd`, `vhdx`, `vmdk`, `vdi`, or `tar` (with optional compression) produces those formats instead of silently emitting only a `.raw` file. The overlay always assembles a RAW image internally, then converts it to the requested formats after the post-build RAW inspection; a template whose `disk.artifacts` omits `raw` deletes the intermediate RAW during conversion, and a build with `disk.artifacts` empty or listing only `raw` is unchanged (plain RAW, no conversion step).

- Build from scratch with `--no-cache`: The `build` command now accepts a `--no-cache` flag that runs the build in fresh, unique cache and workspace directories (ignoring any existing caches) and removes them once the build finishes. The final image is copied into the configured `work_dir` beforehand. `--no-cache` cannot be combined with `--cache-dir` or `--work-dir`.

- Template `extends` inheritance: User templates now accept an optional `extends:` field pointing at a parent template. The parent is resolved relative to the child's directory, and the chain (root → intermediate levels → leaf, up to 4 recommended levels) is folded together before OS defaults are applied — using the same per-section merge rules the two-layer user↔default merge already uses (packages additive+deduped, users merged by `name`, `additionalFiles` merged by `final` path, `disk` replaced wholesale, `kernel`/`bootloader`/`network` per-field, and so on). Cycle detection, target-match enforcement, path-containment guards, and symlink rejection all apply, and the resolved chain is logged at info level during builds. Use `image-composer-tool resolve TEMPLATE.yml` to inspect the chain-merged result before building. See [Template Extends (Inheritance)](./architecture/image-composer-tool-templates.md#template-extends-inheritance) for the full reference.

- `resolve` subcommand for template debugging: A new `image-composer-tool resolve <template.yml>` command prints the merged image template as YAML to stdout, so contributors can see exactly what the tool sees before running a build. By default the extends chain is folded without OS defaults; passing `--full` additionally merges the OS defaults, producing the exact template that would be built. Sensitive fields (user passwords, hash algorithms, and secure boot key/cert/cer paths) are always redacted in the output, and the merged view is computed on demand and never cached.

- ARM64/aarch64 cross-architecture image builds: Ubuntu 24, eLxR 12, and AZL3 images can now be composed on an x86_64 host targeting ARM64. The builder validates host-side prerequisites (arch-test, qemu-user-static), normalizes architectures for `mmdebstrap` and `dpkg`, and forces a host-side ukify execution when the host and target architectures differ.

- Ubuntu 24 ARM64 bootable server image: Added a user template and supporting configuration to produce a bootable Ubuntu 24 `aarch64` server image.

- Ubuntu 26.04 LTS (Resolute Raccoon) support: New OS target and associated configuration for Ubuntu 26.04.

- eLxR Edge 26.04 / eLxR 13 support: New OS provider, image configuration, and user templates for eLxR 13 (elxr-edge-26.04) raw image builds.

- Debian 13 user templates: New raw image template and Desktop Virtualization (IDV) ISO installer template for Debian 13.

- ROS 2 Jazzy robotics templates: New AMR raw image template and a companion ISO installer template for ROS 2 Jazzy edge robotics platforms.

- PTL PV attended and unattended ISO templates: New attended and unattended ISO installer templates for PTL (Platform Validation Toolkit) PV (Para-Virtual) configurations including cloud-init example configuration files.

- Unattended ISO installer with policy-based target disk selection: `live-installer` now supports fully automatic installation using a `selectionPolicy` block in the disk template section. Supported strategies: first, largest, fastest (prefers NVMe over SSD over HDD), and largest-free (selects the disk with the most unallocated span). Removable and externally attached disks are excluded by default and can be included explicitly with `excludeRemovable: false`.

- Declarative network configuration in image templates: A new `systemConfig.network` section defines network interfaces at image composition time. It supports `systemd-networkd` and `netplan` backends, configures DHCP, static IP/CIDR addresses, default gateways (via routes), and DNS nameservers per interface.

- Network configuration view in attended ISO installer: The attended (interactive) ISO installer now includes a "Configure Network" step that allows selecting an interface and entering DHCP or static IP/gateway/DNS settings before installation.

- Local package repository population via `packageRepositories` section: The `packageRepositories` schema now accepts a package list whose entries are HTTPS URLs (downloaded at build time) or local file/directory paths (copied). Archives (.tar, .tar.gz, .tgz, .zip) are extracted for their .deb/.rpm payloads. The `path` field is optional when `packages` is set. A temporary directory is auto-created and cleaned up. An optional `insecureSkipVerify` flag allows skipping TLS certificate verification for downloads from environments with self-signed certificates.

- Full offline/cache mode for DEB and RPM repositories: DEB Packages.gz metadata is now cached by SHA-256 checksum (`packages.parsed.json`) under `cache_dir/` and reused on rebuilds with no network access. RPM `primary.xml` metadata and `primary.location.json` are cached under `cache_dir/rpm-metadata/`. Debian repository GPG keys are cached in `cache_dir/gpg-keys/`. Repository file-existence check results and package-list URLs are cached in-process per run to eliminate redundant HEAD requests.

- DKMS module installation: Package resolution now uses a target-name-aware candidate filter (`filterCandidatesByPriorityWithTarget`) that prefers exact-name matches over Provides virtual package matches, preventing kernel packages that provide a DKMS module name from being selected instead of the actual DKMS package.

**Improved**:

- RPM package cache: `DownloadPackagesComplete` now checks for a valid local cache before contacting the repository. If all required packages are present, no network request is made. Only the missing packages are re-fetched, preserving existing cached files.

- DEB package cache: `DownloadPackages` performs a staleness check against the local `.deb` cache (by name) before downloading. Version-pinned requirements and epoch-prefixed package names are matched correctly.

- Chroot environment package isolation: The chroot-build tool package cache and the initrd package cache are now stored in dedicated subdirectories (`chrootenv/` and `initrd/` respectively) to prevent the stale-cache check from evicting image packages when the two sets do not overlap.

- Chroot cleanup error handling: `CleanupChrootEnv` and `UmountChrootSysfs` now accumulate all cleanup errors rather than short-circuiting on the first failure. All partial errors are surfaced in the returned error.

- Mount rollback on failure: `mountDiskToChroot` and `MountSysfs` now roll back previously mounted paths when a later mount step fails, preventing orphaned bind mounts.

- Loop device cleanup: `LoopSetupDelete` now detects and disables any SWAP partitions on the loop device before calling `losetup -d`, preventing detach failures caused by active swap.

- Loop device error cleanup on creation failure: If loop device creation fails but a partial loop device path is returned, `BuildRawImage` now detaches it immediately rather than leaking the resource.

- Disk partition creation reliability: `createPartitionTable` now retries wipe (`wipefs`) and `sfdisk` commands in separate loops with a 30-second timeout each, verifying via `lsblk/sfdisk` that the expected state is actually reached before proceeding.

- Grub command detection in install root: `getGrubVersion` and `updateGrubConfig` now resolve grub binaries by checking known absolute paths in the install root (`/usr/sbin/`, `/usr/bin/`) before falling back to shell `command -v`. `update-grub` is now also accepted as a valid fallback.

- `apt-get` install with `--no-install-recommends`: DEB package installation in the chroot environment now passes `--no-install-recommends`, reducing unnecessary package pulls.

- sudo suppressed when already root: `GetFullCmdStr` detects when the process is already running as root (`euid == 0`) and omits the redundant inner `sudo` prefix from both chroot and host commands. ICT is launched as root (`sudo -E image-composer-tool build ...`, or the server's `sudo -n ...`), so an inner `sudo` is a root-to-root no-op that only forks an extra process per command; dropping it also avoids permission-escalation errors in CI environments that run as root. When the process is not root the prefix is kept so the per-command sudo model still elevates.

- Partition mount-point path resolution: `resolveInstallRootMountPoint` is now the single canonical function for joining the install root and partition mount points. It handles empty, /-absolute, and relative mount-point strings uniformly.

- Default installer partitioning mode: The attended ISO installer now starts in manual partitioning mode by default; partition template state is cleared when entering manual mode to avoid stale configuration.

- Installer startup scripts hardened: `attendedinstaller` and `unattendedinstaller` shell scripts replaced with `set -euo pipefail`, standardized quote handling, and `[[...]]` conditionals for more robust error propagation.

- Dual GPG key per repo for RPM EMT distro: RPM-based EMT repositories now support a second GPG public key (`pkeys` list), enabling repositories that require two signing keys.

- Boot partition label in EMT-EMF template: Explicit partition labels added to the boot partition.

- `systemd-resolved` enabled at startup for RCD: RCD image builds now enable and start `systemd-resolved` as part of post-install configuration.

- `intel-dlstreamer / OpenVINO` version alignment for RCD: Fixed version mismatch between `intel-dlstreamer` and `openvino` in RCD templates. `intel-dlstreamer` is pinned to 2025.2.0.

- `ukify` lookup paths: `shell.go` now searches additional known installation prefixes for `ukify` so builds on distributions that install it in non-standard locations do not fall back to host-side execution unnecessarily.

- Progress bar terminal output: A trailing newline is now emitted after progress bars finish (`VerifyDEBs`, `VerifyAll`, `FetchPackages`) to prevent the next log line from overwriting the progress bar.

- `CopyDir` empty-source handling: Fixed glob pattern from `/*` to `/.` so that copying an empty source directory does not produce a shell error.

- RPM dependency graph (`PkgName`): `GenerateDot` now uses the `PkgName` field for node names in dependency graphs, producing clean package names instead of raw filenames.

- Network schema validation: IPv4/IPv6 CIDR addresses, gateway addresses, and nameservers in `systemConfig.network` are now validated against typed formats in the JSON schema; DHCP and static addresses cannot be combined on the same interface.

- Debian 13 graphics template ships a desktop terminal and GUI installer: The `debian13-x86_64-bb-graphics-raw.yml` template now adds `gnome-terminal` and `gnome-software` on top of its GNOME desktop stack (`gdm3` + `gnome-session` + `gnome-shell`). Previously the composed desktop had no terminal application in the Activities overview and no graphical way to browse or install packages, because `gnome-shell`/`gnome-session` do not pull those in (only the larger `gnome-core`/`gnome` metapackages do). Both packages merge additively under the template's inherited `additive-and-upgrade` overlay policy; the CLI `apt` is unchanged and already present.

- Image templates grouped by distribution: `image-templates/` is now organized into one subdirectory per `target.dist` (`azl3/`, `debian13/`, `el10/`, `elxr12/`, `elxr13/`, `emt3/`, `ubuntu24/`, `ubuntu26/`) instead of a single flat listing of 60 files. Filenames are unchanged, so `image-templates/ubuntu24-x86_64-minimal-raw.yml` becomes `image-templates/ubuntu24/ubuntu24-x86_64-minimal-raw.yml`. **If you reference a template by path in a script or automation, add the distribution directory.** Templates packaged into the `.deb` under `/usr/share/ict/examples/` gain the same subdirectories. Distribution is the grouping used because an `extends:` chain must be siblings in one directory and must share `os`/`dist`/`arch`/`imageType`, so a distribution directory can never split a valid chain. New guides ship alongside the templates: `image-templates/README.md` (catalog), `COMPOSITION.md` (`extends:` and overlay mode) and `CONVENTIONS.md` (naming), plus a `README.md` per distribution.

- Templates composed with `extends` instead of duplication: Several templates now inherit a base template rather than restating it. `emt3-x86_64-emf-raw.yml` and `emt3-x86_64-dlstreamer.yml` extend `emt3-x86_64-edge-raw.yml`, and `emt3-x86_64-emf-rt-raw.yml` extends `emt3-x86_64-emf-raw.yml`. This removes a 41-package block that had been copied verbatim into four EMT3 templates. Each derived template was verified with `resolve --full` to produce the same functional fields as before. Note that because package lists are a union with no removal syntax, a derived template also installs its parent's packages. And because the three EMT3 templates now inherit `emt3-x86_64-edge-raw.yml`, they also inherit its three sample repositories (`company-internal`, `dev-tools`, `intel-openvino`), so `emf-raw` and `emf-rt-raw` resolve to three repositories where they previously declared none and `dlstreamer` resolves to six rather than three. Those entries are inert — their URLs are the literal placeholder `<URL>`, which `rpmutils` skips before fetching, and EMT3 is RPM-based so apt-source generation never runs for it — so the built image is unchanged. Each of the three templates notes this in its header.

**Fixed**:

- `fix(debian13)`: drop stale kernel version pins from Debian 13 OS defaults: Debian 13 repositories no longer provide kernel version `6.12.74`, so inherited ISO, initrd, and raw configurations (x86_64 and aarch64) failed during package resolution. Removing the pin lets each architecture's kernel metapackage — `linux-image-amd64` on x86_64 and `linux-image-arm64` on aarch64 — select the current repository kernel.

- Templates in subdirectories are now discovered: template scanning walked only the top level of the templates directory and skipped subdirectories, which would have hidden every template from the AI/RAG index and the web UI template list once templates were grouped into per-distribution directories. The scan is now recursive, as are the `image-composer-*` Copilot skill scripts.

- `elxr-cloud-amd64.yml` additional files were silently dropped: the template referenced `files/etc/...` while the files ship in `elxr-cloud-amd64/files/etc/...`, so all four `additionalFiles` entries failed to resolve and were skipped with a warning rather than being copied into the image.

- Drifted RealSense apt pin in the robotics raw template: `ubuntu24-x86_64-robotics-jazzy-raw.yml` pinned `librealsense2` where its ISO counterpart pinned `librealsense2*`, leaving the RealSense sub-packages unpinned in raw images. Both templates now use the glob.

- `fix(config)`: drop stale `kernel.version` pin from the ubuntu24 OS defaults: ubuntu24 builds failed in pre-processing with `kernel version mismatch: requires kernel version "6.17", but available versions are: [6.8.0-31.31 7.0.0-28.28~24.04.1]`. Four default configs under `config/osv/ubuntu/ubuntu24/imageconfigs/defaultconfigs/` pinned `kernel.version: "6.17"` alongside the rolling metapackage `linux-image-generic-hwe-24.04`, and Ubuntu noble no longer ships 6.17. Any template without its own `kernel.version` inherited the stale pin, so `ubuntu24-x86_64-minimal-initrd.yml` and `ubuntu24-x86_64-dkms-demo.yml` failed even though the templates themselves were already clean. #765 removed this antipattern from the 12 affected user templates but did not cover the OS defaults, which is why the failure recurred; `image-templates/robotics-demo-ubuntu24-x86_64.yml` was also missed there and is fixed here. Dropping the pin lets `apt` resolve whatever the metapackage currently points to — no pin, nothing to go stale. Templates that pin a concrete kernel package (for example `linux-image-6.11.0-17-generic`, `linux-image-6.12-intel`) are unaffected.

- `fix(ubuntu)`: `AllowPackages` not propagated to debutils.Repository (#480): The `allowPackages` list in user-provided package repository configuration was silently dropped instead of being passed through to the DEB package resolver.

- `fix(inspect)`: ext4 filesystem misdetection in image inspect (#484): The image inspect command was incorrectly classifying some ext4 partitions as a different filesystem type.

- Fixes for error logs when building UKI (#485): Spurious or incorrect error log entries emitted during UKI image construction were corrected.

- `fix(templates)`: pin intel-dlstreamer to 2025.2.0 (#492): `intel-dlstreamer` in `eLxR/RCD` templates was not version-pinned, causing uncontrolled version updates.

- `fix(templates)`: kernel version metadata 6.14 → 6.17 (#494): Template metadata version field for Ubuntu 24 kernels corrected to match the actual installed kernel series.

- `fix(templates)`: pin ubuntu24 edge kernel to noble GA (6.8) (#761): The `ubuntu24-x86_64-edge-raw` and `ubuntu24-aarch64-edge-raw` templates pinned `kernel.version: 6.17` with `linux-image-generic-hwe-24.04`, a combination Ubuntu noble no longer ships — only `6.8.0-31.31` (GA) and `7.0.0-28.28~24.04.1` (HWE-edge) are available. Pin the kernel to the noble GA combination (`6.8` + `linux-image-generic`) so the `build-ubuntu24-immutable` CI job can complete.

- `fix(templates)`: drop stale `kernel.version` pin across ubuntu24 metapackage templates (#765): Every ubuntu24 template that combined a `kernel.version` string with a rolling metapackage (`linux-image-generic-hwe-24.04`, `linux-image-generic`, `linux-image-generic*`) was one HWE roll away from the same class of CI break that #494, #669, and #761 fixed. Drop the `kernel.version` line from those templates so `apt` resolves whatever the metapackage currently points to — no pin, nothing to go stale. Templates with an intentionally-pinned concrete kernel package (e.g. `linux-image-6.11.0-17-generic` in `ubuntu24-server-cloud-amd64.yml`) are unaffected.

- RPM DOT file naming bug (#538): `GenerateDot` used the raw filename (e.g., `glibc-2.38-16.azl3.x86_64.rpm`) as a node label instead of the canonical package name (`glibc`), producing incorrect dependency graphs.

- Swap partition cleanup before loop device detach (#568): Building images that include a swap partition would fail at teardown because the loop device was busy. The swap partition is now detected and disabled with `swapoff` before `losetup -d`.

- Ubuntu 24 ARM64 minimal raw template boot partition type: The `xbootldr` partition in `ubuntu24-aarch64-minimal-raw.yml` had an incorrect `fsType: vfat`. It is now corrected to `ext4`.

- Local DEB repo path in chroot: `initDebLocalRepoWithinInstallRoot` used an incorrect path separator for the `/cdrom/cache-repo` mount point inside the chroot, causing package installation failures.

- Deferred cleanup of local DEB repo: De-initialization of the local Debian repository inside the install root is now performed via a defer statement, ensuring cleanup happens even when package installation fails midway.

- `fix(scripts)`: remove Intel-internal proxy from repository configuration (#561): An Intel-internal proxy URL was hardcoded in repository configuration, causing failures in external environments.

**Known Issues**:

- Unattended ISO installer is a first-pass implementation: The unattended installer (`ubuntu24-x86_64-minimal-unattended-iso.yml`) does not yet support all advanced partition layouts (e.g., `LVM`, `LUKS`). Complex partition schemes must use the attended installer or a custom startup script.

- ARM64 cross-architecture builds require host tools: Builds targeting aarch64 from an `x86_64` host require arch-test and qemu-user-static installed on the build host. The builder will detect and report missing dependencies but does not install them automatically.

- Loop devices not destroyed when image building is terminated abruptly: When the image build process is terminated abruptly using `ctrl-C`, loop devices created just prior to `ctrl-C` are not removed automatically. The loop devices must be manually removed by the user.

## Version 1.0

**Release Date**: December 12, 2025

**Features**:

- Support for building OS images with Intel® specific OOT Kernel packages.
- Support for building Wind River eLxr 12 images.
- Support for adding multiple Debian package repositories, e.g., Intel® and OSV.
- Ability to set priority for repositories to manage conflicts.
- Ability to prioritize specific packages to manage conflicts.
- Caching for consistent and faster composition.
- Debian repository GPG keys are now cached in `cache_dir/gpg-keys` and reused on rebuilds to avoid re-downloading.
- RPM repository metadata is now cached in `cache_dir/rpm-metadata` and reused on rebuilds to avoid network fetches.
- Native support for Debian and RPM based distributions.
- Support for building immutable OS images with DM-Verity and read-only file
  system support.
- Generation of signed OS images using provided keys for Secure Boot.
- Support for Unified Kernel Image (UKI) with systemd over UEFI BIOS or
  Legacy BIOS.
- Verbose and filtered logging based on severity to provide easy troubleshooting.
- User-defined OS image configuration.
- Seamless support for AI software stacks -
  [Edge AI Libraries](https://docs.openedgeplatform.intel.com/2025.2/ai-libraries.html)
  in user space of the OS distribution.
- Support for composing the OS images to include ECG Sample Apps.

**Known Issues/Opens**:

- Installation from ISO images on NVMe SSD and via USB is not functional on
  RPL platforms.
- Face Detection and Recognition application output video is not
  displayed locally.
- Support for building Ubuntu OS images is being considered.
