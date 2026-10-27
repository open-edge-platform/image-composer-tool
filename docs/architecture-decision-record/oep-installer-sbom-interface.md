# Interface Specification: OEP Installer → ICT Profile Package Requirements

- **Status:** Draft (for agreement between the OEP Installer and ICT teams)
- **Date:** 2026-10-27
- **Schema version:** `1`
- **Implements:** [ADR: Integrate OEP Installer Profiles Through a Solution-Owned DEB Dependency Interface](./adr-oep-installer-intent-manifest.md)

## Purpose

This document defines the contract between `openedge-cli` (OEP Installer) and
the Image Composition Tool (ICT):

- **Part A (compose time):** the command ICT runs to get a profile's
  repository-installable Debian packages, and the format of its output
- **Part B (first boot):** how ICT invokes `openedge-cli` on the deployed
  system to finish the profile installation

OEP Installer remains the source of truth for each profile. ICT installs the
repository-installable packages before first boot. `openedge-cli` installs
everything else on first boot.

The contract builds on the current `openedge-cli list --sbom` output and
keeps its key names. The changes requested from `openedge-cli` are:

1. Accept `--os-release <file>` so the output describes the target image,
   not the build host (A.1).
2. Exit non-zero, with a message on stderr, for an unknown profile (A.3).
3. Print a header document with `schema-version`, `cli-version`, and
   `profile` first (A.5).
4. In the `docker_ce` handler, rename `apt-entry` to `apt-source` and
   `apt-file` to `apt-list-file` (A.6).
5. For packages pinned with `Pin: version`, also list the exact version in
   `pkg-list` (A.6). The first profile, `handheld_multi_modal`, has no such
   pins.
6. Validate the `list --sbom` output of every profile against the JSON Schema
   in the appendix, ideally in CI. This catches problems like item 4 before
   ICT sees them.

The key words MUST, MUST NOT, SHOULD, and MAY are used as in RFC 2119.

---

## Part A: Compose-time query

### A.1 Command

```text
openedge-cli list --sbom --os-release <file> <profile>
```

| Input | Meaning |
| --- | --- |
| `<profile>` | Exactly one profile identifier, as listed by `openedge-cli list`. |
| `--os-release <file>` | An `os-release(5)` file describing the **target image**. The CLI MUST read it instead of the host's `/etc/os-release`. |

ICT runs the query on the build host, which is often a different Ubuntu
release, or a different distribution, than the image it builds. Today the CLI
reads the host's `/etc/os-release`, so repository suites and `.deb` URLs come
out for the build host instead of the target. For example, an Ubuntu 22.04
build host gets `jammy` repositories for a `noble` image.

ICT generates the file from the template's target in a temporary directory.
It is not copied into the image. The file sets these fields, and the CLI MUST
NOT rely on any others: `NAME`, `PRETTY_NAME`, `ID`, `ID_LIKE`, `VERSION_ID`,
`VERSION_CODENAME`, and `UBUNTU_CODENAME`. For an Ubuntu 24.04 target:

```ini
NAME="Ubuntu"
PRETTY_NAME="Ubuntu 24.04 LTS"
ID=ubuntu
ID_LIKE=debian
VERSION_ID="24.04"
VERSION_CODENAME=noble
UBUNTU_CODENAME=noble
```

Version 1 covers `amd64` targets only.

### A.2 Behavioral requirements

The query MUST:

1. **Have no side effects.** It must not use `sudo`, install packages, or
   change apt configuration.
2. **Be reproducible.** The same CLI version, profile, and `--os-release`
   contents produce the same output. Values must not come from online
   "latest release" lookups.
3. **Use the same definitions as `install`.** The packages and repositories
   reported for a profile must be the ones `install` would set up for it.
4. **Keep stdout clean.** Only the YAML stream (A.4) goes to stdout.
   Diagnostics go to stderr.

### A.3 Exit status

| Exit | Meaning | ICT behavior |
| --- | --- | --- |
| `0` | Success | Use the output. A header with no component documents is valid: the profile needs no DEBs. |
| non-zero | Unknown profile, unsupported target, or internal error | Fail the build and show stderr. |

Today an unknown profile produces empty output and exit `0`. ICT cannot tell
that apart from a valid profile with no package requirements.

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
| `cli-version` | string | yes | Released version of `openedge-cli` (for example `"v2026.3.0"`). |
| `profile` | string | yes | The profile the output was produced for. |

### A.6 Component documents

A component document is what one `*_sbom_*` handler prints today. It contains
a package list, a repository group, or both. A document with only a
repository group configures the repository without requesting packages from
it.

#### Package list

