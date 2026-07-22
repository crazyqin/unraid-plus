// Package api wires routes, middleware, and handlers into a *gin.Engine.
package api

import (
	"net/http"
	"time"

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

// Version and StartTime are set by main.go at startup. They default to "dev"
// and the process start time so the health endpoint works standalone.
var (
	Version   = "dev"
	StartTime = time.Now()
)

// Build constructs the HTTP server (legacy — creates its own handler).
func Build(cfg *config.Config, pool *ssh.Pool, ur *unraid.Client, hub *ssh.TerminalHub) http.Handler {
	h := handler.New(pool, ur, hub, cfg)
	return buildRouter(cfg, h)
}

// BuildWithHandler constructs the HTTP server using a pre-created handler.
func BuildWithHandler(cfg *config.Config, pool *ssh.Pool, ur *unraid.Client, hub *ssh.TerminalHub, h *handler.Handler) http.Handler {
	return buildRouter(cfg, h)
}

func buildRouter(cfg *config.Config, h *handler.Handler) http.Handler {
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

	// UI authentication (v0.5). If UNRAIDPP_UI_PASSWORD is unset, the
	// middleware is a no-op and the app behaves as v0.1-v0.4 (no login).
	authH := handler.NewAuthHandler(cfg.UIPassword, cfg.DataDir)
	authStore := authH.Store()

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"ok":      true,
			"version": Version,
			"uptime":  int(time.Since(StartTime).Seconds()),
		})
	})

	// Auth routes are registered BEFORE the auth middleware group so they
	// remain accessible without a session. /api/auth/status is public so
	// the frontend can probe whether login is required on boot.
	r.POST("/api/auth/login", authH.Login)
	r.POST("/api/auth/logout", authH.Logout)
	r.GET("/api/auth/status", authH.AuthStatus)
	r.POST("/api/auth/setup", authH.SetupPassword)
	r.POST("/api/auth/change-password", authH.ChangePassword)

	// All /api/* routes below require a valid session cookie IF
	// UNRAIDPP_UI_PASSWORD is set. If unset, AuthRequired is a no-op.
	api := r.Group("/api")
	api.Use(authStore.AuthRequired())

	// h is the handler passed from main.go (needed for auto-reconnect).

	// Connection / onboarding
	api.POST("/connect", h.Connect)
	api.POST("/disconnect", h.Disconnect)
	api.POST("/auth/rotate-key", h.RotateKey)

	// Server management (v0.8+): multi-server persistence
	api.GET("/servers", h.ListServers)
	api.POST("/servers/:id/reconnect", h.ReconnectServer)
	api.DELETE("/servers/:id", h.DeleteServer)

	// Dashboard
	api.GET("/dashboard", h.Dashboard)

	// Docker
	api.GET("/docker/containers", h.ListContainers)
	api.POST("/docker/containers/:id/:action", h.ContainerAction)
	api.GET("/docker/stats", h.DockerStats)

	// Storage
	api.GET("/storage", h.Storage)

	// Array control (v0.5): start/stop the Unraid array + parity check.
	api.POST("/storage/array/:action", h.ArrayAction)
	api.POST("/storage/parity/:action", h.ParityCheckAction)
	api.GET("/storage/parity-status", h.ParityStatus)

	// SMART cache invalidation (manual refresh button on the Storage page).
	api.POST("/smart/refresh", h.SmartRefresh)

	// Files (v0.5: upload/download/rename/mkdir; v0.6: preview)
	api.GET("/files", h.ListFiles)
	api.GET("/files/preview", h.PreviewFile)
	api.GET("/files/download", h.DownloadFile)
	api.POST("/files/upload", h.UploadFile)
	api.POST("/files/delete", h.DeleteFiles)
	api.POST("/files/rename", h.RenameFile)
	api.POST("/files/mkdir", h.MkdirFile)

	// VMs
	api.GET("/vms", h.ListVMs)
	api.POST("/vms/:id/:action", h.VMAction)

	// WebSocket: SSH terminal (also gated by auth if enabled)
	r.GET("/ws/terminal", authStore.AuthRequired(), func(c *gin.Context) {
		serveTerminal(h.Hub(), c.Writer, c.Request)
	})

	// WebSocket: Docker container logs
	r.GET("/ws/docker-logs", authStore.AuthRequired(), h.DockerLogs)

	// Serve frontend SPA
	r.NoRoute(handler.SPA())

	if authStore.IsEnabled() {
		logger.Infof("UI authentication enabled (UNRAIDPP_UI_PASSWORD set)")
	} else {
		logger.Infof("UI authentication disabled (set UNRAIDPP_UI_PASSWORD to enable)")
	}
	logger.Infof("routes mounted")
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
