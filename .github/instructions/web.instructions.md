---
applyTo: "web/**"
---

# Web UI conventions

Use these in addition to the root `copilot-instructions.md`. The web UI (`web/`) is a
React 19 + TypeScript + Vite + Tailwind frontend, embedded into the `image-composer-tool`
Go binary via `//go:embed` and served by the `serve` subcommand (see
[web/README.md](../../web/README.md)).

## Dev loop

Run the backend and frontend separately for hot-reload:

- Terminal 1: `go run ./cmd/image-composer-tool serve --sudo`
- Terminal 2: `cd web && npm run dev`

Vite serves at `http://localhost:5173` and proxies `/api/v1` to `:8080`.

## Verification

- **Type check**: `cd web && npx tsc --noEmit` before declaring a frontend change done.
- **Verify in a browser**: for any UI change, load it via the dev server (or the rebuilt
  binary) and exercise the feature — passing `tsc`/tests does not confirm the UI actually
  works. Check the golden path and obvious edge cases.

## Full embed rebuild

Needed to test through the real Go binary, not just Vite:

```bash
(cd web && npm ci && npm run build)
rm -rf internal/webui/dist && cp -r web/dist internal/webui/dist
go build -o ./build/image-composer-tool ./cmd/image-composer-tool/
```

Hard-refresh (Ctrl/Cmd+Shift+R) after restarting `serve` to bypass the cached bundle.
