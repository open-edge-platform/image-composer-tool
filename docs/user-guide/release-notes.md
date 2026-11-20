# Release Notes: Image Composer Tool

## Version 2026.3

**Release Date**: TBD

**New**:

1. **Web UI for composing images**

   `image-composer-tool serve` serves a browser UI for composing images, at
   <http://127.0.0.1:8080> by default. Run it from a checkout, or with only
   Docker installed (item 2). See the
   [Web UI Quick Start](./get-started/web-ui-quick-start.md).

   - **Server**: `serve` takes `--host`, `--port`, `--templates-dir`,
     `--work-dir` and `--ict-binary`; `--manifest`, `--package-repos` and
     `--edge-pack` replace the shipped Basic-tab template list, repository
     catalog and Edge Pack catalog. With `--sudo`, builds run through `sudo`;
     `serve --print-sudoers` prints the rules they need and
     `scripts/install-sudoers.sh` installs them.
   - **Proxy**: `serve` passes its own `HTTP_PROXY`, `HTTPS_PROXY`,
     `FTP_PROXY` and `NO_PROXY` to each build as `--http-proxy`,
     `--https-proxy`, `--ftp-proxy` and `--no-proxy`, so an image built behind
     a proxy gets a matching `systemConfig.proxy` and first-boot provisioning
     can reach the network. These override a template's own proxy and are not
     passed for overlay templates. Proxy credentials are redacted in the logs.
     The generated sudoers rules keep only these variables, and only for the
     build command; only a variable that is set is forwarded.
     `scripts/build-and-serve-webui.sh` removes `NO_PROXY` entries that would
     send public Intel repository traffic around the proxy, logging each one
     it drops, and reads other always-direct hosts from `EXTRA_NO_PROXY`.
   - **Basic tab**: pick a Use Case, SKU, Platform and Operating System, all
     required, to build a curated template (a Kernel choice appears only when
     the template list offers kernel variants). Use cases include
     **Platform Enablement** (the EdgePack SKUs in item 6 and the Nova Lake
     **Upstream BKC** SKU in item 7) and **Fed Aero**, which offers the two
     generic host-OS blueprints on Ubuntu 24.04 for Panther Lake. Choices
     without a template yet are shown as *(coming soon)*.
   - **Users and credentials**: **Compose Image** opens a **Users and
     credentials** dialog on both tabs.
     - It asks for a password or SSH public key for every template user that
       has neither, except one whose login is a `startupScript`, instead of
       letting the build fail minutes in.
     - **+ Add user** adds an account with the options a template can set:
       password and/or SSH key, sudo, groups, shell, home directory, startup
       script and password maximum age. An added account needs a password or
       SSH key unless it is confined to a startup script, and a name the
       template already declares is refused, so a login supplied for an
       existing account never changes its privileges.
     - Passwords are hashed on the host as soon as they are received and are
       never written to disk, logged or archived in plain text. Logins are
       asked for again for every build.
     - When **Compose Image** is unavailable, the dialog names what is
       missing.
   - **Advanced tab**: start from the Basic selection and change the image
     name, packages and disk layout in four steps (Target, Packages, Disk,
     Review). The changes are written to a generated `extends` template, so
     the curated template is never changed. The step indicator turns Target
     green once its fields are filled, Packages and Disk once something is
     changed there, and a step red when its values will not build. **Next**
     stays disabled until the required fields are filled, with a note naming
     what is missing; required fields are marked with a red asterisk.
   - **Packages step, Repositories tab**:
     - Search every repository the target offers at once. Results appear as
       each repository answers, and the list says when some repositories
       could not be reached. Package indexes are cached, and every
       repository's index starts downloading when the step opens.
     - Browse one repository at a time, picked from the **Choose repository**
       dropdown, which groups repositories under *Currently in template* and
       *Available (not in template)*. Every repository the template installs
       from through its `extends` chain is listed and in use, including ones
       the catalog does not carry, such as a vendor snapshot, labelled by host
       and path. An optional repository comes into use when you add a package
       from it and is released when its last package is removed; ✓ marks and
       an *N of M repositories in use* count show which are in use.
     - Each package is listed once, with every version it is available at as
       a chip, newest first by the target's version rules. Choosing a chip
       pins that version; choosing the pinned chip again unpins it. A selected
       or template package is tagged **Pinned** or **Unpinned**, followed in
       bold by the version a build would install: for **Unpinned**, the one
       the build picks, from the highest-priority repository first and then
       the newest, with *via* naming the repository when it is not the one
       being browsed. A template package whose version you change is tagged
       **Modified**.
     - **Show only selected (N selected)** lists what is picked from the
       browsed repository, including the template's own packages that it
       publishes, with all their versions. **Select all on this page** adds
       every package shown.
     - **Update to latest version** pins every package added from the browsed
       repository, and the template's own packages that it publishes, to the
       newest version it offers. A template pin whose version that repository
       does not publish, such as a vendor build, is left as it is, and a note
       names the repository it comes from. Nothing changes if the repository
       cannot be read, or if the template's packages would take the selection
       past the 256 packages a build accepts.
     - The **Selected** panel lists the packages added or changed in this
       tab under **You are adding**, grouped by the repository each resolves
       from.
     - Each catalog repository carries the signing key the shipped templates
       use for it, so an Advanced build verifies it as a hand-written template
       would. Package search accepts a repository's `Release` only when it is
       signed by a key in that repository's keyring.
     - The optional repository catalog is per target. `intel-oneapi` and
       `mozilla-apt` are offered for every target, and Debian 13 and Ubuntu
       26.04 Server have Docker CE, Gazebo and ROS 2 Lyrical repositories.
   - **Packages step, Edge Packs tab**: browse Intel Edge Pack packages by
     capability rather than by repository.
     - Choose a **Base Runtime**, then domains (**Media**, **Manageability**)
       or individual packages in them, pinned the same way as on the
       Repositories tab. No domain can be selected until a base runtime is
       chosen. **Standard** is available; **Real-time** is shown, with the
       reason it cannot be selected yet.
     - A collapsible **Included with Standard Edge Pack Profile** list shows
       what the runtime's metapackage installs, read from the repository
       index so it always matches the build.
     - A runtime or domain the matched template already installs is shown
       checked and locked. One the target does not publish, or whose
       prerequisite repositories are not offered for it, is shown locked with
       the reason.
     - Both tabs share one selection. Any Edge Pack package turns on
       `systemConfig.dkms.enabled` in the generated template, so its DKMS
       modules are built for the image's kernel (see item 6).
     - Ubuntu 24.04 and 26.04 each use their own EdgePack repository. Edge
       Packs are not offered for overlay templates.
     - When the Edge Pack repository is slow, the tab waits at most 3 s and
       lists the packages without versions until its index arrives.
   - **Disk step**: edit the disk name and size, and add, remove, reorder and
     resize GPT partitions, either by size (offsets are calculated for you)
     or by offset, including offsets from the end of the disk
     (`end: "-20GiB"`). The output artifacts can be `raw`, `qcow2`, `vhd`,
     `vhdx`, `vmdk` or `vdi`, with optional `gz`, `xz` or `zstd` compression,
     for RAW images, and exactly one `tar` + `gz` artifact for WSL2. Values
     are checked against what the builder accepts: overlapping partitions are
     errors and gaps are warnings, and the 1 MiB the builder keeps before
     end-relative partitions is counted. MBR is shown but not yet
     selectable. The **Partitions** and **Output Artifacts** sections are
     collapsed by default, with a one-line summary. For an overlay template,
     the baseline's partition layout is kept, and only the size, maximum size
     and output artifacts can be changed. See
     [ADR: Advanced mode Disk step](../architecture-decision-record/adr-web-ui-disk-step.md).
   - **Review step**: an **Advanced customization** card lists what the build
     changes: the image name, packages added, repositories enabled, the disk
     layout, users added and users given a login (names only). **Export
     YAML** downloads the generated template.
   - **Compose Image tab**:
     - Follow a build's status and log. The log pane shows the latest 5,000
       lines; copying or downloading the log gives the whole log.
     - Only one build runs at a time. While one is running, the UI says
       **additional concurrent builds are currently restricted**.
     - **Artifacts** lists the images and the SBOM, in that order, under
       **Artifact**, **Name** and **Size**. Click a name to download the file;
       hover over it to see its path on disk.
     - **Compose details** shows the Basic selection, the Advanced
       customization card, and the template the build ran against, for
       download. The download is always the full, merged template.
     - **Retry compose** resubmits exactly what the build used, including
       logins, added users, packages and disk changes, even after a server
       restart. A GitHub token for the OEP Installer is not reused.
     - **History** is paged, 10 builds per page, newest first. Each row is
       titled with the SKU and shows the use case, platform, OS and image
       type beneath it.
   - **User Guide**: the **?** button opens an in-app guide, with its own
     address (`#guide/<topic>`), that walks through the Basic and Advanced
     tabs and following a compose. It works even when the API server is
     unreachable.
   - **Shutdown**: when `serve` is stopped, it stops accepting builds,
     cancels a running build and waits up to 20 s for it to clean up, so it
     does not leave mounts and loop devices behind on the host.

