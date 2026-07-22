package handler

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/crazyqin/unraid-plus/server/internal/web"
	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

// SPA returns a gin.HandlerFunc that serves the embedded React SPA.
// Anything that isn't an API/WS route and isn't a real file falls back to
// index.html so client-side routing works on refresh.
func SPA() gin.HandlerFunc {
	dist, err := web.Dist()
	if err != nil {
		logger.Warnf("frontend embed unavailable: %v", err)
		return func(c *gin.Context) {
			c.Data(http.StatusServiceUnavailable, "text/plain; charset=utf-8",
				[]byte("frontend not bundled; run `pnpm build` in web/ then rebuild the server\n"))
		}
	}
	fileServer := http.FileServer(http.FS(dist))

	return func(c *gin.Context) {
		path := strings.TrimPrefix(c.Request.URL.Path, "/")

		// Try the file directly first.
		if path != "" {
			if _, err := fs.Stat(dist, path); err == nil {
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
		}
		// Fall back to index.html for client-side routes.
		c.Request.URL.Path = "/"
		fileServer.ServeHTTP(c.Writer, c.Request)
	}
}
