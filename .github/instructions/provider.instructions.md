---
applyTo: "internal/provider/**/*.go"
---

# OS Provider conventions

Use these in addition to the root `copilot-instructions.md`. The `Provider` interface is defined in [internal/provider/provider.go](../../internal/provider/provider.go).

## Package layout

Each provider lives in `internal/provider/{osname}/` and **must** export:

```go
// OsName must match the template's target.os. It may be multi-word,
// e.g. "ubuntu", "wind-river-elxr", "redhat-compatible-distro".
const OsName = "ubuntu"

// Register constructs the provider and registers it with the core
// provider registry. Called from cmd/image-composer-tool/build.go.
func Register(targetOs, targetDist, targetArch string) error {
    provider.Register(&ubuntu{ /* fields */ }, targetDist, targetArch)
    return nil
}
```

`Register()` is invoked from the `switch` in `cmd/image-composer-tool/build.go`
(`case ubuntu.OsName: ubuntu.Register(os, dist, arch)`). Registration is explicit —
do not auto-register via `init()`. The core `provider.Register(p, dist, arch)` keys
the provider by `p.Name(dist, arch)`.

## Interface contract

The `Provider` interface is (see [internal/provider/provider.go](../../internal/provider/provider.go)):

```go
type Provider interface {
    Name(dist, arch string) string
    Init(dist, arch string) error
    PreProcess(template *config.ImageTemplate) error
    BuildImage(template *config.ImageTemplate) error
    PostProcess(template *config.ImageTemplate, err error) error
}
```

Implement each method — they run in this order:

| Method | Responsibility | Allowed side effects |
|---|---|---|
| `Name(dist, arch)` | Return the unique provider id (combines OS, dist, arch) | None |
| `Init(dist, arch)` | One-time setup: import GPG keys, register repos | Key/repo state, empty dirs |
| `PreProcess(template)` | Resolve + download packages into the cache | Network IO, writes to `cache/pkgCache/` |
| `BuildImage(template)` | Create chroot, install packages, run image-maker | Writes to workspace, mounts/unmounts |
| `PostProcess(template, err)` | Final steps + cleanup; `err` is the build error (nil on success) | Writes to output dir, unmounts |

> Note: the interface does **not** take `context.Context` — the build pipeline
> does not currently thread a context. Only `internal/ai/` uses `context.Context`.
> Do not add a `ctx` parameter to provider methods to "match Go convention" —
> match the existing interface instead.

## Required patterns

- **Logger.** Declare `var log = logger.Logger()` at package level (every existing provider does). Log at `Info` for phase boundaries, `Debug` for per-package detail, `Warn`/`Error` for recoverable/fatal issues.
- **Shell.** Use `internal/utils/shell` — never raw `exec.Command`. New commands must be added to the allowlist with justification.
- **HTTP.** Use `network.GetSecureHTTPClient()` for any download. Never `http.DefaultClient`.
- **Paths.** `filepath.Clean` any path derived from the template. Chroot dirs are `0700`.
- **Cleanup.** Named returns + `defer` for unmounts and tempdir removal. Never leave loop devices or bind mounts dangling on error.

## Package resolution

- Debian-family: use helpers in `internal/ospackage/debutils/`.
- RPM-family: use helpers in `internal/ospackage/rpmutils/`.
- Do not shell out to `apt`/`dnf` inside the provider — those calls belong in the chroot installers (`internal/chroot/deb/`, `internal/chroot/rpm/`).

## Errors

- Wrap with phase context: `fmt.Errorf("ubuntu PreProcess: resolve deps: %w", err)`.
- Missing-package errors should populate the `Missing_Requested_Packages_*.json` report (see existing providers for the pattern).
- Surface unrecoverable errors up to `cmd/`; do not call `os.Exit` from a provider.

## Tests

- Mock the chroot and shell layers via interfaces — never run a real `apt-get` from a unit test.
- Per-OS smoke build runs in CI integration jobs, not in `earthly +test`.

> Adding a whole new provider? Use the step-by-step workflow in
> [.github/prompts/add-os-provider.prompt.md](../prompts/add-os-provider.prompt.md)
> (run `/add-os-provider` in chat). This file is the *conventions* reference it relies on.