2. **Run ICT with only Docker installed**

   - A new ICT container image, defined by `build/ict/Dockerfile`, holds
     everything needed to build OS images: the `image-composer-tool` binary
     with the Web UI compiled in, `live-installer`, `config/`,
     `image-templates/`, and every program a build runs, under `/opt/ict` in
     the same layout as a checkout. Build it with `earthly +ict-image` (named
     `ict` unless you set `ICT_IMAGE_NAME`), and check that it has every
     program ICT may run with `earthly +ict-image-preflight`.
   - Web UI: `docker compose up --build` from a checkout builds the image and
     serves the UI at <http://127.0.0.1:8080>. Go, Node.js, build programs and
     sudoers rules are not needed on the host. Build history and OS images
     are kept in Docker named volumes, so they survive `docker compose down`.
     See the [Web UI Quick Start](./get-started/web-ui-quick-start.md).
   - CLI: `scripts/build-in-container.sh TEMPLATE OUTPUT_DIR` builds a
     template from the host inside the container and copies the OS image and
     its SBOM into `OUTPUT_DIR/<system-config-name>/`, owned by you.
     `--mount-point` sets where `OUTPUT_DIR` is mounted in the container,
     `--context` mounts a wider directory when the template references files
     outside its own, and two named volumes keep downloaded packages and the
     work tree for later builds. See the
     [CLI Quick Start (Docker)](./get-started/cli-docker-quick-start.md).
   - **The container is privileged and has the host's `/dev` mounted, so it is
     root-equivalent on the host.** The Web UI has no authentication of its
     own; its port is published on `127.0.0.1` only, and should stay that way.

