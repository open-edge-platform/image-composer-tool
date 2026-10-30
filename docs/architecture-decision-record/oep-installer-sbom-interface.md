# Interface Specification: OEP Installer → ICT Profile Package Requirements

- **Status:** Draft (for agreement between the OEP Installer and ICT teams)
- **Date:** 2026-10-27
- **Updated:** 2026-10-30 (OEP Installer publishes the output; ICT imports it)
- **Schema version:** `1`
- **Implements:** [ADR: Integrate OEP Installer Profiles Through a Solution-Owned DEB Dependency Interface](./adr-oep-installer-intent-manifest.md)

## Purpose

This document defines the contract between `openedge-cli` (OEP Installer) and
the Image Composition Tool (ICT):

- **Part A (build time):** the output files the OEP Installer publishes with
  the CLI for each supported configuration, their format, and how ICT
  imports them
- **Part B (first boot):** how ICT invokes `openedge-cli` on the deployed
  system to finish the profile installation

OEP Installer remains the source of truth for each profile. ICT installs the
repository-installable packages before first boot. `openedge-cli` installs
everything else on first boot.

The contract builds on the existing `openedge-cli list --sbom` output. The
following changes were requested from `openedge-cli`:

1. Exit non-zero, with a message on stderr, if any requested profile is
   unknown (A.3). Implemented.
2. Print a header document with `schema-version`, `cli-version`,
   `module-list`, and `cli-url` first (A.5). Implemented.
3. Validate the `list --sbom` output of every profile against the JSON Schema
   in the appendix, ideally in CI, so problems are caught before ICT sees
   them. Implemented.
4. In the kernel handlers (`linux_image_6.18_intel`,
   `linux_image_generic_7.0`), also print the kernel parameters they set, as
   `kernel-cmdline` (A.6). Implemented.
5. Publish one output file per supported configuration, generated on a
   reference host that runs the target OS and kernel, next to the CLI (A.1).
   In progress: the Handheld blueprint file is published.
6. Record the arguments the output was generated with in the header, as
   `arguments` (A.5). Requested.

The key words MUST, MUST NOT, SHOULD, and MAY are used as in RFC 2119.

---

## Part A: Build-time package requirements

### A.1 Published output files

The output of `openedge-cli list --sbom` depends on the host it runs on: its
OS release, its running kernel (`uname -r`, `/boot/config-*`,
`/proc/cmdline`), and the packages the CLI selects for them. For example,
`kernel_module_essential` lists `linux-headers-$(uname -r)` and the `gcc-N`
the running kernel was built with. ICT builds images on any Ubuntu 24.04 host
and in containers, which share the host's kernel, so it does not run the
query itself. The OEP Installer team runs it on a reference host and
publishes the output:

- **One file per configuration.** A configuration is a set of modules and
  arguments for one target OS release, for example
  `openedge-cli-federal-aerospace list --sbom handheld_blueprint --edgepack`
  for Ubuntu 24.04.
- **Generated on a reference host** that runs the target OS release and the
  target kernel.
- **Published next to the CLI** that generated it, in the `rendered/`
  directory of the `intel-sandbox/oep-installer-cli` repository, named
  `sbom-<blueprint>-<os>-<version>.yaml`, for example
  `sbom-handheld-blueprint-ubuntu-24.04.5.yaml`.
- **Regenerated** whenever the CLI, the modules or arguments, or the
  reference host's kernel change.

The first published files cover the three Federal and Aerospace blueprints,
Handheld, Uncrewed Aerial Vehicle (UAV), and Mission Decision Support,
generated on a host running the Journey 1 kernel.

A file pins the reference host's kernel release, for example
`linux-headers-7.0.0-38-generic`, so an image template that uses it must
install that exact kernel (A.6). When Ubuntu publishes a newer kernel and its
archive stops listing the pinned release's packages, builds fail until the
file is republished for the new kernel.

Version 1 covers `amd64` targets only.

### A.2 Generation requirements

Generating an output file MUST:

1. **Have no side effects.** It must not use `sudo`, install packages, or
   change apt configuration.
2. **Use the same definitions as `install`.** The packages and repositories
   reported for a configuration must be the ones `install` would set up for
   it on the reference host.
3. **Be reproducible.** Regenerating with the same CLI on the same reference
   host gives the same output. Values must not come from online "latest
   release" lookups.
4. **Keep the file clean.** Only the YAML stream (A.4) goes into the file.
   Diagnostics go to stderr.

### A.3 Exit status

The CLI exits `0` on success and non-zero, with a message on stderr, for an
unknown profile or module, an unsupported target, or an internal error. A
file MUST NOT be published from a failed run. A header with no component
documents is valid: the configuration needs no DEBs.