| Key | Type | Description |
| --- | --- | --- |
| `pkg-list` | array of string | Packages the profile requires. Each entry is one of: a package name; `name=version`, where `version` is an exact Debian version string; a glob over package names (`*`, `?`, `[...]`); or an `https://` URL of an artifact that is not in an apt repository. |
| `component` | string | Optional. Module name (for example `"docker_io"`), shown in ICT error messages. |
| `apt-repository` | string | Optional. An Ubuntu archive component (`universe`, `multiverse`, `restricted`). ICT images already enable all of them, so ICT ignores this key. |

`pkg-list` contains **direct** requirements only. ICT resolves and installs
the transitive dependency closure and records it in the image SBOM. The same
package may appear in several documents.

ICT handles `pkg-list` entries as follows:

| Entry | ICT behavior |
| --- | --- |
| Package name or `name=version` | Resolved from the template's repositories plus the repositories in the output, and installed. A package that no configured repository provides fails the build. Two `name=version` entries for the same package with different versions fail the build. |
| Glob (for example `intel-oneapi-runtime-*`) | Expanded to **every** matching package name in the configured repositories. For each name, a `name=version` entry anywhere in the stream decides the version. Otherwise ICT chooses the version as for a plain package name: repository priority first (including `Pin: origin` priorities from the output), then the newest version. A glob that matches nothing fails the build. |
| `https://` URL (for example the `libze1` `.deb` or the `linux-npu-driver` `.tar.gz`) | Skipped. `openedge-cli` installs it on first boot, so it is not in the ICT-generated SBOM, even when it is a `.deb`. |
| Kernel package (`linux-image-*`, `linux-headers-*`, `linux-modules-*`, `linux-generic*`) | Skipped with a warning. The image template selects the kernel. |

#### Repository group

These are the keys the current CLI already prints. If any of the required
keys is present, all of them MUST be.

| Key | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | no | Repository host or label (for example `"repositories.intel.com"`), shown in ICT messages. |
| `gpg-key` | string | yes | HTTPS URL of the signing key. |
| `key-file` | string | yes | Absolute path of the dearmored keyring in the image. |
| `apt-source` | string | yes | One one-line `deb` entry: `deb [options] <uri> <suite> <component>`, with exactly one component. ICT takes the repository URL, suite (codename), and component from this line. The suite is usually the release codename, but may be a fixed value the repository uses (for example `all` or `ubuntu24`). |
| `apt-list-file` | string | yes | Absolute path of the list file under `/etc/apt/sources.list.d/`. |
| `apt-src-source` | string | no | The matching `deb-src` entry. ICT writes it to `apt-list-file` but does not use it. |
| `apt-pref` | array of string | no | Lines of an `apt_preferences(5)` file. A new stanza starts at each `Package:` line. Requires `apt-pref-file`. |
| `apt-pref-file` | string | with `apt-pref` | Absolute path under `/etc/apt/preferences.d/`. |
| `gpg-key-fingerprint` | string | no | Full 40-hex-digit OpenPGP fingerprint. When present, ICT verifies the downloaded key against it. |

ICT writes `apt-list-file`, `key-file`, and `apt-pref-file` into the image at
these exact paths, with the content `openedge-cli install` would write. When
`openedge-cli` later writes the same files on first boot, it replaces them
with equivalent content. If ICT wrote the repository anywhere else, apt would
see the same source twice with different `Signed-By` values, and
`apt-get update` could fail with "Conflicting values set for option
Signed-By".

At compose time, ICT's dependency resolution uses only one form of `apt-pref`
stanza: `Package: *` with `Pin: origin <host>`, taken as the priority of that
repository. ICT does not use `Pin: version` stanzas. A package the profile
needs at a pinned version MUST therefore also appear in `pkg-list` as
`name=version`, with the exact version the pin selects. For example, a stanza
pinning `intel-oneapi-runtime-*` to `2025.3.*` comes with exact entries such
as `"intel-oneapi-runtime-dpcpp-cpp=2025.3.1-123"` (illustrative version).
Without them, ICT installs the newest version in the repository, and
`openedge-cli` finds a different version from the one it pins.

#### Fix in the current CLI

The `docker_ce` handler prints `apt-entry` and `apt-file`. The emitter
rejects both as unsupported keys, so that block loses its repository today.
They MUST be `apt-source` and `apt-list-file`.

### A.7 Example

`openedge-cli list --sbom --os-release ./os-release-noble handheld_multi_modal`
(abridged):