3. **Install ICT from a GitHub release**

   - Each `ict-*` release tag publishes the ICT Debian package
     (`ict_<version>_amd64.deb`), its SPDX SBOM, `install-ict.sh` and
     `SHA256SUMS` on GitHub Releases, after install, remove and purge tests on
     Ubuntu 24.04 and 26.04 and Debian 12 and 13. A release-candidate tag such
     as `ict-2026.3-rc1` is published as a prerelease, with the package
     version `2026.3~rc1`.
   - `scripts/install-ict.sh` installs the package and the host tools a build
     uses on Ubuntu and Debian. `--tag TAG` downloads the package and
     `SHA256SUMS` from that tag's release and verifies the checksum before
     installing; `--deb FILE` installs a package you already have.
   - `scripts/build-deb.sh --version V` builds the same package locally.

4. **Unattended ISO provisioning**

   An `imageType: iso` template can now install a fully configured system
   hands-free, applied the same way by raw builds and by the live installer.
   See the [Unattended ISO Installer Tutorial](./get-started/unattended-iso-provisioning.md)
   and [ADR: Provisioning Settings for the Unattended ISO Installer](../architecture-decision-record/adr-unattended-iso-provisioning.md).

   - `systemConfig.users[].sshAuthorizedKeys` and `sshAuthorizedKeysFiles`
     install SSH public keys with the correct ownership and modes.
   - `systemConfig.proxy` writes `HTTP_PROXY`, `HTTPS_PROXY`, `FTP_PROXY` and
     `NO_PROXY` to `/etc/environment`, apt and the systemd default
     environment.
   - `systemConfig.provisioning.scripts[]` runs your scripts from generated
     systemd units at first boot or every boot, in a configured order. A
     script takes command-line arguments (`args`) and environment variables
     (`env`) from the template, can be limited with `timeoutSec`, and can be
     followed by a reboot with `rebootAfter`.
     `systemConfig.provisioning.beforeLogin: true` runs the scripts before the
     login prompt, so console and SSH logins (except root) wait until
     provisioning has finished.
   - When a provisioning script fails, the console login prompt and the
     message after login, including over SSH, name the script and the
     `sudo journalctl -u` command that shows its log, until the script
     succeeds. On an image whose `/etc` is not persistent, such as
     `imageType: img`, the notice lasts only until the next reboot.
   - `stopOnFailure: true` keeps a failed first-boot script from running
     again at every boot. The notice then tells an administrator to fix the
     problem and run `sudo /usr/libexec/ict-provision-report retry <name>`, or
     to finish by hand and run `sudo /usr/libexec/ict-provision-report done
     <name>`. `recoveryHint` adds the template's own hint to the notice.
   - `systemConfig.cloudInit` installs cloud-init and seeds it through a
     pinned NoCloud datasource.
   - `systemConfig.aptPolicy.upgradeAllowedRepos` limits which template
     repositories may upgrade installed packages. Ubuntu and Debian archives
     keep upgrading unless the image is immutable, in which case nothing
     upgrades.
   - `systemConfig.bootloader.bootEntryPolicy` (`preserve` or `exclusive`):
     the installer now sets the firmware `BootOrder` explicitly, with the
     installed disk first.
   - Partition `start` and `end` accept offsets from the end of the disk
     (`end: "-20GiB"`), so the root partition can fill a disk of any size
     while fixed-size data or swap partitions follow it.
   - ISO builds keep the SPDX SBOM of the installed system and a
     `<image>-<version>.composition.json` file next to the ISO.
   - The `build` flags `--disk-strategy`, `--hostname`, `--http-proxy`,
     `--https-proxy`, `--ftp-proxy`, `--no-proxy` and
     `--ssh-authorized-key USER=FILE` override the matching template fields.
     The disk, hostname and proxy overrides are rejected for overlay
     templates.
   - `systemConfig.provisioning`, `cloudInit`, `aptPolicy` and `oepInstaller`
     are create-mode only: an overlay template that sets any of them fails
     up front, naming each such section.

