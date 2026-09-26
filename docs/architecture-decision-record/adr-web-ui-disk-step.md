# ADR: Advanced Mode Disk Step — Two Editing Modes over One Partition Model

**Status**: Proposed (for team discussion)
**Date**: 2026-08-31
**Authors**: ICT Team
**Technical Area**: Web UI / Template Configuration

---

## Summary

The Advanced tab's Disk step edits `disk` through **one model with two editing
modes**: *size-based*, where a size per partition drives contiguous `start`/`end`
offsets, and *offset-based*, where the schema's offset strings are typed directly
and sizes are derived. Size-based is the default and the safe mode; offset-based
exists for layouts size-based cannot express. Switching converts in place, so
neither is lossy.

The output-format control (`disk.artifacts[]`) lives on this step, not on the
Target step's Image Type dropdown.

---

## Context

### Problem statement

`os-image-template.schema.json` gives each partition `start` and `end` offset
strings and **no size field**. Every template in `image-templates/` and
`config/osv/` writes contiguous MiB offsets by hand:

```yaml
- id: EFI    start: 1MiB     end: 1025MiB
- id: SWAP   start: 1025MiB  end: 3073MiB
- id: ROOT   start: 3073MiB  end: "0"     # 0 = rest of the disk
```

Editing those directly means the user maintains the invariant that each `start`
equals the previous `end`. Insert a partition, resize one, or reorder two, and
every offset below has to be recomputed by hand; get it wrong and partitions
silently overlap.

