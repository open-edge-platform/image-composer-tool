# Analysis: `extends` Template Inheritance — Merge-Layer Triage

**Status**: Informational (analysis, not a decision)
**Date**: 2026-07-30
**Technical Area**: Template Configuration / Merge System
**Companion**: [ADR: Extending Templates](./adr-template-extends.md)

---

## Summary

A review of the shipped `extends` feature (release 2026.1) against its implementation in
`internal/config/merge.go`. The chain resolver — path containment, cycle detection,
symlink rejection, fold ordering — is sound and well tested. The defects are in the
**merge layer that `extends` folds over**, and they predate the feature: `extends` makes
them reachable more often because a chain applies `MergeConfigurations` once per level.

Six defects are recorded below (D1–D6). Each was reproduced by executing the merge
functions directly or by running the built binary; the reproductions are quoted inline.
D1 and D3 can silently discard user configuration and are the ones worth fixing first.

This document is informational. It proposes no schema or behavioural change on its own;
it exists so the defects and their evidence are recorded in one place.

---

## 1. How the feature works

A user template may name one parent. The parent is resolved relative to the child's own
directory, and the chain is folded underneath the OS defaults:

```
OS defaults → root → intermediate levels → leaf
```

`resolveExtendsChainFromLeaf` (`internal/config/merge.go:712`) walks leaf → root, then
reverses (`:797-802`), so `foldChain` (`:954`) applies parents before children and the
leaf wins. Five production entry points reach it:

```
cmd/image-composer-tool/build.go:246      LoadAndMergeTemplate
cmd/image-composer-tool/validate.go:48    LoadAndMergeTemplate           (--merged)
cmd/image-composer-tool/validate.go:85    ResolveAndMergeExtendsChain    (default path)
cmd/image-composer-tool/resolve.go:57     LoadAndMergeTemplate           (--full)
cmd/image-composer-tool/resolve.go:75     ResolveAndMergeExtendsChain    (default path)
internal/image/isomaker/isomaker.go:164   LoadAndMergeTemplate  ← nested initrd template
internal/api/handlers_read.go:128         LoadAndMergeTemplate  ← HTTP compose preview
```

Note that `resolveExtendsChain` (`:673`) and `resolveExtendsChainWithPaths` (`:704`) are
called only from `merge_test.go`; production always uses `ResolveAndMergeExtendsChain` or
`resolveExtendsChainFromLeaf` directly.

### What the resolver gets right

- **Dual containment.** A lexical guard (`:827-833`) plus a symlink-resolved guard
  (`verifyResolvedContainment`, `:842-862`) both reject a parent outside the child's
  directory, so neither `../` traversal nor a `child/link → /outside` directory symlink
  escapes.
- **Canonical cycle keys.** `visited` is keyed on `canonicalPath` (`:868`), so a directory
  symlink cannot alias two textual paths to one file and evade detection. The stored index
  lets the error name the exact cycle: `circular extends detected: a.yml -> b.yml -> a.yml`.
- **Symlink and extension policy.** Every template in the chain is read through
  `security.SafeReadFile(..., RejectSymlinks)` (`config.go:359`); parents get an explicit
  `security.CheckSymlink` (`:772`); only `.yml`/`.yaml` are accepted (`config.go:366`).
- **Redaction in `resolve`.** Passwords, `hash_algo`, the three secure-boot key paths, and
  the resolved FDE passphrase all become `[REDACTED]` (`:210-250`).

---

## 2. Defect register

### D1 — `codename` is treated as a unique key, but it is not · severity: high

`mergePackageRepositories` (`internal/config/merge.go:567-595`) identifies repositories by
`codename`, overwriting the first match:

```go
for _, userRepo := range userRepos {
	found := false
	for i, defRepo := range merged {
		if defRepo.Codename == userRepo.Codename {
			merged[i] = userRepo
			found = true
			break
		}
	}
	if !found {
		merged = append(merged, userRepo)
	}
}
```

`codename` is a Debian *suite* name, not a repository identifier — it becomes the suite
field of the generated deb line (`internal/config/apt_sources.go:140`), so it cannot be
renamed to disambiguate. Third-party repositories legitimately collide on it, and
`$defs.PackageRepository` in `os-image-template.schema.json` declares **no `required`
fields and no uniqueness constraint**.

`image-templates/ubuntu24-x86_64-robotics-jazzy-iso.yml` declares seven repositories, of
which **three share codename `noble`**: ROS 2 Jazzy (`:74`), Gazebo Harmonic (`:111`), and
Intel RealSense (`:117`).

