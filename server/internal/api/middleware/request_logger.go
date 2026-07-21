// Package middleware contains HTTP middleware shared across routes.
package middleware

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/your-org/unraidpp/server/pkg/logger"
)

// RequestLogger logs one line per request, skipping /health noise at info level.
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/health" {
			c.Next()
			return
		}
		start := time.Now()
		c.Next()
		logger.Infof("%s %s %d %s",
			c.Request.Method,
			c.Request.URL.Path,
			c.Writer.Status(),
			time.Since(start).Round(time.Millisecond),
		)
	}
}
