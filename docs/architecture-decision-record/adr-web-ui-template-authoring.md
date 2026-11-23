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
- **Author** (new): edits the whole template, saves it to your machine, opens
  it again later, and builds an image from it.

Author mode is shown only when the server allows it, and then Advanced starts
in it. For now a server setting turns it on. Later, the user's role will
decide.

---

## Why

The Upstream BKC owner keeps the BKC template up to date. They start from the
curated NVL template, change it, and save it on their laptop. Later they open
it again, change it, and save it. Sometimes they build an image from it.

Customize can't do this:

- It saves only the changes, not the whole template.
- It can't remove or unpin packages.
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
| Packages | add, pin, upgrade | add, pin, upgrade, **unpin**, **remove** |
| Result | the changes only | **the whole template** |
| Last step | Export YAML | **Save Template**, **Save As…** |
| Build | the curated template plus the changes | **the edited template itself** |

The Target, Packages, Disk and Review steps stay the same. Author mode adds an
**Open Template…** button to the Target step.

In the Packages step a template package's checkbox is live: unchecking it
removes the package. Choosing its selected version chip again unpins it. The
**Changes** panel lists what differs from the template (Added, Modified,
Removed), with **Reset to template defaults** to undo it all.

### 2. Edits happen in the browser and keep the file as written

The browser parses the YAML only to find where things are, then replaces just
the changed lines in the original text. Comments, spacing and order are kept,
so a saved template differs from the original only where you changed it, and
git diffs stay small.

### 3. Open and save use your machine

- **Open:** a file picker. **Save / Save As:** the browser's save dialog.
  Browsers without it download the file instead. After opening, **Save**
  writes back to the same file.
- **Saving records where the template came from:** `metadata.basedOn` names
  the curated template it started from. Opening reads it back and selects the
  matching use case, platform and OS. A file without it opens for the current
  selection.
- **× beside the opened file's name** closes it and goes back to the use
  case's own template.
- Curated templates on the server are never changed.

Shared storage (git, S3 or a database), history and review come in phase 2.

### 4. The server decides who can author

- `GET /api/v1/session` returns `{capabilities: {authoring: true|false}}`.
- Phase 1: `serve --enable-authoring`, or `ICT_WEBUI_AUTHORING=true` in
  Docker Compose. It's off by default.
- Without it, the server doesn't send template files, and refuses an opened
  template (403).
- Later: the user's role sets the same value. The UI doesn't change.

### 5. Opened and edited templates sit beside the curated one

To compose or build an opened or edited template, the server writes it next to
the curated template it is based on, uses it, and removes it afterwards. So
paths relative to the template resolve the same way as for the curated one.

A build from Author mode keeps its use case, so history labels it as usual and
Retry rebuilds the same template. The logins dialog adds its logins on top.

### 6. Phase 1 limits

- Only templates without `extends`.
- You can edit packages, the repositories they need, disk and image name.
  Everything else, such as the kernel (`replaceKernel`), scripts and proxy,
  is kept as written.
- Paths in the template, such as the baseline image, are read on the server.
- Making a saved template the curated one that Basic mode uses means copying
  the file into the server's templates folder by hand.

---

## Trade-offs

- **The gate covers Author mode, not the whole API.** The build API still
  accepts a complete template as `yaml`, as it did before. Server-side saving
  in phase 2 must check the permission.
- **Files stay on one machine** until phase 2.
- **Two YAML libraries in the UI** until older code moves to the new one.

---

## Options we didn't pick

- **A browser-only switch:** anyone could turn it on, and it would be thrown
  away when roles arrive.
- **Flatten the Customize changes on the server:** loses comments, and still
  can't remove packages.
- **Edit curated templates on the server:** changes shared files with no
  review.
- **Build from the raw `yaml` path:** writes the template into the build's own
  folder, which breaks relative paths, skips the logins and loses the use case
  in history.
- **A comment line instead of `metadata.basedOn`:** lost when comments are
  stripped.
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
| 2026-11-23 | Updated to what was built: unpinning, the Changes panel, Open Template with `metadata.basedOn`, building from Author mode, and server-side handling of opened templates |