### A.4 Output format

The output is a **multi-document YAML stream**, as today. Documents are
separated by a line containing only `---`. A trailing `---` and empty
documents are allowed and ignored.

- The **first** document MUST be the header document (A.5).
- Every later document is a component document (A.6).
- Each line is `"key": <value>`, where the value is a JSON string or a JSON
  array of strings, as the current emitter writes them. ICT parses the stream
  as YAML.
- ICT ignores keys it does not know, so OEP can add optional keys without a
  version bump. Removing or renaming a key, or changing what it means,
  requires a new `schema-version`.

### A.5 Header document

| Key | Type | Required | Description |
| --- | --- | --- | --- |
| `schema-version` | string | yes | `"1"` for this specification. ICT rejects versions it does not support. |
| `cli-version` | string | yes | The `openedge-cli` version: the release version (for example `"v2026.3.0"`), which changes with each release, or `"main"` for builds before the first release. ICT records it; reproducibility comes from the commit and SHA-256 ICT pins (A.8). |
| `module-list` | array of string | yes | The profiles or modules the output was produced for, in the order requested. |
| `cli-url` | string | no | Web URL of the CLI file that generated the output (for example `https://github.com/intel-sandbox/oep-installer-cli/blob/main/rendered/openedge-cli-federal-aerospace`). ICT warns when its repository and file name differ from the template's CLI `url`. |
| `arguments` | array of string | requested | The arguments the output was generated with (for example `["--edgepack"]`). When present, ICT checks that they equal the template's `args`, which first boot passes to `install`. |

### A.6 Component documents

A component document is what one `*_sbom_*` handler prints today. It contains
a package list, a repository group, or both. A document with only a
repository group configures the repository without requesting packages from
it.

#### Package list

| Key | Type | Description |
| --- | --- | --- |
| `pkg-list` | array of string | Packages the profile requires. Each entry is one of: a package name; `name=version`, where `version` is an exact Debian version string; `name=version*`, meaning the newest version that starts with `version`; a glob over package names (`*`, `?`, `[...]`); an `http://` or `https://` URL of an artifact that is not in an apt repository; or `-name`, meaning the profile needs the package not to be installed. |
| `kernel-cmdline` | array of string | Optional. Kernel command-line parameters the module needs (for example `"xe.max_vfs=7"`), as the kernel handlers set them in `GRUB_CMDLINE_LINUX` today. |
| `component` | string | Optional. Module name (for example `"docker_io"`), shown in ICT error messages. |
| `apt-repository` | string | Optional. An Ubuntu archive component (`universe`, `multiverse`, `restricted`). ICT images already enable all of them, so ICT ignores this key. |

`pkg-list` contains **direct** requirements only. ICT resolves and installs
the transitive dependency closure and records it in the image SBOM. The same
package may appear in several documents.

ICT handles `pkg-list` entries as follows:

| Entry | ICT behavior |
| --- | --- |
| Package name, `name=version`, or `name=version*` | Resolved from the template's repositories plus the repositories in the output, and installed. For `name=version*` (for example `"intel-edge-base-standard=2026.3*"`), ICT installs the newest version that starts with `2026.3`. A package that no configured repository provides, or no version that matches, fails the build. Two entries for the same package with versions that cannot both be satisfied fail the build. |
| Glob (for example `intel-oneapi-runtime-*`) | Expanded to **every** matching package name in the configured repositories. For each name, a `name=version` or `name=version*` entry anywhere in the stream decides the version. Otherwise ICT chooses the version as for a plain package name: highest priority first (repository priority and the `apt-pref` stanzas from the output), then the newest version. A glob that matches nothing fails the build. |
| `http://` or `https://` URL (for example the `libze1` `.deb` or the `linux-npu-driver` `.tar.gz`) | Skipped. `openedge-cli` installs it on first boot, so it is not in the ICT-generated SBOM, even when it is a `.deb`. |
| Kernel image (`linux-image-*`, `linux-generic*`) | Must be one of the kernel packages the template installs (`systemConfig.kernel`, after its globs are resolved). Otherwise the build fails. ICT does not add or replace a kernel. |
| Kernel headers, modules, or tools for a release (`linux-headers-<release>`, `linux-modules-<release>`, `linux-tools-<release>`) | Installed when `<release>` is the template's kernel release. Any other release fails the build. |
| `-name` (for example `"-ubuntu-desktop"`) | If the template's package list contains `name`, the build fails. Otherwise the entry is ignored. The template's package list is the merged `systemConfig.packages`, after `extends`. It does not include packages installed only as dependencies. |

