package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/crazyqin/unraid-plus/server/internal/api/middleware"
)

// If cfg.UIPassword is empty, all auth endpoints return {"enabled": false}
// and the middleware is a no-op — the app behaves exactly as v0.1-v0.4.
// AuthHandler wraps the session store for UI authentication.
// If cfg.UIPassword is empty, all auth endpoints return {"enabled": false}
// and the middleware is a no-op — the app behaves exactly as v0.1-v0.4.
type AuthHandler struct {
	store *middleware.SessionStore
}

// NewAuthHandler creates an AuthHandler. If password is empty, the handler
// and middleware are both effectively disabled.
func NewAuthHandler(password string, dataDir string) *AuthHandler {
	return &AuthHandler{
		store: middleware.NewSessionStore(password, dataDir),
	}
}

// Store exposes the underlying SessionStore so the router can register
// the AuthRequired middleware.
func (a *AuthHandler) Store() *middleware.SessionStore {
	return a.store
}

type loginReq struct {
	Password string `json:"password"`
}

// Login verifies the password and sets a session cookie.
func (a *AuthHandler) Login(c *gin.Context) {
	if !a.store.IsEnabled() {
		c.JSON(http.StatusOK, gin.H{"ok": true, "enabled": false, "message": "未启用认证"})
		return
	}

	clientIP := c.ClientIP()
	if middleware.IsBlocked(clientIP) {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"ok":      false,
			"message": "登录尝试过多，请 15 分钟后再试",
		})
		return
	}

	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		errOut(c, http.StatusBadRequest, "请求格式错误")
		return
	}

	token := a.store.Login(req.Password)
	if token == "" {
		middleware.RecordFailure(clientIP)
		c.JSON(http.StatusUnauthorized, gin.H{"ok": false, "message": "密码错误"})
		return
	}

	middleware.RecordSuccess(clientIP)
	c.SetCookie(a.store.CookieName(), token, int(a.store.TTL().Seconds()), "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"ok": true, "enabled": true, "message": "登录成功"})
}

// Logout clears the session cookie.
func (a *AuthHandler) Logout(c *gin.Context) {
	token, _ := c.Cookie(a.store.CookieName())
	a.store.Logout(token)
	c.SetCookie(a.store.CookieName(), "", -1, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已登出"})
}

// AuthStatus reports whether UI authentication is enabled and whether the
// current request is authenticated.
func (a *AuthHandler) AuthStatus(c *gin.Context) {
	if !a.store.IsEnabled() {
		c.JSON(http.StatusOK, gin.H{"enabled": false, "authenticated": true})
		return
	}
	token, _ := c.Cookie(a.store.CookieName())
	c.JSON(http.StatusOK, gin.H{
		"enabled":       true,
		"authenticated": a.store.IsValid(token),
	})
}

// SetupPassword sets the UI password for the first time (when auth is
// currently disabled). After setting, the caller is automatically logged in.
//
// POST /api/auth/setup  {"password": "..."}
// → 200 {"ok": true} + session cookie
// → 409 {"ok": false} if password already set
func (a *AuthHandler) SetupPassword(c *gin.Context) {
	if a.store.IsEnabled() {
		errOut(c, http.StatusConflict, "密码已设置，请使用修改密码功能")
		return
	}
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Password == "" {
		errOut(c, http.StatusBadRequest, "密码不能为空")
		return
	}
	if len(req.Password) < 4 {
		errOut(c, http.StatusBadRequest, "密码至少 4 位")
		return
	}
	a.store.SetPassword(req.Password)
	// Auto-login after setup
	token := a.store.Login(req.Password)
	c.SetCookie(a.store.CookieName(), token, int(a.store.TTL().Seconds()), "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"ok": true, "enabled": true, "message": "密码设置成功"})
}

// ChangePassword changes the UI password. Requires the current password
// to be provided. All existing sessions are revoked.
//
// POST /api/auth/change-password  {"current": "...", "new": "..."}
// → 200 {"ok": true}
func (a *AuthHandler) ChangePassword(c *gin.Context) {
	if !a.store.IsEnabled() {
		errOut(c, http.StatusConflict, "尚未设置密码，请使用初始化设置")
		return
	}
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errOut(c, http.StatusBadRequest, "请求格式错误")
		return
	}
	// Verify current password
	token := a.store.Login(req.Current)
	if token == "" {
		errOut(c, http.StatusUnauthorized, "当前密码错误")
		return
	}
	if len(req.New) < 4 {
		errOut(c, http.StatusBadRequest, "新密码至少 4 位")
		return
	}
	// Revoke all sessions, set new password, auto-login
	a.store.RevokeAll()
	a.store.SetPassword(req.New)
	newToken := a.store.Login(req.New)
	c.SetCookie(a.store.CookieName(), newToken, int(a.store.TTL().Seconds()), "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "密码已修改"})
}
