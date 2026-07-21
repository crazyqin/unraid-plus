// Package unraid wraps Unraid's own HTTP API (the JSON endpoints under
// /webgui/api/*) so handlers don't have to know the wire format. In v0.x we
// mostly hit SSH for live stats; this client is a thin shim for the cases
// where the WebGUI API is more convenient (e.g. listing VMs through libvirt).
package unraid

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/your-org/unraidpp/server/internal/config"
	"github.com/your-org/unraidpp/server/pkg/logger"
)

// Client is the Unraid HTTP API client. It carries the user's session cookie
// once authenticated; in v0.x we mostly fall back to SSH for stats so this
// client is intentionally minimal.
type Client struct {
	cfg     *config.Config
	httpc   *http.Client
	cookies []*http.Cookie
	apiBase string
}

// NewClient constructs an Unraid API client. The actual base URL is provided
// per-request via Connect() (because the user supplies it during onboarding).
func NewClient(cfg *config.Config) *Client {
	return &Client{
		cfg: cfg,
		httpc: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SetBase updates the API base used for subsequent requests. Called by the
// connect handler with the user-supplied URL.
func (c *Client) SetBase(base string) {
	c.apiBase = strings.TrimRight(base, "/")
}

// Base returns the currently configured API base.
func (c *Client) Base() string { return c.apiBase }

// SetCookies stores session cookies after a successful login.
func (c *Client) SetCookies(cks []*http.Cookie) { c.cookies = cks }

// get performs a GET to /webgui/api/<path> and JSON-decodes into v.
// In v0.x this is mostly a placeholder; the real VM/Docker lists come from
// SSH commands like `docker ps` and `virsh list`.
func (c *Client) get(path string, v any) error {
	if c.apiBase == "" {
		return fmt.Errorf("unraid api base not set")
	}
	url := c.apiBase + "/webgui/api/" + strings.TrimLeft(path, "/")
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unraid api %s: %d %s", path, resp.StatusCode, string(body))
	}
	if v == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// Probe does a lightweight GET to confirm the API is reachable. Returns no
// error if the host simply responds (even with 401).
func (c *Client) Probe() error {
	if c.apiBase == "" {
		return fmt.Errorf("unraid api base not set")
	}
	req, _ := http.NewRequest(http.MethodGet, c.apiBase+"/", nil)
	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	logger.Debugf("unraid probe %s -> %d", c.apiBase, resp.StatusCode)
	return nil
}