The `kernel-cmdline` entries must be a subset of the parameters in the
template's `systemConfig.kernel.cmdline`: each entry must appear there
exactly, for example `xe.max_vfs=7`. Otherwise the build fails. The template
may set other parameters as well, such as `console=` or `quiet`.

These checks keep the template in charge of the kernel and still give the
profile the kernel it expects. The kernel handlers skip their installation
step when the running kernel already matches (they check `uname -r`). Their
kernel parameters are only applied in that installation step, so on an ICT
image they must already be in the template's command line.

#### Repository group

These are the keys the current CLI already prints. If any of the required
keys is present, all of them MUST be.

| Key | Type | Required | Description |
| --- | --- | --- | --- |
| `repo-name` | string | no | Repository host or label (for example `"repositories.intel.com"`), shown in ICT messages. |
| `gpg-key` | string | yes | `https://` URL of the signing key. `http://` is accepted for internal repositories; see below. |
| `key-file` | string | yes | Absolute path of the dearmored keyring in the image. |
| `apt-source` | string | yes | One one-line `deb` entry: `deb [options] <uri> <suite> <component>`, with exactly one component. ICT takes the repository URL, suite (codename), and component from this line. The suite is usually the release codename, but may be a fixed value the repository uses (for example `all` or `ubuntu24`). |
| `apt-list-file` | string | yes | Absolute path of the list file under `/etc/apt/sources.list.d/`. |
| `apt-src-source` | string | no | The matching `deb-src` entry. ICT writes it to `apt-list-file` but does not use it. |
| `apt-pref` | array of string | no | Lines of an `apt_preferences(5)` file. A new stanza starts at each `Package:` line. Requires `apt-pref-file`. |
| `apt-pref-file` | string | with `apt-pref` | Absolute path under `/etc/apt/preferences.d/`. |
| `gpg-key-fingerprint` | string | no | Full 40-hex-digit OpenPGP fingerprint. When present, ICT verifies the downloaded key against it. |

A signing key fetched over `http://` is not protected in transit, and the
key decides which packages apt trusts from that repository. For an `http://`
key, `gpg-key-fingerprint` SHOULD be provided. ICT verifies the key against
it when present and logs a warning when it is absent.

The same repository group may appear in several documents, for example once
per EdgePack module. ICT merges groups that are identical. Two groups that
name the same `apt-list-file`, `key-file`, or `apt-pref-file` with different
content fail the build.

ICT writes `apt-list-file`, `key-file`, and `apt-pref-file` into the image at
these exact paths, with the content `openedge-cli install` would write. When
`openedge-cli` later writes the same files on first boot, it replaces them
with equivalent content. If ICT wrote the repository anywhere else, apt would
see the same source twice with different `Signed-By` values, and
`apt-get update` could fail with "Conflicting values set for option
Signed-By".

ICT also applies the `apt-pref` stanzas when it resolves packages at compose
time, so it selects the same versions apt selects on the deployed system.
Version 1 supports stanzas of this form:

```text
Package: <names or globs, separated by spaces>
Pin: origin <host>  |  Pin: version <version or glob>
Pin-Priority: <integer>
```

For example, the oneAPI module pins `intel-oneapi-runtime-*` to
`Pin: version 2025.3.*` with priority `1001`, so ICT installs the newest
2025.3 release of those packages, not a newer major release. ICT follows
`apt_preferences(5)`: each candidate version gets the priority of the stanza
that applies to it, the highest priority wins, and among equal priorities the
newest version wins. A negative priority excludes the version. Any other
`Pin:` form fails the build rather than being ignored.

A `name=version` entry in `pkg-list` still selects that exact version,
whatever the stanzas say.

### A.7 Example

`openedge-cli-federal-aerospace list --sbom handheld_blueprint --edgepack`,
run on a reference host (abridged, illustrative):

```yaml
"schema-version": "1"
"cli-version": "v2026.3.0"
"cli-url": "https://github.com/intel-sandbox/oep-installer-cli/blob/v2026.3.0/rendered/openedge-cli-federal-aerospace"
"module-list": ["handheld_blueprint"]
"arguments": ["--edgepack"]
---
"pkg-list": ["curl","ca-certificates"]
---
"pkg-list": ["https://github.com/oneapi-src/level-zero/releases/download/v1.32.0/libze1_1.32.0+u24.04_amd64.deb"]
---
"pkg-list": ["docker.io","docker-buildx","docker-compose-v2"]
---
"repo-name": "repositories.intel.com"
"apt-source": "deb [signed-by=/usr/share/keyrings/intel-graphics.gpg] https://repositories.intel.com/gpu/ubuntu noble unified"
"apt-list-file": "/etc/apt/sources.list.d/intel-graphics.list"
"gpg-key": "https://repositories.intel.com/gpu/intel-graphics.key"
"key-file": "/usr/share/keyrings/intel-graphics.gpg"
"pkg-list": ["clinfo","libze-intel-gpu1","intel-media-va-driver-non-free","intel-opencl-icd"]
---
"pkg-list": ["https://github.com/intel/linux-npu-driver/releases/download/v1.38.0/linux-npu-driver-v1.38.0.20260910-34487311128-ubuntu2404.tar.gz"]
---
```

