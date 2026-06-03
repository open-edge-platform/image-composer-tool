# Copilot Instructions for image-composer-tool

> These instructions are loaded into every Copilot Chat / agent session in this repo.
> Keep them **concise, current, and actionable**. Move detail to `docs/` or to scoped
> `.github/instructions/*.instructions.md` files (see [Companion customization files](#companion-customization-files)).

---

## TL;DR for agents (read this first)

1. **Plan, then act.** For any task with >1 step, create a `manage_todo_list` plan, mark items in-progress/completed as you go.
2. **Gather context efficiently.** Parallelize independent reads/searches. Read large ranges in one call. Stop searching once you can act.
3. **Implement, don't just suggest.** Default to making the change. Use the smallest diff that solves the problem.
4. **Verify after every edit.** Run `get_errors` on touched files; run `earthly +test-quick` (or `go test ./...`) and `earthly +lint` (or `golangci-lint run`) before declaring done.
5. **Match existing patterns.** Project logger (not `fmt.Println`), `network.GetSecureHTTPClient()` (not `http.DefaultClient`), `internal/utils/shell` (not raw `exec.Command`), `filepath.Clean` on user paths.
6. **Stay in scope.** No drive-by refactors, no new comments/docstrings on code you didn't change, no new abstractions for one-off use.
7. **Update docs in the same PR** when behavior changes (see [Documentation](#documentation)).
8. **Never bypass safety gates.** No `--no-verify`, no `git push --force`, no skipping tests. Confirm with the user before destructive or shared-system actions.

---

## Architecture Overview

ICT builds custom Linux images from pre-built packages. Key components:

- **Provider** (`internal/provider/`) — Orchestrates builds per OS (azl, debian13, elxr, emt, rcd, ubuntu). Implements the `Provider` interface with `Name`, `Init`, `PreProcess`, `BuildImage`, `PostProcess`. Each provider exports an `OsName` constant and a `Register()` function.
- **Image makers** (`internal/image/`) — Output formats: `rawmaker/`, `isomaker/`, `initrdmaker/`.
- **Chroot** (`internal/chroot/`) — Isolated build environments with package installers for `deb/` and `rpm/`.
- **Config** (`internal/config/`) — Template loading, defaults+user merge, validation.
- **OsPackage** (`internal/ospackage/`) — Package utilities: `debutils/`, `rpmutils/` for dependency resolution.

Data flow: CLI → Config loads template → `Provider.Init` → `Provider.PreProcess` (downloads packages) → `Provider.BuildImage` (creates chroot, installs packages, generates image) → `Provider.PostProcess`.

---

## Agent workflow rules

These rules let the agent move fast without breaking things. They reflect 2025-era best practice for VS Code Copilot agent mode.

### Context gathering
- Prefer the workspace search/read tools over shelling out to `grep`/`find`/`cat`.
- **Parallelize** independent read-only operations (`grep_search`, `read_file`, `file_search`) in a single tool batch.
- Use the **Explore subagent** for broad codebase questions ("where is X used", "how does Y work") instead of chaining many searches in the main thread — it preserves your context budget.
- Stop searching once you have enough to act. If overlapping queries return the same files, you're done.

### Planning & progress
- Use `manage_todo_list` for any multi-step task. Exactly one item `in-progress` at a time. Mark `completed` immediately on finish — do not batch.
- Skip the todo list for trivial single-step changes.

### Editing
- Read a file before editing it. Use `multi_replace_string_in_file` when making several edits in one or more files in a single turn.
- Include 3–5 lines of unchanged context before and after each replacement target so matches are unique.
- **Never edit files by running shell commands** (`sed`, `awk`, heredoc redirects) unless the user explicitly asks.
- For rename refactors, prefer `vscode_renameSymbol` over text replace — it's semantics-aware.

### Verification loop
After each meaningful edit:
1. `get_errors` on the changed files.
2. Run targeted tests for the affected package (e.g. `go test ./internal/provider/ubuntu/...`).
3. Before handoff, run `earthly +test-quick` (or `go test ./...`) and `earthly +lint`.

### Anti-patterns the agent should refuse
- Adding `// TODO`, docstrings, or type annotations to code that wasn't otherwise changed.
- Creating "helper" packages or interfaces for a single caller.
- Catching errors only to re-throw them with no added context.
- Pulling in new dependencies without a stated reason.
- Generating new markdown "summary of changes" files unless requested — put that in the PR description instead.

### Memory
- Record durable repo facts (verified commands, gotchas, non-obvious conventions) under `/memories/repo/`. Consult before re-investigating something.
- Use `/memories/session/` for in-progress task notes; do not pollute repo memory with one-off context.

---

## Build and Test

Always use **Earthly** when available. Fallback to plain Go if Earthly is missing.

| Task | Earthly (preferred) | Go fallback |
|------|---------------------|-------------|
| Build | `earthly +build` | `go build ./...` |
| Test (fast) | `earthly +test-quick` | `go test ./...` |
| Test (coverage) | `earthly +test` | `go test -coverprofile=coverage.out ./...` |
| Lint | `earthly +lint` | `golangci-lint run` |

CI runs Earthly, so verify with `earthly +test` and `earthly +lint` before opening a PR.

Coverage threshold is enforced and auto-ratcheted — see `.coverage-threshold`.

---

## Adding a New OS Provider

1. Create package in `internal/provider/{osname}/` implementing the `provider.Provider` interface (see [internal/provider/provider.go](../internal/provider/provider.go)).
2. Register in the `switch` in `cmd/image-composer-tool/build.go`.
3. Add default configs in `config/osv/{osname}/` and example templates in `image-templates/`.
4. Add tests and update `docs/architecture/architecture.md`.

> Full provider conventions + checklist: [.github/instructions/provider.instructions.md](instructions/provider.instructions.md) (auto-applies to `internal/provider/**/*.go`).
> End-to-end scaffold prompt: [.github/prompts/add-os-provider.prompt.md](prompts/add-os-provider.prompt.md).

---

## Testing Patterns

stdlib `testing` only (no testify), table-driven with `t.Run()`, AAA layout, `t.TempDir()` for filesystem, `t.Parallel()` where safe.

> Full conventions: [.github/instructions/go-tests.instructions.md](instructions/go-tests.instructions.md) (auto-applies to `**/*_test.go`).

---

## Error Handling

- **Always wrap** with context: `fmt.Errorf("failed to X: %w", err)`.
- Use **named returns + `defer`** for cleanup (see `docs/architecture/image-composer-tool-coding-style.md` §4.3).
- Never ignore errors with `_` (caught by `errcheck`).
- Validate inputs at system boundaries only — don't defensively re-validate internal callers.

---

## Logging

- Use the project logger, **not** `fmt.Println` or the `log` stdlib:
  ```go
  import "github.com/open-edge-platform/image-composer-tool/internal/utils/logger"

  var log = logger.Logger()
  ```
- Every logging package declares `var log = logger.Logger()` at package level.
- **Three-layer logging strategy**:
  - **Utilities** (`internal/utils/`): primarily return errors; `log.Debugf()` sparingly.
  - **Business logic** (`internal/provider/`, `internal/config/`, …): log business context (`log.Infof`, `log.Warnf`, `log.Errorf`) and return errors with technical context.
  - **Top-level orchestrators** (`cmd/`): only return errors — logging happens below.
- Levels: `log.Debugf()`, `log.Infof()`, `log.Warnf()`, `log.Errorf()`.

---

## Code Style

- **Line length**: 120 chars max.
- **Function length**: under 50 lines — split larger ones.
- **Parameters**: max 4–5; use a config/options struct beyond that.
- **Imports**: stdlib → third-party → local (blank-line separated).
- **Struct-based design over globals** — prefer dependency injection.
- **Interface naming**: end with `-er` (`PackageInstaller`, `ConfigReader`).
- **Named returns + defer** for cleanup (the standard pattern; not "goto fail").
- **Linters** (`earthly +lint`): `govet`, `gofmt`, `errcheck`, `staticcheck`, `unused`, `gosimple` — all errors must be handled.
- Shell scripts: `set -euo pipefail`.
- See `docs/architecture/image-composer-tool-coding-style.md` for the full guide.

---

## Security

- **HTTP clients**: Always use `network.NewSecureHTTPClient()` or the singleton `network.GetSecureHTTPClient()` from `internal/utils/network/` — enforces TLS 1.2+ with approved cipher suites. **Never** use `http.DefaultClient`.
- **Command execution**: Use `internal/utils/shell/` (allowlisted commands). **Never** use raw `exec.Command()`.
- **Input validation**: Sanitize user-provided filenames/paths; always `filepath.Clean()` on paths derived from input.
- **Template validation**: Templates are validated against `os-image-template.schema.json` via `image-composer-tool validate`.
- **File permissions**: `0700` chroot dirs, `0755` general dirs, `0644` data files, `0640` log files.
- **Secrets**: Never write tokens/keys to files or logs. Do not embed secrets in test fixtures.
- CI runs **Trivy** (HIGH/CRITICAL fail), **Gitleaks** (secret detection), **Zizmor** (Actions auditing).

---

## Documentation

**Every PR that changes behavior must include corresponding documentation updates.** Treat docs as part of the change, not an afterthought.

| What changed | Docs to update |
|---|---|
| CLI flags or commands | `docs/architecture/image-composer-tool-cli-specification.md`, `docs/tutorial/usage-guide.md` |
| Build process / Earthfile targets | `docs/tutorial/usage-guide.md`, this file's **Build and Test** section |
| Image template schema or fields | `docs/architecture/image-composer-tool-templates.md`, relevant `image-templates/*.yml` examples |
| New OS provider | `docs/architecture/architecture.md`, this file's **Adding a New OS Provider** section |
| New tutorial-worthy feature | Add or update a guide in `docs/tutorial/` |
| Architecture or design decisions | Add an ADR in `docs/architecture/` |
| Security-related changes | `docs/architecture/image-composition-tool-security-objectives.md` |
| Caching behavior | `docs/architecture/image-composer-tool-caching.md` |
| Coding conventions | `docs/architecture/image-composer-tool-coding-style.md` |
| Dependencies or multi-repo setup | `docs/architecture/image-composer-tool-multi-repo-support.md` |
| User-facing features or fixes | `docs/release-notes.md` |

If no docs need updating, **say so explicitly in the PR description**.

---

## Git Commits & PRs

- Sign commits: `git commit -S`.
- **Conventional commits**: `type(scope): description` (`feat`, `fix`, `docs`, `test`, `refactor`, `chore`, `build`, `ci`, `perf`).
- Branch prefixes: `feature/`, `fix/`, `docs/`, `refactor/`.
- **Use `.github/PULL_REQUEST_TEMPLATE.md`** for every PR.
- **Do not** add AI co-author trailers (e.g. `Co-authored-by: Copilot …`) unless the repo explicitly asks for them.
- **Do not** force-push to shared branches. Never use `--no-verify`.
- Squash trivial fixup commits locally before pushing.
- The `gh` CLI is available — use it for PR/issue interactions over manual web steps.

---

## CI Quality Gates

All PRs must pass before merge:

| Gate | What it checks |
|------|----------------|
| Unit tests + coverage | `earthly +test` — threshold auto-ratchets via `.coverage-threshold` |
| Lint | `earthly +lint` — `govet`, `gofmt`, `errcheck`, `staticcheck`, `unused`, `gosimple` |
| Trivy scan | Dependency vulns (HIGH/CRITICAL) + SBOM generation |
| Gitleaks | Secret-leak detection |
| Zizmor | GitHub Actions security auditing |
| Integration builds | Per-OS/arch image builds |

---

## Image Template Conventions

Name `<dist>-<arch>-<purpose>-<imageType>.yml`. Minimal user templates need only `image` + `target`; always include a `metadata` block. Packages merge _additively_; `disk` _replaces_ entirely. Validate with `image-composer-tool validate -t <template.yml>` before committing.

> Full conventions: [.github/instructions/image-templates.instructions.md](instructions/image-templates.instructions.md) (auto-applies to `image-templates/**/*.yml`).

---

## Companion customization files

This single file is loaded into every chat. Keep it lean — path-specific and workflow-specific guidance lives in scoped companions:

- **`.github/instructions/*.instructions.md`** — Path-scoped rules with `applyTo:` frontmatter. Existing:
  - [go-tests.instructions.md](instructions/go-tests.instructions.md) — `applyTo: "**/*_test.go"`
  - [provider.instructions.md](instructions/provider.instructions.md) — `applyTo: "internal/provider/**/*.go"`
  - [image-templates.instructions.md](instructions/image-templates.instructions.md) — `applyTo: "image-templates/**/*.yml"`
- **`.github/prompts/*.prompt.md`** — Reusable task prompts. Existing:
  - [add-os-provider.prompt.md](prompts/add-os-provider.prompt.md)
- **`.github/chatmodes/*.chatmode.md`** — Custom agent modes with restricted tool sets (e.g. a read-only "reviewer" mode). _Not yet added._
- **`.vscode/mcp.json`** — Shared MCP servers (e.g. GitHub, fetch, container tooling) so every contributor's agent has the same capabilities. _Not yet added._
- **`AGENTS.md`** (repo root) — mirrors a subset of these instructions for non-Copilot agents (Cursor, Claude Code, etc.). Keep [AGENTS.md](../AGENTS.md) and this file in sync.

When a section here grows past a screenful or only applies to a subdirectory, split it out into one of the above and leave a one-line summary + pointer behind.

---

## Key Files

- `image-templates/*.yml` — Example image templates
- `config/osv/` — OS-specific default configurations
- `internal/config/config.go` — `ImageTemplate` struct definition
- `internal/provider/provider.go` — Provider interface
- `internal/utils/logger/` — Project logger (zap-based)
- `internal/utils/network/securehttp.go` — Secure HTTP client
- `internal/utils/shell/shell.go` — Command execution with allowlist
- `.golangci.yml` — Linter configuration
- `.coverage-threshold` — Current test coverage threshold
- `docs/architecture/` — ADRs and design docs
