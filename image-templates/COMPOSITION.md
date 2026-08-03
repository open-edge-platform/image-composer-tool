# Composing templates

You have a template that builds the image you want, and now you need a variant:
the same thing plus a driver, plus a package set, plus one config command.
Copying 250 lines of YAML works until the day the original changes and the copy
does not.

Two features avoid the copy:

- **`extends:`** — inherit another template and declare only the difference.
  Everything is resolved at build time from source templates.
- **overlay mode** (`baseline:` + `overlayPolicy:`) — install packages into an
  image that has already been built, without rebuilding it.

They answer different questions. `extends:` is for *authoring* a family of
related images. Overlay is for *adding to a finished artifact* you already have.

---

## `extends:`

```yaml
extends: "ubuntu24-x86_64-minimal-raw.yml"

image:
  name: my-image
  version: "24.04"

target:
  os: ubuntu          # must match the parent exactly
  dist: ubuntu24
  arch: x86_64
  imageType: raw

systemConfig:
  name: my-config
  packages:
    - htop            # added on top of the parent's packages
```

`ubuntu24/ubuntu24-x86_64-extends-example-raw.yml` is that example, ready to run.

### Two rules constrain where a parent can live

**The parent must be in the child's own directory** (or below it). The path is
resolved relative to the child, and anything that climbs out is rejected:

```
extends path escapes child template's directory: /…/ubuntu24-x86_64-minimal-raw.yml
```

So `extends: "../ubuntu24/minimal.yml"` does not work, and neither does a shared
`_base/` directory one level up. **Every template in a chain is a sibling.**
This is enforced twice, once lexically and once after resolving symlinks, in
`resolveExtendsParentPath` and `verifyResolvedContainment`
(`internal/config/merge.go`).

**Every level must share the same target** — `os`, `dist`, `arch` *and*
`imageType`:

```
extends target mismatch at level 1: child targets ubuntu/ubuntu24/x86_64/iso but parent targets ubuntu/ubuntu24/x86_64/raw
```

A practical consequence: a `-raw` and a `-iso` template **cannot share a
parent**, however similar they are. `ubuntu24-x86_64-robotics-jazzy-raw.yml` and
`…-iso.yml` have the same 37 packages, the same 7 repositories and the same 12
commands, and there is currently no way to factor that out.

Together these two rules are why this directory is grouped by distribution: any
grouping drawn from `{os, dist, arch, imageType}` can never split a chain.

### How layers combine

Chains are single-inheritance and linear: `root → … → leaf`, no diamonds. The OS
defaults sit underneath everything:

```
OS defaults → root template → … → leaf template
```

| Section | Behaviour |
|---|---|
| `systemConfig.packages` | **union**, de-duplicated, child appended after parent |
| `systemConfig.configurations` | **concatenated**, root first, leaf last, no de-duplication |
| `packageRepositories` | merged **by `codename`** — a match *overrides*, a new codename appends |
| `disk` | **replaced as a whole block** when the child provides one |
| `systemConfig.users` | merged by `name` |
| `systemConfig.additionalFiles` | merged by `final` destination path |
| `systemConfig.kernel`, `.bootloader`, `.network` | per-field override |
| `systemConfig.immutability`, `.fde` | merged only if the child provides the section |
| `image.name`, `image.version`, scalars | non-empty child value wins |
| `target` | replaced wholesale |
| `metadata` | **not inherited at all** — see below |
| `extends` | stripped from the merged output |

Chains deeper than 4 levels log a warning but still build.

### Four things that will surprise you

**1. You cannot remove an inherited package.** Package lists are a union and
there is no removal syntax. If the parent installs it, so does the child.

This decides which parent you pick: inheriting a heavier base drags in packages
the child never uses and cannot drop — a template that boots with systemd-boot,
for instance, still inherits any GRUB packages its parent installs. Choosing the
leanest workable parent is how you keep that cost down.

**2. Repositories with the same codename collapse.** `packageRepositories` merge
by `codename`, and a match overrides the first existing entry rather than
appending. Duplicate codenames survive only inside a single layer, where the
merge returns the child list untouched.

This is easy to trigger and silent when it happens. A parent declaring one
`noble` repository plus a child declaring two yields **one**:

```yaml
# parent.yml
packageRepositories:
  - codename: "noble"
    url: "https://parent.example/ros2"
# child.yml  ->  resolves to ONE repository: child.example/realsense
packageRepositories:
  - codename: "noble"
    url: "https://child.example/gazebo"
  - codename: "noble"
    url: "https://child.example/realsense"
```

The robotics templates declare three repositories all codenamed `noble` (ROS 2,
Gazebo, RealSense). Split them across layers and two package sources disappear
with no error. **Keep a repository set in one layer**, and check the result with
`resolve --full` whenever you move one.

