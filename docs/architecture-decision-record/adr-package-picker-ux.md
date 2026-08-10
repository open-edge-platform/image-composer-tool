# ADR: Package Picker UX for the Advanced Template Builder

**Status**: Proposed
**Date**: 2026-08-10
**Authors**: ICT Team
**Technical Area**: Web Frontend (Advanced tab — Packages step)
**Related**: [ADR: Web UI and API Tech Stack](adr-web-ui-tech-stack.md), [UI Prototype](../../web/prototype/template-builder.html)

---

## Summary

This ADR records the interaction model for selecting packages and package
versions in the Advanced tab's Packages step, as implemented in the
`web/prototype/template-builder.html` reference prototype. It supersedes the
"Packages step" row of the parent [Web UI Tech Stack ADR](adr-web-ui-tech-stack.md#advanced-tab)
with a more detailed design, and gives the production frontend team concrete
guidance on component behavior, state shape, and layout ordering.

---

## Context

### Problem Statement

In the `main` baseline, the Packages step's package rows had **no version
selection UI at all**: each row was a checkbox plus the package name and
description, full stop. A handful of curated example packages simulated
"pinning" by baking a version straight into the package `name` string (e.g.
`intel-oneapi-runtime-compilers_2025.3.3-30`, `openvino_2025.3.0.19807`) —
there was no control for a user to choose or change that version, and no way
to tell, at a glance, which of a package's available versions (if any) was in
play. The section order was **Search Packages → Use-Case Bundles (always
expanded) → Browse Repositories**.

An interim iteration (not shipped, superseded by this ADR) tried adding
interactive version control via two separate buttons per row — **Latest** and
**Pin** — where **Pin** toggled a secondary row of version buttons. Review of
that iteration surfaced three problems, which this ADR's design (see
[Comparison](#comparison-main-baseline-vs-proposed-design) below) resolves
directly against the `main` baseline:

1. **Two competing controls for one decision.** "Latest" and "Pin" are not
   independent toggles; a package is *either* tracking latest *or* pinned to
   one specific version. Modeling this as two buttons let the UI enter
   contradictory in-between states and required extra clicks to reach a
   pinned version (Pin, then pick a version).
2. **Page-order friction.** The Use-Case Bundles grid rendered fully expanded
   above the Search box on every visit to the Packages step, pushing the more
   frequently used search/browse controls below the fold. (This friction
   already existed in `main`, just with the sections in the other order.)
3. **Weak scanability.** The selected version and the source repository were
   not visually anchored to the package name, so a dense package list (repos
   with hundreds/thousands of packages) was hard to scan for "what did I pick,
   and from where." `main` had no version affordance to anchor at all, and no
   repo-name line on the row either.

### Constraints

- Must keep working with the existing `name` / `name_version` selection
  encoding used by `systemConfig.packages` (see
  [Web UI Tech Stack ADR](adr-web-ui-tech-stack.md)) — this ADR only changes
  the UI layer, not the wire/template format.
- Must scale to repositories with tens of thousands of packages (suggested vs.
  full-catalog browsing, windowed rendering) — already an established pattern
  in the prototype and not being revisited here.
- Must remain usable with only a handful of packages selected as well as
  dozens — bundle discovery shouldn't dominate the page in either case.

---

## Comparison: `main` Baseline vs Proposed Design

| Aspect | `main` baseline | Proposed (this ADR) |
|---|---|---|
| Version selection control | None. Version is either absent or hardcoded into the package `name` string (e.g. `openvino_2025.3.0.19807`); not user-selectable. | A single row of mutually-exclusive chips: `Latest` + each known version, capped with `+N more`/`show less`. |
| Package data model | Some `PACKAGE_DB` entries encode a fixed version in `name`; no separate `version`/`versions` fields. | Every entry carries an explicit `version` (latest) and optional `versions` array; `name` is always the plain package name. |
| Package row layout | Single line: checkbox, name, description. | Multi-line: name + version badge on top, repo name below, version chips below that. |
| Repo attribution on the row | Not shown per-row (only implied by which repo pane you're browsing). | Repo name shown explicitly on every row, including in global search results. |
| Use-Case Bundles default state | Always expanded. | Collapsed by default with a `Show`/`Hide` disclosure toggle. |
| Packages step section order | Search Packages → Use-Case Bundles → Browse Repositories. | Use-Case Bundles (collapsed) → Search Packages → Browse Repositories. |
| Search dropdown rows | Name + description + repo id chip; no version affordance. | Name + version badge, repo name, and the same version-chip row used in the browse pane. |
| Selection wire format (`systemConfig.packages`) | `name` or a `name` with a version baked in by hand. | Unchanged: `name` (latest) or `name_version` (pinned) — see [State shape](#state-shape). |

The row layout, version-chip control, and bundle-ordering changes above are
this ADR's decisions; the wire format they produce is intentionally identical
to what `main` already accepted.

---

## Decision

### 1. Version selection is a single row of chips, not two buttons

Replace the **Latest** / **Pin** button pair with one row of mutually
exclusive chips: `Latest` first, then each known version, capped at 2 visible
by default with a `+N more` / `show less` expander for packages with many
versions.

```
[ Latest ] [ 2025.3.3-30 ] [ 2025.2.2-24 ]        ← ≤ 2 extra versions: all shown
[ Latest ] [ 255.4-1ubuntu8 ] [ 255.4-1ubuntu7 ] [+3 more]   ← capped, expandable
```

- Exactly one chip is ever `active` at a time (`Latest` or one version).
- Clicking a chip is a single action that both selects and applies —
  no separate "confirm pin" step.
- `Latest` is the default/active chip when nothing is pinned, so doing nothing
  still yields a well-defined, visible state.

**Rationale**: this removes the invalid intermediate state (pinned-but-no-version-chosen)
entirely, and cuts the click count for pinning a specific version from two
interactions to one.

### 2. Package name row: version badge inline, repo name below, versions below that

Each package row is laid out top-to-bottom as:

```
<name>                              <version-badge>
<repo name>
[ Latest ] [ v1 ] [ v2 ] [+N more]
```

- The version badge next to the name always reflects the *current* selection
  state for that package — `Latest` or the pinned version string — so the
  header line alone answers "what will be installed."
- The repo name sits on its own line directly under the package name and
  above the version chips, giving a consistent scan order (what → where from →
  which version) that holds whether the row is in the repo browser pane or the
  global search dropdown.

### 3. Use-Case Bundles is collapsed by default, Search Packages follows immediately after

Reorder the Packages step to: **Use-Case Bundles (collapsed, toggleable)** →
**Search Packages** → **Browse Repositories**.

- Bundles default to collapsed (`Show`/`Hide` disclosure toggle) so the page
  opens on the more frequently used search/browse affordances.
- Users who want curated cross-repo bundles (e.g. "ROS 2 Jazzy Desktop") can
  expand them on demand; the toggle state can persist per session if the
  production implementation wants that (not required for MVP).

### 4. Applies uniformly to both the repo browser pane and package search results

Both the "Browse Repositories" pane rows and the global "Search Packages"
dropdown rows use the same name → repo → version-chip layout and the same
chip-based version picker, so users learn the interaction once.

---

## Component / State Guidance for Production

| Prototype element | Production component | Notes |
|---|---|---|
| Version chip row (`Latest` + versions, capped) | `<ToggleGroup>` (single-select) with an overflow "show N more" affordance | One `value` per package: `"latest"` or a version string. Cap the initially-rendered options (prototype default: 2) and reveal the rest on demand rather than paginating. |
| Version badge next to name | `<Badge>` bound to the same selection state as the toggle group | Derive the label from the same source of truth as the chips — never store it separately, to avoid drift. |
| Repo name line | Plain text/`<span>` under the name, above the chip row | No interactivity; purely orientation. |
| Use-Case Bundles disclosure | `<Collapsible>` defaulting to closed | Keep the "applied/partial" bundle-state badges from the existing bundle cards; only the default open/closed state and page position change. |
| Selected-package rail (right column) | Unchanged | Continues to show `latest` vs `pinned <version>` per package, grouped by repo. |

### State shape

No backend/template changes. The existing `name` / `name_version` selection
encoding is retained; the chip group's selected value maps directly onto that
encoding:

- Chip `Latest` selected → store `name`
- Chip `<version>` selected → store `name_version`

---

## Consequences

**Positive**

- One decision, one control: fewer clicks, no invalid intermediate states.
- Consistent name → repo → version scan order improves scanability in
  high-density repos (thousands of packages).
- Collapsed-by-default bundles reduce the average vertical scroll distance to
  reach Search/Browse, the higher-frequency actions.

**Trade-offs**

- Packages with more than a couple of versions require an extra click
  (`+N more`) to reveal the full version list — accepted to keep the default
  row height compact and single-line.
- Collapsing bundles by default trades one click of discoverability for less
  clutter; mitigated by keeping the disclosure affordance visible and labeled
  at all times (never hidden behind a menu).

---

## Alternatives Considered

| Option | Rejected because |
|---|---|
| Keep `main`'s baseline: no version UI, versions hardcoded into `name` | Not user-selectable at all; doesn't scale past a handful of hand-picked demo packages and blocks any real "pin a version" use case |
| Keep separate Latest/Pin buttons, just restyle them | Doesn't resolve the invalid-intermediate-state problem; only a visual patch |
| Dropdown/`<Select>` for version instead of chips | Hides all versions behind an extra click even for the common 2-3-version case; chips keep the common case one click while still scaling via "show more" |
| Bundles expanded by default, Search below (either order, as in `main`) | Keeps the page-order friction this ADR sets out to fix |
| Version badge as a tooltip instead of inline text | Tooltips aren't scannable at a glance across a long list; an always-visible badge is |

---

## References

- Prototype: [`web/prototype/template-builder.html`](../../web/prototype/template-builder.html)
- Parent ADR: [Web UI and API Tech Stack](adr-web-ui-tech-stack.md)
