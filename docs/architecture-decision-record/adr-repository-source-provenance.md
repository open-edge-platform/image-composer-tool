# ADR: Repository-Controlled Source Package Provenance

- **Status**: Proposed
- **Date**: 2026-11-16
- **Authors**: ICT Team
- **Technical Area**: Package Resolution / SBOM / Source Provenance

## Summary

Introduce an opt-in setting on each package repository so ICT can publish exact
source-package locations for the binaries installed in an image. Developers
should be able to start from a generated provenance manifest or SPDX SBOM,
identify the corresponding source package, and obtain its build inputs without
booting the image or reconstructing its repository configuration.

Deliver the capability in two phases:

1. **Discover and describe**: resolve source metadata and publish verified source
   links, checksums, and binary-to-source mappings. Do not download source archives.
2. **Retrieve and preserve**: consume those records to download, retain, and
   optionally unpack verified source bundles; collect explicit revision and build
   evidence when available.

Both phases are proposed, not implemented. Evidence recovered for individual
packages establishes feasibility, not image-wide coverage or build reproducibility.

## Context

### Observed Package Behavior

An Ubuntu NVL image audit demonstrated the following:

| Example | Installed binary to source mapping | Recoverable evidence |
| --- | --- | --- |
| GStreamer | `gstreamer1.0-tools` maps to `gstreamer1.0`, version `1.28.2-1ppa1~resolute1` | Exact source descriptor, upstream archive, Debian packaging archive, and repository checksums; package metadata identifies a Git repository but not a specific commit. |
| Mesa | `mesa-vulkan-drivers`, `libgl1-mesa-dri`, `mesa-libgallium`, `libegl-mesa0`, and `libgbm1` map to `mesa`, version `26.0.8-1ppa1~resolute1` | One shared source bundle; examining the archive reveals a Git archive revision, while Debian packaging selects an NVL enablement patch with its own patch-header revision. |

The Mesa bundle includes the ANV Vulkan driver, Iris OpenGL driver, Intel shader
compiler, and shared NIR compiler sources. Kernel graphics drivers and GPU
firmware are separate components and require their own provenance.

### NVL Platform BKC Debugging Scope

Use an Ubuntu NVL platform BKC overlay template and its final installed
inventory as the initial integration scenario. The following domains are
coverage targets, not claims that a particular template installs every component
or that its sources are available:

| Domain | Debugging use | Provenance to identify when present |
| --- | --- | --- |
| Kernel and DKMS | Out-of-tree i915/Xe driver bugs and custom kernel patches | Installed kernel and header packages, separately packaged DKMS sources, and their exact source versions and patch evidence. |
| Mesa (3D/Vulkan) | Mesa compiler and Intel graphics driver issues | Mesa binary packages and their shared source bundle, including ANV, Iris, and compiler sources when published. |
| Media (OpenVPL and Media Driver) | Encode/decode pipeline failures | Installed OpenVPL and Intel Media Driver packages and their respective source bundles and revisions when available. |
| FFmpeg | Custom video integration and cartwheel patch issues | Installed FFmpeg packages and their exact source bundle, including vendor packaging and patch evidence when published. |
| Camera HAL (IPU6/7) | Sensor tuning and camera pipeline hangs | Installed camera HAL and IPU-related packages and their source bundles, plus available tuning and configuration evidence. |

Resolve the actual binary-to-source mappings from package metadata, not domain
names or assumed package names. Report missing, proprietary, locally installed,
or otherwise untraceable components explicitly. DKMS modules built on the image
or at boot, kernel build configuration, camera tuning assets, firmware, and
debug symbols may require evidence beyond source-package metadata; do not infer
their origin or exact running version from a source package alone. Runtime and
hardware debugging remain outside this ADR's provenance guarantees.

The audit also exposed workflow gaps:

- Installed dpkg metadata retains source identity but not necessarily repository
  origin, source archive URLs, or the Git commit used to build a binary.
- Configuring a binary repository does not enable its source indexes or preserve
  the metadata used during the build. APT may know a vendor version only through
  its installed-package database.
- Manual recovery required source-repository configuration, network and proxy
  troubleshooting, and distinguishing a post-update hook timeout from successful
  index downloads.
- The delta SBOM retained binary URLs and hashes but no source links. Its singular
  `checksum` field also prevented standard SPDX checksum queries from working.
