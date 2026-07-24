// Package unraid wraps Unraid's WebGUI HTTP session management and official
// GraphQL API client.
//
// Authentication flow: POST /login to obtain a PHPSESSID session cookie and
// csrf_token cookie, then use those cookies for GraphQL API requests.
// Data priority: GraphQL > SSH. HTML scraping is completely removed.
//
// Multi-server: each server gets its own session stored by serverID.
package unraid

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

// csrfTokenRe extracts Unraid's CSRF token from WebGUI HTML/JS.
// Matches: csrf_token="...", csrf_token: "...", "csrf_token":"...", var csrf_token = "..."
var csrfTokenRe = regexp.MustCompile(`(?i)csrf_token["'\s:=]+["']([A-Za-z0-9]+)["']`)

// serverSession holds the per-server HTTP session (base URL + cookies).
type serverSession struct {
	apiBase           string
	jar               *cookiejar.Jar // per-server cookie jar
	apiKey            string         // optional x-api-key for GraphQL auth
	csrfToken         string         // csrf_token cookie value for GraphQL requests
	graphqlAvailable  bool           // set true after ProbeGraphQL succeeds
}

// Client is the Unraid HTTP API client. It manages per-server sessions
// so multiple Unraid servers can be connected simultaneously.
type Client struct {
	httpc *http.Client // shared transport, per-request cookie jars
	mu    sync.RWMutex
	sessions map[string]*serverSession // keyed by serverID ("host:port")
	timeout  time.Duration
}

// NewClient constructs an Unraid API client.
func NewClient() *Client {
	return &Client{
		sessions: make(map[string]*serverSession),
		timeout:  15 * time.Second,
	}
}

// Login authenticates to the Unraid WebGUI and stores the session.
// The WebGUI login is a standard HTML form POST to /login with
// username + password. On success, the server sets a session cookie
// (PHPSESSID) that we capture in the per-server cookie jar.
//
// Returns nil on success. A failed login is not fatal — handlers
// will fall back to SSH. The user may need to set the WebGUI password
// separately from the SSH password (they're the same by default on Unraid).
func (c *Client) Login(serverID, apiBase, username, password string) error {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return fmt.Errorf("create cookie jar: %w", err)
	}

	// Skip TLS verification for self-signed Unraid certs
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	httpc := &http.Client{
		Timeout:   c.timeout,
		Transport: transport,
		Jar:       jar,
		// Don't auto-follow redirects — we want to capture the login response
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	defer httpc.CloseIdleConnections()

	base := strings.TrimRight(apiBase, "/")

	// Step 1: GET /login to obtain a PHPSESSID cookie.
	// Unraid uses PHP sessions — the login form validation requires an active
	// session. Without this step, POST /login always returns 200 (re-renders
	// the login page with an error) because there's no session to authenticate against.
	loginPageURL := base + "/login"
	resp, err := httpc.Get(loginPageURL)
	if err != nil {
		if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline") {
			return fmt.Errorf("连接超时：无法访问 %s，请检查服务器地址和网络", base)
		}
		if strings.Contains(err.Error(), "connection refused") {
			return fmt.Errorf("连接被拒绝：%s 未响应，请检查 WebGUI 是否启用", base)
		}
		return fmt.Errorf("无法访问 %s：%w", base, err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	// Step 2: POST to the login form with the session cookie now in the jar.
	loginURL := base + "/login"
	form := url.Values{}
	form.Set("username", username)
	form.Set("password", password)

	resp, err = httpc.PostForm(loginURL, form)
	if err != nil {
		if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline") {
			return fmt.Errorf("登录请求超时：%s 响应过慢", base)
		}
		return fmt.Errorf("登录请求失败：%w", err)
	}
	defer resp.Body.Close()

	// Read body (required for connection reuse)
	io.ReadAll(resp.Body)

	// Unraid redirects on successful login (302 -> /Dashboard or /Main).
	// If we get 200, the login page was re-rendered (bad credentials).
	// If we get 302, login succeeded.
	// Note: Unraid 7.x redirects to /Dashboard; older versions use /Main.
	finalPath := resp.Request.URL.Path
	if resp.StatusCode == http.StatusOK && !strings.Contains(finalPath, "Dashboard") && !strings.Contains(finalPath, "Main") {
		return fmt.Errorf("登录失败：用户名或密码错误（状态 200，未重定向）")
	}

	// Extract cookies from the jar for our base URL
	parsedBase, _ := url.Parse(base)
	cookies := jar.Cookies(parsedBase)

	// Look for csrf_token in cookies (name may vary: csrf_token, unraid_csrf_token, etc.)
	var csrfToken string
	for _, ck := range cookies {
		name := strings.ToLower(ck.Name)
		if name == "csrf_token" || strings.Contains(name, "csrf") {
			csrfToken = ck.Value
			break
		}
	}

	if len(cookies) == 0 {
		// Some Unraid versions may return 302 without setting cookies visible
		// to the jar. If we got redirected to /Dashboard, login likely succeeded
		// -- treat it as success and create a minimal session.
		if strings.Contains(finalPath, "Dashboard") || strings.Contains(finalPath, "Main") {
			logger.Infof("unraid api login redirect without cookies for %s, creating session anyway", serverID)
		} else {
			return fmt.Errorf("登录返回无会话 Cookie，请确认 WebGUI 正常运行")
		}
	}

	// Store the session with a fresh jar containing the login cookies
	sessionJar, _ := cookiejar.New(nil)
	sessionJar.SetCookies(parsedBase, cookies)

	c.mu.Lock()
	c.sessions[serverID] = &serverSession{
		apiBase:   base,
		jar:       sessionJar,
		csrfToken: csrfToken,
	}
	c.mu.Unlock()

	// Official GraphQL API validates X-CSRF-Token against emhttp.var.csrfToken.
	// The token is usually embedded in WebGUI HTML after login, not always a cookie.
	// Fetch Dashboard and scrape it when cookie capture failed.
	if csrfToken == "" {
		if tok := c.scrapeCSRFToken(serverID); tok != "" {
			csrfToken = tok
			c.mu.Lock()
			if sess := c.sessions[serverID]; sess != nil {
				sess.csrfToken = tok
			}
			c.mu.Unlock()
		}
	}

	if csrfToken != "" {
		logger.Infof("unraid api login success for %s (cookies: %d, csrf_token captured)", serverID, len(cookies))
	} else {
		logger.Infof("unraid api login success for %s (cookies: %d, no csrf_token found — GraphQL may need API key)", serverID, len(cookies))
	}
	return nil
}