5. **OEP Installer blueprints**

   - Compose OEP Installer blueprints into Ubuntu images with the new
     `systemConfig.oepInstaller` template section. ICT imports the package
     list the OEP Installer publishes for a blueprint, installs its Debian
     packages and apt repositories at build time, checking them against the
     template's kernel and kernel parameters, and applies its apt preferences
     when resolving packages, so the image gets the versions `apt` would
     pick on the device. On first boot it runs `openedge-cli install` for the
     blueprint as the template's user.
   - Only `sbomUrl` and `user` are required. Without `url`, ICT installs the
     CLI that the package list names in its `cli-url` header, verified
     against its `cli-sha256` header when present; without `modules` or
     `args`, it uses the list's own; without `sbomSha256`, the list is not
     verified. A template can point `sbomUrl` at a branch and pick up OEP
     Installer fixes without changing. The template the image carries
     records the CLI and digests that were used.
   - Files in a private GitHub repository need a GitHub token. ICT reads it,
     in order of precedence, from `build --oep-installer-token-file FILE`,
     `ICT_OEP_INSTALLER_TOKEN`, the file `ICT_OEP_INSTALLER_TOKEN_FILE`
     names, or the file the `oep_installer.token_file` setting in
     `image-composer-tool.yml` names. A token file lets the Web UI build
     such templates with `serve --sudo`, which does not pass the token
     variable through `sudo`, and from Docker Compose through a secret that
     `docker-compose.yml` shows commented out. Keep the file owned by root
     with mode 0600; ICT warns when other users can read it.
   - In the Web UI, the **Users and credentials** dialog has a **GitHub token
     for the OEP Installer** field for a template with
     `systemConfig.oepInstaller`. The token is used for that build only: it
     is not stored, and **Retry compose** does not reuse it.
   - When the OEP Installer fails on first boot, it is not run again at every
     boot. The login notice tells an administrator to fix the problem and run
     `sudo /usr/libexec/ict-provision-report retry oep-installer`, or to
     resume by hand and then run
     `sudo /usr/libexec/ict-provision-report done oep-installer`.

6. **Intel EdgePack templates and DKMS module builds**

   - Raw image templates for Intel's EdgePack platform-enablement packages
     (Panther Lake and Wildcat Lake), on Ubuntu 24.04 and 26.04:
     `ubuntu24-x86_64-edgepack-raw.yml` and
     `ubuntu26-x86_64-edgepack-raw.yml` (desktop), and the headless
     `ubuntu24-x86_64-edgepack-server-raw.yml` and
     `ubuntu26-x86_64-edgepack-server-raw.yml`, which add
     `ubuntu-server-minimal` instead of the desktop packages. They install
     `intel-edge-base-standard`, `intel-edge-media-ffmpeg`,
     `intel-edge-media-gst` and `intel-edge-manageability` from the signed
     EdgePack repository (`noble` or `resolute` suite), plus the packages the
     platform minimal template needs, such as build, debug, network and
     storage tools. NPU, IPU and edge-compute-essentials
     enablement comes with `intel-edge-base-standard`. The kernel is pinned
     to `linux-image-7.0.0-34-generic` rather than the rolling HWE
     metapackage. The 26.04 templates use a 12 GiB disk, the 24.04 ones
     6 GiB.
   - Unattended-ISO templates for the same package sets:
     `ubuntu24-x86_64-edgepack-unattended-iso.yml`,
     `ubuntu24-x86_64-edgepack-server-unattended-iso.yml`,
     `ubuntu24-x86_64-edgepack-standard-only-unattended-iso.yml` (only
     `intel-edge-base-standard`, without the Media or Manageability
     packages), `ubuntu26-x86_64-edgepack-unattended-iso.yml` and
     `ubuntu26-x86_64-edgepack-server-unattended-iso.yml`. They pin the kernel
     to `7.0.0-38`, which `edge-gfx-dkms` 7.0 builds against, and install
     `acpica-tools` and `iptables`. Their `admin` user is in the `sudo`,
     `video`, `render` and `audio` groups, and their `systemConfig.network.backend`
     is `netplan`. The desktop templates install a terminal emulator
     (`gnome-terminal` on 24.04, `ptyxis` on 26.04), and the 26.04 ones do
     not list `dbus-x11`, which kept GDM from starting a desktop. The server
     templates boot to `multi-user.target` and request DHCP with the NIC's
     MAC address, so a DHCP reservation for the board keeps working after a
     re-flash.
     `admin.pub` is a placeholder with no key, so build with
     `--ssh-authorized-key admin=FILE`, or set a password or key in the
     template, or the build fails at validation.
   - The desktop templates install a real Firefox `.deb` from Mozilla's
     repository (`packages.mozilla.org`), pinned at priority 1001 for the
     `firefox` package only; Ubuntu's own `firefox` package installs nothing
     in an image build.
   - In the Web UI Basic tab, the **Platform Enablement** use case offers
     **EdgePack Minimal Desktop** and **EdgePack Minimal Server** on Ubuntu
     24.04 and 26.04, and **EdgePack Standard Base Only (Demo)** on Ubuntu
     24.04, all for Panther Lake and Wildcat Lake. They build the unattended
     ISOs, and the credentials dialog asks for the `admin` user's login.
   - The new `systemConfig.dkms` section builds and verifies the DKMS
     modules that installed packages register, such as EdgePack's
     `edge-gfx-dkms`, `edge-edac-dkms`, `edge-issei-dkms` and
     `edge-mei-dkms`, against the image's target kernel instead of the build
     host's. A module that displaces an in-tree driver is handled. When
     package installation or a module build fails, the available `make.log`
     files are kept under `imagebuild/<systemConfigName>/dkms-logs/`.