- Several binaries share a source package. A binary-by-binary source download
  would duplicate work and storage.
- Retrieving sources later proves availability at retrieval time, not that ICT
  captured them at build time or that the published binary was built from them.

## Decision

Start with repository-controlled, metadata-only provenance for Debian-family
targets. Reuse ICT's package resolver, secure HTTP client, repository trust
configuration, and cache. Do not run guest `apt update` as a prerequisite.

Source discovery is separate from source artifact downloads, source unpacking,
build dependency installation, and rebuilding. Normal builds must not install
source trees or development dependencies into production images.

Phase 1 establishes the canonical binary-to-source provenance records and corrects
the existing SPDX generation issues necessary to publish them reliably. Phase 2
builds directly on these records to retrieve, verify, and retain source artifacts
and additional evidence. Neither phase requires ICT to reproduce package builds
or independently establish that published binaries were built from the identified
sources.

### Source Bundles and VCS References

Accept both source-bundle evidence and producer-declared VCS evidence, but do not
treat them as interchangeable. A source bundle identified by the exact source
package name and version is the canonical Debian source input: it includes the
upstream archive and the packaging or patches selected for that release. A VCS
reference is additional reconstruction evidence unless the producer explicitly
attests that one repository revision contains the complete source and packaging
input set for the declared package version.

| Evidence path | ICT treatment |
| --- | --- |
| Source package in an enabled `Sources` index | Record the `.dsc` and every referenced archive as the complete repository-published source bundle. Do not download it in Phase 1. |
| VCS reference published for a source package | Preserve the repository, revision, optional ref name, declared package version, role, and publication source. Mark it as supporting evidence unless the publisher states that it is a complete, reproducible input set. |
| Both source package and VCS references | Correlate both with the exact source identity and retain them independently. Do not make one route disappear because the other is present. |
| Incomplete or unverified VCS declaration | Record a coverage gap. Do not promote a tag, branch, version suffix, homepage, or current repository HEAD to a package build revision. |

ICT compares producer-declared package versions as exact values after applying
normal Debian version parsing; it does not infer that a similarly named tag or a
revision is the package version. The matching declaration may appear in package
metadata, repository sidecar metadata, or publisher attestations. The provenance
manifest must identify which mechanism supplied it and the verification performed
by ICT. A missing match is not an error in the advisory mode, but it must remain
visible as an unresolved or contradictory declaration.

### Proposed Repository Configuration

The following is proposed syntax, not currently supported template configuration:

```yaml
packageRepositories:
  - codename: resolute
    url: https://packages.example.org/ubuntu/snapshot/
    component: main multimedia kernels
    sourceProvenance:
      enabled: true
      # Optional when the source repository has a different base URL:
      # url: https://sources.example.org/ubuntu/snapshot/
```

- Omission or `enabled: false` preserves existing behavior and causes no additional
  source-index requests.
- The source URL, suite, components, and trust configuration default to those of
  the binary repository. Explicit source-repository overrides must support a
  different URL, suite, components, and signing key where required.
- Authentication uses ICT's credential handling and must not be copied to output
  manifests. Inherited credentials must not be forwarded to a different origin
  without explicit configuration.
- Internal repositories use a release-policy-approved canonical URL or opaque
  repository identifier. The identifier must be stable enough for authorized
  developers to resolve the configured repository without exposing credentials,
  query-string tokens, or unrelated private endpoints. Public documentation,
  fixtures, and examples continue to use placeholder public URLs.
- Enabling discovery does not modify the image's APT sources or enable `deb-src`
  inside the image. Guest source configuration remains a separate concern.
- Initially, this option is valid only for supported Debian-family repositories;
  other repository types must produce an explicit validation error if enabled.

### Producer Evidence Contract

ICT consumes publisher evidence; it cannot create missing upstream, packaging, or
build evidence from package names. The producer-side contract therefore belongs in
the EdgePack or repository contribution standard, while this ADR defines the
normalized manifest model and the version-matching rule that ICT enforces.

A producer that wants a package to be independently recoverable should publish:

1. An exact source package identity and complete source-bundle references in the
   `Sources` index, including the `.dsc` and every archive checksum and size.