### A.8 How ICT processes the output

ICT imports the output file on the build host before it resolves packages:

1. **Download.** ICT downloads the output file named in the template
   (`sbomUrl`) and the CLI (`url`), and verifies each against its SHA-256
   (`sbomSha256`, `sha256`). It downloads the raw files
   (`raw.githubusercontent.com`), not the GitHub web pages, pinned to a
   specific commit or tag. Downloading from the private sandbox repository
   needs a GitHub token, which ICT sends only to GitHub hosts. This is no
   longer needed once the files are published in a public repository.
2. **Parse and validate.** ICT reads the YAML stream and checks that:
   - the first document is the header, its `schema-version` is supported,
     its `module-list` equals the template's `modules`, and its `arguments`,
     when present, equal the template's `args`
   - every document matches the JSON Schema in the appendix
   - the rules the schema cannot express hold: `signed-by` in `apt-source`
     equals `key-file`, and every package name or glob in `pkg-list`
     resolves from a configured repository
   - kernel packages and `kernel-cmdline` entries match the template, and no
     `-name` entry names a package in the template's package list (A.6)

   Any failure stops the build with an error naming the failing document.
3. **Convert packages.** `pkg-list` entries are added to the image's package
   list as described in A.6: names and globs as given, `name=version`
   converted to ICT's `name_version` form, duplicates removed. URLs, kernel
   packages, and `-name` entries are not added.
4. **Convert repositories.** Each repository group becomes an ICT package
   repository used for resolution: URL, suite, and component from
   `apt-source`, and the key from `gpg-key`. The `apt-pref` stanzas set
   package and repository priorities for resolution (A.6). For the
   image, ICT writes `apt-list-file`, `key-file`, and `apt-pref-file` at the
   installer's paths (A.6) instead of its own generated repository files. If
   the template already defines a repository with the same URL and suite,
   the entry from the output replaces it.
5. **Compose.** ICT resolves the complete dependency set, installs it, and
   records it in the image SBOM, as for any other template packages.

### A.9 Open questions for the OEP Installer team

1. **Install options.** The EdgePack module installs with
   `--no-install-recommends` and allows downgrades of a fixed package list.
   Neither appears in the SBOM output. Are they needed for the result to be
   correct, or only to make the install on a Canonical image succeed?
2. **Incompatible preference files.** EdgePack removes
   `/etc/apt/preferences.d/priorities` and `edgepack.pref` before installing.
   Can ICT images contain these files?
3. **`ros2-apt-source`.** This module installs a `.deb` that configures an apt
   repository, and looks up the latest release online. Profiles that use it
   are out of scope until it has a repository group with a pinned version.

---

## Part B: First-boot execution

ICT copies the pinned CLI (`url`, `sha256`) into the image, so first boot
does not need to download the installer. It should be the CLI that generated
the output file (`cli-url` in A.5).

ICT runs the installer from a first-boot provisioning unit, after the network
is online, as the template-defined user. It first runs the copied file's
`setup` command, which saves the CLI as `~/.local/bin/openedge-cli` and adds
bash completion and an agent skill file, then runs the installation:

```text
<copied CLI file> setup
openedge-cli install <module> [<module>...] [<argument>...]
```

The modules and arguments are the template's `modules` and `args`, which
match the output file's header (A.5).

- **Sudo.** ICT gives the user passwordless sudo only while the unit runs,
  so `ensure_sudo` takes its `sudo -n` path. The grant is a separate
  `/etc/sudoers.d/` drop-in and does not change any sudo rights the user
  already has. The unit adds it at the start of each attempt and removes it
  whenever the attempt ends, on success or failure, including when the unit
  is stopped for a reboot. A `tmpfiles.d` rule also deletes it early in every
  boot, so a power loss or crash cannot leave it in place.
- **Proxy.** The unit inherits the image's proxy settings (`http_proxy`,
  `https_proxy`, `no_proxy`).
- **Retries and reboots.** The unit runs on every boot until the command exits
  `0`. When `openedge-cli` reboots the system itself (the `reboot` module), the
  unit runs `install` again after the reboot.

