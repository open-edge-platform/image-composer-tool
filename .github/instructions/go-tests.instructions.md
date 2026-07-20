---
applyTo: "**/*_test.go"
---

# Go test conventions

Use these in addition to the root `copilot-instructions.md`.

## Framework
- **stdlib `testing` only.** No `testify`, `gomega`, `ginkgo`, or other assertion libs.
- One test file per source file: `foo.go` → `foo_test.go`, same package (use `_test` package only when you need to test through the public API to break an import cycle).

## Structure
- **Table-driven** by default. Sub-tests via `t.Run(tt.name, …)`.
- **AAA**: Arrange, Act, Assert — keep the three blocks visually separated by a blank line.
- Helpers must call `t.Helper()` on the first line so failure lines point at the caller.

```go
func TestFoo(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {name: "happy path", input: "a", want: "A"},
        {name: "empty", input: "", wantErr: true},
    }
    for _, tt := range tests {
        tt := tt
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            got, err := Foo(tt.input)
            if (err != nil) != tt.wantErr {
                t.Fatalf("Foo(%q) err = %v, wantErr %v", tt.input, err, tt.wantErr)
            }
            if got != tt.want {
                t.Errorf("Foo(%q) = %q, want %q", tt.input, got, tt.want)
            }
        })
    }
}
```

## Filesystem & state
- Always use `t.TempDir()` for files/dirs. **Never** write into the repo tree or `/tmp` directly.
- Reset package-level state in `TestMain` or at the top of each test (see `resetBuildFlags()` in `cmd/image-composer-tool/build_test.go`).
- Use `t.Cleanup(...)` for teardown instead of `defer` when the cleanup must run in a sub-test.

## Parallelism
- Add `t.Parallel()` to every IO-light, state-free test (top-level **and** inside sub-tests).
- Capture the loop var (`tt := tt`) before `t.Parallel()` in table-driven sub-tests.
- Do **not** parallelize tests that mutate globals, chdir, set env vars without `t.Setenv`, or touch shared fixtures.

## Env / context
- Use `t.Setenv("KEY", "val")` — never `os.Setenv` (auto-restored, parallel-safe).
- Pass `context.Background()` or `t.Context()` (Go 1.24+) — don't reach for `context.TODO()` in tests.

## Skipping
- `t.Skip("reason")` for environment-dependent tests (require root, network, qemu, etc.).
- Gate slow/integration tests behind `testing.Short()`:
  ```go
  if testing.Short() { t.Skip("skipping in -short mode") }
  ```

## Assertions
- Plain `if`/`t.Errorf`/`t.Fatalf`. Use `t.Errorf` to continue, `t.Fatalf` to stop.
- For struct equality: `reflect.DeepEqual` or `cmp.Diff` (already a transitive dep — verify before adding).
- Always include the **input and expected vs. got** in failure messages.

## Coverage
- Aim to keep the package at or above the repo threshold (`.coverage-threshold`).
- Cover error paths, not just the happy path. `errcheck` will catch ignored errors; tests should exercise them.

## Anti-patterns
- ❌ `time.Sleep` for synchronization — use channels or `eventually`-style polling with a deadline.
- ❌ Tests that depend on test execution order.
- ❌ Network calls to real hosts — use `httptest.Server` or fixtures under `testdata/`.
- ❌ Logging via `fmt.Println` in tests — use `t.Logf`.
