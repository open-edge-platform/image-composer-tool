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
- Enabling discovery does not modify the image's APT sources or enable `deb-src`
  inside the image. Guest source configuration remains a separate concern.
- Initially, this option is valid only for supported Debian-family repositories;
  other repository types must produce an explicit validation error if enabled.

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
   policy. Preserve the original metadata and its verification results.
4. **Match exact source identities.** Use the binary's declared source name and
   version and constrain matches to its selected repository or explicit source
   override. Never substitute another version, upstream tag, or repository's
   same-version publication silently. For inherited packages, a candidate match
   is not proof of installation origin; verified binary-hash evidence or retained
   build provenance is needed to strengthen that association.
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

### Manifest and SPDX Contract

The provenance manifest is the canonical structured record. Each binary record
references a source record and retains evidence of that association. Each source
record includes source name/version, repository snapshot and component, metadata
digests, signing-key fingerprint, verification outcome, artifact locations and
hashes, observation time, and retrieval status. Do not include credentials or
secret-bearing URLs.

Use separate fields for mapping, metadata verification, and artifact retrieval.
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

SPDX uses separate source-package entries with the `.dsc` download location and
binary `GENERATED_FROM` source relationships representing repository-declared
mappings, not attested build execution. Describe the evidence basis in
`sourceInfo` and reference the companion manifest through standard SPDX fields.
Do not assert a generated-from relationship for uncertain candidate matches.
Represent additional source artifacts using standard SPDX constructs, with the
complete URL/hash list always available in the companion manifest.

Emit standard `checksums`, not the current singular `checksum`, and validate the
whole document against the SPDX schema. Continue accepting legacy checksum input
when reading existing SBOMs. Overlay merges must preserve source records and
relationships, remove obsolete relationships, and use IDs that distinguish
versions, architectures, and source identities.

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
- Automated fixtures cover Debian source defaults, explicit source versions,
  epochs/binNMUs, multiarch, multiple upstream archives, repository ambiguity,
  baseline/script inventory gaps, missing indexes/versions, signature/hash
  failures, cache behavior, and network failures. Include representative NVL
  domain packages and unavailable or proprietary sources without relying on
  private repositories. Normal non-opt-in builds make no source requests, and
  SPDX schema validation passes.

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
4. **Collect typed revision evidence.** Inspect archives and packaging for explicit
   Git archive headers, recorded revision files, VCS references, patch series,
   and patch-header IDs. Preserve the evidence location, digest, and meaning.
   Distinguish archive-base, upstream, packaging, patch, and attested-build
   revisions. Never infer a build commit from a homepage, version suffix, tag,
   or the current HEAD of a repository.
5. **Retain build and debugging evidence where published.** Associate available
   `.buildinfo`, `.changes`, build logs, toolchain/dependency versions, build IDs,
   matching debug symbols, and publisher attestations with verified artifacts.
   These can be published separately from `Sources`; their absence must be
   explicit and must not prevent a verified source bundle from being useful.
6. **Provide a separate, opt-in rebuild check.** Use an isolated build environment,
   record exact inputs and commands, and compare output binary digests with the
   captured DEBs. Report attempted, unavailable-input, failed, non-identical,
   or identical results per artifact. A successful compilation is not an
   identical rebuild, and an identical rebuild is not proof of historical build
   execution. Capture publisher-signed source-to-binary attestations separately.

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
- Records distinguish links resolved, bytes verified, artifacts retained,
  sources unpacked, revision evidence found, build evidence available, and rebuild
  comparison outcomes. Full coverage is claimed only for a stated inventory and
  policy whose required checks actually pass.
- Tests exercise corrupted downloads, descriptor/index disagreement, interrupted
  transfers, deduplication, offline retrieval, archive safety, missing evidence,
  patch application, and honest rebuild/attestation result reporting.

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
documentation, cache documentation, security objectives, and release notes with
Phase 1. Phase 2 additionally updates the CLI specification and usage guide for
retrieval/unpacking, retention, and optional rebuild checks. Examples use public
placeholder repositories and never embed private endpoints or credentials.

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
