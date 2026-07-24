package handler

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/crazyqin/unraid-plus/server/internal/ssh"
	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

type connectReq struct {
	// Primary: WebGUI address (required). e.g. "https://tower.local" or "192.168.1.99"
	// If just an IP/hostname without scheme, https:// is prepended.
	APIBase    string `json:"apiBase"`
	Password   string `json:"password"`
	User       string `json:"user"`       // default "root"
	Label      string `json:"label"`      // optional friendly name

	// SSH settings (optional — auto-derived from apiBase by default)
	Host       string `json:"host"`        // derived from apiBase if empty
	SSHPort    int    `json:"sshPort"`     // default 22
	PrivateKey []byte `json:"privateKey"`
	Passphrase string `json:"passphrase"`
}

type connectResp struct {
	Ok              bool   `json:"ok"`
	Message         string `json:"message"`
	HostFingerprint string `json:"hostFingerprint,omitempty"`
	ServerVersion   string `json:"serverVersion,omitempty"`
	ServerID        string `json:"serverId,omitempty"`
	SSHAvailable    bool   `json:"sshAvailable"`
	APIAvailable    bool   `json:"apiAvailable"`
}

// Connect handles the onboarding wizard's "test and connect" step.
// v0.3+: WebGUI-first connection. The user provides the Unraid server URL
// and password; we login to the WebGUI first, then try SSH in the background.
// If WebGUI login succeeds but SSH fails, the server is still usable (API-only
// mode: Docker/VM actions, disk spin, UPS, parity control all work; terminal
// and SFTP are unavailable).
func (h *Handler) Connect(c *gin.Context) {
	var req connectReq
	if err := c.ShouldBindJSON(&req); err != nil {
		errOut(c, http.StatusBadRequest, "请求格式错误: "+err.Error())
		return
	}

	// --- Normalize inputs ---
	if req.APIBase == "" && req.Host != "" {
		// Legacy: host provided but no apiBase → derive
		req.APIBase = req.Host
	}
	if req.APIBase == "" {
		errOut(c, http.StatusBadRequest, "请提供 Unraid 服务器地址（如 tower.local 或 192.168.1.99）")
		return
	}
	// Save original before normalizing (to detect explicit scheme)
	origAPIBase := req.APIBase
	// Normalize: strip scheme if user typed one, we'll try both
	apiBaseNoScheme := req.APIBase
	apiBaseNoScheme = strings.TrimPrefix(apiBaseNoScheme, "https://")
	apiBaseNoScheme = strings.TrimPrefix(apiBaseNoScheme, "http://")
	if req.User == "" {
		req.User = "root"
	}
	if req.Password == "" && len(req.PrivateKey) == 0 {
		errOut(c, http.StatusBadRequest, "请提供密码")
		return
	}

	// Derive host from apiBase for SSH
	if req.Host == "" {
		req.Host = hostFromAPIBase(req.APIBase)
	}
	if req.SSHPort == 0 {
		req.SSHPort = 22
	}

	sid := serverID(req.Host, req.SSHPort)
	resp := connectResp{Ok: true, ServerID: sid}

	// --- Step 1: WebGUI login (primary) ---
	// Try HTTPS first; if it fails with a protocol error (HTTP server on HTTPS
	// port), fall back to plain HTTP. Many home Unraid setups run HTTP only.
	apiAvailable := false
	if req.Password != "" {
		// Determine which scheme(s) to try based on user input
		var schemes []string
		if strings.HasPrefix(origAPIBase, "http://") {
			schemes = []string{"http"}
		} else if strings.HasPrefix(origAPIBase, "https://") {
			schemes = []string{"https"}
		} else {
			// No explicit scheme: try HTTPS first, fall back to HTTP
			schemes = []string{"https", "http"}
		}

		for _, scheme := range schemes {
			apiURL := scheme + "://" + apiBaseNoScheme
			if err := h.ur.Login(sid, apiURL, req.User, req.Password); err != nil {
				logger.Warnf("WebGUI login %s://%s failed: %v", scheme, apiBaseNoScheme, err)
				// If HTTPS got "HTTP response to HTTPS client", try HTTP next
				if scheme == "https" && strings.Contains(err.Error(), "HTTP response to HTTPS") {
					logger.Infof("Unraid appears to be HTTP-only, retrying with http://")
					continue
				}
			} else {
				apiAvailable = true
				req.APIBase = apiURL // remember the working URL
				break
			}
		}
	}
	resp.APIAvailable = apiAvailable

	// --- Step 1.5: Probe GraphQL API (if WebGUI login succeeded) ---
	// The official Unraid GraphQL API at /graphql is available on Unraid 7.2+.
	// Probing is fast (5s timeout) and we cache the result so handlers can
	// use GraphQL-first data fetching without re-probing on every request.
	if apiAvailable {
		go func() {
			if h.ur.ProbeGraphQL(sid) {
				logger.Infof("GraphQL API detected for %s — will use GraphQL-first data fetching", sid)
			}
		}()
	}

	// --- Step 2: SSH connect (best-effort, non-blocking) ---
	sshAvailable := false
	mode := ssh.AuthPassword
	if len(req.PrivateKey) > 0 {
		mode = ssh.AuthKey
	}

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

	// Forget any prior connection to the same host — TOFU will revalidate.
	_ = h.pool.Forget(req.Host, req.SSHPort)

	result, err := h.pool.Connect(connCfg)
	if err != nil {
		// SSH failed — not fatal if API is available
		if apiAvailable {
			logger.Infof("SSH connect for %s failed (API-only mode): %v", sid, err)
			hint := friendlySSHError(err)
			if req.SSHPort == 22 && isNetworkErr(err) {
				hint += "（默认端口 22 被使用，如 SSH 在其他端口请在高级设置中修改）"
			}
			resp.Message = "WebGUI 连接成功，SSH 连接失败（终端和文件功能不可用）" + " — " + hint
		} else {
			// Both failed — report both errors for clarity
			sshHint := friendlySSHError(err)
			if req.SSHPort == 22 && isNetworkErr(err) {
				sshHint += "（默认端口 22 被使用，如 SSH 在其他端口请在高级设置中修改）"
			}
			errOut(c, http.StatusBadGateway, "WebGUI 和 SSH 均连接失败。WebGUI: 请检查地址和密码；SSH: "+sshHint)
			return
		}
	} else {
		sshAvailable = true
		resp.HostFingerprint = result.HostFingerprint
		resp.ServerVersion = result.ServerVersion
		if !apiAvailable {
			resp.Message = "SSH 连接成功，WebGUI 登录失败（部分功能受限）"
		} else {
			resp.Message = "连接成功"
		}
	}
	resp.SSHAvailable = sshAvailable

	// Persist server config to servers.json
	if h.sm != nil {
		if err := h.sm.Upsert(connCfg, req.Password); err != nil {
			logger.Warnf("persist server %s failed: %v", sid, err)
		}
		if mode == ssh.AuthKey && len(req.PrivateKey) > 0 {
			if err := h.sm.SaveServerKey(sid, req.PrivateKey); err != nil {
				logger.Warnf("save key for server %s failed: %v", sid, err)
			}
		}
	}

	// Backward compat
	h.ur.SetBase(req.APIBase)

	c.JSON(http.StatusOK, resp)
}