2. A role-aware VCS declaration where meaningful: repository URL, immutable
   revision, optional branch or tag, input role (`upstream`, `packaging`, `patch`,
   or `other`), and the package version for which that reference was declared.
   Existing fields such as `Vcs-Git` may provide some of this information, but
   a repository URL alone does not establish a revision or package-version
   match. Exact field encoding and any authenticated sidecar format are
   contribution standard decisions, not inferred by ICT.
3. A statement of completeness when a VCS revision is intended to replace a
   source bundle. Without that producer attestation, a revision is supporting
   evidence rather than a complete reconstruction recipe.
4. `.buildinfo` and `.changes` for release builds when available, plus publisher
   attestations or build logs required by the release policy. ICT records missing
   evidence explicitly; availability of `.buildinfo` does not by itself prove a
   reproducible build.

The contribution standard should define the concrete Debian control or sidecar
field names. The logical normalized declaration is:

```yaml
vcsInputSet:
  packageVersion: 1.2.3-4vendor1
  complete: true
  sourceReferences:
    - role: upstream
      repository: https://code.example.org/project/upstream.git
      revision: 0123456789abcdef
      ref: release-tag-or-branch       # optional
    - role: packaging
      repository: https://code.example.org/project/packaging.git
      revision: abcdef0123456789
    - role: patch
      name: debian/patches/vendor-enablement.patch
      origin: https://code.example.org/project/patches.git
      revision: fedcba9876543210
```

A tag or branch name is a convenience label for the declared immutable revision;
it never replaces that revision. Producer declarations containing a package
version different from the resolved binary/source package version are retained as
contradictory evidence and are not accepted as matches. Completeness is a property
of the declared set: `complete: true` means the producer attests that the listed
references and any referenced source archives form the full package input set.
Individual references do not inherit that claim in isolation.

### Debug-Critical Scope and Strict Policy

The complete reconciled inventory remains the canonical output. A template or
EdgePack release manifest may additionally declare a curated set of
debug-critical package selectors, for example kernel and DKMS, Mesa,
OpenVPL/Media Driver, FFmpeg, and camera HAL. This list is a prioritization and
policy input, not a replacement for the full generated manifest and not a claim
that every named domain is installed.

Selectors apply after the final inventory is reconciled. Absent selectors report
no installed package rather than creating a requirement for an unrelated
component. A build-level strict policy can initially target either the complete
inventory or only the curated selectors; its exact YAML syntax is deferred to the
schema design. In curated mode, unmapped critical packages fail strict validation,
while other advisory gaps remain reported but non-fatal. Every mode still emits the
full inventory and its gaps.

## Phase 1: Discover and Describe

### Phase 1 Implementation Scope

1. **Carry source identity through the package pipeline.** Preserve source name,
   source version, selected repository, binary architecture, and available binary
   hashes from resolution through overlays and manifest generation. Parse
   `Source: name`, `Source: name (version)`, and Debian defaults correctly,
   including epochs, binNMUs, multiarch, and architecture-independent packages.
   Bump the parsed metadata cache version when its stored shape changes.
2. **Reconcile the final installed inventory.** Account for packages retained from
   the baseline, added or upgraded by ICT, removed during composition, and
   installed by configuration scripts. Do not equate an overlay delta with the
   full image. Packages whose source or repository cannot be established remain
   explicitly unresolved; scripts installing local DEBs do not imply a known
   repository origin.
3. **Resolve source metadata for enabled repositories.** Fetch the applicable
   `Sources` indexes listed by repository metadata, supporting the published
   compression formats and multiline Debian control fields. Verify signed
   Release/InRelease metadata, index digests, and index sizes using ICT's trust
   policy. Preserve the original metadata, available VCS declarations, and their
   verification results without fetching referenced VCS repositories.
4. **Match exact source identities and declarations.** Use the binary's declared
   source name and version and constrain matches to its selected repository or
   explicit source override. Compare every producer VCS declaration with that
   exact package version. Never substitute another version, upstream tag, or
   repository's same-version publication silently. For inherited packages, a
   candidate match is not proof of installation origin; verified binary-hash
   evidence or retained build provenance is needed to strengthen that
   association.
5. **Publish complete source artifact locations.** Record the source descriptor
   (`.dsc`) and every archive referenced by the exact `Sources` stanza, including
   URLs, sizes, and SHA-256 values. Support more than one upstream archive or
   packaging format rather than assuming every package has exactly three files.
   Resolve paths safely within the configured repository; reject traversal and
   unapproved origins. Retain the `.dsc` reference for Phase 2 cross-checking.
