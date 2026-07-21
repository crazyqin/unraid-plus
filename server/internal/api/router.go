// Package api wires routes, middleware, and handlers into a *gin.Engine.
package api

import (
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/your-org/unraidpp/server/internal/api/handler"
	"github.com/your-org/unraidpp/server/internal/api/middleware"
	"github.com/your-org/unraidpp/server/internal/config"
	"github.com/your-org/unraidpp/server/internal/ssh"
	"github.com/your-org/unraidpp/server/internal/unraid"
	"github.com/your-org/unraidpp/server/pkg/logger"
)

// Build constructs the HTTP server.
func Build(cfg *config.Config, pool *ssh.Pool, ur *unraid.Client, hub *ssh.TerminalHub) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.RequestLogger())
	r.Use(cors.New(cors.Config{
		AllowOriginFunc:  func(string) bool { return true }, // dev only — proxy in prod
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"*"},
		AllowCredentials: true,
	}))

	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	api := r.Group("/api")
	h := handler.New(pool, ur, hub, cfg)

	// Connection / onboarding
	api.POST("/connect", h.Connect)
	api.POST("/disconnect", h.Disconnect)
	api.POST("/auth/rotate-key", h.RotateKey)

	// Dashboard
	api.GET("/dashboard", h.Dashboard)

	// Docker
	api.GET("/docker/containers", h.ListContainers)
	api.POST("/docker/containers/:id/:action", h.ContainerAction)

	// Storage
	api.GET("/storage", h.Storage)

	// Files
	api.GET("/files", h.ListFiles)
	api.POST("/files/delete", h.DeleteFiles)

	// VMs
	api.GET("/vms", h.ListVMs)
	api.POST("/vms/:id/:action", h.VMAction)

	// WebSocket: SSH terminal
	r.GET("/ws/terminal", func(c *gin.Context) {
		serveTerminal(hub, c.Writer, c.Request)
	})

	// WebSocket: Docker container logs (follow/tail configured via query).
	r.GET("/ws/docker-logs", h.DockerLogs)

	// Serve frontend SPA
	r.NoRoute(handler.SPA())

	logger.Infof("routes mounted: %d endpoints", len(api.BasePath())+1)
	return r
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func serveTerminal(hub *ssh.TerminalHub, w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Warnf("ws upgrade failed: %v", err)
		return
	}
	hub.Serve(c)
}
