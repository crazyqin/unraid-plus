package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/crazyqin/unraid-plus/server/internal/ssh"
	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

type connectReq struct {
	Host       string `json:"host"`
	APIBase    string `json:"apiBase"`
	SSHPort    int    `json:"sshPort"`
	User       string `json:"user"`
	Password   string `json:"password"`
	PrivateKey []byte `json:"privateKey"`
	Passphrase string `json:"passphrase"`
	Label      string `json:"label"`
}

type connectResp struct {
	Ok              bool   `json:"ok"`
	Message         string `json:"message"`
	HostFingerprint string `json:"hostFingerprint,omitempty"`
	ServerVersion   string `json:"serverVersion,omitempty"`
	ServerID        string `json:"serverId,omitempty"`
}

// Connect handles the onboarding wizard's "test and connect" step. We
// deliberately accept either password or pre-supplied private key — the
// "zero-config" UX is just password mode with a friendly wizard.
//
// v0.8+: After a successful connect, the server config is persisted to
// servers.json so it survives restarts and page refreshes. The frontend
// can query GET /api/servers to restore connection state.
func (h *Handler) Connect(c *gin.Context) {
	var req connectReq
	if err := c.ShouldBindJSON(&req); err != nil {
		errOut(c, http.StatusBadRequest, "请求格式错误: "+err.Error())
		return
	}
	if req.Host == "" {
		errOut(c, http.StatusBadRequest, "host 不能为空")
		return
	}
	if req.SSHPort == 0 {
		req.SSHPort = 22
	}
	if req.User == "" {
		req.User = "root"
	}
	if req.APIBase == "" {
		req.APIBase = "https://" + req.Host
	}
	if req.Password == "" && len(req.PrivateKey) == 0 {
		errOut(c, http.StatusBadRequest, "需要提供密码或私钥")
		return
	}

	mode := ssh.AuthPassword
	if len(req.PrivateKey) > 0 {
		mode = ssh.AuthKey
	}

	// Forget any prior connection to the same host — TOFU will revalidate.
	_ = h.pool.Forget(req.Host, req.SSHPort)

	connCfg := &ssh.ConnConfig{
		Host:       req.Host,
		Port:       req.SSHPort,
		User:       req.User,
		AuthMode:   mode,
		Password:   req.Password,
		PrivateKey: req.PrivateKey,
		Passphrase: req.Passphrase,
		APIBase:    req.APIBase,
		Label:      req.Label,
	}

	result, err := h.pool.Connect(connCfg)
	if err != nil {
		status := http.StatusUnauthorized
		if isNetworkErr(err) {
			status = http.StatusBadGateway
		}
		errOut(c, status, friendlySSHError(err))
		return
	}

	// Persist server config to servers.json (v0.8+)
	id := serverID(req.Host, req.SSHPort)
	if h.sm != nil {
		if err := h.sm.Upsert(connCfg, req.Password); err != nil {
			logger.Warnf("persist server %s failed: %v", id, err)
		}
		// If key auth, save the private key for auto-reconnect
		if mode == ssh.AuthKey && len(req.PrivateKey) > 0 {
			if err := h.sm.SaveServerKey(id, req.PrivateKey); err != nil {
				logger.Warnf("save key for server %s failed: %v", id, err)
			}
		}
	}

	// Tell the Unraid client which API base to talk to (backward compat).
	h.ur.SetBase(req.APIBase)

	// Attempt WebGUI login to establish an HTTP API session.
	// This enables the Unraid HTTP API channel for Docker/VM actions,
	// disk spin, UPS status, parity control, etc.
	// Login failure is not fatal — handlers will fall back to SSH.
	sid := serverID(req.Host, req.SSHPort)
	if req.Password != "" {
		go func() {
			if err := h.ur.Login(sid, req.APIBase, req.User, req.Password); err != nil {
				logger.Warnf("WebGUI login for %s failed (non-fatal, SSH fallback): %v", sid, err)
			}
		}()
	}

	c.JSON(http.StatusOK, connectResp{
		Ok:              true,
		Message:         "连接成功",
		HostFingerprint: result.HostFingerprint,
		ServerVersion:   result.ServerVersion,
		ServerID:        sid,
	})
}

// ListServers returns all saved servers and their connection status.
// The frontend calls this on boot to restore connection state.
func (h *Handler) ListServers(c *gin.Context) {
	if h.sm == nil {
		c.JSON(http.StatusOK, gin.H{"servers": []any{}})
		return
	}

	entries := h.sm.List()
	type serverInfo struct {
		ID        string `json:"id"`
		Host      string `json:"host"`
		Port      int    `json:"port"`
		User      string `json:"user"`
		AuthMode  string `json:"authMode"`
		Label     string `json:"label"`
		Connected bool   `json:"connected"`
		LastSeen  string `json:"lastSeen"`
	}

	out := make([]serverInfo, 0, len(entries))
	for _, e := range entries {
		out = append(out, serverInfo{
			ID:       e.ID,
			Host:     e.Host,
			Port:     e.Port,
			User:     e.User,
			AuthMode: e.AuthMode,
			Label:    e.Label,
			Connected: h.pool.Connected(e.Host, e.Port),
			LastSeen: e.LastSeen,
		})
	}
	c.JSON(http.StatusOK, gin.H{"servers": out})
}

// DisconnectServer tears down the SSH connection for a specific server.
func (h *Handler) DisconnectServer(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		// Fallback: disconnect all (legacy behavior)
		h.pool.ForgetAll()
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已断开所有连接"})
		return
	}

	entry := h.sm.Get(id)
	if entry == nil {
		errOut(c, http.StatusNotFound, "服务器不存在")
		return
	}
	_ = h.pool.Forget(entry.Host, entry.Port)
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已断开"})
}

