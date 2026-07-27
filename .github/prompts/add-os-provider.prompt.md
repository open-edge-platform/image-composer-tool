---
mode: agent
description: Scaffold a new OS provider end-to-end (package, registration, defaults, template, tests, docs).
---

# Add a new OS provider

You are adding a new OS provider to image-composer-tool. Follow the project's [provider conventions](../instructions/provider.instructions.md) and [test conventions](../instructions/go-tests.instructions.md).

## Inputs to collect from the user

If any of these are missing, ask **once** with `vscode_askQuestions` (one batch, all questions) before writing any code:

1. **OS name** (lowercase, used as `OsName` and in template `target.os`), e.g. `fedora`.
2. **OS family** — `deb` or `rpm` (decides which installer + ospackage helpers to wire up).
3. **Supported architectures** — any subset of `x86_64`, `aarch64`.
4. **Default OS version** to ship with (e.g. `40`).
5. **Upstream package repos** — base URL(s) and signing-key location.
6. **Image types to support initially** — any subset of `raw`, `iso`, `initrd`.

## Plan

Create a `manage_todo_list` covering exactly these steps, then execute in order:

1. **Read the closest analogue.** If `deb`, study `internal/provider/ubuntu/`; if `rpm`, study `internal/provider/azl/` (or `emt`). Read the full provider file plus its tests.
2. **Scaffold the package** at `internal/provider/{osname}/{osname}.go`, matching the interface in `internal/provider/provider.go`:
   - `const OsName = "{osname}"` (must match the template `target.os`; may be multi-word).
   - A provider struct (e.g. `type {osname} struct{ … }`) implementing the interface methods.
   - `func Register(targetOs, targetDist, targetArch string) error` that calls `provider.Register(&{osname}{…}, targetDist, targetArch)`.
   - Implement `Name(dist, arch string) string`, `Init(dist, arch string) error`, `PreProcess(template *config.ImageTemplate) error`, `BuildImage(template *config.ImageTemplate) error`, `PostProcess(template *config.ImageTemplate, err error) error`. **No `context.Context`** — the interface does not take one. Wrap errors with phase context and use `var log = logger.Logger()`.
3. **Wire registration** in `cmd/image-composer-tool/build.go` — add a `case {osname}.OsName:` arm to the `switch os` that calls `{osname}.Register(os, dist, arch)` and wraps the error. Confirm by re-reading the edited section.
4. **Default configs** in `config/osv/{osname}/` — mirror the layout of the analogue provider (package lists, disk layout, bootloader). Use the user-provided repo URLs and signing keys.
5. **Example templates** in `image-templates/`, one per chosen image type, named `<dist>-<arch>-minimal-<imageType>.yml` (filenames use the `target.dist` value, e.g. `azl3-…`, `ubuntu24-…`, not the `OsName`). Each must have a populated `metadata` block.
6. **Unit tests** in `internal/provider/{osname}/{osname}_test.go` covering at minimum: the `OsName` constant, `Name(dist, arch)` output, `Init` error paths, and registration. Mock chroot + shell — no real package installs.
7. **Docs**:
   - Add the provider to `docs/user-guide/architecture/architecture.md`.
   - Add a release note under `docs/user-guide/release-notes.md`.
   - Update the **Adding a New OS Provider** section of `.github/copilot-instructions.md` only if the *steps* changed (not just the provider list).
8. **Validate**:
   - `image-composer-tool validate image-templates/<new-template>.yml` for each new template.
   - `go test ./internal/provider/{osname}/...`
   - `earthly +test-quick` and `earthly +lint` (or Go fallbacks).
9. **Summary** — report what was created, what tests passed, and what the user should run to do a real integration build.

## Guardrails

- Do **not** add new third-party dependencies without asking.
- Do **not** call `apt`/`dnf` directly from the provider — use `internal/chroot/{deb,rpm}/`.
- Do **not** commit secrets or private repo URLs to fixtures.
- Stay within the directories listed above; no drive-by edits elsewhere.