6. **Deduplicate source records.** Share a record across binaries with the same
   repository snapshot and source identity. Include metadata digests in identity
   and cache decisions so equal names and versions from different publications
   cannot overwrite one another.
7. **Emit a versioned provenance manifest and standard SPDX.** Write both as build
   artifacts and embed the lightweight provenance manifest beside the image SBOM.
   Bind the build-side manifest to the final image digest; the embedded copy must
   not require a self-referential image digest. Distinguish full-inventory and
   delta coverage in both outputs.
8. **Correct and validate SPDX generation.** Address existing SPDX generation
   defects as part of Phase 1, including the non-standard singular checksum field,
   preservation of package relationships during overlay merges, and stable SPDX
   identifiers for binary and source packages. Ensure generated SBOMs conform to
   the selected SPDX specification and that source-package relationships
   accurately reflect the available evidence. These corrections are prerequisites
   for publishing source provenance through SPDX, not separate follow-on work.
   Maintain backward compatibility when consuming existing ICT-generated SBOMs.

### Manifest and SPDX Contract

The provenance manifest is the canonical structured record. Each binary record
references a source record and retains evidence of that association. Each source
record includes source name/version, repository snapshot and component, metadata
digests, signing-key fingerprint, verification outcome, artifact locations and
hashes, observation time, and retrieval status. Do not include credentials or
secret-bearing URLs.

Source records also contain zero or more typed VCS input sets. Each set records
the producer-declared package version, a completeness attestation, and its typed
`sourceReferences`. Each reference records its role, repository or origin,
immutable revision, optional ref name, and evidence location. Upstream, Debian
packaging, and patch references are separate entries so one revision cannot
ambiguously stand in for several input histories. Patch references retain the
patch filename or series position and the origin/revision declared in its
metadata; Phase 1 records them only when they are already published in repository
metadata or another inspected producer declaration. Patch headers inside source
archives remain Phase 2 evidence.

Use separate fields for mapping, metadata verification, artifact retrieval, and
VCS declaration status. A resolved source bundle does not imply VCS revision
verification; a declared revision does not imply that its repository was fetched,
its revision exists, or its content was included in the binary. A reference can
therefore be `declared`, `version-matched`, `verified`, `contradictory`, or
`unavailable`, independently of source-bundle discovery. The manifest must keep the
matching package version and the role of each reference visible.

### Developer Retrieval Paths

Phase 1 gives developers the exact `.dsc` URL and archive hashes. For a source
bundle accessible to the developer, the immediate path is to pass that `.dsc` URL
to `dget`, or to perform the equivalent authenticated download with a tool that
honors the descriptor. ICT does not require or configure `deb-src` in the image,
and Phase 1 does not install build dependencies. Resolving build dependencies from
`debian/control` remains a separate step and does not change the recorded source
identity.

Phase 2 adds the proposed `sources fetch` command so consumers can retrieve and
verify the complete recorded bundle without rediscovering repository metadata.
The matching optional unpack path applies the source format and patch series in an
isolated work directory. A VCS route is useful when a producer declares a complete,
version-matched input set; otherwise a developer may use its typed references to
inspect upstream or packaging history, but must not clone a single commit and
assume it reconstructs the Debian package.

Examples of discovery outcomes are `resolved`, `disabled`, `origin-unknown`,
`source-index-missing`, `source-version-missing`, `network-error`,
`verification-failed`, and `unsupported`. Phase 1 records artifact retrieval as
`not-requested`. Unverified metadata must never be reported as verified; existing
explicitly unsigned repository policies remain visibly unverified.

Discovery is advisory by default: unavailable source metadata produces warnings
and explicit records rather than silently disappearing or changing binary
selection. An explicit build-level strict policy fails when a source mapping for
an enabled repository cannot be verified. Neither mode accepts failed signature
or checksum verification as a successful mapping. Record stale-cache use and
metadata expiry; strict mode must not silently treat expired evidence as fresh.

