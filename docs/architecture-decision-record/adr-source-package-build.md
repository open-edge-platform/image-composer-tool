# ADR: Building Packages from Source During Image Composition

**Status**: Proposed
**Date**: 2026-09-10
**Authors**: ICT Team
**Technical Area**: Package Management / Build System

---

## Summary

Add first-class support for building a binary package from a **pinned source
package** during image composition. A new top-level `sourcePackages`
field lets a template point at a Debian source package (`.dsc`) or RPM source
package (`.src.rpm`). ICT builds it in a **separate build
chroot** (distinct from the target rootfs, for filesystem separation — not a
security sandbox) before the package-install pass,
automatically resolving build dependencies from the source package's own
metadata, and deposits the resulting binary package into the local repository
the installer already consumes. The built package is then installed through the
normal apt/dnf-from-local-repo path — **without any `dpkg -i`/`rpm -i` and
without a `configurations.cmd` build script**.

The motivating case is shipping a package (e.g. QEMU) for which a pre-built
binary artifact is not available and which therefore must be compiled as part of
the compose.

---

## Context

### Problem Statement

Some images need a package for which no pre-built binary is available through a
configured repository, and for which producing the artifact outside the compose
is not an option. Today the only way to get such a package into an image is the
generic escape hatch `systemConfig.configurations[].cmd`, i.e. a hand-authored
shell command that downloads source, compiles it, and installs the result with
`dpkg -i`.

That approach has significant drawbacks:

- **Opaque and unauditable** — the entire build is an arbitrary `bash -c`
  string; the shell allowlist cannot reason about it, and Trivy/SBOM tooling
  cannot audit what the build fetches or compiles.
- **No runtime dependency resolution** — `dpkg -i` installs a bare file and does
  not pull the package's *runtime* dependencies; separately, the *build*
  dependencies must be hand-enumerated in the template.
- **Non-deterministic** — commands typically fetch from a moving upstream at
  build time.
- **Untracked payload (source builds)** — a `make install` from source leaves
  files the package database and scanners do not know about. (A `dpkg -i` of a
  locally produced `.deb`, by contrast, *does* register in the package database;
  its problem is the missing runtime-dependency resolution above, not
  visibility.)
- **Slated for removal** — `dpkg -i`-in-`cmd` is a pattern the project intends to
  retire in favor of first-class, typed template constructs.

### Current System

ICT already assembles images from packages using a local-repository install
path:

- `packageRepositories[].packages[]` **stages** binary package files (`.deb`/
  `.rpm`, from an `https://` URL, a local file, or a directory) into a local
  repository. It does **not** install them; to be installed, the package **name**
  must also appear in `systemConfig.packages[]`
  (`internal/ospackage/debutils/download.go`, install list built in
  `internal/image/imageos/imageos.go`).
- Installation is performed by building a local file repository from the package
  cache (`dpkg-scanpackages`) and installing via **apt against that local repo**
  (`internal/chroot/deb/installer.go`) — `dpkg -i` is **not** used on the normal
  path. The RPM path is analogous.
- The local-repo scan and the package install form a **single early block**
  inside `installImagePkgs`; the local repo is torn down immediately afterward,
  and custom `configurations` commands run strictly **after** that block. As a
  result, an artifact produced *late* in the build (e.g. by a `cmd`) cannot be
  fed back into the apt-from-local-repo install in the same compose.

### Key Design Constraint

The source-built binary must be produced **and indexed before package
resolution**, not merely before installation. During `PreProcess`,
`DownloadPackagesComplete` resolves every `systemConfig.packages[]` name against
a candidate set built from repository metadata (`Packages`/`UserPackages`/
`LocalUserPackages`), consulting the package cache only *after* matching; a binary
that is not present as resolver-visible repository metadata at that point is
reported missing. The build and local-metadata
generation must therefore run at the **start of the download/resolve phase**; the
install pass (which scans the local repo once, early, then dismantles it) then
consumes the already-built artifact.

Building must happen in a **separate build chroot** (which carries its own build
toolchain) rather than the target rootfs — this satisfies the ordering constraint
and keeps the build toolchain out of the shipped image. This build chroot
provides filesystem separation and image cleanliness, **not** a security sandbox
(see *Build Environment Separation* below).