7. **Other new and updated templates**

   - Nova Lake BKC (Best Known Configuration) starter:
     `image-templates/ubuntu26/ubuntu26-x86_64-nvl-bkc-overlay-raw.yml` is an
     overlay template that layers onto an installed Ubuntu 26.04 RAW image and
     adds a single package; the full BKC template replaces it under the same
     name where it is deployed. It backs the Web UI's **Upstream BKC** SKU
     under **Platform Enablement**, on **NVL (Nova Lake)** with Ubuntu 26.04.
   - `debian13-x86_64-bb-dracut-raw.yml` now boots a GNOME-on-Xorg desktop
     with a GDM login, with a dconf setting that works around a Mutter
     click-position bug. It also ships a sample first-boot systemd oneshot
     unit (`first-boot-sample.service`): a script that runs once, on the
     device's first boot only, gated by a marker file, with its message
     mirrored to the journal, a log file, `dmesg` and the serial console.

8. **Overlay builds**

   - `overlayPolicy.allowDowngrade` permits baseline package downgrades
     selected by explicit APT `name_version` requests with
     `packageOperation: additive-and-upgrade`. It defaults to false and does
     not bypass dependency integrity, protected boot packages or removal
     policy. Unpinned packages and dependencies are not selected for
     downgrade automatically.
   - Overlay builds print a planned package-change report after dependency
     resolution and preflight, before the build begins. It lists
     resolver-selected dependency upgrades along with additions and approved
     removals; install-time cascade removals may differ from the plan and
     appear in the final statistics.
   - Overlay templates support `systemConfig.proxy`, written the same way as
     in create mode, after the build-time configuration commands. Add
     site-specific settings in a local or internal template. Build downloads
     still use the host's proxy environment.

9. **New build options and template fields**

   - `build --replace-additional-file FINAL=FILE` replaces the host file of
     the `systemConfig.additionalFiles` entry installed at `FINAL` for one
     build, so an input that changes per build, such as a vendor tarball,
     can be supplied on the command line without editing the template. It
     does not add files: it fails if no entry has that `final` path or if
     `FILE` is not an existing regular file. It can be repeated.
   - `systemConfig.bootloader.timeoutSec` sets how long GRUB shows its menu
     before booting the default entry (`GRUB_TIMEOUT`), so a template no
     longer needs an `/etc/default/grub.d` drop-in for it. It applies to
     create-mode builds with the `grub` provider; with `systemd-boot` the
     template is rejected.

**Changed**:

*Build, templates and validation*

- Building ICT now requires Go 1.26 or newer.
- Every user in a template needs a credential. `systemConfig.users[]` with
  neither a `password` nor an SSH key now fails the build, and
  `image-composer-tool validate --merged` reports it, instead of being created
  with an empty password. The only exception is a user with a
  `startupScript`. A key file that holds only comments does not count as a
  key. Four shipped templates define such a user:
  `ubuntu24-x86_64-minimal-unattended-iso.yml`,
  `generic-handheld-os-template.yml`,
  `generic-companion-os-server-template.yml` and
  `ubuntu24-x86_64-generic-handheld-os-desktop-raw.yml`, as do the EdgePack
  unattended-ISO templates. For any of them, set `password`, or build with
  `--ssh-authorized-key USER=FILE`.
- `systemConfig.users[].shell`, `home` and `passwordMaxAge` are now applied;
  previously every account got `/bin/bash` and the default home. `home`
  applies only to accounts the build creates: an account already in the
  image, such as `root`, keeps its home. A `password` that is a crypt(3) hash
  is always set as a hash, even without `hash_algo`. A user with SSH keys but
  no `password` has its password locked, so it can log in only with those
  keys; if it also has sudo access (`sudo: true` or a `sudo` or `wheel`
  group), it gets passwordless sudo, since a locked password cannot answer
  the sudo prompt. `systemConfig.hostname` must be an RFC 1123 host name.
- When a template that `extends` another, or a user template merged with the
  OS defaults, lists a package the other already lists, only one entry now
  survives the merge. An entry that pins a version (`curl_8.5.0-2`) replaces
  the inherited entry for that package, pinned or not; a bare entry (`curl`)
  leaves the inherited one in place. Before, both entries survived, and the
  build installed one of them or failed at the package-install step. No
  shipped template is affected.
- Disk partition offsets are validated before a build starts or an
  installation disk is wiped. Positive offsets are measured from the start of
  the disk; negative ones, such as `-20GiB`, from the end of the selected
  disk. Only the final partition may use `end: "0"`; overlapping, reversed,
  zero-start and end-relative offsets beyond the selected disk are rejected.
  Overlaps between positive and negative offsets are checked once the disk
  is selected and its size is known, but before it is wiped. Policy-based
  disk selection counts the largest absolute and end-relative offsets plus a
  1 MiB margin in its minimum-size check. Raw builds and the ISO live
  installer use the same checks; an explicit disk path bypasses the
  selection policy but not the layout check.