// scrapeCSRFToken GETs a WebGUI page and extracts csrf_token from HTML/JS.
// Required for session-cookie GraphQL auth when the token is not in cookies.
func (c *Client) scrapeCSRFToken(serverID string) string {
	sess := c.getSession(serverID)
	if sess == nil {
		return ""
	}

	// Prefer Dashboard (post-login landing); fall back to Main / root.
	for _, path := range []string{"/Dashboard", "/Main", "/"} {
		body, status, err := c.get(serverID, path)
		if err != nil || status != http.StatusOK {
			continue
		}
		if m := csrfTokenRe.FindSubmatch(body); len(m) >= 2 {
			tok := string(m[1])
			// Reject placeholder / empty tokens from sample templates
			if tok != "" && tok != "0000000000000000" {
				logger.Infof("csrf_token scraped from %s for %s", path, serverID)
				return tok
			}
		}
	}
	return ""
}

// SetCSRFToken sets the CSRF token for a server session (e.g. from SSH var.ini).
func (c *Client) SetCSRFToken(serverID, token string) {
	if token == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if sess, ok := c.sessions[serverID]; ok {
		sess.csrfToken = token
	}
}

// HasSession returns whether we have a stored WebGUI session for the server.
func (c *Client) HasSession(serverID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.sessions[serverID]
	return ok
}

// GetSession returns the API base URL for a server's WebGUI session.
// Returns empty string if no session exists.
func (c *Client) GetSession(serverID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if s, ok := c.sessions[serverID]; ok {
		return s.apiBase
	}
	return ""
}

// RemoveSession drops the stored session for a server (e.g. on disconnect).
func (c *Client) RemoveSession(serverID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.sessions, serverID)
}

// getSession returns the server session or nil.
func (c *Client) getSession(serverID string) *serverSession {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessions[serverID]
}

