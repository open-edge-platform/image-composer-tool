# Copilot Instructions for os-image-composer

## Architecture Overview

OS Image Composer builds custom Linux images from pre-built packages. Key components:

- **Provider** (`internal/provider/`) - Orchestrates builds per OS (azl, elxr, emt, rcd, ubuntu). Implements `Provider` interface with `Name`, `Init`, `PreProcess`, `BuildImage`, `PostProcess` methods. Each provider exports an `OsName` constant and a `Register()` function
- **Image makers** (`internal/image/`) - Creates output formats: `rawmaker/`, `isomaker/`, `initrdmaker/`
- **Chroot** (`internal/chroot/`) - Isolated build environments with package installers for `deb/` and `rpm/`
- **Config** (`internal/config/`) - Template loading, merging defaults with user templates, validation
- **OsPackage** (`internal/ospackage/`) - Package utilities: `debutils/`, `rpmutils/` for dependency resolution

Data flow: CLI → Config loads template → Provider.Init → Provider.PreProcess (downloads packages) → Provider.BuildImage (creates chroot, installs packages, generates image) → Provider.PostProcess

## Build and Test

Always use **Earthly** for builds and testing when available. If Earthly is not configured in your environment, you can fall back to standard Go commands:

| Task | Earthly (preferred) | Go fallback |
|------|---------------------|-------------|
| Build | `earthly +build` | `go build ./...` |
| Test (fast) | `earthly +test-quick` | `go test ./...` |
| Test (coverage) | `earthly +test` | `go test -coverprofile=coverage.out ./...` |
| Lint | `earthly +lint` | `golangci-lint run` |

Note: CI runs Earthly, so always verify with `earthly +test` and `earthly +lint` before opening a PR if possible, to avoid CI failures.

Coverage threshold is enforced in CI and auto-ratcheted — see `.coverage-threshold` for the current value

## Adding a New OS Provider

1. Create package in `internal/provider/{osname}/`
2. Implement `provider.Provider` interface (see `internal/provider/provider.go`)
3. Register in `cmd/os-image-composer/build.go` switch statement
4. Add default configs in `config/osv/{osname}/`
5. Create example templates in `image-templates/`

## Testing Patterns

- Use **stdlib `testing` only** — no testify or external assertion libraries
- Use **table-driven tests** with `t.Run()` for multiple cases:
```go
tests := []struct{ name string; input string; want error }{...}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) { ... })
}
```
- Follow the **AAA pattern**: Arrange (setup), Act (execute), Assert (verify)
- Test file naming: `*_test.go` in same package
- Reset shared state in tests (see `resetBuildFlags()` pattern in `cmd/os-image-composer/build_test.go`)

## Error Handling

- **Always wrap** with context: `fmt.Errorf("failed to X: %w", err)`
- Use named returns with defer for cleanup (see `docs/architecture/os-image-composer-coding-style.md`)
- Never ignore errors with `_`

## Logging

- Use the project logger, **not** `fmt.Println` or `log` stdlib:
```go
import "github.com/open-edge-platform/os-image-composer/internal/utils/logger"

var log = logger.Logger()
```
- Every package that logs should declare `var log = logger.Logger()` at package level
- **Three-layer logging strategy**:
  - **Utilities** (`internal/utils/`): primarily return errors; use `log.Debugf()` sparingly
  - **Business logic** (`internal/provider/`, `internal/config/`, etc.): log with business context (`log.Infof`, `log.Warnf`, `log.Errorf`) and return errors with technical context
  - **Top-level orchestrators** (`cmd/`): only return errors — logging happens at lower levels
- Available levels: `log.Debugf()`, `log.Infof()`, `log.Warnf()`, `log.Errorf()`

## Code Style

- **Line length**: 120 characters max
- **Function length**: under 50 lines — break up larger functions
- **Parameters**: max 4–5 per function; use a config/options struct beyond that
- **Imports**: stdlib → third-party → local (blank line separated)
- **Struct-based design over globals** — prefer dependency injection
- **Interface naming**: should end with `-er` when possible (e.g., `PackageInstaller`, `ConfigReader`)
- **Named returns + defer for cleanup** — the standard cleanup pattern (not "goto fail"); see [coding style Section 4.3](../docs/architecture/os-image-composer-coding-style.md)
- **Linters** (`earthly +lint`): `govet`, `gofmt`, `errcheck`, `staticcheck`, `unused`, `gosimple` — all errors must be handled (`errcheck` is enforced)
- Shell scripts: `set -euo pipefail`
- See `docs/architecture/os-image-composer-coding-style.md` for the full guide

