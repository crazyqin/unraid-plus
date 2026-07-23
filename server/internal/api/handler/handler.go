// Package handler contains all HTTP handlers for the unraid-plus API.
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
// request with a 503. Used as a one-liner at the top of every data handler
// that requires SSH (terminal, SFTP, /proc/* reads, state files).
func (h *Handler) activeClient(c *gin.Context) (*ssh.Client, bool) {
	cli, _, ok := h.activeClientWithID(c)
	return cli, ok
}

// activeClientWithID returns the SSH client and the server ID.
// The server ID is needed for per-server HTTP API sessions.
func (h *Handler) activeClientWithID(c *gin.Context) (*ssh.Client, string, bool) {
	// v0.8+: check for ?serverId= parameter to support multi-server
	if id := c.Query("serverId"); id != "" && h.sm != nil {
		entry := h.sm.Get(id)
		if entry == nil {
			errOut(c, 404, "服务器 "+id+" 不存在")
			return nil, "", false
		}
		cli, err := h.pool.Get(entry.Host, entry.Port)
		if err != nil {
			errOut(c, 503, "服务器 "+entry.Host+" SSH 连接不可用（可能处于 API-only 模式）")
			return nil, id, false
		}
		return cli, id, true
	}
	// Fallback: return the first active connection (legacy single-server)
	cli, err := h.pool.Active()
	if err != nil {
		c.AbortWithStatusJSON(503, gin.H{
			"ok":      false,
			"message": "尚未连接到 Unraid 服务器",
			"hint":    "请先完成初始化向导 /onboarding",
		})
		return nil, "", false
	}
	// Derive serverID from the active connection config
	cfg, _ := h.pool.ActiveConfig()
	sid := "_default"
	if cfg != nil {
		sid = serverID(cfg.Host, cfg.Port)
	}
	return cli, sid, true
}

// getServerID returns the server ID for the current request.
// Unlike activeClient, this does NOT require SSH — it works in API-only mode.
// Returns ("", false) if no server context is available at all.
func (h *Handler) getServerID(c *gin.Context) (string, bool) {
	if id := c.Query("serverId"); id != "" {
		if h.sm != nil && h.sm.Get(id) != nil {
			return id, true
		}
		return "", false
	}
	// Try active SSH connection
	cfg, err := h.pool.ActiveConfig()
	if err == nil && cfg != nil {
		return serverID(cfg.Host, cfg.Port), true
	}
	// Try any persisted server
	if h.sm != nil {
		entries := h.sm.List()
		if len(entries) > 0 {
			return entries[0].ID, true
		}
	}
	return "", false
}

// hasAPISession returns whether the server has an active WebGUI session.
func (h *Handler) hasAPISession(c *gin.Context) bool {
	sid, ok := h.getServerID(c)
	if !ok {
		return false
	}
	return h.ur.HasSession(sid)
}

// errOut writes a uniform {"ok":false,"message":…} error.
func errOut(c *gin.Context, status int, msg string) {
	c.AbortWithStatusJSON(status, gin.H{"ok": false, "message": msg})
}