// hostFromAPIBase extracts the hostname from a URL like "https://tower.local:443"
// or "192.168.1.99" or "tower.local". Returns the raw host portion (no port).
//
// Go's url.Parse treats "tower.local" (no scheme, no //) as a path, not a host.
// We prepend "//" when there's no scheme so the parser treats it as an authority.
func hostFromAPIBase(apiBase string) string {
	s := apiBase
	// If no scheme present, prepend "//" so url.Parse sees it as an authority.
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		s = "//" + s
	}
	u, err := url.Parse(s)
	if err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	// Ultimate fallback: strip scheme, then take everything before the first ":"
	s = strings.TrimPrefix(apiBase, "https://")
	s = strings.TrimPrefix(s, "http://")
	return strings.SplitN(s, ":", 2)[0]
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
		ID           string `json:"id"`
		Host         string `json:"host"`
		Port         int    `json:"port"`
		User         string `json:"user"`
		AuthMode     string `json:"authMode"`
		Label        string `json:"label"`
		Connected    bool   `json:"connected"`
		SSHAvailable bool   `json:"sshAvailable"`
		APIAvailable bool   `json:"apiAvailable"`
		LastSeen     string `json:"lastSeen"`
	}

	out := make([]serverInfo, 0, len(entries))
	for _, e := range entries {
		sid := e.ID
		sshOK := h.pool.Connected(e.Host, e.Port)
		apiOK := h.ur.HasSession(sid)
		out = append(out, serverInfo{
			ID:           sid,
			Host:         e.Host,
			Port:         e.Port,
			User:         e.User,
			AuthMode:     e.AuthMode,
			Label:        e.Label,
			// Connected if either transport works (API-only is still "online")
			Connected:    sshOK || apiOK,
			SSHAvailable: sshOK,
			APIAvailable: apiOK,
			LastSeen:     e.LastSeen,
		})
	}
	c.JSON(http.StatusOK, gin.H{"servers": out})
}

