# ICT Web UI

React 19 + TypeScript + Vite frontend for the Image Composer Tool web UI.

## Development

Start the Vite dev server (proxies `/api/v1` to the Go backend on `:8080`):

```bash
npm ci
npm run dev
# UI available at http://localhost:5173
```

The Go backend must be running separately:

```bash
# From repo root
go run ./cmd/image-composer-tool serve
```

## Building for embedding

The compiled assets are embedded into the Go binary via `//go:embed` in
`internal/webui/embed.go`. Build and stage them before compiling the binary:

```bash
npm run build                                        # outputs to web/dist/
rm -rf ../internal/webui/dist
cp -r dist ../internal/webui/dist
```

Then from the repo root:

```bash
go build -o ./build/image-composer-tool ./cmd/image-composer-tool/
```

## Type checking

```bash
npx tsc --noEmit
```