- A template's `metadata` block (`description`, `use_cases`, `keywords`) is
  kept in its resolved output, so it reaches `resolve --full`, the Web UI and
  `template-dump.yaml`. It is still not inherited: a child without
  `metadata` resolves to none, and a child with its own replaces the
  parent's whole block. Resolved templates list their top-level keys in the
  order the shipped templates use (`metadata`, `image`, `target`, …,
  `systemConfig`).
- `template-dump.yaml`, the resolved template an installer ISO carries and
  the copy left in the image build directory, no longer includes the
  per-package SBOM metadata, which made it thousands of lines long. The
  metadata is written to a `sbom-metadata.yaml` file beside it, in the build
  directory and in the ISO, so the live installer still produces a full
  SPDX document. A template that already carries a `sbomPackageMetadata`
  block, such as a dump from an earlier release, has it moved to that file.
  An ISO built by an earlier release is unaffected: the installer falls back
  to the inline block when `sbom-metadata.yaml` is missing.
- The apt pin ICT writes into an image for a repository that sets
  `allowPackages` now covers only the listed packages (and the names the
  build-time filter accepts for them, which includes names that start with a
  listed entry); the repository's other packages stay at apt's default
  priority. Before, the pin covered every package the repository publishes.
  This affects templates such as those using the Intel overlay and Debian
  backports. Images that cannot upgrade from a repository (immutable images
  and `aptPolicy`) keep the whole-repository pin. Repositories whose pin file
  names would collide (same suite and host, no IDs) now each keep their own
  file.
- Immutable Ubuntu and Debian images include apt pins that stop package
  upgrades.
- Build commands get the host's `http_proxy`, `https_proxy`, `ftp_proxy` and
  `no_proxy` in both lowercase and uppercase when the host sets only one
  spelling, so tools such as `wget` in `systemConfig.configurations`, which
  read only the lowercase form, use the proxy. The build log names the proxy
  variables that are forwarded, never their values.
- The output of a configuration command is logged at DEBUG when it succeeds,
  and logged and appended to the error when it fails.
- The CLI's **Generated Artifacts** summary lists only the images and SBOMs,
  not every file in the image build directory.
- ISO builds pass `-iso-level 3` to xorriso, so additional files larger than
  4 GiB fit on the ISO. Additional files are stored on the ISO in a directory
  per in-image destination, under their source file name, so two files with
  the same name no longer overwrite each other, and a destination that is an
  existing directory (such as `/etc`) receives `/etc/<file name>` as in a
  raw build. ISO prerequisite checks now also cover the ISO template's own
  `additionalFiles`, resolving relative paths as the build does.
- The live installer removes only firmware boot entries labelled exactly
  `ICT` (previously any label containing `ICT`), resolves `/dev/disk/by-*`
  target paths to the kernel device, and uses the architecture's removable
  EFI loader path (`BOOTAA64.EFI` on aarch64).
- The ICT container image (item 2) replaces the builder image
  (`build/builder/Dockerfile`), which could not complete a build. The
  `+builder-image`, `+builder-image-push` and `+builder-image-preflight`
  Earthly targets are removed.

*Overlay*

- The overlay installer's dependency audit compares failures before and
  after every non-empty install or removal transaction, including
  upgrade-only transactions, ignoring failures that were already there. A
  failure that an upgrade-only transaction introduces fails the build without
  cascade removal, even with `allowPackageRemoval: true`. Explicitly
  authorized removal cascades keep their bounds and protected-package guards.
- Platform-enablement templates that need private repositories are kept in
  the internal template repository, not the public template catalog.

*Templates*

- Firefox now comes from Mozilla's repository (`packages.mozilla.org`,
  suite `mozilla`) instead of the Mozilla Team PPA in
  `generic-handheld-os-template.yml`,
  `generic-companion-os-server-template.yml` and
  `ubuntu24-x86_64-generic-handheld-os-desktop-raw.yml`. The PPA's
  `InRelease` is signed with an rsa1024 key, which Ubuntu 26.04's apt
  rejects and which Ubuntu 24.04's apt already marks as below its next
  minimum key size, so `apt-get update` would fail on images that keep it.
  The repository is pinned at priority 1001 for the `firefox` package only;
  the companion server template, which installs no Firefox, no longer pins
  the whole repository either.