// DisconnectServer tears down the connection for a specific server.
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
	h.ur.RemoveSession(id)
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
// v0.3+: Tries WebGUI login first, then SSH. If SSH fails but API works,
// server is still usable in API-only mode.
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

	// WebGUI login — try with stored apiBase first, fall back HTTP if HTTPS fails
	apiAvailable := false
	if cfg.Password != "" {
		apiBase := cfg.APIBase
		if err := h.ur.Login(id, apiBase, cfg.User, cfg.Password); err != nil {
			logger.Warnf("WebGUI login for %s on reconnect failed: %v", id, err)
			// If HTTPS failed due to HTTP server, try HTTP
			if strings.HasPrefix(apiBase, "https://") && strings.Contains(err.Error(), "HTTP response to HTTPS") {
				httpBase := "http://" + strings.TrimPrefix(apiBase, "https://")
				logger.Infof("Reconnect: retrying with %s", httpBase)
				if err2 := h.ur.Login(id, httpBase, cfg.User, cfg.Password); err2 != nil {
					logger.Warnf("WebGUI login (http) for %s on reconnect also failed: %v", id, err2)
				} else {
					apiAvailable = true
					cfg.APIBase = httpBase
				}
			}
		} else {
			apiAvailable = true
		}
	}

	// Probe GraphQL API if WebGUI login succeeded
	if apiAvailable {
		go func() {
			if h.ur.ProbeGraphQL(id) {
				logger.Infof("GraphQL API detected for %s on reconnect", id)
			}
		}()
	}

	// SSH connect
	_ = h.pool.Forget(cfg.Host, cfg.Port)
	result, sshErr := h.pool.Connect(cfg)
	sshAvailable := sshErr == nil

	if !apiAvailable && !sshAvailable {
		status := http.StatusBadGateway
		if isNetworkErr(sshErr) {
			status = http.StatusBadGateway
		}
		errOut(c, status, friendlySSHError(sshErr))
		return
	}

	h.ur.SetBase(cfg.APIBase)

	resp := connectResp{
		Ok:           true,
		ServerID:     id,
		APIAvailable: apiAvailable,
		SSHAvailable: sshAvailable,
	}

	if sshAvailable {
		resp.HostFingerprint = result.HostFingerprint
		resp.Message = "重连成功"
	} else {
		resp.Message = "WebGUI 重连成功，SSH 失败（终端和文件功能不可用）"
	}

	c.JSON(http.StatusOK, resp)
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
	case strings.Contains(s, "no such host"):
		return "DNS 解析失败：无法找到主机，请检查服务器地址是否正确"
	case strings.Contains(s, "host key mismatch"):
		return "服务器指纹变更，可能存在中间人攻击，连接被拒绝"
	}
	return s
}

// genED25519 wraps ssh.ParsePrivateKey + crypto/ed25519 generate. Kept in
// keygen.go to keep this file focused on HTTP.
// saveKey is also defined in keygen.go.