Reproduced by calling `mergePackageRepositories` with a parent holding one unrelated repo
and those three as the child:

```
result count=2
  codename="foo"   url="https://foo"
  codename="noble" url="https://librealsense.realsenseai.com/Debian/apt-repo"
→ 3 noble repos collapsed to 1
```

ROS appends (no match) → Gazebo matches ROS and overwrites it → RealSense matches Gazebo
and overwrites that. `ros-jazzy-desktop` and `gz-harmonic` become unresolvable.

**Why it is latent today.** Both ubuntu24 raw and ISO default configs declare no
`packageRepositories`, so the `len(defaultRepos) == 0` early return at `:571` hands the
child's list back untouched. Any chain or default config that introduces a repository
exposes it. The failure is loud (dependency resolution) but arrives only after repository
metadata has been fetched and parsed.

**Suggested direction.** Key on `url`/`path`, or on the `(codename, url)` pair; or make a
child's repository list replace rather than merge. A load-time duplicate check would at
least surface the collision instead of silently resolving it.

---

### D2 — `targetsMatch` includes `imageType`, so `-raw`/`-iso` siblings cannot share a parent · severity: high (by design)

```go
// merge.go:878
func targetsMatch(a, b TargetInfo) bool {
	return a.OS == b.OS && a.Dist == b.Dist && a.Arch == b.Arch && a.ImageType == b.ImageType
}
```

Confirmed against the built binary with an `iso` child extending a `raw` parent:

```
$ image-composer-tool resolve child.yml
Error: resolving template: extends target mismatch at level 1:
  child targets ubuntu/ubuntu24/x86_64/iso but parent targets ubuntu/ubuntu24/x86_64/raw
```

This is defensible — a chain must resolve to one OS default config, and
`LoadDefaultConfig` keys off `imageType` (`:35-47`) — but it means the shape users reach
for first (one payload, several output formats) is the one shape the feature forbids.
`disk` is already wholesale-replaced (`:130`), so a raw child over an iso parent would
otherwise compose cleanly.

The largest duplication in `image-templates/` is exactly this shape:
`ubuntu24-x86_64-robotics-jazzy-{raw,iso}.yml` are 254/229 lines with only 57 differing,
and they have **already drifted** — the RealSense apt pin is `librealsense2*` in the ISO
(`:215`) but bare `librealsense2` in the RAW (`:240`).

**Suggested direction.** Either allow an `imageType` delta (the leaf's `imageType` already
drives the default lookup at `:926-929`) or document the restriction prominently in the
extends chapter, naming this pair as the known non-use-case.

---

### D3 — incomplete emptiness predicates silently discard child configuration · severity: high

`MergeConfigurations` short-circuits to the parent's whole `SystemConfig` when the child's
looks empty:

```go
// merge.go:136
if !isEmptySystemConfig(userTemplate.SystemConfig) {
	mergedTemplate.SystemConfig = mergeSystemConfig(defaultTemplate.SystemConfig, userTemplate.SystemConfig)
} else {
	mergedTemplate.SystemConfig = defaultTemplate.SystemConfig
}
```

**(a) `network:`-only or `fde:`-only `systemConfig` is dropped.** `isEmptySystemConfig`
(`:611-623`) never consults `Network` or `FDE`, although `SystemConfig` declares both
(`config.go:305`, `:308`) and `isEmptyNetworkConfig` exists at `:629` and is used inside
`mergeSystemConfig`. Reproduced:

```
isEmptySystemConfig({Network: {Backend: "netplan"}})      == true   → child network dropped
isEmptySystemConfig({FDE: {Enabled: true, ...}})          == true   → child fde dropped
```

A `network:`-only delta template is the archetypal `extends` use case, so this is a silent
no-op precisely where the feature is most useful.

**(b) `requireEmpty`-only `disk` is dropped.** `isEmptyDiskConfig` (`:599-609`) checks
`SelectionPolicy.Strategy` and `.ExcludeRemovable` but not `.RequireEmpty`
(`config.go:45`, schema-backed). Reproduced: `isEmptyDiskConfig` returns `true`, so the
whole `disk` block is discarded. `requireEmpty` gates whether unattended install may
overwrite a **non-empty disk**, which makes this security-relevant.

**(c) `kernel.uki` defeats the short-circuit but is never applied.**
`isEmptyKernelConfig` (`:637`) counts `UKI`, so `kernel: {uki: true}` is judged non-empty
and flows into `mergeKernelConfig` — which never copies it (`// Note: name and uki fields
come from defaults`, `:562`). Reproduced:
`isEmptyKernelConfig(uki:true)=false, merged.UKI=false`.