- `generic-handheld-os-template.yml` and
  `generic-companion-os-server-template.yml` are vendored from
  [`edge-node-infrastructure-blueprint`](https://github.com/open-edge-platform/edge-node-infrastructure-blueprint/tree/release-2026.2.0/infrastructure/host-os/ict)
  `release-2026.2.0`. `generic-handheld-os-template.yml` was updated in
  place to that revision: its swap partition grows from 3073MiB to 5121MiB,
  and it installs the 6.18 Intel kernel and media stack from a pinned
  snapshot of the Intel edge overlay (`download.01.org/edge-linux-overlay`)
  instead of the rolling `intel-linux-overlay` suite, so images built from it
  differ from earlier releases. Their `user` account has an empty password:
  set a password, or build with `--ssh-authorized-key user=FILE`.
- The `Edge Node Infrastructure Blueprint BKC` template,
  `ubuntu24-x86_64-minimal-ptl-pv-raw.yml`, is not offered in the Web UI; it
  can still be built from the CLI.

*Security*

- RPM signatures are checked against the keys of the repository each RPM
  came from, including RPMs from local `path` repositories, which were
  skipped before; such a repository now needs `pkey`, or `[trusted=yes]` to
  opt out. `[trusted=yes]` covers only RPMs that repository served in the
  current build, so a cached RPM it would serve is downloaded again. Debian
  `Release` metadata is verified before it is cached, and again before
  cached metadata is used after a refresh fails partway. A refresh is retried
  up to 3 times, each on a fresh connection, when a mirror briefly serves a
  mismatched `Release` and `Release.gpg`.

**Fixed**:

*Security*

- RPM file names from repositories are validated and quoted before they
  reach `rpm`, and `inspect` ignores SBOM files with unsafe names or paths,
  so a crafted file name can no longer inject commands.
- A `packageRepositories` `pkey` URL that returned a truncated file or an
  HTML error page was cached and installed in every later image, which then
  failed `apt-get update` with `NO_PUBKEY` on the device. Key data is now
  checked to be an OpenPGP public key before it is used or cached, and a bad
  cached key is downloaded again. Keyserver `pks/lookup` URLs are cached per
  key ID.

*Build and package resolution*

- Raw image builds failed when a package's post-install script built for
  `uname -r`, as the Edge Pack 2026.3 `edge-*-dkms` packages do. Inside the
  build chroot `uname -r` reports the build host's kernel, so those scripts
  built for the wrong kernel and aborted the install. While Debian packages
  are installed, ICT now replaces the image's `/usr/bin/uname` (with
  `dpkg-divert`) with a temporary wrapper whose `uname -r` reports the newest
  installed kernel (under `/boot`, or `/lib/modules` when `/boot` is still
  empty), and restores the real `uname` when installation finishes. Every
  other `uname` call is unchanged, and before a kernel is installed
  `uname -r` still reports the host's.
- Package downloads timed out behind a TLS-inspecting proxy, which holds a
  response's headers until it has fetched and scanned the file. Package
  downloads now wait up to 5 minutes for the response headers; other HTTP
  requests keep the 30 s limit. A server that accepts a package request and
  never answers now takes up to 15 minutes (3 attempts) to fail; one that
  cannot be reached still fails on the 30 s connection timeout.
- A package download that a mirror answered with 429, 502, 503 or 504 failed
  the build after about 10 s of retries. While the latest failure is one of
  those statuses, a download now gets up to 6 attempts, waiting 15 s and
  doubling up to 2 minutes, plus one second per download worker (at most
  about 6½ minutes of waiting with the default 8 workers). A `Retry-After`
  header is honored up to 2 minutes; a server that asks for longer fails the
  download at once. Every other failure keeps the previous 3 attempts.
- A Debian-family build that reused a package cache could install an older
  version of a package than a build with an empty cache, so two builds of
  the same template differed in their SBOMs. The cache check now matches the
  exact `.deb` file name, which includes the version, and the build uses only
  the files it resolved, including, for raw images, the packages installed
  while bootstrapping and the image's local apt repository.
- Changing a repository's `allowPackages` list had no effect until the build
  cache was deleted. The parsed package-metadata cache now records the filter
  it was parsed with and is reused only when it matches; caches written by
  earlier versions are parsed again once.
- A package whose `Filename` in the repository index contains a literal `%`
  (as aptly writes for a `+` in a version) could not be downloaded. ICT now
  percent-encodes the `%` as `apt` does.
- apt repositories that publish only a combined `InRelease` file, instead of
  a detached `Release` and `Release.gpg`, are supported, including EdgePack's
  public repository. The format is detected and verified either way, and
  rebuilds keep using the detected format.
- Dependencies on a versioned virtual `Provides:` (for example Debian's Qt6
  ABI packages) are resolved against the version the provider declares for
  that capability, not the provider's own package version. An unversioned
  `Provides:` no longer satisfies a versioned dependency.
- `systemConfig.configurations` commands containing multi-line scripts or
  shell metacharacters (`$()`, backticks, `$var`) reach the chroot unchanged,
  instead of having their whitespace collapsed or being partly expanded by
  the outer shell.
- `packageRepositories` are turned into apt sources and preferences for
  `debian` targets, as they already were for Ubuntu and eLxr, so `aptPolicy`
  and immutable pinning now apply to Debian images.
- Generated `/etc/fstab` lists partitions in template order and skips
  non-swap partitions without a mount point, which used to produce a
  malformed line.
- On build hosts where `qemu-user-static` is only a virtual package, such as
  Ubuntu 26.04, every Debian-family build failed in pre-processing with *no
  installation candidate*. ICT now installs `qemu-user-binfmt` there, and the
  cross-architecture check accepts either binary.
- A hung `sfdisk` could stall a build indefinitely. Both `sfdisk` calls are
  now limited to 30 s, and before attaching an image file to a loop device,
  any loop device still attached to the same file is detached.

*Unattended ISO*

