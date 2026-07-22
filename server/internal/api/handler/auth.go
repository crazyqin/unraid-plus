package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/your-org/unraidpp/server/internal/api/middleware"
)

// AuthHandler wraps the session store for UI authentication.
// If cfg.UIPassword is empty, all auth endpoints return {"enabled": false}
// and the middleware is a no-op — the app behaves exactly as v0.1-v0.4.
type AuthHandler struct {
	store *middleware.SessionStore
}

// NewAuthHandler creates an AuthHandler. If password is empty, the handler
// and middleware are both effectively disabled.
func NewAuthHandler(password string) *AuthHandler {
	return &AuthHandler{
		store: middleware.NewSessionStore(password),
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
//
// POST /api/auth/login  {"password": "..."}
// → 200 {"ok": true} + Set-Cookie: unraidpp_session=...
// → 401 {"ok": false, "message": "密码错误"}
// → 200 {"ok": true, "enabled": false} if auth is disabled (no password set)
func (a *AuthHandler) Login(c *gin.Context) {
	if !a.store.IsEnabled() {
		c.JSON(http.StatusOK, gin.H{"ok": true, "enabled": false, "message": "未启用认证"})
		return
	}

	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		errOut(c, http.StatusBadRequest, "请求格式错误")
		return
	}

	token := a.store.Login(req.Password)
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"ok": false, "message": "密码错误"})
		return
	}

	// Set HttpOnly cookie. Secure=false because the app is typically
	// accessed over LAN HTTP (not HTTPS). SameSite=Lax prevents CSRF
	// from cross-origin POSTs while allowing top-level navigation.
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
// current request is authenticated. The frontend uses this on boot to
// decide whether to show the login page.
//
// GET /api/auth/status
// → {"enabled": false}  — no password set, app is open
// → {"enabled": true, "authenticated": true}  — logged in
// → {"enabled": true, "authenticated": false} — needs login
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
