// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Package webui embeds the compiled web UI (web/dist) into the Go binary so the
// server ships as a single artifact. The committed dist/ contains only a
// placeholder page; the real production build is produced by `cd web && npm run
// build` and copied into internal/webui/dist/ before compiling (see the build
// target). When only the placeholder is present, the API still runs — the UI is
// simply served by the Vite dev server in development instead.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Assets returns the embedded web UI file system rooted at dist/.
func Assets() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}

// HasRealBuild reports whether a real UI build (not just the placeholder) is
// embedded, so the server can decide whether to serve the SPA or defer to the
// dev server. It checks for the hashed asset bundle Vite emits.
func HasRealBuild() bool {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return false
	}
	entries, err := fs.ReadDir(sub, "assets")
	return err == nil && len(entries) > 0
}