## Security

- **HTTP clients**: Always use `network.NewSecureHTTPClient()` or the singleton `network.GetSecureHTTPClient()` from `internal/utils/network/` — enforces TLS 1.2+ with approved cipher suites. Never use `http.DefaultClient`
- **Command execution**: Use the `internal/utils/shell/` package which maintains an allowlist of approved system commands. Never use raw `exec.Command()`
- **Input validation**: Sanitize user-provided filenames and paths; use `filepath.Clean()` on paths
- **Template validation**: Templates are validated against JSON schema (`os-image-template.schema.json`) via `os-image-composer validate`
- **File permissions**: `0700` for chroot dirs, `0755` for general dirs, `0644` for data files, `0640` for log files
- CI runs **Trivy** (dependency vulnerability scanning — fails on HIGH/CRITICAL), **Gitleaks** (secret detection), and **Zizmor** (GitHub Actions security auditing)

## Documentation

**Every PR that changes behavior must include corresponding documentation updates.** Documentation is a first-class deliverable — treat it as part of the code change, not an afterthought.

Before opening a PR, check whether your changes affect any of these and update accordingly:

| What changed | Docs to update |
|---|---|
| CLI flags or commands | `docs/architecture/os-image-composer-cli-specification.md`, `docs/tutorial/usage-guide.md` |
| Build process or Earthfile targets | `docs/tutorial/usage-guide.md`, this file's **Build and Test** section |
| Image template schema or fields | `docs/architecture/os-image-composer-templates.md`, relevant `image-templates/*.yml` examples |
| New OS provider | `docs/architecture/architecture.md`, this file's **Adding a New OS Provider** section |
| New tutorial-worthy feature | Add or update a guide in `docs/tutorial/` |
| Architecture or design decisions | Add an ADR in `docs/architecture/` |
| Security-related changes | `docs/architecture/image-composition-tool-security-objectives.md` |
| Caching behavior | `docs/architecture/os-image-composer-caching.md` |
| Coding conventions | `docs/architecture/os-image-composer-coding-style.md` |
| Dependencies or multi-repo setup | `docs/architecture/os-image-composer-multi-repo-support.md` |
| User-facing features or fixes | `docs/release-notes.md` |

## Git Commits & PRs

- Sign commits: `git commit -S`
- Conventional commits: `type(scope): description` (feat, fix, docs, test, refactor, chore)
- **Always use** `.github/PULL_REQUEST_TEMPLATE.md` for PRs
- Branch prefixes: `feature/`, `fix/`, `docs/`, `refactor/`
- **Always update documentation** alongside code changes (see **Documentation** section above). If no docs need updating, explicitly state so in the PR description

## CI Quality Gates

All PRs must pass these checks before merging:

| Gate | What it checks |
|------|----------------|
| Unit tests + coverage | `earthly +test` — threshold auto-ratchets via `.coverage-threshold` |
| Lint | `earthly +lint` — golangci-lint with `govet`, `gofmt`, `errcheck`, `staticcheck`, `unused`, `gosimple` |
| Trivy scan | Dependency vulnerabilities (HIGH/CRITICAL) + SBOM generation |
| Gitleaks | Secret leak detection in commits |
| Zizmor | GitHub Actions workflow security auditing |
| Integration builds | Per-OS/arch image builds |

## Image Template Conventions

- **Naming**: `<dist>-<arch>-<purpose>-<imageType>.yml` (e.g., `emt3-x86_64-minimal-raw.yml`)
- **Minimal user templates**: only `image` + `target` sections required; OS defaults handle the rest
- **Always include a `metadata` block** with `description`, `use_cases`, and `keywords` for discoverability
- **Merge behavior**: packages are _additive_ (merged by name); `disk` configuration _replaces_ entirely
- **Validate** before committing: `os-image-composer validate -t <template.yml>`

## Key Files

- `image-templates/*.yml` - Example image templates
- `config/osv/` - OS-specific default configurations
- `internal/config/config.go` - `ImageTemplate` struct definition
- `internal/provider/provider.go` - Provider interface
- `internal/utils/logger/` - Project logger (zap-based)
- `internal/utils/network/securehttp.go` - Secure HTTP client
- `internal/utils/shell/shell.go` - Command execution with allowlist
- `.golangci.yml` - Linter configuration
- `.coverage-threshold` - Current test coverage threshold
- `docs/architecture/` - ADRs and design docs
