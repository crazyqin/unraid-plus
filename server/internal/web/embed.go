// Package web embeds the built frontend (web/dist) into the Go binary so the
// production runtime is a single file.
//
// During `go build` from the repo root, the Docker build copies
// `/web/dist` into `internal/web/dist`. If that directory is empty (dev build
// without a frontend), the embed still compiles — the handler just returns a
// friendly "build the frontend first" page.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Dist returns the embedded frontend filesystem rooted at the dist directory.
// Callers typically wrap with fs.Sub to strip the "dist" prefix.
func Dist() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