```yaml
"schema-version": "1"
"cli-version": "v2026.3.0"
"profile": "handheld_multi_modal"
---
"pkg-list": ["curl","ca-certificates"]
---
"pkg-list": ["https://github.com/oneapi-src/level-zero/releases/download/v1.32.0/libze1_1.32.0+u24.04_amd64.deb"]
---
"pkg-list": ["docker.io","docker-buildx","docker-compose-v2"]
---
"name": "repositories.intel.com"
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

ICT runs the query on the build host before it resolves packages:

1. **Run.** ICT downloads the pinned `openedge-cli`, verifies its SHA-256,
   generates the target `os-release` file (A.1), and runs the query. A
   non-zero exit fails the build and shows stderr.
2. **Parse and validate.** ICT reads the YAML stream and checks that:
   - the header's `schema-version` is supported and its `profile` is the one
     requested
   - every document matches the JSON Schema in the appendix
   - the rules the schema cannot express hold: `signed-by` in `apt-source`
     equals `key-file`, and every non-URL, non-kernel `pkg-list` entry
     resolves from a configured repository

   Any failure stops the build with an error naming the failing document.
3. **Convert packages.** `pkg-list` entries are added to the image's package
   list as described in A.6: names and globs as given, `name=version`
   converted to ICT's `name_version` form, URLs and kernel packages skipped,
   duplicates removed.
4. **Convert repositories.** Each repository group becomes an ICT package
   repository used for resolution: URL, suite, and component from
   `apt-source`, the key from `gpg-key`, and the priority from a
   `Package: *` / `Pin: origin` stanza in `apt-pref`, if present. For the
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
4. **Distribution.** Where will tagged releases of `openedge-cli` be published
   (stable URL plus SHA-256)? ICT pins both in the image template.

---

## Part B: First-boot execution

ICT copies the pinned `openedge-cli` it used for the compose-time query into
the image. The query and the first-boot install therefore use the same CLI
version, and first boot does not need to download the installer.

ICT runs the installer from a first-boot provisioning unit, after the network
is online, as the template-defined user:

```text
openedge-cli install <profile> [<template-provided arguments>...]
```

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
2. Running `install <profile>` again after a reboot or a failure continues the
   installation, as `resume` does. Steps already done are skipped, including
   packages and repositories ICT installed.

Profiles that need credentials (for example model-hub tokens) at install time
are out of scope for version 1.

---

## Appendix: JSON Schema (per document)

Validate each document in the stream on its own. The first document must
match `header`, and every other non-empty document must match `component`.

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
        { "pattern": "^[a-z0-9][a-z0-9+.-]+(=[A-Za-z0-9.+~:-]+)?$" },
        { "pattern": "^[a-z0-9*?\\[][a-z0-9+.*?\\[\\]-]*$" },
        { "pattern": "^https://\\S+$" }
      ]
    },
    "header": {
      "type": "object",
      "required": ["schema-version", "cli-version", "profile"],
      "properties": {
        "schema-version": { "const": "1" },
        "cli-version": { "$ref": "#/$defs/nonEmpty" },
        "profile": { "$ref": "#/$defs/nonEmpty" }
      }
    },
    "component": {
      "type": "object",
      "not": { "required": ["schema-version"] },
      "properties": {
        "component": { "$ref": "#/$defs/nonEmpty" },
        "pkg-list": { "type": "array", "minItems": 1, "items": { "$ref": "#/$defs/pkg" } },
        "apt-repository": { "enum": ["universe", "multiverse", "restricted"] },
        "name": { "$ref": "#/$defs/nonEmpty" },
        "gpg-key": { "type": "string", "pattern": "^https://" },
        "gpg-key-fingerprint": { "type": "string", "pattern": "^[0-9A-F]{40}$" },
        "key-file": { "type": "string", "pattern": "^/[^\\s]+$" },
        "apt-source": { "type": "string", "pattern": "^deb (\\[[^\\]]*\\] )?https?://\\S+ \\S+ \\S+$" },
        "apt-src-source": { "type": "string", "pattern": "^deb-src " },
        "apt-list-file": { "type": "string", "pattern": "^/etc/apt/sources\\.list\\.d/[^/\\s]+$" },
        "apt-pref": { "type": "array", "minItems": 1, "items": { "type": "string" } },
        "apt-pref-file": { "type": "string", "pattern": "^/etc/apt/preferences\\.d/[^/\\s]+$" }
      },
      "dependentRequired": {
        "name": ["gpg-key", "key-file", "apt-source", "apt-list-file"],
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
        { "required": ["apt-source"] },
        { "required": ["apt-repository"] }
      ]
    }
  }
}
```

ICT also checks that `signed-by` in `apt-source` equals `key-file`, and that
every non-URL, non-kernel `pkg-list` entry resolves from a configured
repository.