**Suggested direction.** Add `Network` and `FDE` to `isEmptySystemConfig`, add
`SelectionPolicy.RequireEmpty` to `isEmptyDiskConfig`, and make `mergeKernelConfig` and
`isEmptyKernelConfig` agree on `UKI`.

---

### D4 — `metadata` and `sbomPackageMetadata` cannot be inherited · severity: medium

`metadata` is declared in **both** `$defs.UserTemplate` and `$defs.FullTemplate` and is
used by the shipped extends example (`ubuntu24-x86_64-extends-example-raw.yml:2`) and
heavily by the robotics templates (30 lines: 11 use-cases, 17 keywords, the input to the
RAG index in `internal/ai/rag`). But `ImageTemplate` has **no `Metadata` field** — only
`SBOMPackageMetadata` (`config.go:202`) — so `yaml.Unmarshal` discards it.

Reproduced by round-tripping a template with a populated `metadata:` through
`parseYAMLTemplate` → `MarshalTemplateYAML`: the block is absent from the output.
Consequence: splitting a template into parent and child silently loses discovery metadata
from whichever file does not carry it, and `resolve` never shows it.

`sbomPackageMetadata` has the same symptom for a different reason — the field exists but
appears **zero times** in `merge.go`, so it survives only from the parent copy at `:95`.

---

### D5 — relative `additionalFiles` paths resolve against the wrong directory · severity: medium

`GetAdditionalFileInfo` (`config.go:747-785`) resolves a relative `local:` by iterating
`t.PathList` and taking the **first** `os.Stat` hit. `PathList` is a union accumulated at
`:98-102`, so with a chain it holds the OS-defaults directory plus every chain directory.
A leaf's relative path can therefore resolve to a same-named file beside the root template
or beside the default config.

Compare `resolveFDEPassphrase` (`config.go:898-910`), which anchors to the directory of
the file that declared the value:

```go
if !filepath.IsAbs(passphraseFile) {
	passphraseFile = filepath.Join(filepath.Dir(templatePath), passphraseFile)
}
```

Two relative-path fields, two different resolution rules.

---

### D6 — correctness and diagnostic warts · severity: low

- **Input mutation.** The nil-default branch (`:88-92`) writes `userTemplate.Extends = ""`
  through the caller's pointer and returns that same pointer, while every other path
  copies at `:95`. Reproduced: `in.Extends == "" && out == in`. Both `resolve.go:65` and
  `validate.go:75` hold that pointer.
- **Misleading target-mismatch message.** `leafTarget` is pinned at the first iteration
  (`:741`) and never advances, so "child targets X" always names the *leaf* even when the
  mismatch is between two intermediate levels, and `level` counts hops from the leaf while
  [the ADR](./adr-template-extends.md) describes it as a position in the chain.
- **Non-reproducible output.** `mergeUsers` (`:417`) and `mergeAdditionalFiles` (`:375`)
  emit through Go map range, so `resolve` output is not byte-stable and `additionalFiles`
  install order varies between runs. Documented as a caveat in the template reference; no
  test detects it.
- **Unbounded work.** Depth is warn-only (`:792-795`, deliberate per the ADR) and
  `ValidateAgainstSchema` constructs a fresh compiler per call
  (`internal/config/validate/validate.go:59`), so a deep chain repeats full schema
  compilation per level. Reachable from `internal/api/handlers_read.go:128`.
- **`configurations` accumulate without dedup** at every level (`:382`) — intentional, but
  a footgun for non-idempotent commands. The robotics templates' `>> /etc/bash.bashrc`
  entries are not idempotent.

---

## 3. A false alarm, recorded so it is not re-derived

It is tempting to conclude that a **mid-chain** `baseline.mode: overlay` lets the
create-mode default package set leak back in: the truncation at `:154` tests
`userTemplate.IsOverlayMode()`, and `userTemplate` is the current layer, so at a later
layer whose `Baseline` is nil the test is false and `mergePackages` runs normally.

It does not leak. The truncation fires at the overlay-declaring layer and every later
layer unions onto the **already-truncated accumulator** — the defaults were removed and
nothing restores them. `Baseline` also propagates forward via `:118`, so `IsOverlayMode()`
stays true on the merged result and the `build.go:294` capability gate still sees it.

---

## 4. Test-coverage gaps