// doRequest performs an HTTP request to a server's WebGUI endpoint.
// The path should be relative (e.g. "/plugins/dynamix.docker.manager/include/Events.php").
func (c *Client) doRequest(serverID, method, path string, form url.Values) ([]byte, int, error) {
	sess := c.getSession(serverID)
	if sess == nil {
		return nil, 0, fmt.Errorf("no api session for server %s", serverID)
	}

	fullURL := sess.apiBase + path
	var bodyStr string
	if form != nil {
		bodyStr = form.Encode()
	}

	req, err := http.NewRequest(method, fullURL, strings.NewReader(bodyStr))
	if err != nil {
		return nil, 0, err
	}

	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("X-Requested-With", "XMLHttpRequest") // Unraid AJAX check

	// Attach cookies from the per-server jar
	parsedURL, _ := url.Parse(fullURL)
	if sess.jar != nil {
		for _, ck := range sess.jar.Cookies(parsedURL) {
			req.AddCookie(ck)
		}
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	httpc := &http.Client{
		Timeout:   c.timeout,
		Transport: transport,
	}
	defer httpc.CloseIdleConnections()

	resp, err := httpc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	// Update cookies in session jar from response
	if sess.jar != nil {
		parsedURL2, _ := url.Parse(fullURL)
		sess.jar.SetCookies(parsedURL2, resp.Cookies())
	}

	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}

// post performs a POST to a WebGUI endpoint with form data.
func (c *Client) post(serverID, path string, form url.Values) ([]byte, int, error) {
	return c.doRequest(serverID, http.MethodPost, path, form)
}

// get performs a GET to a WebGUI endpoint.
func (c *Client) get(serverID, path string) ([]byte, int, error) {
	return c.doRequest(serverID, http.MethodGet, path, nil)
}

// makeTransport creates an HTTP transport that skips TLS verification
// (Unraid uses self-signed certs by default).
func makeTransport() *http.Transport {
	return &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
}

// ---------------------------------------------------------------------------
// Disk Spin API (ToggleState.php)
// ---------------------------------------------------------------------------

// DiskSpin sends a spin up/down command via ToggleState.php.
// device: e.g. "sdb", "sdall" (all disks)
// action: "spinup" or "spindown"
func (c *Client) DiskSpin(serverID, device, action string) ([]byte, int, error) {
	form := url.Values{}
	form.Set("device", device)
	form.Set("action", action)
	return c.post(serverID, "/plugins/dynamix/include/ToggleState.php", form)
}

// ---------------------------------------------------------------------------
// UPS Status API (UPSstatus.php)
// ---------------------------------------------------------------------------

// UPSStatus fetches the UPS status via UPSstatus.php.
// Returns raw HTML/JSON body (Unraid's UPS page is HTML-based).
func (c *Client) UPSStatus(serverID string) ([]byte, int, error) {
	return c.get(serverID, "/plugins/dynamix.apcupsd/include/UPSstatus.php")
}

// ---------------------------------------------------------------------------
// Parity Control API (ParityControl.php)
// ---------------------------------------------------------------------------

// ParityControl sends a parity control action.
// Valid actions: "start", "resume", "cancel", "correcting" (check with corrections)
func (c *Client) ParityControl(serverID, action string) ([]byte, int, error) {
	form := url.Values{}
	form.Set("action", action)
	return c.post(serverID, "/plugins/dynamix/include/ParityControl.php", form)
}

// ---------------------------------------------------------------------------
// SMART Info API (SmartInfo.php)
// ---------------------------------------------------------------------------

// SMARTInfo fetches SMART info for a device via SmartInfo.php.
// cmd: "info" (get SMART data)
// name: device identifier (e.g. "sdb")
// port: device port
func (c *Client) SMARTInfo(serverID, cmd, name, port string) ([]byte, int, error) {
	form := url.Values{}
	form.Set("cmd", cmd)
	form.Set("name", name)
	if port != "" {
		form.Set("port", port)
	}
	return c.post(serverID, "/plugins/dynamix/include/SmartInfo.php", form)
}

// ---------------------------------------------------------------------------
// Share Management API
// ---------------------------------------------------------------------------

// ShareList fetches the share list via ShareList.php.
func (c *Client) ShareList(serverID string) ([]byte, int, error) {
	return c.get(serverID, "/plugins/dynamix/include/ShareList.php")
}

// ShareData fetches share details via ShareData.php.
// share: the share name
func (c *Client) ShareData(serverID, share string) ([]byte, int, error) {
	form := url.Values{}
	form.Set("share", share)
	return c.post(serverID, "/plugins/dynamix/include/ShareData.php", form)
}

// ---------------------------------------------------------------------------
// System Info API
// ---------------------------------------------------------------------------

// SystemLog fetches the system log via the WebGUI.
// The syslog is at /var/log/syslog on Unraid, accessible through the WebGUI.
func (c *Client) SystemLog(serverID string) ([]byte, int, error) {
	return c.get(serverID, "/plugins/dynamix/include/Syslog.php")
}

// Probe does a lightweight GET to confirm the API is reachable.
func (c *Client) Probe(serverID string) error {
	sess := c.getSession(serverID)
	if sess == nil {
		return fmt.Errorf("no api session for server %s", serverID)
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	httpc := &http.Client{
		Timeout:   5 * time.Second,
		Transport: transport,
	}
	defer httpc.CloseIdleConnections()

	req, _ := http.NewRequest(http.MethodGet, sess.apiBase+"/", nil)
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	logger.Debugf("unraid probe %s -> %d", sess.apiBase, resp.StatusCode)
	return nil
}

// Keep old methods for backward compatibility during migration
// SetBase / Base are no-ops for now but prevent compile errors

// SetBase is kept for backward compat. Does nothing — sessions are per-server now.
func (c *Client) SetBase(base string) {}

// Base returns empty string — sessions are per-server now.
func (c *Client) Base() string { return "" }

// SetCookies is kept for backward compat. Does nothing.
func (c *Client) SetCookies(cks []*http.Cookie) {}