SPDX represents source packages as separate package entries, including the .dsc
download location and available checksums. Producer VCS references are emitted
through standard SPDX fields where the selected specification supports an
unambiguous representation; the companion manifest remains authoritative for
input roles and producer completeness declarations. Binary-to-source associations
must use relationship types supported by the selected SPDX specification, without
implying verified build execution. In particular, GENERATED_FROM must only be used
where its semantics are justified by the available evidence. Repository-declared
source mappings and their verification status are documented through standard
SPDX annotations or comments and the companion provenance manifest. Uncertain
candidate mappings must not be represented as verified relationships.

The existing SPDX generation must also be corrected to emit standard checksums
rather than the current singular checksum field. Generated documents must pass
schema validation, including after overlay merges. Existing SBOMs containing the
legacy field remain readable for backward compatibility.

### Phase 1 Exit Criteria and Accomplishments

- Opting in a repository generates exact source links and checksums without
  downloading source archives or changing the installed package set.
- GStreamer resolves to its exact vendor source version and expected artifact
  list. The five Mesa binaries reference one exact source record.
- For an NVL BKC template, reconcile installed packages across the kernel/DKMS,
  Mesa, OpenVPL/Media Driver, FFmpeg, and camera HAL domains. Each applicable
  package has an evidence-backed source mapping or an explicit coverage gap;
  absent domains are reported rather than assumed installed.
- A developer can find the `.dsc` and all archive URLs from the manifest or SPDX
  without booting the image or discovering repository configuration manually.
  Producer-declared VCS references are present alongside the source bundle with
  their roles, immutable revisions, optional refs, version-match outcome, and
  completeness attestation.
- Curated debug-critical selectors identify the strict-policy subset while the
  full manifest still reports every installed package and coverage gap. Absent
  domain packages are reported as absent, not treated as required packages.
- Automated fixtures cover Debian source defaults, explicit source versions,
  epochs/binNMUs, multiarch, multiple upstream archives, repository ambiguity,
  baseline/script inventory gaps, missing indexes/versions, multi-role VCS
  declarations, version mismatches, incomplete VCS sets, signature/hash failures,
  cache behavior, and network failures. Include representative NVL domain
  packages and unavailable or proprietary sources without relying on private
  repositories. Normal non-opt-in builds make no source requests, and SPDX schema
  validation passes.

### Phase 1 Limitations

- Verified links describe a publication observed at build time; artifacts are not
  downloaded, availability-tested individually, or protected against later
  deletion. Repository access and credentials may still be needed by developers.
- Source archives and their internal revision markers are not inspected. Mesa's
  Git archive revision and vendor patch-header revision are therefore not Phase 1
  discoveries, even though its source-package links are available.
- Repository metadata declares a binary-to-source mapping; it does not prove the
  binary was built from those sources. Unknown baseline origins remain unknown.
- Build environments, matching debug symbols, reproducibility, snaps, RPM source
  packages, untracked downloads, and proprietary firmware sources are outside
  the initial scope. Report coverage gaps, not image-wide completeness.

## Phase 2: Retrieve and Preserve

### Phase 2 Implementation Scope

1. **Add manifest-driven retrieval.** Introduce a proposed `sources fetch` command
   accepting a provenance manifest, binary package selector, and output directory.
   Resolve a binary selector to its deduplicated source record. An optional build
   policy can invoke the same retrieval path for selected or all resolved source
   records. Phase 1's repository setting alone must not trigger archive downloads.
2. **Verify and retain complete bundles.** Download only the recorded versions,
   enforce size and SHA-256 checks, and cross-check the `.dsc` source identity and
   referenced archives against the recorded index evidence. Verify independent
   descriptor signatures when present, recording missing/unknown signatures
   separately from the authenticated repository evidence. Use atomic,
   content-addressed storage outside temporary build directories. Make the
   retention/export policy explicit and provide offline reuse.
3. **Support optional safe unpacking.** Reuse `dpkg-source` for Debian source
   formats and patch application in an unprivileged, isolated work directory.
   Apply traversal, symlink, size, and resource limits to untrusted archives. Do
   not install build dependencies, execute package build scripts, or modify the
   production image as a side effect of retrieval or unpacking.
4. **Collect and verify typed revision evidence.** Inspect archives and packaging
   for explicit Git archive headers, recorded revision files, VCS references,
   patch series, and patch-header IDs. Preserve the evidence location, digest,
   and meaning. Where policy permits and a producer supplied an immutable
   revision, verify that the revision exists without resolving a branch or tag to
   a different current value. Distinguish archive-base, upstream, packaging,
   patch, and attested-build revisions. Never infer a build commit from a
   homepage, version suffix, tag, or the current HEAD of a repository.