This relies on behavior `openedge-cli` already has. Please confirm:

1. With no TTY and passwordless sudo, `install` never prompts or waits for
   input.
2. Running the same `install` command again after a reboot or a failure continues the
   installation, as `resume` does. Steps already done are skipped, including
   packages and repositories ICT installed.

Profiles that need credentials (for example model-hub tokens) at install time
are out of scope for version 1.

---

## Appendix: JSON Schema (per document)

Validate each document in the stream on its own. The first document must
match `header`, and every other non-empty document must match `component`.
The OEP Installer validator, `scripts/validate-sbom.py` in the
`oep-installer-cli` repository, uses the same schema; keep the two
identical.

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://open-edge-platform.github.io/image-composer-tool/oep-sbom-v1.schema.json",
  "title": "openedge-cli list --sbom document (schema version 1)",
  "oneOf": [{ "$ref": "#/$defs/header" }, { "$ref": "#/$defs/component" }],
  "$defs": {
    "nonEmpty": { "type": "string", "minLength": 1 },
    "pkg": {
      "type": "string",
      "anyOf": [
        { "pattern": "^[a-z0-9][a-z0-9+.-]+(=[A-Za-z0-9.+~:-]+)?[*]?$" },
        { "pattern": "^[a-z0-9*?\\[][a-z0-9+.*?\\[\\]-]*$" },
        { "pattern": "^https://\\S+$" },
        { "pattern": "^http://\\S+$" },
        { "pattern": "^-[a-z0-9][a-z0-9+.-]+$" }
      ]
    },
    "header": {
      "type": "object",
      "required": ["schema-version", "cli-version", "module-list"],
      "properties": {
        "schema-version": { "const": "1" },
        "cli-version": { "$ref": "#/$defs/nonEmpty" },
        "cli-url": { "$ref": "#/$defs/url" },
        "module-list": { "type": "array", "minItems": 1, "items": { "$ref": "#/$defs/nonEmpty" } }
      }
    },
    "url": {
      "type": "string",
      "anyOf": [
        { "pattern": "^https://\\S+$" },
        { "pattern": "^http://\\S+$" }
      ]
    },
    "component": {
      "type": "object",
      "not": { "required": ["schema-version"] },
      "properties": {
        "component": { "$ref": "#/$defs/nonEmpty" },
        "pkg-list": { "type": "array", "minItems": 1, "items": { "$ref": "#/$defs/pkg" } },
        "kernel-cmdline": { "type": "array", "minItems": 1, "items": { "type": "string", "pattern": "^\\S+$" } },
        "apt-repository": { "enum": ["universe", "multiverse", "restricted"] },
        "repo-name": { "$ref": "#/$defs/nonEmpty" },
        "gpg-key": { "$ref": "#/$defs/url" },
        "gpg-key-fingerprint": { "type": "string", "pattern": "^[0-9A-F]{40}$" },
        "key-file": { "type": "string", "pattern": "^/[^\\s]+$" },
        "apt-source": { "type": "string", "pattern": "^deb (\\[[^\\]]*\\] )?https?://\\S+ \\S+ \\S+$" },
        "apt-src-source": { "type": "string", "pattern": "^deb-src " },
        "apt-list-file": { "type": "string", "pattern": "^/etc/apt/sources\\.list\\.d/[^/\\s]+$" },
        "apt-pref": { "type": "array", "minItems": 1, "items": { "type": "string" } },
        "apt-pref-file": { "type": "string", "pattern": "^/etc/apt/preferences\\.d/[^/\\s]+$" }
      },
      "dependentRequired": {
        "repo-name": ["gpg-key", "key-file", "apt-source", "apt-list-file"],
        "gpg-key": ["key-file", "apt-source", "apt-list-file"],
        "key-file": ["gpg-key", "apt-source", "apt-list-file"],
        "apt-source": ["gpg-key", "key-file", "apt-list-file"],
        "apt-list-file": ["gpg-key", "key-file", "apt-source"],
        "apt-src-source": ["apt-source"],
        "apt-pref": ["apt-pref-file"],
        "apt-pref-file": ["apt-pref"]
      },
      "anyOf": [
        { "required": ["pkg-list"] },
        { "required": ["kernel-cmdline"] },
        { "required": ["apt-source"] },
        { "required": ["apt-repository"] }
      ]
    }
  }
}
```

ICT also checks the rules in A.8 that the schema cannot express: `signed-by`
in `apt-source` equals `key-file`, package names and globs resolve, kernel
packages and `kernel-cmdline` match the template, and `-name` entries do not
name template packages.