---

## Decision

Add an optional **top-level** `sourcePackages` list (a sibling of
`packageRepositories`, not an entry inside it — see *Schema placement* below).
Each entry references a **pinned, checksummed** source package. During compose,
ICT:

1. Downloads the source package (and its components) in the pre-build download
   phase.
2. Creates a **separate build chroot** for the target distribution/architecture
   (filesystem separation, not a security sandbox — see below).
3. **Automatically installs build dependencies** from the source package's own
   declared metadata (`Build-Depends` in `debian/control` / `BuildRequires` in
   the `.src.rpm`'s spec) using the distribution's native resolver — the user
   specifies nothing about build dependencies. (Resolver availability differs by
   RPM provider — see the *RPM resolver split* in *Changes Required*.)
4. Builds the binary package with the distribution's standard builder. For a
   `.dsc`, unpack the source tree with `dpkg-source -x <file>.dsc` and run
   `dpkg-buildpackage -b -us -uc` **inside the extracted directory** — `-us -uc`
   make it an **unsigned** build, since the throwaway chroot has no maintainer key
   and the binary is installed from a local repo, so `.buildinfo`/`.changes`
   signing is neither possible nor needed (`dpkg-buildpackage` also does not accept
   a `.dsc` directly). For a `.src.rpm`, `rpmbuild --rebuild`.
5. Registers the built binary package as a **resolver-visible local repository**
   — generating repository metadata so the artifact enters the resolver's
   candidate set (alongside `Packages`/`UserPackages`/`LocalUserPackages`)
   **before** `DownloadPackagesComplete` matches names — not merely copied into the
   install cache (which the resolver consults only *after* matching). The resolved
   artifact is then staged in the install cache and installed by the normal pass.

The built binary package is installed exactly like any other package: by naming
it in `systemConfig.packages[]`. This mirrors the existing stage-vs-install
split of `packageRepositories[].packages[]`. Selection is made deterministic when
a configured repository also provides the same name — see *Selection precedence*
under *Validation Rules*.

### Schema placement

`sourcePackages` is a **top-level** template section rather than an entry inside
`packageRepositories[]`. A source entry cannot satisfy the existing
`PackageRepository` selector (which requires `codename` plus one of `path`,
`url`, or `packages`) or its signing-key requirement (`pkey`/`pkeys`), and those
semantics do not apply to a single checksummed source artifact. Integrity for a
source entry is provided **per entry by the mandatory `checksum`**, so it is
modeled as its own section. Built artifacts are still deposited into the same
local repository the installer already scans, so the install path is unchanged.

### Build Environment Separation and Image Cleanliness

The build chroot exists **only** to produce the binary package and is
**discarded** once the artifact has been captured. This provides **filesystem
separation and image cleanliness — it is not a security sandbox** (see the
security note below).

- It is a **separate, throwaway** environment, distinct from the target rootfs.
  It is created for the source build and **torn down/deleted** immediately after
  the binary package is copied out.
- The **build toolchain and all build dependencies** (`build-essential`,
  `meson`, `-dev` headers, etc.) are installed **into the build chroot, never
  into the target rootfs**. They do **not** appear in the final image.
- The **only** thing that crosses from the build chroot into the image is the
  resulting **binary package** (and, through the normal install path, its
  declared **runtime** dependencies). Nothing else — no source tree, no
  intermediate build artifacts, no compilers.
- Consequently the shipped image stays minimal (the image-cleanliness property is
  a firm guarantee); the toolchain's presence is scoped to the ephemeral build
  environment.

**Security note — the build chroot is not a security boundary.** Building a
source package executes arbitrary `debian/rules` (or RPM scriptlets) on the build
host. A chroot changes only the filesystem root; it still shares the host kernel,
network, PID namespace, and capabilities, and a checksum provides *integrity*,
not *trust*. Therefore:

- **Source packages must be trusted** — pinned and checksummed from a trusted
  origin. This feature is not a mechanism for building untrusted third-party
  sources.
- **The build step should be hardened** — run it with restricted
  mounts/capabilities and with **network access disabled during the build
  itself** (build dependencies are fetched and installed in a preceding, separate
  step, so the compile does not need the network). Any residual host-exposure
  risk must be documented for operators.

### Template Example

```yaml
sourcePackages:                    # top-level section (sibling of packageRepositories)
  - source: https://deb.debian.org/debian/pool/main/q/qemu/qemu_8.2.0-1.dsc
    checksum: sha256:0a1b2c...     # required; component tarballs verified against the .dsc
    # configureFlags / build-profile passthrough is OPTIONAL, not build-deps

systemConfig:
  packages:
    - qemu-system-x86              # name the binary package(s) to install
    - qemu-utils                   # one source package may emit several binaries
```

The user provides only what ICT cannot infer: **where the source is** (pinned +
checksummed) and **which built binaries to install**. Build dependencies, build
commands, and the install step are all handled automatically.

### Build & Install Flow

```mermaid
flowchart TD
    A[Download phase: fetch pinned source package + all components + verify checksums] --> B[Create separate build chroot for target dist/arch]
    B --> C[Auto-resolve build-deps from source metadata\napt-get build-dep / dnf builddep]
    C --> D[Build binary package\ndpkg-source -x + dpkg-buildpackage -b -us -uc / rpmbuild --rebuild]
    D --> E[Register built .deb/.rpm as resolver-visible local repo\n(generate metadata) + stage in cache]
    E --> F[Existing single scan+install pass\napt/dnf from local repo]
    F --> G{Binary pkg named in\nsystemConfig.packages?}
    G -- Yes --> H[Installed into target rootfs — no dpkg -i]
    G -- No --> I[Available but not installed]
    E --> X[Build chroot discarded after artifact captured\ntoolchain never enters target image]

    style H fill:#4a4,color:#fff
    style X fill:#48c,color:#fff
```

### Changes Required

| Component | Change |
|-----------|--------|
| `ImageTemplate` struct | Add top-level `SourcePackages []SourcePackage` (`source`, `checksum`, optional passthrough) — a sibling of `PackageRepositories`, not a `PackageRepository` field |
| JSON schema | Add top-level `sourcePackages`; require `source` + `checksum`; do not extend the `PackageRepository` selector/`pkey` constraints |
| `validate` command | Validate pinned source reference, mandatory checksum (strong algorithm; incl. transitive `.dsc` components), path/URL safety |
| Download phase (`PreProcess`) | Fetch source package + all referenced components; verify each against the checksummed descriptor |
| Build chroot (new) | Create separate build env; install build-deps via native resolver; run distro builder; capture artifacts; harden (restricted caps/mounts, no network during compile) |
| Resolver-visible local repo | Expose the built binary as a generated local repository whose metadata enters the resolver candidate set (`LocalUserPackages` / repo metadata) **before** `DownloadPackagesComplete` matches names — not a cache-dir copy (the cache is consulted only after matching); then stage the resolved artifact in the install cache |
| Selection precedence | Give the source-build local repo deterministic precedence over configured repos providing the same name (priority/pinning), or fail on collision |
| Shell allowlist | Add `dpkg-source` / `dpkg-buildpackage` / `apt-get build-dep` (deb) and `rpmbuild` / `dnf builddep` (dnf-based rpm), with justification |
| Caching / provenance | Cache built binary packages keyed on source ref + version + arch + **resolved build-dep versions + toolchain/builder version + build config**; record the same in provenance |
| Provider wiring | Run source-build at the start of the download/resolve phase (before `DownloadPackagesComplete`); deb vs rpm behind `OsName` |
| RPM resolver split | `dnf builddep` is available on `dnf`-based providers (RCD); `tdnf`-based providers (AZL/EMT, per `internal/chroot/chrootenv.go`) lack it — scope initial RPM support to `dnf`, and define `tdnf` `BuildRequires` resolution as a follow-up |
| Tests | Build one source package end-to-end per supported OS/arch; checksum failure; unresolved build-dep; missing binary name |
| Documentation | Template docs, CLI/usage docs, security objectives, release notes |

### Validation Rules

- **Pinned source required** — `source` must reference a specific version as a
  complete source package (`.dsc` for Debian, `.src.rpm` for RPM), not a moving
  branch or `latest`. A standalone `.spec` is **not** a source package (it
  requires separately fetched `Source`/`Patch` inputs) and is out of scope.
- **Checksum mandatory, strong algorithm** — every `sourcePackages` entry must
  carry a `checksum` in `<algo>:<hex>` form using a **cryptographically strong
  algorithm (SHA-256 or stronger)**; MD5, SHA-1, and digests whose length does not
  match the named algorithm are rejected by validation. For a **`.dsc`**, the
  descriptor is verified and then **every component it references**
  (`.orig`/`debian` tarballs) is verified against the hashes *inside* that
  descriptor before any build runs — pinning only the `.dsc` is insufficient. For a
  **`.src.rpm`**, the single top-level digest covers its embedded sources/patches,
  so verifying the `.src.rpm` digest is sufficient for its payload.
- **Reproducible build inputs** — build dependencies are resolved from the
  configured repositories, which can move independently of the source checksum.
  To hold the determinism and cache guarantees, deployments should use
  **snapshot-pinned repositories** and/or ICT should **lock the exact resolved
  build-dependency versions** (plus toolchain/builder version and any build
  configuration) into build provenance and the cache key (see *Caching* and
  *Risks*).
- **Path/URL safety** — remote sources over the project secure HTTP client
  (TLS 1.2+); local paths cleaned with `filepath.Clean` and confined; symlinks
  rejected.
- **Stage vs install** — listing a source package builds and stages it; it is
  installed only if a resulting binary package name appears in
  `systemConfig.packages[]` (consistent with `packageRepositories[].packages[]`).
- **Selection precedence for source-built binaries** — naming a binary in
  `systemConfig.packages[]` does **not** by itself guarantee the source-built
  artifact is chosen when a configured repository also provides that name: the
  Debian and RPM resolvers pick among duplicate candidates by repository priority
  and version, so a repository copy (e.g. a higher version) could win. ICT must
  make this deterministic — give the source-build's local repository higher
  precedence (repository priority/pinning, analogous to
  `packageRepositories[].priority`/`allowPackages`) **or fail on collision** — and
  must not fall back to default version-ordering.
- **Deterministic failure** — if a declared build dependency is not resolvable
  from the configured repositories, or the build fails, the compose fails at
  build time with a clear error; no partial/loose install occurs.

---

## Consequences

### Benefits

- **First-class and cmd-free** — replaces the `dpkg -i`-in-`cmd` build pattern
  with a typed, validated construct; the build recipe lives in the source
  package, not an opaque shell string.
- **Zero build-dependency burden on the user** — build-deps are read from the
  source package's own metadata and installed automatically.
- **Tracked and scannable** — the result is a real package in the package
  database, visible to Trivy and SBOM tooling; nothing loose in the rootfs.
- **Dependency resolution** — runtime dependencies are pulled via the normal
  install path (unlike `dpkg -i`).
- **Minimal, separate build environment** — the build toolchain lives in a
  throwaway build chroot and never enters the shipped image.
- **Reuses existing infrastructure** — the built package flows through the same
  local-repo/apt install path as every other package.

### Risks and Mitigations

| Risk | Mitigation |
|------|-----------|
| Compile cost on every compose | Cache built binary packages (see *Caching / provenance* key) |
| Build output varies as configured repos move (build-deps) even with an unchanged source checksum | Snapshot-pin build repositories and/or lock resolved build-dep + toolchain versions into provenance and the cache key |
| Build depends on network/upstream availability | Pinned + checksummed source (incl. transitive components); fetch confined to the download phase; fail deterministically |
| Build-deps unavailable from configured repos | Clear build-time failure; user adds a repo providing them |
| Build runs arbitrary `debian/rules`/scriptlets on the host, and a chroot is not a security boundary | Restrict source packages to trusted, pinned origins; harden the build step (restricted caps/mounts, no network during compile); document residual host-exposure risk |
| `tdnf`-based RPM providers (AZL/EMT) lack `dnf builddep` | Scope initial RPM support to `dnf`-based providers (RCD); define `tdnf` `BuildRequires` resolution as a follow-up |
| A configured repo provides the same package name (possibly a higher version) as the source-built binary | Give the source-build local repo deterministic precedence (priority/pinning) or fail on collision — never silently install the repo copy |
| New builder commands widen the shell allowlist | Add only the specific distro builder/build-dep commands, with justification; run inside the sanctioned shell layer rather than opaque `bash -c` |
| Source package may emit multiple binaries | User selects which binaries to install by name in `systemConfig.packages[]` |

### Alternatives Considered

#### Build via `systemConfig.configurations[].cmd` (the current escape hatch) — Rejected

This is how the requirement would be met **today**, and is the baseline this ADR
replaces. A template embeds an arbitrary shell command that installs a toolchain,
fetches source, compiles, and installs the result. Two shapes exist in practice:

```yaml
# Shape A: compile from source in a cmd, then install
systemConfig:
  configurations:
    - cmd: "apt-get install -y build-essential meson ninja-build \
            libglib2.0-dev libpixman-1-dev && \
            wget https://.../qemu-8.2.0.tar.xz && tar xf qemu-8.2.0.tar.xz && \
            cd qemu-8.2.0 && ./configure --prefix=/usr --target-list=x86_64-softmmu && \
            make -j\"$(nproc)\" && make install"

# Shape B: fetch a package file and force-install it
systemConfig:
  configurations:
    - cmd: "wget https://.../qemu_8.2.0-1_amd64.deb -O /tmp/qemu.deb && dpkg -i /tmp/qemu.deb"
```

**Why it is rejected:**

- **Opaque and unauditable.** The entire build is a single `bash -c` string. The
  shell allowlist cannot reason about what runs inside it, and Trivy/SBOM tooling
  cannot audit what the build fetches or compiles.
- **Build dependencies are the user's burden.** Every consumer must hand-maintain
  the build-dep list (`build-essential`, `meson`, `libglib2.0-dev`, …) in the
  template and keep it in sync with upstream — versus reading them from the
  source package's own metadata.
- **Non-deterministic.** Commands typically fetch from a moving upstream at build
  time, with no enforced checksum, so two composes of the "same" template can
  differ.
- **Install is either `dpkg -i` or `make install` — both bad, and this is forced
  by phase ordering.** The local-repo scan + apt install is a single early block
  that is torn down *before* any `configurations` command runs. So a `cmd`
  cannot install through the dependency-resolving apt-from-local-repo path; its
  only options are `dpkg -i` (no runtime dependency resolution) or `make install`
  (untracked files invisible to the package database and scanners). Shape B's
  `dpkg -i` is precisely the pattern the project intends to retire.
- **Runs after package installation.** Because the `cmd` executes after the main
  install pass, ordering and interactions with already-installed packages are
  fragile and hard to reason about.
- **No reuse, guaranteed drift.** Each package that needs this copies and
  maintains its own bespoke build script; there is no shared, validated
  construct.

A structured `sourceBuilds: { steps: [...] }` field was also considered as a
"cleaner" variant, but a list of build commands merely relocates the same opaque
`bash -c` into a new field without gaining auditability, determinism, or
automatic build-dep resolution. Delegating the recipe to the source package's own
build system — the decision in this ADR — is what actually makes the construct
first-class.

#### Other alternatives

- **Consume a pre-built binary package built outside the compose** — Rejected for
  this requirement; producing the artifact externally is not an option here.
- **`make install` from a raw upstream source tree into the target rootfs** —
  Rejected. Leaves untracked files invisible to the package database and
  scanners; pollutes the target with build toolchain.
- **Building in the target rootfs instead of a separate build chroot** — Rejected.
  Conflicts with the single early install pass (ordering), and leaks the build
  toolchain into the shipped image.

---

## Non-Goals

- A general-purpose "compile arbitrary source" framework — this is scoped to
  building a **distribution source package** into a binary package.
- Building from raw, unpackaged upstream source trees.
- Building a standalone `.spec` without a complete `.src.rpm` — it requires
  separately fetched `Source`/`Patch` inputs and is out of scope.
- Building **untrusted** third-party source packages — sources must be trusted,
  pinned, and checksummed; this is not a sandbox for arbitrary/unknown sources.
- `tdnf`-based RPM build-dependency resolution — initial RPM support targets
  `dnf`-based providers; `tdnf` is a follow-up.
- Runtime/first-boot building on the target device.
- Retiring `configurations[].cmd` itself — this ADR removes one use of it (source
  compile + install); other uses require their own separate replacements.