Seventeen extends tests in `internal/config/merge_test.go`, nine `resolve` tests, three
`validate` tests. Chain mechanics are well covered: multi-level chains, root-first
ordering, `extends` stripping, cycle detection (including via directory symlink), target
mismatch, missing parent, `../` traversal, directory-symlink escape, and the depth warning
with its no-hard-cap assertion.

The gaps are where D1 and D3 live:

| Gap | Consequence |
|---|---|
| **Per-field chain merge** for `disk`, `users`, `additionalFiles`, `configurations`, `immutability`, `kernel`, `network`, `baseline`, `overlayPolicy`, `packageRepositories` | Every extends test varies only `image.name` and `systemConfig.packages`, so D1 and D3 are structurally invisible to CI. Highest-value gap to close. |
| **Symlinked-parent rejection** (`:772`) | Zero tests. The two symlink tests exercise *directory* symlinks, which hit `verifyResolvedContainment` instead. Documented behaviour, untested. |
| **Absolute `extends:` refs** (`:816`) | Untested branch. |
| **Subdirectory parents** (`extends: "sub/parent.yml"`) | The allowed-but-untested happy path of the containment rule. |
| **Cycles longer than A→B→A** | `displayPaths[cycleStart:]` is only exercised with `cycleStart == 0`; mid-chain slicing unverified. |
| **`extends: ""` / whitespace-only** | `TrimSpace` handling untested. |
| **The shipped example template** | No test resolves `ubuntu24-x86_64-extends-example-raw.yml`. |
| **Fuzz corpus / API paths** | No `extends` in the fuzz corpora; `handlers_read.go:128` untested with extends templates. |

---

## 5. Documentation reconciliation

| Item | Detail |
|---|---|
| ADR status stale | [adr-template-extends.md](./adr-template-extends.md) is still `Status: Proposed` though the feature shipped in 2026.1. The overlay ADRs were flipped to `Accepted`. |
| ADR behind implementation | Its validation list omits three enforced rules: the `.yml`/`.yaml` restriction, canonical-path cycle keying, and the *dual* containment guards. Its merge table omits `baseline`/`overlayPolicy` pointer replacement and the overlay-mode package override. |
| `extends` missing from the field reference | The template reference's "Top-Level Structure" section omits `extends` and says "up to five top-level sections" while listing seven. There is no `### extends` entry in Field Reference; the field appears only in its own later chapter. |
| `validate` chain resolution undocumented | `validate.go:84-97` resolves and validates the whole chain and logs `Resolved extends chain: …`, but the CLI specification's Validate section does not mention it — although the ADR required it. |
| Build-process doc unaware of chains | `image-composer-tool-build-process.md` §1 still describes only the two-layer user↔default merge. |
| Repo instructions contradict the feature | `.github/instructions/image-templates.instructions.md` lists allowed top-level keys as `metadata, image, target, disk, systemConfig, packageRepositories`, excluding `extends`, `baseline`, and `overlayPolicy`. |
| `AGENTS.md` omits `extends` | Despite instructing itself to stay in sync with `.github/copilot-instructions.md`, which covers it. |
| ADRs unpublished | `docs/user-guide/index.md`'s toctree covers Get Started / Configuration / Architecture only; `docs/architecture-decision-record/` has no index entry. |
| No package removal | Packages are union-only at every level; a child cannot remove a parent's package. Stated in the repo instructions but not in the extends chapter, where it matters most. |

Terminology note: "baseline" and "overlay" in the template reference refer to the
*disk-image overlay* feature, not to template inheritance. The only real coupling is the
overlay-mode package override at `:154`.

---

## 6. Suggested order of work

1. **Fix the repository identity key (D1).** The only defect that can silently destroy a
   working template's repository set, and a shipped template already triggers it.
2. **Complete the emptiness predicates (D3).** Three one-line changes that together close
   every known silent drop.
3. **Add per-field chain tests.** Until a chain test varies `disk`, `users`,
   `additionalFiles`, `network`, `kernel`, and `packageRepositories`, this class of defect
   cannot be caught by CI — and it is what would have caught D1 and D3.
4. **Decide D2 explicitly** — allow an `imageType` delta, or document the restriction.
5. **Make `metadata` a real field (D4)**, and merge `sbomPackageMetadata`.
6. **Anchor relative paths to the declaring template (D5)**, matching
   `resolveFDEPassphrase`.
7. **Reconcile the documentation** per §5.
8. **Optional hardening.** Deterministic ordering for `users`/`additionalFiles`, a hard
   depth ceiling for the API path, a cached schema compiler.