5. **Retain build and debugging evidence where published.** Associate available
   `.buildinfo`, `.changes`, build logs, toolchain/dependency versions, build IDs,
   matching debug symbols, and publisher attestations with verified artifacts.
   These can be published separately from `Sources`; their absence must be
   explicit and must not prevent a verified source bundle from being useful.
6. **Enable downstream reproducibility verification.** Preserve available source
   artifacts, build metadata, toolchain and dependency information, and publisher
   attestations in a form consumable by external build-verification workflows. ICT
   is responsible for collecting, verifying, and retaining available provenance
   evidence, but does not execute package rebuilds or perform binary reproducibility
   comparisons as part of this ADR. Such capabilities may be addressed independently
   in the future.

### Phase 2 Exit Criteria and Accomplishments

- The GStreamer and Mesa bundles can be retrieved, verified, exported, and reused
  offline from manifests without repeating repository discovery.
- Unpacking Mesa applies the selected NVL patch; the manifest distinguishes its
  Git archive revision from the patch-header revision. GStreamer's unavailable
  build commit remains explicitly unavailable rather than guessed.
- For available NVL BKC source records, retrieval and optional unpacking expose
  published kernel/DKMS, media, FFmpeg, and camera sources and patch evidence;
  unavailable sources, tuning assets, debug symbols, and build evidence remain
  explicit gaps rather than inferred provenance.
- Records distinguish links resolved, bytes verified, artifacts retained, sources
  unpacked, revision evidence found, and build evidence available. Source availability,
  integrity, and build provenance are reported independently. No reproducibility or
  build-execution claims are made without corresponding external verification or
  publisher attestations.
- Tests exercise corrupted downloads, descriptor/index disagreement, interrupted
  transfers, deduplication, offline retrieval, archive safety, missing evidence,
  patch application, and accurate reporting of available or missing build evidence
  and publisher attestations.

### Phase 2 Limitations

Retention protects only the artifacts actually captured under the configured
policy. A signed repository plus matching source hashes is not a publisher build
attestation. Archive and patch revisions need not identify a single final package
build commit. Missing build dependencies, proprietary inputs, signing keys,
toolchains, or publisher evidence can still prevent an identical rebuild.

Phase 2 completes the source discovery-to-retention workflow and reports the
strength of available build evidence; it does not guarantee complete provenance
for every component or bit-for-bit reproducibility for every package. Graphics
runtime validation still requires suitable hardware and separate kernel/firmware
provenance.

## Delivery and Documentation

Implement Phase 1 in small reviewable increments: repository configuration and
source identity, authenticated source-index resolution, then manifest/SPDX output
and inventory integration. Implement Phase 2 separately after the Phase 1 contract
is validated. Do not couple the first useful source-link feature to a rebuild
service or support for every package ecosystem.

Update the template schema and reference, repository configuration guide, SBOM
documentation, cache documentation, security objectives, release notes, and the
EdgePack or repository contribution standard with Phase 1. The contribution
standard owns concrete producer field names, completeness attestations, and the
expected publication policy for `.buildinfo` and related evidence. Phase 2
additionally updates the CLI specification and usage guide for source retrieval,
unpacking, retention, provenance evidence collection, and integration with
external build-verification workflows. Examples use public placeholder
repositories and never embed private endpoints or credentials.

## Alternatives and Consequences

- **Only enable guest `deb-src`:** insufficient; it preserves neither exact
  source links nor build-time evidence and requires a running, networked image.
- **Only publish an upstream Git URL/tag:** insufficient for vendor packaging,
  applied patches, multiple upstream archives, and exact source-package versions.
- **Download every source during every image build:** improves retention but adds
  storage, time, network, and availability costs. Keep this explicit in Phase 2.
- **Generate a candidate source URL from naming conventions:** unsafe; source
  identities, repository layouts, and archive lists must come from evidence.

The decision adds metadata requests, cache entries, and a versioned provenance
contract for opted-in repositories. In return, developers get actionable source
locations early, followed by durable verified bundles and stronger evidence
without overstating what ICT knows.
