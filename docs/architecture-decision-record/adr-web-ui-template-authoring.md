# ADR: Template Authoring in the Web UI

**Status**: Proposed (for team discussion)
**Date**: 2026-11-19
**Authors**: ICT Team
**Technical Area**: Web UI / Templates

---

## Summary

Advanced mode gets a second mode, **Author**, next to today's **Customize**.

- **Customize** (no change): adds your changes on top of a curated template, as
  a small `extends` file.
- **Author** (new): edits the whole template, then saves it to your machine.

Author mode is shown only when the server allows it. For now a server setting
turns it on. Later, the user's role will decide.

---

## Why

The Upstream BKC owner keeps the BKC template up to date. They start from the
curated NVL template, change it, and save it on their laptop. Later they open
it again, change it, and save it. Sometimes they build an image from it.

Customize can't do this:

- It saves only the changes, not the whole template.
- It can't remove packages.
- It needs the original template on the server. A template from the laptop
  has none.

There are no user roles yet, and the technical review is in late November
2026. So phase 1 is one user and local files.

---

## Decision

### 1. Author reuses the Advanced steps

| | Customize | Author |
|---|---|---|
| Starts from | a curated template | a curated template, or a file from your machine |
| Packages | add, pin, upgrade | add, pin, upgrade, **remove** |
| Result | the changes only | **the whole template** |
| Last step | Export YAML | **Save Template** |

Author adds one first step, **Templates**, to open or pick a template. The
Packages, Disk and Review steps stay the same.

### 2. Edits happen in the browser and keep comments

The browser edits the YAML in place with the `yaml` library, so comments and
order are kept. Saving changes only what you changed, which keeps git diffs
small. Nothing is written on the server.

### 3. Open and save use your machine

- **Open:** a file picker.
- **Save / Save As:** the browser's save dialog. Browsers without it download
  the file instead.
- Curated templates on the server are never changed.

Shared storage (git, S3 or a database), history and review come in phase 2.

### 4. The server decides who can author

- `GET /api/v1/session` returns `{capabilities: {authoring: true|false}}`.
- Phase 1: `serve --enable-authoring`, or `ICT_WEBUI_AUTHORING=true` in
  Docker Compose. It's off by default.
- Later: the user's role sets the same value. The UI doesn't change.

### 5. Phase 1 limits

- Only templates without `extends`.
- Files the template points to beside itself (`additionalFiles`) aren't found.
- Paths in the template, such as the baseline image, are read on the server.
- You can edit packages, repositories, disk and image name. Everything else is
  kept as is.

---

## Trade-offs

- **The setting only hides the UI.** The build API already accepts a whole
  template, so phase 1 doesn't block it. Server-side saving in phase 2 must
  check the permission.
- **Author builds have no use case or SKU,** so history shows the template name
  and Retry can't rebuild from a selection.
- **Files stay on one machine** until phase 2.

---

## Options we didn't pick

- **A browser-only switch:** anyone could turn it on, and it would be thrown
  away when roles arrive.
- **Flatten the Customize changes on the server:** loses comments, and still
  can't remove packages.
- **Edit curated templates on the server:** changes shared files with no
  review.
- **A plain YAML text editor:** loses the package search, version pinning and
  disk checks the wizard already has.

---

## References

- [`adr-web-ui-advanced-mode-extends.md`](adr-web-ui-advanced-mode-extends.md):
  how Customize works
- [`adr-template-extends.md`](adr-template-extends.md): `extends` rules

---

## Revision History

| Date | Change |
|---|---|
| 2026-11-19 | Initial proposal |
