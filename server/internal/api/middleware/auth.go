// Package middleware contains HTTP middleware for the unraid++ API.
package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// SessionStore is an in-memory store for session tokens. Each token maps
// to an expiry time. Tokens are 32-byte random hex strings set as
// HttpOnly cookies. The store is process-local — sessions don't survive
// restarts, which is the intended behavior for a single-instance app.
//
// We don't use JWT or signed cookies because:
//  1. The app is single-instance (no horizontal scaling), so in-memory is fine
//  2. We want the ability to revoke sessions (change password = clear all)
//  3. Simpler to reason about than JWT expiry + refresh
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]time.Time // token → expiry
	password string
}

// NewSessionStore creates a session store. If password is empty, auth is
// disabled and AuthRequired becomes a no-op.
func NewSessionStore(password string) *SessionStore {
	return &SessionStore{
		sessions: map[string]time.Time{},
		password: password,
	}
}

const (
	sessionCookieName = "unraidpp_session"
	sessionTTL        = 24 * time.Hour
)

// IsEnabled reports whether UI authentication is active (password non-empty).
func (s *SessionStore) IsEnabled() bool {
	return s.password != ""
}

// Login verifies the password and creates a new session. Returns the token
// to be set as a cookie. Returns "" if the password doesn't match.
func (s *SessionStore) Login(password string) string {
	if subtle.ConstantTimeCompare([]byte(password), []byte(s.password)) != 1 {
		return ""
	}

	tok := generateToken()
	s.mu.Lock()
	s.sessions[tok] = time.Now().Add(sessionTTL)
	s.mu.Unlock()
	return tok
}

// Logout removes a session token.
func (s *SessionStore) Logout(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

// IsValid checks if a token exists and hasn't expired. Expired tokens are
// cleaned up lazily during this check.
func (s *SessionStore) IsValid(token string) bool {
	if token == "" {
		return false
	}
	s.mu.RLock()
	exp, ok := s.sessions[token]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		s.mu.Lock()
		delete(s.sessions, token)
		s.mu.Unlock()
		return false
	}
	return true
}

// RevokeAll removes all sessions (used when password is changed).
func (s *SessionStore) RevokeAll() {
	s.mu.Lock()
	s.sessions = map[string]time.Time{}
	s.mu.Unlock()
}

// AuthRequired is gin middleware that blocks requests without a valid
// session cookie. If the session store's password is empty, this is a
// no-op (backwards compatible with v0.1-v0.4).
func (s *SessionStore) AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !s.IsEnabled() {
			c.Next()
			return
		}

		token, _ := c.Cookie(sessionCookieName)
		if s.IsValid(token) {
			c.Next()
			return
		}

		// Check if this is an API call or a page load. API calls get 401
		// JSON; page loads redirect to /login (the frontend route).
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"ok":      false,
				"message": "未登录或会话已过期",
				"code":    "AUTH_REQUIRED",
			})
			return
		}
		// SPA fallback — let it through so the frontend can render /login.
		c.Next()
	}
}

// generateToken creates a 32-byte random hex string.
func generateToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// CookieName returns the session cookie name (exported for the handler
// to use when setting/clearing cookies).
func (s *SessionStore) CookieName() string {
	return sessionCookieName
}

// TTL returns the session TTL for setting cookie MaxAge.
func (s *SessionStore) TTL() time.Duration {
	return sessionTTL
}
