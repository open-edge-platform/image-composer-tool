# ADR: Language-ecosystem packages (pip, npm) during image composition

**Status**: Proposed
**Date**: 2026-09-16
**Authors**: Image Composer Tool Team
**Technical Area**: Image Composition / Package Management / SBOM

---

## Summary

The tool installs only OS-native packages today: `systemConfig.packages[]` resolves
to `.deb`/`.rpm` artifacts, which are fetched into a local cache during
`PreProcess` and installed offline inside the chroot. Images that need Python or
Node.js libraries have no declarative path; the only way to get them in is the
generic shell hook `systemConfig.configurations[].cmd`
(`internal/image/imageos/imageos.go`, `addImageConfigs`), which the templates
guide explicitly discourages.

This ADR proposes supporting **pip and npm packages as first-class,
declarative template content**, under a new `systemConfig.languagePackages[]`
block. The design carries over the properties the OS-package path provides —
**artifacts hash-verified before use**, fetch-once-then-install-offline, and an
SBOM entry per installed component — and **adds** exact version pinning, which
the OS path does not have (`systemConfig.packages[]` accepts names and globs, and
the resolver selects candidates from current repository metadata). Pinning comes
from requiring a **hash-pinned lockfile as the input** and declining to implement
a dependency resolver. Packages install **system-wide into a dedicated,
allowlisted target prefix**, never into a user's home directory and never into
the distribution-managed Python `site-packages`.

Scope is deliberately narrow: **build-host create-mode targets only (`raw` and
its artifact formats, plus WSL2), pip first, and npm lifecycle scripts never
executed.** ISO targets are rejected in v1 (Decision 5) because the install runs
later on the target machine, where the staged inputs do not exist; initrd and
**overlay** templates are rejected outright, the latter because providers branch
to the overlay pipeline before any maker runs and would otherwise skip this
stage silently.

## Context

- **Requirement:** ship images with Python/Node libraries baked in (ML/robotics
  and edge-agent images being the recurring drivers) without the operator falling
  back to arbitrary shell commands.
- **The current escape hatch is unacceptable as a sanctioned path.** A
  `cmd: "pip install requests"` entry runs arbitrary shell in the chroot with no
  version pinning, fetches from PyPI/npm live on every build (breaking
  offline/air-gapped builds), resolves dependencies invisibly, and produces **no
  SBOM entry** — the shipped image has components the tool has no record of.
- **The OS-package path sets the bar.** Packages are downloaded once into a cache
  (`internal/ospackage/pkgfetcher/pkgfetcher.go`, `FetchPackages`), a local file
  repository is generated from it, and the chroot install runs against that repo
  only (`internal/chroot/deb/installer.go`, `internal/chroot/rpm/installer.go`).
  Installed packages land in the SPDX SBOM at `/usr/share/sbom`
  (`internal/config/manifest/manifest.go`, `internal/image/imageos/imageos.go`,
  `generateSBOM`).
- **SBOM is OS-package-shaped and filtered against the OS package database.**
  `generateSBOM` (`imageos.go`) shells out to `dpkg -l | awk '/^ii/ {print
  $2}'` or `rpm -qa` inside the chroot and **keeps only** those
  `template.FullPkgListBom` entries whose name appears in the result. A pip or
  npm component never appears in `dpkg -l`, so appending language components to
  `FullPkgListBom` would see them filtered straight back out (Decision 8).
- **SPDX checksum support is narrow.** `validSPDXAlgos` (`manifest.go`) admits
  only `SHA1`, `SHA256`, `MD5`; `buildSPDXPackage` (`manifest.go`) copies
  `Checksum.Value` verbatim; the document declares `SPDX-2.3`.
  `SPDXPackage` (`manifest.go`) has no `externalRefs` field.
