// Package unraid wraps Unraid's WebGUI HTTP API (PHP AJAX endpoints).
//
// Unraid does NOT expose a formal REST API. The WebGUI uses PHP includes
// that accept POST parameters and return JSON or HTML. This client logs in
// to the WebGUI to obtain a session cookie, then calls those endpoints.
//
// Multi-server: each server gets its own session stored by serverID.
// Handlers should prefer HTTP API for Docker/VM actions (and new features
// like disk spin, UPS, parity control) and fall back to SSH on failure.
// SSH remains the transport for terminal, SFTP, /proc/* reads, and
// real-time stats.
package unraid

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

// serverSession holds the per-server HTTP session (base URL + cookies).
type serverSession struct {
	apiBase string
	jar     *cookiejar.Jar // per-server cookie jar
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

	// POST to the login form. Unraid's login endpoint is /login.
	loginURL := base + "/login"
	form := url.Values{}
	form.Set("username", username)
	form.Set("password", password)

	resp, err := httpc.PostForm(loginURL, form)
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read body (required for connection reuse)
	io.ReadAll(resp.Body)

	// Unraid redirects to /Main on successful login (302 -> /Main).
	// If we get 200, the login page was re-rendered (bad credentials).
	// If we get 302, login succeeded.
	if resp.StatusCode == http.StatusOK && !strings.Contains(resp.Request.URL.Path, "Main") {
		return fmt.Errorf("login failed: invalid credentials (status 200, not redirected to /Main)")
	}

	// Extract cookies from the jar for our base URL
	parsedBase, _ := url.Parse(base)
	cookies := jar.Cookies(parsedBase)

	if len(cookies) == 0 {
		return fmt.Errorf("login returned no session cookies")
	}

	// Store the session with a fresh jar containing the login cookies
	sessionJar, _ := cookiejar.New(nil)
	sessionJar.SetCookies(parsedBase, cookies)

	c.mu.Lock()
	c.sessions[serverID] = &serverSession{
		apiBase: base,
		jar:     sessionJar,
	}
	c.mu.Unlock()

	logger.Infof("unraid api login success for %s (cookies: %d)", serverID, len(cookies))
	return nil
}

// HasSession returns whether we have a stored WebGUI session for the server.
func (c *Client) HasSession(serverID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.sessions[serverID]
	return ok
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

// ---------------------------------------------------------------------------
// Docker API (Events.php)
// ---------------------------------------------------------------------------

// DockerAction sends a container action via Events.php.
// action: start, stop, restart, pause, resume
// Returns the response body and status code.
func (c *Client) DockerAction(serverID, action, containerID string) ([]byte, int, error) {
	form := url.Values{}
	form.Set("action", action)
	form.Set("container", containerID)
	return c.post(serverID, "/plugins/dynamix.docker.manager/include/Events.php", form)
}

// DockerResponse is the JSON response from Events.php.
type DockerResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

// DockerActionOK calls DockerAction and returns a parsed response.
func (c *Client) DockerActionOK(serverID, action, containerID string) (*DockerResponse, error) {
	body, status, err := c.DockerAction(serverID, action, containerID)
	if err != nil {
		return nil, err
	}
	var resp DockerResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		// Some endpoints return non-JSON on success
		if status >= 200 && status < 300 {
			return &DockerResponse{Success: true}, nil
		}
		return nil, fmt.Errorf("docker action %s failed: HTTP %d, body: %s", action, status, string(body))
	}
	return &resp, nil
}

// ---------------------------------------------------------------------------
// VM API (VMajax.php)
// ---------------------------------------------------------------------------

// VMAction sends a VM action via VMajax.php.
// action: domain-start, domain-stop, domain-destroy, domain-restart, domain-pause, domain-resume
// uuid: the VM's UUID
func (c *Client) VMAction(serverID, action, uuid string) ([]byte, int, error) {
	form := url.Values{}
	form.Set("action", action)
	form.Set("uuid", uuid)
	return c.post(serverID, "/plugins/dynamix.vm.manager/include/VMajax.php", form)
}

// VMResponse is the JSON response from VMajax.php.
type VMResponse struct {
	Success bool   `json:"success"`
	State   string `json:"state"`
	Error   string `json:"error"`
}

// VMActionOK calls VMAction and returns a parsed response.
func (c *Client) VMActionOK(serverID, action, uuid string) (*VMResponse, error) {
	body, status, err := c.VMAction(serverID, action, uuid)
	if err != nil {
		return nil, err
	}
	var resp VMResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		if status >= 200 && status < 300 {
			return &VMResponse{Success: true}, nil
		}
		return nil, fmt.Errorf("vm action %s failed: HTTP %d, body: %s", action, status, string(body))
	}
	return &resp, nil
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
