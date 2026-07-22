// Package middleware contains HTTP middleware for the unraid-plus API.
package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// loginAttempt tracks failed login attempts per client IP for rate limiting.
// After maxFailedAttempts (5) within the attemptWindow (5 minutes), the IP
// is blocked for blockDuration (15 minutes). This prevents brute-force
// password guessing without needing an external rate-limiter.
type loginTracker struct {
	mu       sync.Mutex
	attempts map[string]*attemptState
}

type attemptState struct {
	failures  int
	firstFail time.Time
	blockedAt time.Time
}

const (
	maxFailedAttempts = 5
	attemptWindow     = 5 * time.Minute
	blockDuration     = 15 * time.Minute
)

var loginTrackerInst = &loginTracker{
	attempts: map[string]*attemptState{},
}

// IsBlocked checks if the given IP is currently rate-limited.
func IsBlocked(ip string) bool {
	loginTrackerInst.mu.Lock()
	defer loginTrackerInst.mu.Unlock()
	s, ok := loginTrackerInst.attempts[ip]
	if !ok {
		return false
	}
	if s.blockedAt.IsZero() {
		return false
	}
	if time.Since(s.blockedAt) < blockDuration {
		return true
	}
	// Block expired — reset.
	delete(loginTrackerInst.attempts, ip)
	return false
}

// RecordFailure increments the failure count for an IP.
func RecordFailure(ip string) {
	loginTrackerInst.mu.Lock()
	defer loginTrackerInst.mu.Unlock()
	s, ok := loginTrackerInst.attempts[ip]
	if !ok {
		s = &attemptState{}
		loginTrackerInst.attempts[ip] = s
	}
	if s.firstFail.IsZero() || time.Since(s.firstFail) > attemptWindow {
		s.firstFail = time.Now()
		s.failures = 1
	} else {
		s.failures++
	}
	if s.failures >= maxFailedAttempts && s.blockedAt.IsZero() {
		s.blockedAt = time.Now()
	}
}

// RecordSuccess clears the failure history for an IP.
func RecordSuccess(ip string) {
	loginTrackerInst.mu.Lock()
	defer loginTrackerInst.mu.Unlock()
	delete(loginTrackerInst.attempts, ip)
}

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
	dataDir  string // directory for persisting the UI password
}

// NewSessionStore creates a session store. If password is empty, auth is
// disabled and AuthRequired becomes a no-op. dataDir is used to persist
// the UI password across restarts when set via API (not env var).
func NewSessionStore(password string, dataDir string) *SessionStore {
	s := &SessionStore{
		sessions: map[string]time.Time{},
		dataDir:  dataDir,
	}
	// Priority: env var password > persisted password file
	if password != "" {
		s.password = password
	} else {
		s.loadPersistedPassword()
	}
	return s
}

const (
	sessionCookieName = "unraid_plus_session"
	sessionTTL        = 24 * time.Hour
)

// IsEnabled reports whether UI authentication is active (password non-empty).
func (s *SessionStore) IsEnabled() bool {
	return s.password != ""
}

// SetPassword updates the password and persists it to disk. Existing sessions
// are NOT revoked automatically — the caller decides whether to call RevokeAll().
func (s *SessionStore) SetPassword(password string) {
	s.mu.Lock()
	s.password = password
	s.mu.Unlock()
	s.persistPassword()
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

// persistPassword writes the current password to <dataDir>/.ui_password.
// The file is mode 0600 and contains the password in plaintext (the file
// system permissions are the security boundary, same as .enc_key).
func (s *SessionStore) persistPassword() {
	s.mu.RLock()
	dir := s.dataDir
	pw := s.password
	s.mu.RUnlock()
	if dir == "" || pw == "" {
		return
	}
	path := dir + "/.ui_password"
	_ = os.WriteFile(path, []byte(pw), 0o600)
}

// loadPersistedPassword reads the UI password from <dataDir>/.ui_password.
func (s *SessionStore) loadPersistedPassword() {
	if s.dataDir == "" {
		return
	}
	path := s.dataDir + "/.ui_password"
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	s.password = strings.TrimSpace(string(data))
}
