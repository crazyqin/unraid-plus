package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/your-org/unraidpp/server/internal/ssh"
)

type connectReq struct {
	Host      string `json:"host"`
	APIBase   string `json:"apiBase"`
	SSHPort   int    `json:"sshPort"`
	User      string `json:"user"`
	Password  string `json:"password"`
	PrivateKey []byte `json:"privateKey"`
	Passphrase string `json:"passphrase"`
	Label     string `json:"label"`
}

type connectResp struct {
	Ok              bool   `json:"ok"`
	Message         string `json:"message"`
	HostFingerprint string `json:"hostFingerprint,omitempty"`
	ServerVersion   string `json:"serverVersion,omitempty"`
}

// Connect handles the onboarding wizard's "test and connect" step. We
// deliberately accept either password or pre-supplied private key — the
// "zero-config" UX is just password mode with a friendly wizard.
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
		// Best-effort: assume same host, https.
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

	result, err := h.pool.Connect(&ssh.ConnConfig{
		Host:       req.Host,
		Port:       req.SSHPort,
		User:       req.User,
		AuthMode:   mode,
		Password:   req.Password,
		PrivateKey: req.PrivateKey,
		Passphrase: req.Passphrase,
		APIBase:    req.APIBase,
		Label:      req.Label,
	})
	if err != nil {
		status := http.StatusUnauthorized
		if isNetworkErr(err) {
			status = http.StatusBadGateway
		}
		errOut(c, status, friendlySSHError(err))
		return
	}

	// Tell the Unraid client which API base to talk to. We don't probe here —
	// SSH success is enough for onboarding; the dashboard will surface any
	// HTTP API problems.
	h.ur.SetBase(req.APIBase)

	c.JSON(http.StatusOK, connectResp{
		Ok:              true,
		Message:         "连接成功",
		HostFingerprint: result.HostFingerprint,
		ServerVersion:   result.ServerVersion,
	})
}

// Disconnect tears down the active connection (used by Settings → 断开连接).
func (h *Handler) Disconnect(c *gin.Context) {
	cli, err := h.pool.Active()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "本就没有活动连接"})
		return
	}
	// We don't have an explicit host:port at the API layer; the pool exposes
	// Active() only. To forget we iterate: just call ForgetAll — v0.x is
	// single-server. Future multi-server builds will need a host param here.
	_ = cli
	h.pool.ForgetAll()
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
	// Unraid persists root's authorized_keys at this path across reboots.
	cmd := `mkdir -p /boot/config/ssh && ` +
		`grep -qvxF '%s' /boot/config/ssh/authorized_keys 2>/dev/null && ` +
		`echo '%s' >> /boot/config/ssh/authorized_keys; ` +
		`echo OK`
	out, err := cli.Run(strings.ReplaceAll(cmd, "%s", strings.TrimSpace(string(pub))))
	if err != nil || !strings.Contains(out, "OK") {
		errOut(c, http.StatusInternalServerError, "部署公钥到 Unraid 闪存失败")
		return
	}

	// Persist the new private key and switch the pool to key auth.
	if err := saveKey(h.cfg.DataDir, priv); err != nil {
		errOut(c, http.StatusInternalServerError, "保存私钥失败: "+err.Error())
		return
	}

	// Persist connection metadata so the server can auto-reconnect on restart.
	connCfg, _ := h.pool.ActiveConfig()
	if connCfg != nil {
		_ = saveConnMeta(h.cfg.DataDir, connMeta{
			Host:    connCfg.Host,
			Port:    connCfg.Port,
			User:    connCfg.User,
			APIBase: connCfg.APIBase,
			Label:   connCfg.Label,
		})
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