- The live installer aborted on installer environments with a merged
  `/usr`, such as the Ubuntu 26.04 initramfs, with `command chroot full path
  recorded in [/usr/sbin/chroot] not exist`. ICT now also finds `chroot` and
  other system tools under `/usr/bin`, `/sbin` and `/bin`.
- The unattended installer's "Another unattended installer instance is
  already running" message misled anyone watching a console other than the
  one running the install, such as QEMU with `-nographic`. Every console
  starts the installer and only the first runs it; the others now say so and
  follow `/tmp/unattended-installer.log` until the installer finishes or
  fails, so a failure is visible on every console.

*Overlay*

- Debian overlay planning preserves baseline `Depends` and `Pre-Depends`
  constraints and alternatives. With `packageOperation: additive-and-upgrade`,
  library upgrades that break previously satisfied requirements of retained
  native-architecture baseline packages now upgrade those packages too,
  through the existing `Breaks` repair loop (at most four extra resolves).
  Preflight blocks retained dependencies that would be newly unsatisfied,
  including by unavailable or unchanged candidates and blocking pins.
  Explicit pins are not relaxed, and no blanket upgrades, removals or
  downgrades are authorized. Upgraded versioned `Provides` and `Multi-Arch`
  metadata are preserved, and `Architecture: all` is treated as the native
  architecture, so an `all` replacement cannot hide newly broken
  foreign-architecture dependencies.
- Debian overlay dependency audits ignore apt's changing installed-version
  explanation when comparing a broken dependency across transactions,
  avoiding false build failures when an unrelated upgrade changes the version
  named in `apt-get check` output.
- Ubuntu `linux-main-modules-*` packages are treated as kernel companions,
  not extra bootable kernel images. Old companions can be removed only under
  a valid explicit kernel replacement.
- Overlay kernel replacement treats a Debian `name_version` request as the
  package name when its name and version match resolved or dependency
  metadata, avoiding a false extra-kernel-image ambiguity.
- Under `packageOperation: additive-and-upgrade`, an exact APT
  `name_version` pin that differs from the baseline now enters the install
  set even when it is older, and preflight rejects it unless `allowDowngrade`
  is enabled, instead of ignoring the pin and later reporting unsatisfied
  dependencies. Matching follows the Debian resolver for omitted epochs and
  artifact-style requests. Pins that compare equal under Debian version
  rules (for example `2.12-1` and `0:2.12-1`) remain no-ops.
- A `systemConfig.proxy` with only `noProxy` now removes a stale
  ICT-managed apt proxy file from the base image. Other apt files are
  unchanged.

**Known Issues**:

- Builds need network access to their package repositories; offline builds
  are not supported.
- Ubuntu 24.04 is the recommended build host.
- The `uname -r` wrapper used while Debian packages are installed is a
  stop-gap; packages should not assume `uname -r` is the target kernel.
- `overlayPolicy.allowDowngrade` is not supported end-to-end for RPM overlay
  builds, and RPM baseline reverse-dependency repair is not implemented.
- In the Web UI, the Real-time Edge Pack runtime is not yet available, MBR
  partition tables cannot be selected in the Disk step, and Edge Packs are
  not offered for overlay templates.
- The Web UI has no authentication, and its container is root-equivalent on
  the host (see item 2 under **New**).

## Version 2026.2

**Release Date**: September 9, 2026

**New**:

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

6. **Full Disk Encryption (FDE) for RAW images**

   This release supports selectively encrypting disk partitions in RAW images
   with user-specified passphrases in the user template for encryption and
   decryption. Sealing encryption keys in a TPM is not supported.

7. **Image composition for WSL environments**

   The tool can now compose Ubuntu images compatible with WSL environments.

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

**Known Issues**:

- **Custom partition layouts with the overlay feature are not supported**:

  The tool does not support user-specified disk partition layouts in the output
  image. The base image partitions are passed through to the RAW or QCOW2 output image.

- **SBOM generation for base images without an embedded SBOM**:

  If the base image does not contain an embedded Software Bill of Materials (SBOM),
  the resulting image generates an SBOM only for the additionally installed
  packages. Packages from the base image are not included in the SBOM output.

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

- Repository metadata caching for DEB and RPM repositories: DEB Packages.gz metadata is now cached by SHA-256 checksum (`packages.parsed.json`) under `cache_dir/` and reused on rebuilds. RPM `primary.xml` metadata and `primary.location.json` are cached under `cache_dir/rpm-metadata/`. Debian repository GPG keys are cached in `cache_dir/gpg-keys/`. Repository file-existence check results and package-list URLs are cached in-process per run to eliminate redundant HEAD requests.

- DKMS module installation: Package resolution now uses a target-name-aware candidate filter (`filterCandidatesByPriorityWithTarget`) that prefers exact-name matches over Provides virtual package matches, preventing kernel packages that provide a DKMS module name from being selected instead of the actual DKMS package.

**Improved**:

- RPM package cache: `DownloadPackagesComplete` now checks for a valid local cache before contacting the repository. Packages already present are reused and only the missing packages are re-fetched, preserving existing cached files.

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
