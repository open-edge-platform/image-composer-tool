# ADR: Advanced Mode Builds via a Generated `extends` Delta

**Status**: Proposed (for team discussion)
**Date**: 2026-08-20
**Authors**: ICT Team
**Technical Area**: Web UI / Template Configuration / Merge System

---

## Summary

The web UI's Advanced mode lets a user adjust attributes of a curated template
before building. Rather than rewriting the curated template or inventing a
parallel override mechanism, the server generates a small **delta template that
`extends` the curated one** and builds that. The Review step displays
`resolve --full` of the same delta.

Advanced mode is therefore a front-end for the template inheritance ICT already
ships (see [`adr-template-extends.md`](adr-template-extends.md)), not a new
feature in the merge system.

---

## Context

### Problem Statement

Advanced mode's purpose is to let a user modify template attributes before
building: image name today, packages and disk settings later. Three properties
are required:

1. **The Review step must show what will actually be built.** It is the last
   screen before a build starts.
2. **The curated template must not be modified.** It is source-controlled and
   shared by every user of that combination.
3. **Adding the next attribute must not require new plumbing.** Advanced mode is
   explicitly a first step; a design that needs a new CLI flag, wire field, and
   validation path per attribute does not scale.

### Current System

Basic mode posts a selection (`{compose: …}`), the server resolves it to a
curated template through the manifest, and the CLI builds that file in place.
Advanced mode currently composes and displays only; it has no build path.

The merge system already supports exactly the operation Advanced mode needs.
`extends` lets a child template restate a few fields and inherit everything
else, with per-attribute merge strategies (override, additive, merge-by-key)
that are already specified and tested.

### Key Constraint: where a generated delta may live

`resolveExtendsParentPath` (`internal/config/merge.go`) requires the parent to
resolve at or below the **child's** directory and rejects `..`. Curated
templates live in `TemplatesDir` (default `image-templates/`), so a generated
delta can only live in that directory or an ancestor of it, **not** in the
per-build work directory. This is a deliberate containment guard that also
protects CLI users, so this ADR works within it rather than relaxing it.

---

## Decision

### 1. The server generates the delta; Advanced mode builds it

For a build with no modifications, behavior is unchanged from Basic mode: build
the curated template directly. When the user has modified something, the server
renders a delta:

```yaml
extends: ubuntu24-x86_64-robotics-jazzy-raw.yml
image:
  name: my-custom-name       # the modification
  version: "24.04"           # copied from the parent (schema-required)
target:                      # copied from the parent verbatim (must match)
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: raw
```

- Written into `TemplatesDir` as `.ict-adv-<uuid>.yml`, gitignored, removed when
  the build finishes.
- The parent is referenced as a **bare sibling filename**. This satisfies the
  containment guard and keeps `PathList` rooted at `image-templates/`, so the
  parent's relative `additionalFiles` continue to resolve.
- Rendered from a purpose-built struct with `omitempty` throughout, *not* from
  `config.ImageTemplate`, whose `Image`/`Target`/`SystemConfig`/`Disk` fields
  largely lack `omitempty` and would emit empty `packages: []` / `kernel: {}`
  blocks into a schema with `additionalProperties: false`.
- Produced by marshaling a struct, **never by string templating**, so a
  user-supplied value cannot inject YAML.
- Generation is **deterministic**: the same selection and modifications produce
  the same bytes, so the file the Review step resolved and the file the build
  runs are identical without sharing server state.
- The delta is validated through the existing
  `validate.ValidateUserTemplateIssues` path before use, so a generator bug
  surfaces as a clear error rather than a confusing merge failure.

### 2. Review shows `resolve --full` of the same delta

`ComposeResult.YAML` changes meaning from "curated file, verbatim" to "resolved
final template", produced with the functions `resolve --full` already uses
(`config.LoadAndMergeTemplate` → `config.RedactSensitiveData` →
`config.MarshalTemplateYAML`).

Because Review and the build run the same file through the same merge engine,
they cannot drift apart. This is the central property of the design.

### 3. The wire stays `{compose: …}`, so modifications ride along with the selection

The `BuildRequest.yaml` mode (post a complete self-contained template) is *not*
used. `StartBuild` computes a build's `Summary` only when `compose` is present,
and a nil summary blanks the Build Details configuration table, labels every
history row `template.yml`, and breaks Retry, which reconstructs its request
purely from the summary.

Keeping `{compose: …}` and adding modification fields to it means the server
owns delta generation, and `LoadAndMergeTemplate` on the delta yields a summary
that already reflects the modifications. No special-casing anywhere downstream.

### 4. Image Type is not adjustable in Advanced mode