**3. `metadata` does not inherit.** It is valid in the schema, but there is no
corresponding field on `ImageTemplate`, so it is discarded at parse time. It
feeds template discovery and search, read per file. Every template needs its own
`metadata` block, including children.

**4. `disk` is all-or-nothing.** Providing any `disk` in a child replaces the
parent's block entirely rather than merging field by field. To change only the
size, restate the whole layout. Note `disk.name` is only ever logged — it does
not affect output paths or partitioning, which key off `systemConfig.name`.

Also worth knowing: `configurations` are concatenated in chain order, so a
parent's commands always run first. And `users` and `additionalFiles` are
de-duplicated through a map, so their **order in the merged output is not
stable** between runs; do not depend on it.

### Splitting a template safely

The candidates that work cleanly are the ones where the child's package set is a
**superset** of the parent's — then inheriting adds nothing unwanted. In this
directory that was true of the EMT3 and Debian families:

```
emt3-x86_64-edge-raw.yml            41 packages
  -> emt3-x86_64-emf-raw.yml        +2
  |    -> emt3-x86_64-emf-rt-raw.yml  + real-time GPU driver
  -> emt3-x86_64-dlstreamer.yml     +8, +3 repositories
```

Watch out for **order-coupled packages**. The robotics templates list
`intel-oneapi-runtime-*` before `ros-jazzy-desktop` so the resolver pins those
versions first. Because child packages are appended after the parent's, a split
that puts them in different layers can reorder them. Keep order-coupled entries
in the same layer, and mark them — the robotics template uses an
`ORDER-COUPLED:` comment.

Before and after any split, compare the resolved output:

```bash
./image-composer-tool resolve <template> --full > /tmp/before.yml
# ...split the template...
./image-composer-tool resolve <template> --full > /tmp/after.yml
diff /tmp/before.yml /tmp/after.yml
```

Expect differences only in `metadata`, `image.name` and `disk.name`. Anything
else means the image changed.

---

## Overlay mode

Overlay installs packages into an image that already exists. The baseline is
copied into the workspace first and is never modified in place.

```yaml
baseline:
  mode: overlay
  source:
    path: /path/to/ubuntu-24.04-base.img   # or url: https://…
    format: raw                            # raw | qcow2 | vhd | vhdx

overlayPolicy:
  packageOperation: additive-only
  conflictPolicy: fail

systemConfig:
  name: overlay
  packages:
    - tree
    - jq
```

`ubuntu24/ubuntu24-x86_64-overlay-raw.yml` is that example. Its `source.path` is
a deliberate placeholder, so point it at a real baseline before building.

Rules the loader enforces:

- `mode: overlay` **requires** `baseline.source`, with exactly one of `path` or
  `url`. A `url` must be `https`.
- `overlayPolicy` is only allowed when `mode` is `overlay`. Because `mode`
  defaults to `create`, a template that sets `overlayPolicy` without a
  `baseline` is rejected.
- Overlay is additive in v1: packages are added, never removed or downgraded,
  and a conflict fails the preflight gate.
- The bootloader and kernel are left alone.

**Overlay changes the package rule.** Normally packages are a union; in overlay
mode the merged list is *exactly* what the template declares. The baseline
already ships the base toolchain, and unioning it back in would drag in
bootloader packages whose pinned versions the frozen baseline cannot satisfy.

### Combining overlay with `extends`

A child can inherit `baseline` and `overlayPolicy` from its parent. If it does,
it must declare **neither** — `overlayPolicy` on its own fails validation before
the merge happens, because the leaf is validated as a standalone file first.

---

## Debugging

```bash
# the chain only, without OS defaults — good for checking inheritance
./image-composer-tool resolve image-templates/emt3/emt3-x86_64-emf-rt-raw.yml

# everything, exactly as the build will see it
./image-composer-tool resolve image-templates/emt3/emt3-x86_64-emf-rt-raw.yml --full
```

`resolve` prints the chain it walked:

```
Extends chain: emt3-x86_64-edge-raw.yml -> emt3-x86_64-emf-raw.yml -> emt3-x86_64-emf-rt-raw.yml
```

Passwords, hash algorithms and secure-boot key paths are redacted, so the output
is safe to paste into an issue.

One caveat: `resolve` prints `additionalFiles.local` values as written, not as
resolved. A path that no longer resolves is dropped at build time with a
`Ignoring additional file entry with non-existent local path` warning, which
`resolve` will not show you. Relative `local:` paths are resolved against the
directory of each template in the chain, so a path that worked before a move may
need a `../` after it.

## Reference

- [Template reference](../docs/user-guide/architecture/image-composer-tool-templates.md#template-extends-inheritance)
- [ADR: template `extends`](../docs/architecture-decision-record/adr-template-extends.md)
- [ADR: image extension / overlay](../docs/architecture-decision-record/adr-image-extension.md)
- Implementation: `internal/config/merge.go`