- **The shell allowlist is a hard gate — for the top-level command only.**
  `commandMap`
  (`internal/utils/shell/shell.go`) contains no `python3`, `pip`, `node`, or
  `npm` entry, and `GetFullCmdStr` (`shell.go`) resolves through
  `verifyCmdWithFullPath`, which **fails** for any command absent from the map
  — but `bash` **is** already in `commandMap`, and `verifyCmdWithFullPath`
  treats the quoted argument to `bash -c` as opaque (it does not recursively
  verify what's inside the quotes). An existing `configurations[].cmd` can
  therefore already run `bash -c 'python3 -m pip install …'` inside the chroot
  today, with or without this change — the proposed npm installer (Decision 4)
  relies on that exact same opacity for its own `bash -c '…'` wrapper. Adding
  `python3`/`npm` as top-level `commandMap` entries does not close that gap or
  meaningfully expand what a `cmd` hook can already reach; it only sanctions a
  direct, top-level invocation for this stage instead of the indirect one that
  was always available.
  `shell.QuoteArg` (`shell.go`) exists for safe argument construction.
- **Ordering constraint.** Package installation runs in `installImagePkgs`
  (`imageos.go`), before user accounts are created in `createUser`
  (via `updateImageConfig`, both in `imageos.go`). No user or home directory
  exists at package-install time.
- **Install flows differ by *maker*, not just by function.** `InstallRootfs`
  (SBOM in the same function) is driven by `wsl2maker`; `InstallInitrd` (**no**
  `generateSBOM`) by `initrdmaker`; `InstallImageOs` (SBOM near the end of the
  function) by **two** callers — `rawmaker` (on the build host) and
  `cmd/live-installer/install.go` (**on the target machine at install
  time**). An ISO build composes an `initrdmaker` over a *separate initrd
  template* (`isomaker.go`) and ships `template-dump.yaml` for the live
  installer, which rehydrates from `SBOMPackageMetadata` because
  `FullPkgListBom` is not serialized (`install.go`).
- **Cross-arch builds validate host tooling, they do not populate the rootfs.**
  `validateCrossArchDeps` (`internal/chroot/deb/installer.go`) checks that
  `arch-test`, `qemu-<arch>-static`, and `binfmt-support` exist **on the host**
  and errors with an `apt-get install` hint otherwise; foreign-arch execution
  then relies on the host's `binfmt_misc` registration.
- **No shared installer abstraction exists.** deb and rpm are two separately
  shaped interfaces (`deb/installer.go`, `rpm/installer.go`) behind a
  hardcoded `pkgType` dispatch (`chrootbuild.go`).

## Decision Points

### 1. A hash-pinned lockfile is the input; the tool re-emits a sanitized one

**Decision:** Each entry names a **lockfile** pinning every direct and transitive
dependency to an exact version with an integrity hash. The operator generates it
out of band (`pip-compile`, `npm install`); the tool **parses, verifies, and
installs**, and rejects anything not fully pinned.

**For pip, the operator's file is never handed to the installer.** Parsing
yields a normalized set of pinned components, and the tool **emits its own
canonical install file** into the staging directory (Decision 4) from that set.
This is a security property, not a convenience: a `--require-hashes`
requirements file may legally contain nested `-r`/`-c` includes and
network-affecting directives (`--index-url`, `--extra-index-url`,
`--find-links`), none of which a per-requirement-line grammar check would catch,
and any of which could pull un-staged bytes and void the offline and pinning
guarantees. Re-emitting only `name==version --hash=sha256:…` lines makes that
surface unreachable by construction. Nested includes and network directives in
the *input* are rejected outright rather than followed.

**Environment markers and extras are preserved, not dropped.** A cross-platform
lockfile legitimately carries lines like `name==version; sys_platform == "linux" --hash=sha256:…` or `name[extra]==version --hash=sha256:…` — dropping
the marker/extra during re-emission would make a conditional dependency
install unconditionally, pulling in components for platforms the target never
runs on (and contradicting Decision 3's multi-hash-candidate handling, which
assumes cross-platform lockfiles are supported). The sanitizer therefore
**carries environment markers and extras through unchanged**, stripping only
the directives and includes called out above; it is a projection of the input
down to `name[extras]==version; marker --hash=…` form, not a rewrite of the
requirement itself.

**npm is the deliberate exception, and is protected differently.** `npm ci`
replays `package-lock.json` by design and cannot consume a re-emitted
substitute, so the operator's lockfile *is* staged and installed from
(Decision 6). Re-emission is therefore replaced by three other controls, all
applied before anything is fetched or installed: the URI and integrity policy
below, `--offline` against a pre-primed cache so no node can reach the network
regardless of what its `resolved` says, and a scrubbed environment
(Decision 4). The asymmetry is intentional — pip's input is a *script* for the
installer, npm's is a *manifest* the installer must see verbatim.

**Where pip wheels are fetched from.** A requirements file carries names,
versions, and hashes but **no artifact URLs**, and this design rejects
`--index-url`/`--find-links` *and* direct-URL requirements in the input while
installing with `--no-index` — so the fetch phase needs its own source of truth,
and it is the **only** one (a URL embedded in a requirement is rejected, not
followed, so there is exactly one place bytes can originate). That source is a
**fetch-time index** carried on the entry itself as an optional `index` field
(a single `https://` URL; when omitted it defaults to PyPI, and is set to a
private index or mirror for air-gapped and enterprise builds). Precedence is
therefore unambiguous: entry `index` if present, else the PyPI default; there is
no lockfile-level or global override to reconcile. The `index` value is
validated like any fetch URL (`https://` only) and is used **only** to discover
and download during `PreProcess`.

The index host is not the only host in play: the default PyPI **index**
(`pypi.org`) serves package metadata but redirects wheel downloads to a
separate content host, `files.pythonhosted.org`. The fetch allowlist is
therefore **two** policies, not one — the `index` host itself, plus a
separate **artifact-host allowlist** carried on the entry as an optional
`artifactHost` field, defaulting to `files.pythonhosted.org` when `index` is
the PyPI default. A private index that serves artifacts from a different host
than its index URL **must** set `artifactHost` explicitly — there is no
allow-all fallback, and an `index` other than the PyPI default with no matching
`artifactHost` is a validation error rather than an implicit same-host
assumption. The redirect policy below re-applies this same host check to every
hop. The install step never consults `index` or `artifactHost`, and every
discovered artifact is still hash-verified, so this affects *where* bytes come
from but never *which* bytes are accepted.

Because `pip-compile` emits an `--index-url` header into its output by default —
which this parser rejects — the documented generation workflow must pass
`pip-compile --no-emit-index-url --generate-hashes`. The templates guide does
not yet describe a `languagePackages`/`pip-compile` workflow, since no schema
or implementation exists yet (this ADR is a design document, not that guide's
update) — documenting the workflow is an acceptance criterion of whatever
implementation work follows this ADR, to ship alongside the schema itself
rather than after it, not something this ADR/PR is expected to add.

**Fetch redirects are constrained too.** The project HTTP client
(`internal/utils/network/securehttp.go`) sets no `CheckRedirect`, so Go's
default follows up to 10 redirects — a 3xx from an allowlisted host to an
arbitrary one would reopen the very SSRF/exfil path the `resolved`/`index`
allowlist closes. The language fetcher therefore installs a `CheckRedirect` that
re-applies the scheme/host policy to **every** hop (not just the first URL) and
fails closed on a redirect that leaves the allowlist. This applies uniformly to
pip index/wheel fetches and npm `resolved` fetches.

Per-ecosystem pinning rules, stated precisely because a blanket "every node needs
a hash" rule rejects valid lockfiles:

- **pip** — `requirements.txt` in `--require-hashes` form: every requirement is
  `name==version` with at least one `--hash=sha256:…`. Ranges, bare names,
  `-e`/VCS/URL requirements, unpinned lines, includes, and option directives are
  rejected.
- **npm** — `package-lock.json`, `lockfileVersion` 2 or 3. Validation applies to
  **registry-sourced dependency nodes** under `packages`, which must each carry
  `version`, `resolved`, **and** `integrity` (`version` is required alongside the
  artifact pin: `resolved`+`integrity` pins bytes, but the declared version is
  what the SBOM and the reproducibility claim are stated in terms of).

  Those three fields do **not** by themselves establish that a node is
  registry-sourced — `resolved` is an arbitrary URI that the fetcher would
  otherwise dereference — so two further checks apply *before* any fetch:
  - **URI policy**: `resolved` must be `https://` and its host must match an
    allowlisted registry. The entry carries an optional `registry` field
    (mirroring pip's `index` field): a single `https://` host, defaulting to
    `registry.npmjs.org` when omitted, used only to validate `resolved` and
    never itself fetched from directly (the tarball URL always comes from the
    lockfile's `resolved`). This is the only registry configuration surface —
    there is no separate CLI flag or global override to reconcile against, and
    a lockfile whose `resolved` host does not match the declared `registry` is
    rejected rather than silently allowed. `file:`, `http:`, and any other
    scheme are rejected, which closes a local-file-read and SSRF surface that
    an integrity check alone does not cover (integrity validates bytes *after*
    they are read). The same policy is re-applied to every redirect hop (see
    "Fetch redirects" in Decision 1), so a 3xx off an allowlisted host cannot
    bypass it. **Userinfo and any query component are rejected too** —
    `https://user:token@host/…` or a token embedded in the query string would
    otherwise be staged verbatim into the shipped image's `package-lock.json`
    (Decision 6 stages the operator's lockfile as-is), baking a registry
    credential into an artifact that ships to users. Rather than pattern-match
    known credential-parameter names (an enumerable-forever problem — a new
    registry can invent a new query key at any time), a `resolved` URL
    carrying **any** userinfo component or **any** query string at all is a
    validation error in v1. This is stricter than strictly necessary for a
    bare cache-busting query param, but the fixed, reviewable rule is
    "no query, no userinfo" rather than an open-ended allowlist of parameter
    names that would itself need to be a configuration surface.
  - **Integrity algorithm floor**: the SRI digest must be `sha512` or `sha256`.
    A lockfile may legally carry a collision-prone `sha1-…` digest, and
    accepting it would make the npm path weaker than the pip path (which
    mandates SHA-256) while Decision 8 simultaneously adds SHA-512 support.
    `sha1` and `md5` are rejected with an actionable message.

  Exemptions and rejections:
  - The **root node `packages[""]` is exempt** — it describes the project itself
    and legitimately carries neither `resolved` nor `integrity`.
  - Nodes marked **`inBundle: true` are exempt** from the artifact-pin
    requirement — bundled dependencies legitimately lack their own `resolved`/
    `integrity` because their bytes are covered by the containing registry
    tarball. They are **still emitted as SBOM components** (Decision 8): their
    `downloadLocation` is `NOASSERTION` (they have no independent URL) and their
    checksum is omitted. Provenance to the containing package is expressed as an
    SPDX relationship as a **follow-up**, not in v1 — see Decision 8, item 5 for
    why (the current manifest writer discards any relationship beyond the
    mandatory document-to-package `DESCRIBES` set). So "one record per realized
    component" holds for bundled nodes in v1 without asserting a hash the
    lockfile does not provide, even though the relationship itself ships later.
  - Nodes with `link: true` (workspace symlinks) and `file:`/`git:` specifiers
    are **rejected**, since their contents are not hash-addressable.
- **npm toolchain floor.** `lockfileVersion` 2/3 cannot be replayed by an
  arbitrarily old npm, and distro `npm` versions vary, so the stage probes
  `npm --version` inside the chroot and fails with an actionable message if it
  cannot replay the declared `lockfileVersion`. Declaring the bootstrap package
  `npm` is not sufficient on its own.

**Why this is the load-bearing decision:** transitive resolution is the
expensive, low-value half of pip/npm support — pip's backtracking resolver and
npm's tree-deduplication are large, fast-moving domains, and a reimplementation
would be a maintenance sink and a source of resolve-differently-than-upstream
bugs. Delegating resolution upstream reduces this feature from "build a second
package manager" to "fetch, verify, install, record".

**Scope of the reproducibility claim:** the component set is reproducible given
**(the lockfile, the npm manifest, a fixed target toolchain, and the controlled
package-manager configuration of Decision 4)** — not the lockfile alone. For npm
the staged `package.json` is itself an input `npm ci` consults (root/workspace
and dependency metadata affect the tree, and `npm ci` requires it in sync with
the lock), and both ecosystems are sensitive to the environment/config the stage
deliberately controls. It is also not target-independent: wheel selection
depends on the target interpreter and arch (Decision 4), and npm lockfiles may
carry platform-specific `optionalDependencies`. The precise guarantee is
"same (lockfile + manifest) + same target + same controlled config ⇒ same
components".

**Rejected:** an inline package list (`pipPackages: [requests]`) as the primary
input — it forces the tool to resolve, and an unpinned name silently changes what
the image contains between two builds of the same template.

### 2. Declarative template block, not the `configurations` shell hook

**Decision:** Add `systemConfig.languagePackages[]`, an array of typed entries
discriminated by an `ecosystem` field:

```yaml
systemConfig:
  languagePackages:
    - ecosystem: pip                       # pip | npm
      lockfile: files/requirements.txt     # hash-pinned (--require-hashes form)
      target: /opt/telemetry-agent         # venv root; must not already exist
      index: https://pypi.example.internal/simple   # optional; default PyPI
      artifactHost: pypi.example-cdn.internal       # optional; default files.pythonhosted.org
    - ecosystem: npm
      manifest: files/package.json         # required for npm; must match lockfile
      lockfile: files/package-lock.json
      target: /opt/edge-agent              # project dir; deps in node_modules
      registry: https://npm.example.internal        # optional; default registry.npmjs.org
```

`ecosystem`, `lockfile`, and `target` are required; `manifest` is required for
`ecosystem: npm` (Decision 6) and rejected for `pip`. `index` (pip) and
`registry` (npm) are both optional, single `https://` hosts, and are the only
configuration surface for private indexes/registries (Decision 1); `artifactHost`
(pip only) is likewise optional, defaulting to `files.pythonhosted.org` when
`index` is the PyPI default — there is
deliberately **no `allowScripts` field** (Decision 7).

**`target` path policy.** `target` is template-controlled and is interpolated
into commands executed in the chroot, so it is constrained on both safety axes:

- *Shape*: must be absolute and already normalized (rejected if
  `filepath.Clean` would change it), and must contain no shell metacharacters or
  whitespace — a value like `/opt/app; rm -rf /` is rejected at validation, not
  merely quoted later.
- *Location*: must fall beneath an allowlisted dedicated prefix (`/opt/**` or
  `/usr/local/**`). `/`, `/usr`, `/etc`, `/var`, and any distribution-managed
  Python directory are rejected, so the "disjoint from apt/rpm-owned paths"
  invariant of Decision 6 is enforced rather than assumed.
- *Construction*: every interpolation of `target` (and of staged paths) into a
  command uses `shell.QuoteArg` (`internal/utils/shell/shell.go`) or
  structured arguments. The validation above is the primary control; quoting is
  defense in depth.

**Input path containment.** `lockfile` and `manifest` resolve relative to the
template directory, as `additionalFiles[].local` does. `filepath.Clean` alone is
**not** sufficient — it normalizes `../../etc/shadow` without rejecting it, and
these are host-side files read during `PreProcess`. Resolution must clean,
absolutize, resolve symlinks (`filepath.EvalSymlinks`), and then **require
containment beneath the owning template's directory**, rejecting escapes and
absolute paths outside it.

**Merge semantics.** Templates merge with OS defaults and fold through `extends`
chains, so this array needs an explicit rule in the merge table
(`docs/user-guide/architecture/image-composer-tool-templates.md`). Following the
`additionalFiles` precedent (merged by `final` path): **merged by the
(`ecosystem`, `target`) pair** — a matching pair overrides field-by-field, a new
pair is appended, and two entries resolving to the same `target` is a validation
error rather than two installs into one prefix.

**Paths are resolved to canonical form *before* the merge.** A relative
`lockfile` string is ambiguous across an `extends` fold: the template search path
is a *list*, so a parent and child can each ship `files/requirements.txt`, and
merging the raw strings would let a child's override resolve to the parent's file
(or vice versa). Each entry therefore carries the **owning template's directory**
from load time, is resolved to a canonical absolute path before merging, and has
containment enforced relative to *its own* owner — not relative to whichever
template happened to win the merge.

**Rejected:** per-ecosystem top-level fields (`pipPackages`/`npmPackages`) —
duplicates the plumbing; and folding these into `systemConfig.packages[]` — that
list feeds the OS resolver and its install ordering.

### 3. Two-phase fetch-then-install, with hash verification on every use

**Decision:** During `PreProcess`, parse the lockfile and download every pinned
artifact (wheels; npm tarballs) into a per-ecosystem cache directory, keyed by a
**content hash, not by basename** — e.g. `cache/<ecosystem>/<sha256>` — verifying
each artifact's hash against the lockfile before use. Multiple entries can
legally pin different bytes under the same filename (two pip entries with
different `index`/`artifactHost` values, or two npm lockfiles pinning different
versions that happen to tar to the same name), so a basename-keyed cache would
silently overwrite one entry's artifact with another's or serve the wrong bytes
to the wrong install; a hash-addressed path makes that collision structurally
impossible. The fetcher records each artifact's **original filename** alongside
its hash at download time (the filename is known from the download URL, before
the blob is written to its hash-addressed path) — this is metadata the cache
carries, not something staging has to reconstruct. The install step then runs
strictly against that cache with no network access.

**Wheel selection cannot happen at fetch time, so every hash-listed candidate
is fetched — but only wheel candidates.** `PreProcess` runs before
`installImagePkgs` has installed the
target's own Python (Decision 5's ordering), so the fetcher does not yet have
a running target interpreter to match wheel tags against — and a
`--require-hashes` requirement may legally carry more than one `--hash=` line
for the same `name==version` (e.g. `pip-compile` emitting separate `cp310`/
`cp311` or `manylinux`/`musllinux` wheels for a cross-platform lockfile).
`pip-compile --generate-hashes` commonly emits hashes for a **source archive**
alongside wheel hashes for the same requirement, though, and Decision 7
already commits to wheels only (`--only-binary=:all:`) — so "every candidate"
is scoped to that policy: the fetcher resolves and downloads only candidates
whose filename ends in `.whl` and whose digest appears in the requirement's
hash set; a hash entry that resolves only to an sdist is **not fetched**, and
is treated the same as any other unsatisfiable requirement under
`--only-binary=:all:` — a hard error naming the package, not a silent skip or
an undefined fetch attempt. The
fetcher therefore downloads and hash-verifies **every wheel** a
requirement's hash set lists, not one pre-selected wheel; no compatibility
selection runs during `PreProcess` at all. Selection happens **at install
time**, inside the chroot, where `--find-links=$S/wheels --only-binary=:all:`
lets pip's own tag matching — run against the real target interpreter — pick
the one already-fetched, already-verified wheel that applies. This keeps the
offline guarantee intact (nothing is fetched during install) while deferring
the part that genuinely depends on the target to when the target exists.

**pip's `--find-links` needs real wheel filenames, not bare hashes.** `pip`
parses candidate wheels from filenames it finds in a `--find-links` directory
(distribution, version, tags) *before* it ever reads their bytes, so a
directory of `<sha256>`-named blobs presents no candidates it can match against
the sanitized requirements — the content-addressed cache above is for
collision-safe storage and re-hash verification, not for direct consumption by
pip. Staging therefore materializes a **separate, per-install `--find-links`
directory** containing the recorded original `.whl` filenames (hardlinks or
copies of the same verified bytes, not fetched again), which is what `-r
requirements.sanitized.txt --find-links=$S/wheels` actually reads (Decision 4).

**Cache hits must be re-hashed.** The OS-package fetcher skips any cached file of
nonzero size (`pkgfetcher.go`) — a size-only check. Adopting that here would
let a truncated, corrupted, or stale artifact bypass verification and defeat the
pinning guarantee, so the language-package fetcher **deliberately diverges**: a
cache hit is re-hashed against the lockfile and re-fetched on mismatch, with a
hard failure if the re-fetched bytes still mismatch.

Because lifecycle scripts are never executed (Decision 7) and the install file is
tool-generated (Decision 1), the install consumes **only** local staged files —
no network access is available to **pip itself**, and there is no opt-in that
forfeits that. This is narrower than "nothing on the network runs": as
Decision 7 notes, a wheel's own code (`.pth`/`sitecustomize.py`) can still open
connections when the interpreter starts, independent of pip's own
`--no-index`/offline posture — that residual risk is recorded there, not
claimed away here.

### 4. The install runs inside the target chroot, using the image's own toolchain

**Decision:** Execute the installers **inside the chroot** against the target
image's own interpreter, using the existing chroot-exec machinery
(`shell.ExecCmdWithStream(cmd, true, installRoot, envVars)`, as the deb path does
in `imageos.go`). The template must supply, via `systemConfig.packages[]`,
whatever OS packages provide the required **capability** in the target
(a working `python3 -m venv`+`ensurepip`, or `npm`); validation checks the
capability, not specific package names (see "Bootstrap validation" below).

**Shell allowlist work is part of this change, and is not just new map entries
— though see the Context note above that this doesn't close a pre-existing
gap.**
`python3` and `npm` (plus any directly invoked `node`) must be added to
`commandMap` (`internal/utils/shell/shell.go`), because `GetFullCmdStr`
(`shell.go`) routes through `verifyCmdWithFullPath`, which **fails for any
top-level command not in the map** (nested content inside an already-allowlisted
wrapper's quoted argument, e.g. `bash -c '…'`, is not independently checked).

**`commandMap` entries are also validated against the build tool's OWN
container, not just the target chroot — and `python3`/`npm` don't belong
there.** `scripts/preflight.sh` parses every `commandMap` entry and checks it
resolves to a real executable, and the `builder-image-preflight` Earthfile
target runs that check against the ICT builder image itself (the tool's own
execution environment), not any per-OS target chroot. Existing entries like
`dpkg`/`rpm`/`chroot` are genuinely build-host tools ICT invokes directly, so
that check makes sense for them — but `python3`/`npm` here are **target**
capabilities, reached only via `chroot <path> python3 …`, and provisioned into
the target through `systemConfig.packages[]`, never installed in the builder
image itself. Adding them as ordinary `commandMap` entries would therefore
make `builder-image-preflight` fail (or force provisioning python3/npm/node
into the builder image solely to satisfy a check that never exercises them
there). This needs one of: extending `commandMap`/`preflight.sh` with a
target-only marker exempt from the builder-host existence check, or a
separate allowlist for chroot-only commands that the executor consults
instead of the shared `commandMap` for this stage. Recorded here as a
required change to the allowlist mechanism itself, not just new entries in it.

Map entries alone are **not sufficient for the venv interpreter.**
`verifyCmdWithFullPath` (`shell.go`) takes `bin := fields[0]` and does an
exact `commandMap[bin]` lookup, so a command beginning
`<target>/bin/python` is looked up under that literal absolute path — not under
`python3` — and fails closed before the chroot is entered. Because `target` is
template-controlled and dynamic, no static map entry can cover it.

**Decision: drive the venv through the allowlisted system interpreter's
`--python` flag rather than invoking `<target>/bin/python` directly.**
`python3 -m pip install --python <target>/bin/python …` keeps `python3` as the
command `verifyCmdWithFullPath` actually checks — the dynamic,
template-controlled `target` path becomes an *argument* to an allowlisted
command, not the command token the allowlist itself has to reason about. This
was weighed against a narrower verifier rule that would permit an absolute
first token of the form `<P>/bin/python` validated against the allowlisted
prefix (Decision 2) — rejected here because it requires touching
security-critical shared code (`verifyCmdWithFullPath`) and an explicit
security review of that change, for a benefit (avoiding a version floor) that
doesn't outweigh the cost of a standing modification to the allowlist's
verification logic. The `--python` flag imposes a **pip ≥ 23.1** floor instead
(verified by the bootstrap probe below, which already checks the reported pip
version for the `--report` feature — 23.1 is the higher of the two floors,
so one check covers both), which varies by distro but requires no change to
`shell.go` at all.

**Bootstrap validation is by capability, not by package name.** An earlier draft
required the literal names `python3-venv` and `nodejs`/`npm` in
`systemConfig.packages[]`. That is Debian-shaped: rpm-family providers
(`internal/provider/rcd`, `azl`, `elxr`, `emt`) do not consistently ship a
separate `python3-venv`, so name matching would both reject valid templates and
accept packages that do not actually provide `venv`. Validation instead **probes
the built chroot for the capability**, and the probe must exercise the real
operation, not a lighter proxy: `python3 -m venv --help` merely proves the
module loads, so a target whose `ensurepip` is missing would pass and then fail
mid-install. The probe therefore **creates a throwaway venv in a temp path**
(`python3 -m venv <tmp>` and confirms `<tmp>/bin/python -m pip --version`), then
removes it — which is exactly the operation the real stage depends on — plus
`npm --version` for the toolchain floor of Decision 1, for whichever
ecosystems the template actually declares: entries are discriminated by
`ecosystem`, so a pip-only template probes only the venv capability and a
npm-only template probes only `npm --version` — requiring both regardless of
what's declared would contradict the "required capability" framing this
section opens with. **The pip probe verifies the specific features the real
install depends on, not just that pip runs at all:** the install command
(below) needs `--python` (pip ≥ 23.1, the Decision 4 interpreter decision) and
the SBOM inventory (Decision 8) needs `--report` (pip ≥ 22.2) — the probe
checks the reported pip version against **23.1**, the higher of the two
floors, so a single check covers both rather than only confirming the
interpreter starts. Per-provider package-name
hints are used only to make the failure message actionable.

**The commands, in full.** Partial commands have been a source of confusion, so
they are specified concretely (`$S` = the in-chroot staging directory):

```sh
# pip — the ALLOWLISTED system python3, targeting the venv via --python
# (Decision 4's committed interpreter decision; requires pip >= 23.1, checked
# by the bootstrap probe below). isolated from PIP_*/pip.conf, no index.
# See "pip is isolated" below for what the env prefix can and cannot scrub.
python3 -m venv <target>
python3 -m pip install --python <target>/bin/python \
    --isolated --no-input \
    --no-index --find-links=$S/wheels \
    --require-hashes --only-binary=:all: \
    --force-reinstall \
    --report=$S/install-report.json \
    -r $S/requirements.sanitized.txt

# npm — prime the content-addressable cache, then replay the lock, offline.
# The whole snippet runs through an in-chroot shell (see "no working-directory
# mechanism" below for why `cd`/`&&` cannot appear at the top level of the
# command the executor is given). $NPM is the absolute path resolved once
# during the Decision 1 toolchain-floor probe, not a bare `npm` — see below
# for why the bareword form is not safe inside this wrapper. Cache priming
# iterates the hash-named files already on disk under the npm-specific cache
# subdirectory (Decision 3: `cache/npm/<sha256>`, no extension — the glob
# below matches that layout, not a `.tgz` suffix nothing stages), rather than
# a per-artifact interpolated filename — see below for why. `shopt -s
# nullglob` is set inline so an npm entry with zero staged artifacts expands
# the glob to nothing (a no-op `cache add` with no arguments) instead of
# passing the literal unexpanded pattern to `$NPM`.
bash -c 'shopt -s nullglob; $NPM cache add $S/cache/npm/* --cache $S/npm-cache --offline && \
    cd <target> && $NPM ci --offline --ignore-scripts \
    --include=dev --include=optional --cache $S/npm-cache'
```

Note that bare `pip` (the console script) is not used at all — `python3 -m pip
--python <target>/bin/python` is the allowlisted-command form Decision 4
commits to — and `--find-links` supplies no requirements on its
own — hence the explicit `-r`.

**Cache priming uses a glob over hash-named files, not an interpolated
filename, because the filename is untrusted and the command runs inside a
quoted `bash -c`.** The earlier design named each artifact by its recorded
original filename (`<each>.tgz`) and interpolated that string directly into
the wrapper. That filename is derived from the npm `resolved` URL — which the
URI policy above constrains by host, not by path/filename shape — so a
basename containing whitespace, quotes, or shell metacharacters would change
the script and execute as part of it, precisely because it lands inside an
opaque `bash -c` argument nothing downstream re-verifies (see the allowlist
note in Decision 4's opening). `npm cache add` computes integrity from a
tarball's own bytes, not from its filename, so the original filename is never
actually needed for correctness — the fix removes the untrusted string from
the command entirely by globbing the cache's own hash-named files
(`$S/cache/npm/*`, which are tool-generated paths with no attacker input)
rather than quoting the untrusted one more carefully.

**pip's isolation is real for pip's own config, but the executor cannot
unset ambient variables.** `--isolated` makes pip itself ignore `PIP_*` env
vars and config files regardless of what the process environment contains —
that guarantee comes from pip's own documented behavior, not from the shell
layer. It is the primary control. What the shell layer *cannot* do: `envVal`
(`GetFullCmdStr`, `shell.go`) only ever **prepends** `KEY=value` pairs ahead of
the command; `ExecCmdWithStream` runs it via `exec.CommandContext(ctx, "bash",
"-c", fullCmdStr)` with `cmd.Env` left `nil`, so the child inherits the full
parent process environment and there is no mechanism to *unset* an inherited
variable — only to override a *named* one to a new value. `PYTHONPATH`,
`PYTHONHOME`, and similar interpreter-level variables are not covered by
`--isolated` (it only governs pip's config loading, not the interpreter
running it), so the stage additionally overrides that **specific, enumerated**
set (`PYTHONPATH=`, `PYTHONHOME=`, `PYTHONSTARTUP=`) to empty via `envVal`,
rather than claiming to "scrub `PYTHON*`" as an unbounded wildcard — an
ambient variable outside that enumerated set would still be inherited. A true
clean-environment execution mode (constructing `cmd.Env` from an explicit list
instead of inheriting `os.Environ()`) is out of scope for this change but is
the correct long-term fix; it is noted as a follow-up.

**There is no working-directory mechanism, and the executor's `&&` handling
forces the command into an inner shell.** `shell.ExecCmdWithStream(cmdStr,
sudo, chrootPath, envVal)` (`shell.go`) takes no cwd argument, and
`GetFullCmdStr` builds a single string — `chroot <chrootPath> <verified-cmd>` —
that is itself passed whole to `exec.CommandContext(ctx, "bash", "-c",
fullCmdStr)`. A literal top-level `cd <target> && npm ci …` would therefore be
split on `&&` by that **outer** `bash -c`, not by a shell running inside the
chroot: `chroot <chrootPath> cd <target>` would run first (and fail, since
`cd` is a shell builtin with no standalone binary for `chroot` to exec), and
`npm ci …` would either never run (the `&&` short-circuits on the left side's
failure) or, if that failure mode were ever papered over, run **outside the
chroot** entirely. The fix already has a precedent in this codebase
(`shell.IsCommandExist`'s `bash -c 'command -v <cmd>'` construction): wrap the
whole `cd …/npm …` snippet in a single quoted argument to an in-chroot `bash
-c`, e.g. `bash -c 'cd <target> && npm ci …'`. Because `verifyCmdWithFullPath`'s
separator search is quote-aware (`findSeparatorOutsideQuotes`), the `&&` inside
the single quotes is not treated as a top-level separator, so only `bash`
itself needs an allowlist entry (already present) — the `cd`/`npm` tokens
inside the quoted snippet are **not** independently re-verified the way a
top-level `&&`-joined command would be. That is an accepted, narrower trade-off
here because both tokens are fixed by this design (`cd` to the
already-validated `target`, `npm ci` with a fixed flag set), not
template-supplied verbs.

**Wrapping in `bash -c` does not by itself pin *which* `npm` runs.** A bareword
`npm` inside the quoted snippet is resolved by the inner shell against the
chroot's inherited `PATH` at execution time — unconstrained by `GetFullCmdStr`,
since only the outer `bash` token is checked. A different `npm` earlier on
`PATH` than the one the Decision 1 toolchain-floor probe verified (a stale
`/usr/local/bin/npm` left by a prior stage, for instance) would silently
substitute an unverified toolchain. The stage therefore resolves `npm`'s
**absolute path** once, at the same time it runs the `npm --version` probe
(e.g. via `command -v npm` in that same probe invocation), and substitutes that
literal path (`$NPM` above) for every `npm` invocation inside the script,
rather than letting the inner shell re-resolve a bareword at install time.

**The npm environment *and* config files are controlled, not inherited.** `npm
ci` only installs exactly the lockfile if its configuration says so, and npm
draws configuration from three places the executor would otherwise leak in: the
process environment (`NODE_ENV`, and — on Unix — **both** `NPM_CONFIG_*` and
lowercase `npm_config_*`), and user/global `.npmrc` files (`~/.npmrc`,
`$PREFIX/etc/npmrc`). Any of these can omit locked `dev`/`optional` dependencies
or redirect the cache/registry, producing a different inventory from the same
lockfile. The stage therefore scrubs `NODE_ENV` and both case variants of the
config env vars, plus the wider Node runtime surface that isn't `npm_config_*`
at all — `NODE_OPTIONS` (can preload arbitrary code before npm even starts),
`NODE_PATH`, and `NODE_EXTRA_CA_CERTS` — **and** the npm platform-selector
variables that steer optional/native dependency resolution independent of the
lockfile's own declared platforms — `npm_config_os`, `npm_config_cpu`, and
`npm_config_libc` — pinned to the **target's** values explicitly rather than
left to whatever the host environment happens to set (an ambient host-platform
value here could otherwise select a different, even foreign-platform,
optional-dependency tree despite the lockfile) — for the **known, fixed set this stage
itself would otherwise
leak** (there is no npm equivalent of pip's `--isolated`, so this list is
necessarily enumerated rather than a wildcard — see the pip isolation note
above for the same caveat about unenumerated ambient variables), points `HOME`
at an **empty** staging directory, and points
`npm_config_userconfig` and `npm_config_globalconfig` at nonexistent files
inside that same directory — both are *file* paths npm reads `.npmrc`-style
config from, not directories, so pointing them at a directory would make npm
fail trying to read it as a config file rather than simply finding no config —
so no ambient `.npmrc` is read, and passes the
include policy explicitly (`--include=dev --include=optional`) — so the installed
set is a function of the lockfile, the staged manifest, and this stage's
enumerated environment overrides — **not** "the lockfile alone": as noted
above, an ambient `npm_config_*`/`NPM_CONFIG_*` variable outside the enumerated
set is still inherited, and only a true clean-environment execution mode
(noted as a follow-up) would close that residual gap completely.

**npm's cache is content-addressable, not a directory of tarballs.** `npm ci
--offline --cache <dir>` looks packages up in npm's `cacache` by integrity; it
does not scan an arbitrary directory, so simply depositing `.tgz` files there
yields cache misses on a clean build. Staging therefore **populates cacache
explicitly** (`npm cache add --offline` per staged tarball, before the `npm
ci`), which preserves lockfile fidelity — `--offline` on the priming step too,
not just the replay step, so cache population itself cannot silently reach the
network if a staged tarball is missing or unreadable. Rewriting the lockfile's
`resolved` fields to `file:` paths would also work mechanically but mutates the
operator's pinned input, so it is rejected.

**Staging host inputs into the chroot.** The cache, the generated install file,
and the npm manifest live on the host, and a host path is not visible from inside
`installRoot`. Before the install, each is placed at a known path **inside** the
chroot under a dedicated staging directory: the verified artifact cache and the
per-install `--find-links` directory are bind-mounted **read-only**, so nothing
running in the chroot (including the install commands themselves) can swap or
remove a wheel between the host-side hash verification and pip/npm actually
reading it — a writable mount would let that verification be bypassed entirely
between check and use. npm's `cacache` priming target (`$S/npm-cache`) is
necessarily writable (`npm cache add` writes into it) and is therefore a
**separate** directory from the read-only source cache, populated from it but
never mounted over it. Generated files (the sanitized requirements, the staged
npm manifest/lockfile) are copied in. Commands are written against those
in-chroot paths, and staging is torn down on every exit path via named-return
`defer`, the same init/deinit shape as
`initDebLocalRepoWithinInstallRoot` / `deInitDebLocalRepoWithinInstallRoot`
(`imageos.go`).

**Why in-chroot rather than host-side `--root`/`--target`:** wheel selection is a
function of the *target's* Python version, ABI tag, and architecture. Running the
host's pip against a chroot prefix invites installing wheels built for the host's
Python minor version or architecture — a bug class that surfaces only at runtime
on the built image.

**Foreign-arch prerequisite, stated explicitly.** Executing the target's
interpreter in the chroot on a foreign-arch build depends on host `binfmt_misc`
registration plus a `qemu-<arch>-static` binary. `validateCrossArchDeps`
(`deb/installer.go`) only *validates host tooling* and places nothing in the
rootfs, so this stage inherits that prerequisite and must assert it for
rpm-family providers too (where no equivalent validation exists today), failing
with an actionable message rather than `exec format error`.

**Rejected:** a separate build chroot holding the toolchain — for pip the
installed `site-packages` must be produced by the *target's* interpreter anyway,
so the build chroot would have to be a copy of the target.

### 5. Supported per output target, not per install function

**Decision:** Add an `installLanguagePkgs` stage that runs **after**
`installImagePkgs` (so the interpreter exists) and **before that flow's own
configuration update** — `updateImageConfig` in `InstallImageOs`,
`updateRootfsConfig` in `InstallRootfs` (`imageos.go`). The insertion point
is defined per flow rather than by naming one function, since the two supported
flows do not share a configuration step.

Support is gated on the **`target.imageType` and the maker it routes to**, not
on the install function, because `InstallImageOs` has two callers with entirely
different execution contexts:

| `target.imageType` → maker | Install fn | Runs on | SBOM | `languagePackages` |
| --- | --- | --- | --- | --- |
| `raw` → `rawmaker.go` | `InstallImageOs` | build host | yes (`generateSBOM`) | **Supported** |
| `wsl2` → `wsl2maker.go` | `InstallRootfs` | build host | yes (`generateSBOM`) | **Supported** |
| `img` → `initrdmaker.go` | `InstallInitrd` | build host | none | **Rejected** |
| `iso` → `isomaker.go` + `install.go` | `InstallInitrd`, then `InstallImageOs` in the live installer | initrd on build host; OS install **on the target machine** | yes (on target) | **Rejected in v1** |

**Artifact formats are not a separate axis.** `qcow2`, `vhd`, `vhdx`, `vmdk`,
and `vdi` are `disk.artifacts[]` entries (schema enum at
`internal/config/schema/os-image-template.schema.json`), produced by
converting the one rootfs a `raw` build already composed — they are not
distinct makers. All of them are therefore **supported** wherever `raw` is, and
the validation boundary is drawn at `imageType`, so no artifact format needs its
own rule.

- **initrd** is excluded on two independent grounds: an initrd is a minimal
  early-boot environment with no business carrying application libraries, and the
  flow emits no SBOM, so installing there would silently break Decision 8's
  recording guarantee.
- **ISO** is excluded because gating on `InstallImageOs` alone would be wrong. An
  ISO build runs `initrdmaker` over a *separate initrd template*
  (`isomaker.go`) and ships `template-dump.yaml` (`isomaker.go`) for
  `cmd/live-installer`, which calls `InstallImageOs` **on the target machine**
  (`install.go`). The staged lockfile, generated install file, and artifact
  cache are build-host artifacts that are not packaged onto the ISO, and the
  language inventory would need a serialization path onto the medium (as
  `FullPkgListBom` already does via `SBOMPackageMetadata`, `install.go`).
  Until those exist, an ISO target with `languagePackages` is a **validation
  error**, not a silent no-op that ships an image missing its libraries.
  Crucially, that check must **also validate the referenced initrd template**:
  an ISO build composes `initrdmaker` over a *separate* template
  (`isomaker.go`), so rejecting the field only on the outer ISO template
  would still let it reach `InstallInitrd` through the inner one.
- **Overlay mode is rejected outright**, not merely unsupported. Providers
  branch to the overlay pipeline **before** any maker is selected
  (`internal/provider/ubuntu/ubuntu.go`, `internal/provider/debian13/debian13.go`,
  both in the overlay-mode branch ahead of the `target.imageType` switch), so
  an overlay template
  carrying `languagePackages` would bypass this stage entirely and silently emit
  an image with neither the requested packages nor their SBOM entries — the
  exact failure mode this ADR rejects everywhere else. Validation therefore
  fails an overlay template that sets the field, and overlay support remains a
  follow-up with its own preflight/report treatment.

Consequently **per-user installs are out of scope**: `pip install --user`
(`~/.local`) and a user-local `npm install` have nowhere to land, because
`createUser` (`imageos.go`) has not run yet. Only system-wide installs into the
allowlisted prefix are supported.

**The language stage is not the last thing before SBOM, so `target` is frozen
and re-checked.** The per-flow configuration update that follows this stage —
`updateImageConfig`/`updateRootfsConfig` — runs `addImageAdditionalFiles` **and**
`addImageConfigs` (the arbitrary `configurations[].cmd` hook, `imageos.go`)
*before* `generateSBOM`. A template could therefore drop a file into `target`, or
run `cmd: "<target>/bin/pip install …"`, after the locked stage — installing
components the inventory never saw (breaking completeness) or fetching them live
(breaking offline), with SBOM none the wiser. The design defines **target
ownership**: once the stage populates a `target`, that tree is owned by the
language stage. **Immediately after install** — before `updateImageConfig`/
`updateRootfsConfig` run anything, and specifically before any post-stage
`cmd`/`additionalFiles` gets a chance to execute — the stage captures, in one
atomic step, both the realized inventory (Decision 8's per-component record)
**and** a content digest of the `target` tree. `generateSBOM` later reads that
**already-captured** inventory; it does not invoke `pip`/`npm` again itself.
This matters beyond ordering hygiene: Decision 8's inventory commands start the
target interpreter, and a wheel can ship code (`.pth`/`sitecustomize.py`) that
runs on every interpreter start — capturing inventory only once, immediately
after install, means `generateSBOM` never triggers a *second* interpreter
start whose side effects could land after the freeze point. (The *first*
start — pip's own install step — still runs installed code once; that
residual risk is unchanged, see Decision 7.) **Immediately before
`generateSBOM`** the stage re-verifies that `target` is still byte-for-byte
identical to that captured digest. A post-stage mutation of any language
`target` is a
**hard build error naming the affected `target`** — not a silent SBOM gap.
It identifies *which target* drifted, not *which* `additionalFiles`/`cmd`
entry caused it: `addImageAdditionalFiles`/`addImageConfigs`
(`imageos.go:1218`, `imageos.go:1307`) each process their whole array in one
pass with no intermediate check between entries, so pinpointing the specific
entry would need re-hashing `target` after every single entry runs (and
threading that entry's identity through to the failure). That's a reasonable
future enhancement, but v1's guarantee is scoped to what a single before/after
comparison can actually say: something changed this target — not which
configured entry did it.

**This digest catches drift, not a deliberately adversarial round-trip.** The
check detects any *difference* present at `generateSBOM` time — an
`additionalFiles` entry or a naive `cmd` that writes into `target` and leaves
the change in place. It does **not** detect a `configurations[].cmd` crafted to
fetch, install, or exfiltrate through `target` and then restore the tree to its
post-install byte-for-byte state before the digest re-check runs — `cmd` is
arbitrary shell with no isolation of its own, and a digest comparison is
fundamentally a point-in-time check, not a runtime guard. Closing that requires
either rejecting `configurations[].cmd` outright whenever a template also sets
`languagePackages`, or running every post-stage `cmd` under network/filesystem
isolation — both out of scope for v1. The guarantee as shipped is: **accidental
or careless post-stage drift is always caught; a deliberately adversarial
template author can evade it.** That scoping is stated explicitly rather than
implied, since the offline/completeness claims elsewhere in this ADR describe
the former, not a defense against the latter.

**The same limits apply, more sharply, to installs the digest never sees at
all.** The digest only watches the `target` path(s) `languagePackages`
declares. Nothing stops an existing `configurations[].cmd` from running
`pip install --target /opt/other-prefix …` or an npm install rooted somewhere
else entirely — components landing there are outside every declared `target`,
so the digest has nothing to compare and the language inventory never looks
there either. This capability is **not even new** — `bash` is already
allowlisted and `verifyCmdWithFullPath` treats a `bash -c` argument as opaque
(see the Context note in Decision 4's opening), so an existing `cmd` could
already invoke pip/npm this way today, independent of whether this change adds
`python3`/`npm` as top-level entries. Closing it fully
would need per-stage capability scoping in the shell executor, which is out of
scope here. The SBOM-completeness guarantee is therefore explicitly **limited
to the targets `languagePackages` declares** — not a claim that no other
pip/npm install can exist anywhere in the image.

### 6. pip installs into a fresh venv; npm installs a project, not a global prefix

**Decision, pip:** `target` is a **virtual-environment root** created with
`python3 -m venv <target>`.

PEP 668 marks the system interpreter's environment externally managed, so a plain
`pip install` into system `site-packages` either fails or needs
`--break-system-packages` — a flag that accurately describes its effect on an
image whose Python packages are otherwise apt/rpm-owned. A venv sidesteps the
conflict and keeps the tool's writes disjoint from OS-owned paths.

**`target` must be absent — checked at *stage time*, not just config time — for
either ecosystem.** `python3 -m venv` **reuses** an existing directory rather
than replacing it, and `pip install -r` never uninstalls extras, so a
pre-existing pip target would leave unpinned packages installed while the SBOM
reflected only the lockfile. The npm side is not exempt: `npm ci` replaces
`node_modules` but does **not** clear arbitrary pre-existing project files, and a
stray `.npmrc` in the target could redirect resolution or re-enable behavior the
stage disables. Config-time validation **cannot** enforce absence on its own,
because `target` lives inside the chroot and OS package installation runs
*before* this stage — an OS package can legitimately create `/opt/foo` after the
template validates. The stage therefore performs the real check **immediately
before staging**: `target` must not exist and must not be a symlink. The check
also covers the ancestor chain — a pre-existing symlinked *parent* (e.g.
`/opt/link -> /etc` with a `target` of `/opt/link/app`) would let the install
land outside `/opt` without `target` itself being a symlink. This check runs
on the **build host** against `installRoot + target`, and Go's
`filepath.EvalSymlinks` has no concept of a chroot boundary — it would resolve
an **absolute** symlink target against the *host's* root, not `installRoot`,
which is the wrong filesystem entirely and would either mis-resolve or
false-negative. Rather than reimplement chroot-aware symlink resolution, the
check simply **rejects `target` outright if any ancestor path component from
the allowlisted prefix down is a symlink at all** — no resolution or
comparison is attempted, so there's nothing for host/chroot root confusion to
get wrong. If either check fails, the build fails with an actionable error rather
than reusing or clobbering the path. Config-time validation still runs as an
early, cheap first line.

**The venv's seeded packages are inventoried against a baseline captured right
after creation.** `venv` seeds distributions via `ensurepip`, which are real
installed components absent from the lockfile — but *which* ones varies by target
Python version (recent releases no longer guarantee `setuptools` or `wheel`), so
a hard-coded seed list would be wrong on some targets. The stage instead
**snapshots the venv's `pip list` immediately after `python3 -m venv`, before any
lockfile install** — that snapshot is the trusted bootstrap baseline. After
install, the realized set is partitioned against (baseline ∪ lockfile): baseline
entries are recorded as bootstrap components, lockfile entries as pinned
components, and **anything in neither fails the build** (it is an unexplained
package, exactly the drift the completeness guarantee must catch). This is what
lets the cross-check distinguish a legitimate seed from an arbitrary extra
without either blanket-allowing unknowns or falsely rejecting a valid venv.

**The baseline can overlap the lockfile by name, and pip's default "already
satisfied" behavior would let that overlap go unverified.** `ensurepip` always
seeds `pip` itself (and, on some Python versions, `setuptools`/`wheel`) into
the fresh venv — if the lockfile *also* pins one of those names at the same
version the baseline already has, a plain `pip install -r` treats the
requirement as already satisfied and **does not download or hash-check it at
all**, yet the partition above would still record it as a "pinned" component,
asserting a verification that never happened. The install command therefore
runs with **`--force-reinstall`**: every lockfile-pinned entry is always
reinstalled from the staged wheels and hash-verified, regardless of whatever
the bootstrap baseline already provided at the same version, so "pinned" in
the SBOM always means "actually verified this build," not "happened to already
be there."

**Decision, npm:** `target` is a **project directory**, not a global prefix. The
entry's `manifest` and `lockfile` are staged into `target` and the install is
`npm ci` executed there; dependencies land in `<target>/node_modules`.

`--prefix` alone does not mean "global install" — it relocates the project prefix
— while `npm install --global` leaves the lockfile workflow altogether, since a
global install resolves from the registry rather than replaying a lock. `npm ci`
is the only npm mode that installs *exactly* what a lockfile specifies, and it
requires a `package.json` in sync with it; hence `manifest` is required
(Decision 2). Exposing results on `PATH` (a `profile.d` snippet, or symlinks from
the venv `bin/` or `node_modules/.bin`) is the template author's choice, and the
guide will show the pattern.

**Rejected:** `--break-system-packages` into system `site-packages` — creates
files apt/rpm believes it owns, so a later OS upgrade can clobber them. Also
rejected: `npm install --global --prefix` — not lockfile-reproducible.

### 7. Binary-only for pip; lifecycle scripts never executed in v1

**Decision:** pip runs with `--only-binary=:all:` — wheels only; an sdist needing
compilation is a hard error naming the package. npm always runs with
`--ignore-scripts`. **There is no opt-in.**

Building an sdist would require a C/C++ toolchain and arbitrary headers inside
the image, reintroducing the bloat and non-reproducibility this design avoids.

An earlier draft exposed `allowScripts: true`. It is removed because it silently
voided two of this ADR's three headline guarantees and introduced a third
problem, and a per-entry flag is the wrong place to make that trade:

- **Offline** — a lifecycle script is arbitrary code and may open its own
  connections, which neither `--offline` nor the lockfile constrains.
- **SBOM completeness** — a script can fetch or generate components outside
  `node_modules`, which neither the lockfile nor npm's hidden lockfile records,
  so "every installed component is recorded" would become unprovable exactly
  where it matters most.
- **Inheritance** — as a plain Go `bool`, omitted and explicit `false` are
  indistinguishable after an `extends` fold, so a child template could not turn
  an inherited `true` back *off*. Making it safe would require presence tracking
  (a `*bool`) plus a rule that explicit `false` wins — complexity added purely to
  make a security-weakening flag behave.

The npm rationale is about **behavior, not integrity**: a hash-pinned lockfile
guarantees *which bytes* are installed, and scripts from those verified bytes are
still arbitrary code in the build chroot. Pinning constrains provenance; it does
not bound side effects. Re-introducing scripts is a follow-up that must ship with
network isolation *and* an artifact-recording story, not a boolean.

**"Wheel-only" bounds compilation, not all code execution.** `--only-binary=
:all:` (and `--ignore-scripts` for npm) guarantee no *build-time* script runs —
that is the specific claim Decision 7 makes. It does not mean a wheel's own
contents never execute: a wheel can ship a `.pth` file whose line is `exec`'d,
or a `sitecustomize.py`, either of which the interpreter runs automatically on
**every** startup, not just the one pip performs. Decision 8's pip inventory is
captured via `pip install --report` on the **same install invocation**, so it
adds no second interpreter start; npm's `npm ls --all --json` is a second
invocation but runs immediately after `npm ci`, before the freeze digest
(Decision 5), not deferred to `generateSBOM` time. That closes the ordering
risk (a second, later interpreter start whose side effects could land after
the freeze point) but not the underlying one: the *very first* interpreter
start — pip's own install, or npm's `npm ls` right after it — still runs
installed code once, and that code could reach the network or write outside
`target` before the digest is even captured. This is a genuine residual gap
this design does not close in v1: catching it would need
either static inspection of wheel contents for `.pth`/`sitecustomize` before
trusting them, or running install/inventory under network/
filesystem isolation. Recorded here rather than left implicit, since the
offline and SBOM-completeness guarantees elsewhere in this ADR are otherwise
stated as unconditional.

### 8. SBOM: a second inventory, new `Type` values, and PURL external references

**Decision:** Record every installed component in the same SPDX SBOM as OS
packages. **Five v1 changes**, because the existing path cannot carry them,
**plus one item (5) that is a recorded follow-up, not a v1 acceptance
criterion** — it's kept in this numbered list because it's directly downstream
of the other changes here, not because it ships with them:

1. **A separate language-package inventory, captured once at install time.**
   `generateSBOM` (`imageos.go`)
   filters `FullPkgListBom` against the `dpkg -l`/`rpm -qa` name set, so language
   components appended there are discarded. The install stage records what it
   installed into a distinct collection (e.g. `LanguagePkgListBom`) **as part of
   the Decision 5 freeze step**, immediately after install; `generateSBOM`
   **appends** that already-captured collection to `finalPkgs` *after* the OS
   filter, bypassing a filter that cannot match it by construction, without
   re-invoking `pip`/`npm` itself. For pip, the inventory comes from
   `pip install --report $S/install-report.json` on the **same install
   invocation** (Decision 4) rather than a separate `pip list` call afterward —
   this also resolves which exact artifact was installed when a requirement's
   hash set listed multiple platform candidates (Decision 3): the report's
   `download_info` records the actual URL/hash pip resolved and installed, not
   just a name and version `pip list` alone could never disambiguate between.
   **That URL must be scrubbed before it becomes SBOM provenance, not copied
   verbatim.** `buildSPDXPackage` (`manifest.go`) sets `DownloadLocation:
   pkg.URL` directly from `PackageInfo.URL` with no sanitization — unlike
   npm's `resolved` (rejected outright if it carries userinfo/credentials,
   Decision 1), a private `index`/`artifactHost` could embed a signed
   credential in the report URL's query string, which would then ship
   verbatim into `/usr/share/sbom`. The stage therefore strips userinfo and
   the query string from the report's `download_info.url` before it becomes
   `PackageInfo.URL` — or emits `NOASSERTION` for that field if the URL
   carries credentials the strip can't cleanly separate from — applying the
   same policy Decision 1 already applies to npm's `resolved`, to the one
   path that didn't have it.
   For npm, `cd <target> && $NPM ls --all --json` immediately after `npm ci` —
   the documented public
   inventory command, walking the actual on-disk tree, rather than the
   internal `.package-lock.json` cache file (whose presence and format are not
   a stable npm contract across versions); npm's lockfile already pins exactly
   one `resolved`/`integrity` per node, so there is no equivalent multi-candidate
   ambiguity to resolve. Both are partitioned against
   the bootstrap baseline ∪ lockfile per Decision 6, so unexplained on-disk
   packages fail the build rather than being silently recorded. `inBundle` npm
   nodes are included as components with `NOASSERTION` download location and no
   checksum (Decision 1).
2. **New `Type` values.** Extend `ospackage.PackageInfo.Type`
   (`internal/ospackage/ospackage.go`) with `"pip"` and `"npm"`.
3. **PURL external references.** Add `ExternalRefs` to `SPDXPackage`
   (`manifest.go`), populated in `buildSPDXPackage` (`manifest.go`) with
   `referenceCategory: PACKAGE-MANAGER`, `referenceType: purl`, and a locator of
   `pkg:pypi/<name>@<version>` or `pkg:npm/<name>@<version>`. Names must be
   **normalized and encoded**, or a scanner silently fails to match:
   - **PyPI** names are canonicalized per PEP 503 (lowercase, runs of
     `-`/`_`/`.` collapsed to a single `-`) before emission, so `Foo_Bar`
     becomes `pkg:pypi/foo-bar@…` — the form advisories are keyed on.
   - **npm** scoped names put the scope in the PURL **namespace**:
     `@babel/core` → `pkg:npm/%40babel/core@7.0.0`, with the `@` percent-encoded
     and the `/` retained as the namespace/name separator (encoding the slash
     produces an invalid locator).
   - Every PURL component (name, namespace, version) is percent-encoded per the
     PURL spec after normalization.

   A scanner matches OS packages by distro name; a PyPI or npm component has
   none, so without these coordinates the entry is present but unmatchable —
   correct PURLs are what make this feature auditable.
4. **Checksum normalization.** Lockfile hashes cannot be copied verbatim. pip's
   `--hash=sha256:<hex>` maps to the existing `SHA256`. npm `integrity` is
   Subresource Integrity — typically `sha512-<base64>` — while `validSPDXAlgos`
   admits only `SHA1`/`SHA256`/`MD5` and `buildSPDXPackage` copies values
   as-is, so an npm hash would today be dropped as an unknown algorithm or
   emitted as non-conforming base64. The fix: parse the SRI prefix, **add
   `SHA512` to `validSPDXAlgos`** (valid for the declared `SPDX-2.3`), and
   **hex-encode the decoded digest**. If an SRI algorithm is one SPDX 2.3 does
   not permit, record the SHA256 of the downloaded tarball computed during fetch
   instead of emitting an invalid checksum.
5. **The manifest writer must be extended to keep relationships beyond
   DESCRIBES — and that extension must survive the existing merge pipeline.**
   `writeSPDXDocument` (`manifest.go`) unconditionally
   **replaces** `spdx.Relationships` with only the document-to-package
   `DESCRIBES` set it regenerates itself — any other relationship a caller
   attaches beforehand, including the `inBundle` provenance relationship
   promised above, is silently discarded before the file is written. Simply
   making it preserve caller-supplied relationships is **not sufficient on its
   own**: `WriteMergedSPDXToFile` removes packages named in `removedNames`, and
   `writeSPDXDocument` itself runs `sanitizeSPDXIDs`/`dedupeSPDXIDs`, which
   **rewrite** every package's `SPDXID` (grammar normalization, then
   collision-suffixing) — both happen *inside* the same writer, after any
   relationship a caller passed in was already built. A relationship built
   against a pre-removal, pre-rewrite ID would silently reference a package
   that no longer exists in the final document, or an ID that no longer
   matches after renumbering. The fix therefore has two ordering
   requirements, not one: (a) any language relationship whose endpoint was
   removed via `removedNames` is dropped along with it, and (b) relationship
   endpoints are **remapped to the final IDs after** `sanitizeSPDXIDs`/
   `dedupeSPDXIDs` run, not built against IDs computed beforehand — i.e.
   relationships attach by a stable *package identity* the writer already
   knows (ecosystem+name+version, or the object reference itself), with the
   ID substituted in as the last step, not by a string ID computed ahead of
   the writer's own normalization.

   Separately, `SPDXPackage` needs a **stable, deterministic identity**
   for language components in the first place. `ecosystem`+`name`+`version`
   alone is **not** unique under this ADR's own multi-entry design: two
   entries (e.g. different `target`s, or different `index`/`registry` values)
   can legally pin different bytes for the same name and version, and both
   can appear in one image — for a component with an artifact of its own, the
   identity therefore also folds in the **artifact digest** (and the owning
   entry's `target`, if two entries could otherwise still collide on
   name+version+digest). **`inBundle` nodes need a different identity input,
   not a relaxed version of this one**: they are explicitly exempt from
   having `resolved`/`integrity` (Decision 1), so there is no artifact digest
   to fold in, yet the same bundled name+version can legally appear under
   different containing packages with different bytes. Their identity is
   instead **the containing package's identity, plus the node's path within
   the npm dependency tree, plus the owning entry's `target`** — the tree
   path is what disambiguates two same-name-and-version bundled instances
   under different parents, which an artifact digest could never do for a
   node that doesn't have one. The current dedupe
   behavior of suffixing collisions in input order is exactly the fragility
   both identity schemes must replace, not something to build on top of.
   Until all of this lands, the `inBundle` exemption (Decision 1) still holds —
   `NOASSERTION` download location, no checksum — but the provenance
   relationship is **not emitted in v1**; it ships once the writer, ordering,
   and identity schemes above all exist.
6. **`generateSBOM`'s existing error handling must become fatal for this
   guarantee to hold.** `generateSBOM` (`imageos.go:2418`-`2427`) currently
   treats both SPDX serialization failure and SBOM-copy-into-chroot failure as
   **warnings** — `log.Warnf(...)` on each, with the build continuing and the
   function returning success regardless. As written today, a build can
   succeed with the language inventory silently missing or incomplete, which
   contradicts every "SBOM completeness is a build guarantee" statement this
   ADR makes. This design requires that path to become fail-closed **at
   least** when `LanguagePkgListBom` is non-empty — an SPDX write or copy
   failure on a build that installed language packages must fail the build,
   not log and continue. Whether to make this fatal unconditionally (also for
   OS-only builds) is a separate, broader change this ADR does not require.

### 9. Per-ecosystem installers behind explicit dispatch; no retrofit of deb/rpm

**Decision:** Add `internal/ospackage/piputils` and `.../npmutils` (lockfile
parse + fetch) and per-ecosystem installers, dispatched by an explicit switch on
`ecosystem`, following the precedent at `chrootbuild.go`. Do **not** unify
deb/rpm/pip/npm behind a new common installer interface as part of this work —
the ecosystems differ in shape (local-repo generation and apt/rpm resolution vs.
lockfile replay into a prefix), so a common interface would abstract over two
dissimilar things.

## Recommendation

**GO, phased — pip first, npm as a fast-follow.** Decision 1 removes the only
genuinely hard subproblem, leaving work that is individually bounded and mostly
parallel to existing machinery: a fetcher shaped like `pkgfetcher` (minus its
size-only cache shortcut), an installer shaped like the existing chroot
installers, a new stage at a well-defined point, and an SBOM extension.

The standing caveat is maintenance rather than construction: PyPI and npm churn
faster than distro archives and offer weaker immutability guarantees, so the
recurring cost of keeping fetch/verify working is real and should be accepted
knowingly. v1 is constrained to hash-pinned lockfiles, a tool-generated install
file, wheels only, **no lifecycle script execution at all**, a fresh
allowlisted-prefix target, build-host create-mode targets only (`imageType: raw`
— including its qcow2/vhd/vhdx/vmdk/vdi artifacts — and WSL2), and no per-user
installs — which keeps that exposure at its minimum and leaves every relaxation
as an explicit, separately-reviewable follow-up.

## Risks and Mitigations

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Requirements-file directives pull un-staged bytes | `-r`/`-c` includes or `--index-url`/`--find-links` in the operator's file void the offline and pinning guarantees; a per-line grammar check misses them | The operator's file is never passed to pip; the tool re-emits a canonical `name==version --hash=…` file from the parsed set, and rejects includes/directives in the input (Decision 1) |
| Wheel source undefined for pip | A requirements file carries no artifact URLs, and the install is `--no-index`, so the fetch phase has nothing to resolve against — especially for mirrors/private indexes | An optional per-entry `index` field (default PyPI), the single fetch source since direct-URL requirements are rejected; used only during `PreProcess`; discovered wheels still hash-verified. Guide mandates `pip-compile --no-emit-index-url` (Decision 1) |
| PyPI wheel CDN host distinct from the index host | An allowlist covering only `index` would reject every default-PyPI wheel, since `files.pythonhosted.org` serves the actual bytes | A separate `artifactHost` field (default `files.pythonhosted.org`), required for private indexes serving artifacts from a different host (Decision 1) |
| Redirect bypasses the fetch allowlist | The project HTTP client sets no `CheckRedirect`, so a 3xx off an allowlisted host to an arbitrary one reopens the SSRF/exfil path | A `CheckRedirect` re-applies the scheme/host policy to every hop and fails closed on a redirect leaving the allowlist (Decision 1) |
| Post-stage mutation of `target` | `updateImageConfig`/`updateRootfsConfig` run `additionalFiles` + `configurations[].cmd` after the locked stage and before SBOM, so a `cmd` could install into `target` unrecorded / online | `target` is frozen after install; a content digest is re-verified immediately before `generateSBOM`, and any change is a hard build error naming the affected target (not necessarily the specific entry) (Decision 5) |
| pip honors ambient `PIP_*`/`pip.conf` | `PIP_TARGET`/`PIP_PREFIX`/`PIP_USER`/`PIP_CONFIG_FILE` can redirect the install out of the fresh venv or re-enable an index | `--isolated` (pip's own guarantee, independent of the shell layer); `PYTHONPATH`/`PYTHONHOME`/`PYTHONSTARTUP` explicitly overridden to empty since the executor can only override named variables, not unset arbitrary ones (Decision 4) |
| PURL name not canonical | A non-canonical `pkg:pypi/Foo_Bar@…` or unencoded scoped npm name is unmatchable by scanners | PyPI names canonicalized per PEP 503; npm scope in the namespace; all PURL components percent-encoded (Decision 8) |
| npm `resolved` dereferenced as an arbitrary URI | `version`+`resolved`+`integrity` does not prove a node is registry-sourced; a `file:`/`http:` URI is a local-file-read / SSRF surface, and integrity only validates bytes *after* they are read | `resolved` must be `https://` on an allowlisted registry host, enforced before any fetch (Decision 1) |
| Weak npm integrity accepted | A lockfile may legally carry collision-prone `sha1-…`, making the npm path weaker than pip's mandated SHA-256 | Integrity floor of `sha256`/`sha512`; `sha1`/`md5` rejected (Decision 1) |
| Venv interpreter rejected by the allowlist gate | `verifyCmdWithFullPath` does an exact `commandMap[fields[0]]` lookup, so `<target>/bin/python` fails closed and no static entry can cover a dynamic `target` | Committed: drive the install through the allowlisted `python3` via `--python <target>/bin/python`, a pip \u2265 23.1 argument rather than a new allowlist rule (Decision 4) |
| `commandMap` additions break the builder-image preflight check | `python3`/`npm` are target-chroot capabilities, but `scripts/preflight.sh`/`builder-image-preflight` validate every `commandMap` entry exists in the ICT builder image itself | Requires a target-only marker in `commandMap`/`preflight.sh`, or a separate chroot-only allowlist, not ordinary entries (Decision 4) |
| `cd <target> && npm ci` never reaches the chroot as written | `GetFullCmdStr` hands the whole `chroot … cmd` string to an outer `bash -c`, which splits on `&&` before `chroot` runs — `cd` fails as a non-existent binary, and the following `npm ci` either never runs or would run outside the chroot | Wrap the snippet in an in-chroot `bash -c '...'`, following the existing `shell.IsCommandExist` precedent; the quote-aware separator search means only `bash` needs allowlisting (Decision 4) |
| Inherited npm env/config changes the inventory | `NODE_ENV`, `NPM_CONFIG_*`, lowercase `npm_config_*`, and user/global `.npmrc` can omit locked deps or redirect the cache/registry, so one lockfile yields different images | Both env-var case variants scrubbed; `HOME`/`userconfig`/`globalconfig` pointed at an empty dir; `--include=dev --include=optional` explicit (Decision 4) |
| Bootstrap capability probe too shallow | rpm-family providers do not consistently ship `python3-venv`; and `python3 -m venv --help` proves only that the module loads, so a missing `ensurepip` passes and fails mid-install | Probe creates and tears down a real throwaway venv (and checks `pip --version`) plus `npm --version`; package names used only for actionable errors (Decision 4) |
| Overlay template silently skips the stage | Providers branch to the overlay pipeline before any maker, so the image ships without the requested packages *or* SBOM entries | Overlay templates setting `languagePackages` fail validation outright (Decision 5) |
| ISO's inner initrd template slips through | An ISO composes `initrdmaker` over a separate template, so rejecting only the outer template still reaches `InstallInitrd` | ISO validation also validates the referenced initrd template (Decision 5) |
| `target` used for command injection or to clobber OS paths | A value like `/opt/app; rm -rf /`, or `/usr` or `/etc`, escapes the dedicated-prefix intent | Absolute + already-normalized + no metacharacters, restricted to `/opt/**` or `/usr/local/**`, plus `shell.QuoteArg` on interpolation (Decision 2) |
| Pre-existing `target` leaves unpinned packages | `venv` reuses a directory and `pip install -r` removes nothing; and an OS package installed *before* this stage can create `/opt/...`, so config-time validation cannot catch it | Existence/symlink check at **stage time**, immediately before staging, in addition to the cheap config-time check (Decision 6) |
| venv bootstrap packages absent from the SBOM | `ensurepip` seeds real components absent from the lockfile, and the seed set is target-Python-version-dependent (no guaranteed `setuptools`/`wheel`) | A `pip list` baseline is snapshotted right after venv creation; realized set partitioned against baseline ∪ lockfile, unknowns fail the build (Decisions 6, 8) |
| Commands rejected before execution at the top level | `python3`/`npm` are absent from `commandMap`, and `GetFullCmdStr` fails closed for unlisted top-level commands — but not for content nested inside an already-allowlisted wrapper's quoted argument (e.g. `bash -c '...'`, which this stage's own npm installer relies on) | Allowlist additions here are a sanctioning of an already-reachable capability, not new exposure; called out explicitly in the security objectives (Decision 4) |
| npm offline install misses the cache | `npm ci --offline --cache` resolves by integrity from `cacache` and does not scan a tarball directory, so a clean build fails | Staging populates cacache via `npm cache add` per artifact before `npm ci`; lockfile `resolved` rewriting rejected as input mutation (Decision 4) |
| npm too old to replay the lockfile | `lockfileVersion` 2/3 unsupported by the distro's npm; declaring the `npm` package says nothing about version | `npm --version` probed in the chroot with an actionable failure (Decision 1) |
| Over-strict lockfile validation rejects valid input | Root `packages[""]` and `inBundle` nodes legitimately lack an artifact pin | Validation scoped to registry-sourced nodes (requiring `version`+`resolved`+`integrity`); root and `inBundle` exempt; `link`/`file:`/`git:` rejected (Decision 1) |
| Ambiguous relative paths across an `extends` fold | Parent and child both ship `files/requirements.txt`; the search-path list lets an override resolve the wrong file | Paths canonicalized against the owning template's directory *before* merge; containment enforced per-owner (Decision 2) |
| Lockfile path escapes the template directory | An accidental or malicious `../../…` exposes an unrelated host file | Clean + absolutize + `EvalSymlinks` + enforced containment (Decision 2) |
| Stale or corrupted cache entry bypasses verification | Pinning guarantee silently void; the OS fetcher's size-only skip would allow it | Cache hits re-hashed against the lockfile and re-fetched on mismatch (Decision 3) |
| Language packages shipped with no SBOM record | `generateSBOM` filters against `dpkg -l`/`rpm -qa`, which can never match them | Separate inventory appended after the OS filter (Decision 8) |
| `inBundle` provenance relationship silently dropped | `writeSPDXDocument` unconditionally replaces `spdx.Relationships` with only its own regenerated `DESCRIBES` set, discarding any other relationship a caller attaches | Manifest writer extended to preserve caller-supplied relationships and append `DESCRIBES` to them, plus a stable per-component SPDX-ID scheme (Decision 8) — until then the `NOASSERTION`/no-checksum exemption holds without the relationship |
| Same-basename artifacts from different entries collide in the cache | Two entries with different `index`/`registry` values, or two lockfiles, can produce the same filename with different bytes; a basename-keyed cache would overwrite one | Cache keyed by content hash (`cache/<ecosystem>/<sha256>`), not by filename (Decision 3) |
| ISO image silently missing its libraries | The live installer runs `InstallImageOs` on the target machine, where the staged cache and install file do not exist | `languagePackages` rejected at validation for ISO targets in v1; packaging path listed as a follow-up (Decision 5) |
| Language packages installed into an initrd | Bloated early-boot image with no SBOM | Rejected at validation for the initrd flow (Decision 5) |
| npm `integrity` dropped or non-conforming | SBOM checksums missing or invalid for npm components | SRI parsed, `SHA512` added to `validSPDXAlgos`, digest hex-encoded, tarball-SHA256 fallback (Decision 8) |
| Scoped npm PURL unmatchable | `%2F`-encoding the separator yields an invalid locator | Scope is the PURL namespace: `pkg:npm/%40scope/name@version` (Decision 8) |
| Package ships no wheel for the target arch/Python | Build would need a compiler toolchain; result non-reproducible | `--only-binary=:all:` — hard error naming the package (Decision 7) |
| Host paths unreachable from inside the chroot | The install commands cannot see their cache or install file | Explicit staging to in-chroot paths, torn down via `defer` (Decision 4) |
| Foreign-arch interpreter cannot execute | Cross-arch builds fail with `exec format error` | Host `binfmt_misc` + `qemu-<arch>-static` prerequisite asserted for this stage, extended to rpm providers (Decision 4) |
| Bootstrap toolchain inflates the image | Larger minimal images than necessary | Declared explicitly by the author, so the cost is visible; post-install removal is a follow-up |
| Operator burden of generating lockfiles | Feature goes unused; operators revert to `configurations` `cmd` | Document the `pip-compile`/`npm install` workflow with a worked template; revisit inline-list sugar once the pinned path is proven |

## Alternatives Considered

- **Sanction `systemConfig.configurations[].cmd` as the documented path:**
  rejected — no SBOM entry, no enforced pinning, live network per build, no
  offline support, and the templates guide already discourages `configurations`
  for anything a declarative field can express.
- **`additionalFiles` to stage a lockfile plus a `cmd` to install it:** rejected
  — `additionalFiles` is a pure file copy (`local`/`final`/`stage` only, schema
  at `internal/config/schema/os-image-template.schema.json`) with no
  execution capability, so this is the `cmd` alternative wearing a second field.
- **Implement dependency resolution in-tool:** rejected — large, fast-moving
  domain per ecosystem, and divergence from upstream resolvers would be a bug
  source; Decision 1 delegates it.
- **Pass the operator's requirements file straight to pip:** rejected — legal
  include and index directives make it an un-auditable input (Decision 1).
- **Rewrite npm lockfile `resolved` to `file:` paths for offline install:**
  rejected — mechanically simpler than populating cacache, but mutates the
  operator's pinned input (Decision 4).
- **Convert pip/npm packages into `.deb`/`.rpm` and reuse the OS path
  (`fpm`-style):** rejected — inherits an extra packaging tool and its metadata
  quirks, and misrepresents the components' ecosystem in the SBOM.
- **Defer entirely, relying on runtime provisioning (first-boot/cloud-init):**
  legitimate for some deployments and still available, but it defeats the point
  of a pre-baked image (offline first boot, immutable content, build-time audit)
  and leaves the SBOM gap unaddressed.
- **Host-side install into the chroot prefix:** rejected — wrong-Python-version
  and wrong-architecture wheel selection (Decision 4).

## Out of scope / follow-ups

- **ISO target support** — requires packaging the artifact cache and generated
  install file onto the medium and serializing the language inventory into
  `template-dump.yaml` alongside `SBOMPackageMetadata`, then consuming both from
  `cmd/live-installer` (Decision 5).
- **Lifecycle script execution**, which must ship with network isolation *and* a
  story for recording script-produced artifacts in the SBOM — not a boolean
  (Decision 7).
- **Per-user installs** (`pip --user`, user-local `node_modules`) — needs an
  install stage after `createUser` (Decision 5).
- **sdist / native-addon builds**, which would require a toolchain and a build
  chroot; reconsider alongside any source-build capability.
- **Inline unpinned package lists** as convenience sugar, contingent on the tool
  emitting and committing a lockfile so pinning survives.
- **Removing the bootstrap toolchain** (the venv/pip and npm packages,
  whatever each provider names them) from the final
  image after the install stage.
- **Exposing installed entry points on `PATH`** automatically (venv `bin/`,
  `node_modules/.bin`) rather than leaving it to `additionalFiles`.
- **Additional ecosystems** (cargo, gem, go modules) — the
  `ecosystem`-discriminated schema accommodates them; each needs its own
  fetch/verify/install path.
- **Unifying the deb/rpm/pip/npm installers** behind one interface (Decision 9),
  if a fourth ecosystem justifies it.
- **Vulnerability scanning of the emitted SBOM in CI** — the PURL work makes the
  language components scannable, but no workflow currently scans a built image's
  SBOM.
- **Overlay-mode support** — rejected at validation in v1 (Decision 5); an
  overlay baseline needs its own preflight/report treatment, including how a
  language target interacts with baseline-owned paths.
- **Extending `commandMap`/`preflight.sh` with a target-only marker** (Decision
  4) so `python3`/`npm` (and any future chroot-only capability) don't have to
  exist in the ICT builder image just to pass `builder-image-preflight`.