`targetsMatch` compares `imageType` across an extends chain and rejects a
mismatch, so a delta **cannot** change it. That is not an incidental limitation
of this design; it is the same underlying reason a RAW→ISO override cannot
work at all:

- `target.imageType` selects which per-type default config layer is merged in.
  The layers differ in `disk`, `bootloader`, `initramfs`, `immutability`,
  `users`, `packages`, and `kernel`.
- `Target` and `Disk` are wholesale replaces with no unset sentinel, so flipping
  the type on a raw template folds its raw disk and bootloader onto an ISO base.
- Nothing catches this: only `wsl2` has a type-specific schema branch, so
  raw→wsl2 fails loudly while raw→iso passes silently and produces a broken
  image.

Supporting multiple image types per combination requires **separate curated
templates per type**, plus manifest entries and cascading availability in the
UI. That is separate work. Until then the control is displayed but disabled.

---

## Consequences

### Positive

- Review and build cannot diverge; they are the same file and the same merge.
- Each additional editable attribute is a new field in the delta. Merge
  semantics already exist for all of them; no new CLI flag, wire field, or
  validation path.
- No new override mechanism to document, test, or keep in sync with `extends`.
- `buildCommand`'s invariant holds: no user-derived arguments reach the command
  line, since modifications travel inside a server-written file with a
  server-generated name.
- The per-build template download improves. Today, compose-mode re-reads the
  live curated file at download time, so it is not a true snapshot; this design
  archives the resolved template per build.

### Negative / accepted trade-offs

- **Generated files land in a source-controlled directory.** The containment
  guard leaves no alternative short of relaxing it. Mitigated by a dotted
  server-generated name, a `.gitignore` entry, and removal when the build
  finishes (including failed and cancelled builds). Breaks if `TemplatesDir` is
  ever mounted read-only.
- **Review output loses the curated file's comments.** YAML comments are not
  part of the data model, so a struct round-trip discards them. Users see the
  true final state, including every merged default the curated file never shows,
  but not the authors' rationale. A "Resolved / Source" toggle is a cheap
  follow-up if this proves annoying.
- **Review output is verbose.** Many config structs lack `omitempty`, so empty
  collections and zero-valued blocks are emitted. Adding `omitempty` is a
  schema-visible change and is out of scope.
- **A delta is not a pure diff.** The schema requires `image.name`,
  `image.version`, and all four `target` fields on every template, so the delta
  restates them.
- **Packages can be added but never removed.** `systemConfig.packages` merges as
  an append + dedup union. This constrains the future Packages step and is a
  property of `extends`, not of this design.
- **Retry does not replay modifications.** It reconstructs its request from the
  summary, which carries the *effective* image name with no flag distinguishing
  overridden from default. Making Retry modification-faithful is a follow-up.

---

## Alternatives Considered

**Rewrite the curated template server-side.** Requires materializing the
rewritten file somewhere, which breaks relative-path resolution:
`additionalFiles.local` resolves via `PathList`, and at least one shipped
template uses a path resolvable only from `image-templates/`. Also forces either
a yaml round-trip (destroying comments in the file we build from, not just in
the preview) or hand-rolled text splicing.

**A shared `config.Overrides.Apply()` struct transform.** A new type in
`internal/config`, called by both the compose endpoint and a new `--image-name`
CLI flag. Writes no files, but the transform then exists at two call sites that
must stay in sync, and every new attribute costs a new flag plus a new wire
field. The existing `applyOverlayFlagOverrides` could not be reused: it is in
`package main` under `cmd/` and takes a `*cobra.Command`, so the API service
cannot import it.

**Post a complete template via `BuildRequest.yaml`.** The self-contained path
already exists in the contract, but it produces a nil `Summary`; see Decision 3.
Three UI regressions to carry one modified field.

**Relax the extends containment guard** with a server-configured allowed root,
letting the delta live in the per-build work directory. Keeps generated files out
of the source tree, but modifies security-sensitive resolution logic that also
protects CLI users. Rejected as disproportionate to the benefit; worth revisiting
if read-only `TemplatesDir` deployments become real.

---

## References

- [`adr-template-extends.md`](adr-template-extends.md): the inheritance
  mechanism this design builds on
- [`adr-image-extension.md`](adr-image-extension.md)
- [`adr-web-ui-tech-stack.md`](adr-web-ui-tech-stack.md)
- `docs/user-guide/architecture/image-composer-tool-templates.md`: merge
  strategies and the `extends` section
- `internal/config/merge.go`: `resolveExtendsParentPath`, `targetsMatch`,
  `MergeConfigurations`, `foldChain`
- `internal/api/service/compose.go`, `internal/api/service/builds.go`

---

## Revision History

| Date | Change |
|---|---|
| 2026-08-20 | Initial proposal |
