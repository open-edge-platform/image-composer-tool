# ADR: Advanced Mode Disk Step — Size-Oriented Editing over Computed Offsets

**Status**: Proposed (for team discussion)
**Date**: 2026-08-31
**Authors**: ICT Team
**Technical Area**: Web UI / Template Configuration

---

## Summary

The Advanced tab's Disk step edits `disk` in a **size-oriented** model — one size
per partition — and computes the schema's `start`/`end` offsets from it. The
output-format control (`disk.artifacts[]`) lives on this step, not on the Target
step's Image Type dropdown.

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

1. **Size-oriented UI, computed offsets.** The edit model stores `sizeMiB` per
   partition (`null` = "use the rest of the disk"). `computeOffsets()` lays the
   partitions out contiguously from the template's own first start offset, and
   emits `end: "0"` for the rest partition. The computed `start`/`end` are shown
   read-only on every row, so the translation is visible rather than hidden.

2. **The first start offset is carried, not hardcoded.** Every template starts
   at `1MiB`, but the value is read from the seed so an unedited layout re-emits
   byte-identical offsets.

3. **`disk.artifacts[]` is edited on the Disk step.** It is a `Disk` property,
   and it is where QCOW2/VHD/VMDK output actually comes from.

4. **Seed by parsing the resolved template client-side.** `POST /templates/compose`
   already returns the fully merged template (`resolve --full` equivalent). The
   step reads `disk` out of that YAML rather than adding a typed `disk` object to
   the compose response — no contract change, and the same parser/emitter serves
   the client-side template generation the Review step will need.

5. **Loader rules are mirrored client-side, not just schema rules.**
   `extendLastPartitionToFillDisk` is rejected by
   `internal/config/validate/validate.go` for `imageType: iso`, and for `raw`
   unless the last partition is the rootfs. Those checks are not in the JSON
   schema, so a disk block can be schema-clean and still be refused before a
   build starts. The step reports them inline.

---

## Consequences

**Good**

- Resize, insert, remove and reorder are single actions; offsets can never drift
  out of sync because they are derived, not stored.
- A seeded layout round-trips exactly, so "no edits" produces the template's own
  offsets and a reviewer sees only real changes in a diff.
- The QCOW2 requirement is satisfied on the field that actually implements it.

**Trade-offs**

- **Non-contiguous layouts cannot be expressed.** Deliberate gaps between
  partitions are not representable in a size-oriented model. No template in this
  repository uses one; if that changes, the step needs an escape hatch to raw
  offsets.
- **The "rest" partition must stay last.** Its `end: "0"` gives the partition
  after it no offset to start from. Adding a partition inserts *before* a
  trailing rest partition, and reordering across it is blocked.
- **Sizes are whole MiB.** Offsets are rounded up, never down, so a rounded
  partition can never start inside its predecessor.
- Duplicating the loader's auto-expand rules in TypeScript means they can drift
  from the Go implementation. They are covered by unit tests naming the Go
  source, and `POST /templates/validate` remains the authority.

---

## Alternatives considered

**Edit `start`/`end` directly.** No translation layer, and non-contiguous
layouts stay expressible. Rejected: it puts the contiguity invariant on the
user, and every insert or resize becomes a manual recomputation of everything
below it.

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