// DeleteServer removes a saved server config and disconnects it.
func (h *Handler) DeleteServer(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		errOut(c, http.StatusBadRequest, "缺少服务器 ID")
		return
	}

	entry := h.sm.Get(id)
	if entry == nil {
		errOut(c, http.StatusNotFound, "服务器不存在")
		return
	}

	// Disconnect first
	_ = h.pool.Forget(entry.Host, entry.Port)

	// Also drop the HTTP API session
	h.ur.RemoveSession(id)

	// Remove from persisted config
	if err := h.sm.Delete(id); err != nil {
		errOut(c, http.StatusInternalServerError, "删除失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已删除"})
}

// ReconnectServer attempts to reconnect to a previously saved server.
func (h *Handler) ReconnectServer(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		errOut(c, http.StatusBadRequest, "缺少服务器 ID")
		return
	}

	cfg, err := h.sm.ConnConfigFor(id)
	if err != nil {
		errOut(c, http.StatusBadRequest, "无法重连: "+err.Error())
		return
	}

	// Forget existing connection if any
	_ = h.pool.Forget(cfg.Host, cfg.Port)

	result, err := h.pool.Connect(cfg)
	if err != nil {
		status := http.StatusUnauthorized
		if isNetworkErr(err) {
			status = http.StatusBadGateway
		}
		errOut(c, status, friendlySSHError(err))
		return
	}

	h.ur.SetBase(cfg.APIBase)

	// Attempt WebGUI login on reconnect too
	sid := serverID(cfg.Host, cfg.Port)
	if cfg.Password != "" {
		go func() {
			if err := h.ur.Login(sid, cfg.APIBase, cfg.User, cfg.Password); err != nil {
				logger.Warnf("WebGUI login for %s on reconnect failed (non-fatal): %v", sid, err)
			}
		}()
	}

	c.JSON(http.StatusOK, connectResp{
		Ok:              true,
		Message:         "重连成功",
		HostFingerprint: result.HostFingerprint,
		ServerID:        sid,
	})
}

// Disconnect is the legacy single-server disconnect handler.
// v0.8+: redirects to DisconnectServer with the active server ID.
func (h *Handler) Disconnect(c *gin.Context) {
	// Try to find the active server
	activeCfg, err := h.pool.ActiveConfig()
	if err == nil && activeCfg != nil {
		_ = h.pool.Forget(activeCfg.Host, activeCfg.Port)
	} else {
		h.pool.ForgetAll()
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已断开"})
}

// RotateKey generates a new ED25519 SSH key pair, installs the public key on
// the Unraid server (appending to /boot/config/ssh/authorized_keys), and
// flips the pool's auth mode to AuthKey so future connections never use the
// password. The new private key is persisted in the data dir so it survives
// restarts.
func (h *Handler) RotateKey(c *gin.Context) {
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}

	// Generate ED25519 key pair.
	pub, priv, err := genED25519()
	if err != nil {
		errOut(c, http.StatusInternalServerError, "生成密钥对失败: "+err.Error())
		return
	}

	// Append the public key to authorized_keys on the Unraid flash drive.
	// Use shellQuote to prevent command injection with crafted key material.
	keyLine := strings.TrimSpace(string(pub))
	cmd := `mkdir -p /boot/config/ssh && ` +
		`grep -qvxF ` + shellQuote(keyLine) + ` /boot/config/ssh/authorized_keys 2>/dev/null && ` +
		`echo ` + shellQuote(keyLine) + ` >> /boot/config/ssh/authorized_keys; ` +
		`echo OK`
	out, err := cli.Run(cmd)
	if err != nil || !strings.Contains(out, "OK") {
		errOut(c, http.StatusInternalServerError, "部署公钥到 Unraid 闪存失败")
		return
	}

	// Persist the new private key and switch the pool to key auth.
	connCfg, _ := h.pool.ActiveConfig()
	id := ""
	if connCfg != nil {
		id = serverID(connCfg.Host, connCfg.Port)
		if err := saveKey(h.cfg.DataDir, priv); err != nil {
			logger.Warnf("save key file for %s failed: %v", id, err)
		}
		// Also save to server-specific key file
		if h.sm != nil && id != "" {
			if err := h.sm.SaveServerKey(id, priv); err != nil {
				logger.Warnf("save server key for %s failed: %v", id, err)
			}
		}
		// Update the saved server entry to use key auth
		if h.sm != nil {
			if err := h.sm.Upsert(connCfg, ""); err != nil {
				logger.Warnf("update server entry %s to key auth failed: %v", id, err)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "已生成并部署 ED25519 密钥对，后续将使用免密连接",
	})
}

// isNetworkErr best-effort separates "can't reach host" from "auth failed".
func isNetworkErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return containsAny(s, "dial tcp", "connection refused", "i/o timeout", "no route to host", "EOF")
}

func containsAny(s string, sub ...string) bool {
	for _, x := range sub {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

// friendlySSHError massages raw x/crypto/ssh errors into Chinese hints.
func friendlySSHError(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "handshake"):
		if strings.Contains(s, "unable to authenticate") {
			return "认证失败：用户名或密码错误"
		}
		return "SSH 握手失败：" + s
	case strings.Contains(s, "connection refused"):
		return "连接被拒绝：检查 Unraid 是否在线、SSH 端口是否正确"
	case strings.Contains(s, "i/o timeout"):
		return "连接超时：检查 IP 是否可达、是否在同一个网络"
	case strings.Contains(s, "host key mismatch"):
		return "服务器指纹变更，可能存在中间人攻击，连接被拒绝"
	}
	return s
}

// genED25519 wraps ssh.ParsePrivateKey + crypto/ed25519 generate. Kept in
// keygen.go to keep this file focused on HTTP.
// saveKey is also defined in keygen.go.