The design prototype (PR #822) sidesteps this by giving each partition a
`size: "512MiB" | "rest"` field. **That field does not exist** — a template
written that way fails schema validation. The prototype's disk step is also a
mock: its unit dropdown and `+ Add Partition` button have no handlers and the
partition list is read-only, so it settles the layout but not the data model.

### The QCOW2 question

The originating requirement asked for RAW, ISO and QCOW2 output. These are two
different axes:

| Field | Enum | Meaning |
|---|---|---|
| `target.imageType` | `raw`, `img`, `iso`, `wsl2` | how the image is built |
| `disk.artifacts[].type` | `raw`, `qcow2`, `vhd`, `vhdx`, `vmdk`, `vdi`, `tar` | what container the finished image is written to |

QCOW2 exists only on the second. Adding it to the Image Type dropdown would
produce templates the schema rejects.

---

## Decision

1. **One model, two editing modes.** Every partition carries both a `sizeMiB`
   and the schema's `start`/`end` strings. `layoutMode` decides which side is
   authoritative:

   | Mode | Authoritative | Derived | Contiguity |
   |---|---|---|---|
   | `size` (default) | `sizeMiB` (`null` = rest of disk) | `start`/`end` | guaranteed — never stored, so it cannot be broken |
   | `offset` | `start`/`end` (`"0"` = rest of disk) | size per partition | the user's responsibility; gaps and overlaps are reported |

   In size mode `computeOffsets()` lays the partitions out contiguously from the
   template's own first start offset. In offset mode it returns what was typed,
   untouched. Both modes show the derived side read-only on every row, so the
   translation is always visible rather than hidden.

2. **`setLayoutMode()` converts in place, so neither mode is lossy.** Leaving
   size mode writes the computed offsets down; leaving offset mode derives sizes
   (and the first start offset) from whatever the user ended up with. An
   unedited layout emits byte-identical YAML in either mode.

3. **The first start offset is carried, not hardcoded.** Every template starts
   at `1MiB`, but the value is read from the seed so an unedited layout re-emits
   byte-identical offsets.

4. **`disk.artifacts[]` is edited on the Disk step.** It is a `Disk` property,
   and it is where QCOW2/VHD/VMDK output actually comes from.

5. **Seed by parsing the resolved template client-side.** `POST /templates/compose`
   already returns the fully merged template (`resolve --full` equivalent). The
   step reads `disk` out of that YAML rather than adding a typed `disk` object to
   the compose response — no contract change, and the same parser/emitter serves
   the client-side template generation the Review step will need.

6. **Loader rules are mirrored client-side, not just schema rules.**
   `extendLastPartitionToFillDisk` is rejected by
   `internal/config/validate/validate.go` for `imageType: iso`, and for `raw`
   unless the last partition is the rootfs. Those checks are not in the JSON
   schema, so a disk block can be schema-clean and still be refused before a
   build starts. The step reports them inline.

7. **Gaps and overlaps are warnings, not errors.** A gap is legal — the schema
   and the builder both accept it, and it is occasionally deliberate, which is
   the whole reason offset mode exists. An overlap is almost always a mistake,
   but the tool will still attempt it, so blocking it in the UI would be the UI
   inventing a rule the builder does not have.

8. **Every schema field round-trips, whether or not it is prominent.** The
   partition row shows `name`/`fsLabel`/`fsType`/size/`mountPoint`/`start`/`end`;
   `id`, `index`, `type`, `typeUUID`, `mountOptions` and `flags` sit behind a
   per-row details toggle. `Disk.path` and `Disk.selectionPolicy` are read and
   re-emitted but not editable — they select a *target disk* for the live
   installer, which is not what an image-composition step configures.

9. **Field validation is transcribed from the Go implementation, not the JSON
   schema.** The schema types nearly every Disk and Partition field as an
   unconstrained `string`; the real allowlists are in the builder. They are
   collected in `web/src/lib/diskrules.ts`, each with the file and line it came
   from, and split by consequence — a value that fails the build is an error and
   a closed dropdown, a value that degrades quietly is a warning:

   | Field | Rule | Source | UI |
   |---|---|---|---|
   | `fsType` | `fat32 fat16 vfat ext2 ext3 ext4 xfs linux-swap` | `imagedisc.go:728,763` | dropdown; error |
   | `start`/`end` | `"0"`, or integer + `KiB MiB GiB K M G KB MB GB` — exact case, no decimals, no bare bytes, no TiB | `imagedisc.go:329` `VerifyFileSize` | error |
   | `disk.size`/`maxSize` | same suffix table, no `"0"` | `config.go:1405`, `validate.go:257` | error |
   | `artifacts[].type` | `raw qcow2 vhd vhdx vmdk vdi` (RAW), `tar` (WSL2) | `imageconvert.go:246`, `wsl2maker.go:106` | dropdown; error |
   | `artifacts[].compression` | `gz xz zstd` (RAW), `gz`/`gzip` required (WSL2) | `compression.go:58`, `wsl2maker.go:117` | dropdown; error |
   | partition `type` | 15 names in `partitionTypeNameToGUID` | `imagedisc.go:98` | dropdown; **warning** |
   | `typeUUID` | GPT GUID or 4-hex sgdisk code | `imagedisc.go:812`, `config.go:439` | warning |
   | `flags` | `esp grub bios_grub bios-grub boot dmroot` | `imagedisc.go:79` | warning |

   **Two of the schema's own enums are wider than the implementation**, and the
   UI offers the intersection rather than the schema's list, because a value the
   schema permits and the builder then refuses is a trap:
   `artifacts[].type` includes `tar`, which `convertImageFile` has no case for;
   `artifacts[].compression` includes `gzip` and `bz2`, neither of which
   `compression.CompressFile` implements. (Conversely `CompressFile` implements
   `tar.gz`/`tar.xz`, which the schema rejects.) **Reconciling the schema with
   the implementation is a separate change** — this ADR only records that the UI
   does not propagate the mismatch.

10. **Artifact options depend on `target.imageType`.** RAW and overlay run the
    qemu-img pipeline; WSL2 takes a different path that *requires* exactly one
    `tar` + `gz` artifact; ISO and IMG never call `ConvertImageFile` at all, so
    the step says the list is ignored rather than offering a dead control.

11. **The override reaches a build through the existing `extends` delta**, not a
    new mechanism. `ComposeRequest` gains a `disk` object; `buildDelta` emits it
    as the delta's `disk` block, alongside the `image`, `systemConfig.packages`
    and `packageRepositories` the same delta already carries
    ([`adr-web-ui-advanced-mode-extends.md`](adr-web-ui-advanced-mode-extends.md)).
    So the Review step's "Your changes" view, the resolved template, and the
    built image are one file, merged one way.

    Two consequences follow from the wholesale-replace semantics in (1):

    - **The wire type carries the complete block**, including `path` and
      `selectionPolicy`, which the step does not edit. They are read from the
      resolved template and sent back unchanged, because omitting them would
      delete them — `image-templates/ubuntu24/ubuntu24-x86_64-minimal-unattended-iso.yml`
      is a real template that sets both.
    - **An unedited layout is not sent at all.** It round-trips to the parent's
      own disk block, so sending it would generate a delta that changes nothing
      while making the Review pane report an override.

12. **The override is validated in Go, not by the OpenAPI spec.** Nothing
    validates request bodies against the spec at runtime — the handlers decode
    straight into the generated structs — so the spec's `pattern`, `enum`,
    `maxLength` and `additionalProperties: false` are documentation. The typed
    decode drops unknown keys by construction; everything else is enforced by
    `service.ValidateDisk`, which both `Compose` and `resolveBuildTemplate` call.
    The build path validates independently rather than trusting a prior compose,
    matching what `ValidateImageName`/`ValidatePackages` already do.

---

## Consequences

**Good**

- In size mode, resize/insert/remove/reorder are single actions and offsets
  cannot drift out of sync, because they are derived rather than stored.
- Offset mode covers what size mode cannot express (deliberate gaps, or matching
  an existing layout offset-for-offset) without weakening the default.
- A seeded layout round-trips exactly in either mode, so "no edits" produces the
  template's own offsets and a reviewer sees only real changes in a diff.
- The QCOW2 requirement is satisfied on the field that actually implements it.

**Trade-offs**

- **Two modes are two states to test and to explain.** Mitigated by the
  conversion being total in both directions and covered by unit tests, but it is
  strictly more surface than a single mode.
- **The "rest" partition must stay last in both modes.** Its `end: "0"` gives the
  partition after it no offset to start from. Adding a partition inserts *before*
  a trailing rest partition, and reordering across it is blocked.
- **Sizes are whole MiB.** Offsets are rounded up, never down, so a rounded
  partition can never start inside its predecessor.
- **Offset mode can produce an overlapping layout.** By design — it is warned
  about, not prevented. A user who wants the guardrail should stay in size mode.
- Duplicating the loader's auto-expand rules in TypeScript means they can drift
  from the Go implementation. They are covered by unit tests naming the Go
  source, and `POST /templates/validate` remains the authority.

---

## Alternatives considered

**Size-oriented only.** The original decision, and what this PR first shipped.
Simpler, one state, and adequate for every template in this repository — all of
which are contiguous. Superseded because it makes deliberate gaps and
offset-for-offset reproduction of an existing layout impossible to express, with
no escape hatch short of hand-editing the exported YAML.

**Offset-only — edit `start`/`end` directly and nothing else.** No translation
layer at all. Rejected as the sole mode: it puts the contiguity invariant on the
user, so every insert or resize becomes a manual recomputation of everything
below it. It survives as the non-default mode.

**Add a `size` field to the partition schema.** Would let the UI store what it
displays. Rejected: it duplicates information already carried by `start`/`end`,
needs a new precedence rule when both are set, and changes the builder for a
purely presentational problem.

**Return a typed `disk` object from `POST /templates/compose`.** Strongly typed,
no client-side YAML parsing. Rejected for now: it widens the OpenAPI contract for
data the response already carries, and the client needs a YAML emitter for the
Review step regardless.

---

## References

- `internal/config/schema/os-image-template.schema.json` — `$defs.Disk`
- `internal/config/config.go` — `DiskConfig`, `PartitionInfo`, `ArtifactInfo`
- `internal/config/validate/validate.go` — `validateAutoExpandLastPartitionConstraints`
- [`adr-web-ui-advanced-mode-extends.md`](adr-web-ui-advanced-mode-extends.md)
- [`adr-web-ui-tech-stack.md`](adr-web-ui-tech-stack.md)
