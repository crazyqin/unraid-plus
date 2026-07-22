// Package handler contains all HTTP handlers for the unraid++ API.
//
// Each handler is a method on Handler so they share the SSH pool, the Unraid
// API client, the terminal hub and the resolved config. Handlers stay thin:
// they validate input, delegate to internal/ssh or internal/unraid, and shape
// the response. Heavy parsing lives in dedicated helpers next to each handler.
package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/crazyqin/unraid-plus/server/internal/config"
	"github.com/crazyqin/unraid-plus/server/internal/ssh"
	"github.com/crazyqin/unraid-plus/server/internal/unraid"
)

// Handler holds shared dependencies for every endpoint.
type Handler struct {
	pool *ssh.Pool
	ur   *unraid.Client
	hub  *ssh.TerminalHub
	cfg  *config.Config
	sm   *serverManager // v0.8+: multi-server persistence
}

// New constructs a Handler.
func New(pool *ssh.Pool, ur *unraid.Client, hub *ssh.TerminalHub, cfg *config.Config) *Handler {
	return &Handler{
		pool: pool,
		ur:   ur,
		hub:  hub,
		cfg:  cfg,
		sm:   newServerManager(cfg.DataDir),
	}
}

// ServerManager returns the server persistence manager (for auto-reconnect).
func (h *Handler) ServerManager() *serverManager {
	return h.sm
}

// Hub returns the SSH terminal hub (for WebSocket upgrade).
func (h *Handler) Hub() *ssh.TerminalHub {
	return h.hub
}

// activeClient returns the currently-connected SSH client or aborts the
// request with a 503. Used as a one-liner at the top of every data handler.
func (h *Handler) activeClient(c *gin.Context) (*ssh.Client, bool) {
	// v0.8+: check for ?serverId= parameter to support multi-server
	if id := c.Query("serverId"); id != "" && h.sm != nil {
		entry := h.sm.Get(id)
		if entry != nil {
			cli, err := h.pool.Get(entry.Host, entry.Port)
			if err == nil {
				return cli, true
			}
		}
	}
	// Fallback: return the first active connection (legacy single-server)
	cli, err := h.pool.Active()
	if err != nil {
		c.AbortWithStatusJSON(503, gin.H{
			"ok":      false,
			"message": "尚未连接到 Unraid 服务器",
			"hint":    "请先完成初始化向导 /onboarding",
		})
		return nil, false
	}
	return cli, true
}

// errOut writes a uniform {"ok":false,"message":…} error.
func errOut(c *gin.Context, status int, msg string) {
	c.AbortWithStatusJSON(status, gin.H{"ok": false, "message": msg})
}
