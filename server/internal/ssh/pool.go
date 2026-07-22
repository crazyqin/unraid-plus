// Package ssh wraps golang.org/x/crypto/ssh to provide:
//   - a Pool that holds per-server connections (password or key auth)
//   - host-key fingerprinting with a trust-on-first-use known_hosts cache
//   - command execution + SFTP sessions
//   - a TerminalHub that bridges browser WebSockets to interactive SSH shells
package ssh

import (
	"errors"
	"fmt"
	"sync"
)

// Pool manages SSH connections to one or more Unraid servers. In the common
// case it holds exactly one (the currently configured server). The map is
// keyed by `host:port` so multi-server support is mostly free.
type Pool struct {
	mu       sync.RWMutex
	conns    map[string]*managedConn
	knownHosts *knownHosts
	dataDir  string
}

type managedConn struct {
	cfg      *ConnConfig
	conn     *Client
	// when non-empty, the pool can re-establish connections automatically
	// using this credential material (password or private key).
	password   []byte
	privateKey []byte
}

// ConnConfig describes how to reach and authenticate to an SSH server.
type ConnConfig struct {
	Host      string
	Port      int
	User      string
	AuthMode  AuthMode
	Password  string
	PrivateKey []byte
	Passphrase string

	APIBase string // Unraid HTTP API base, e.g. https://tower.local
	Label   string
}

// AuthMode selects the authentication strategy.
type AuthMode int

const (
	AuthPassword AuthMode = iota
	AuthKey
)

// NewPool constructs an empty pool. dataDir is where the trusted-hosts cache
// and any generated key pair are persisted.
func NewPool(dataDir string) *Pool {
	return &Pool{
		conns:      make(map[string]*managedConn),
		knownHosts: newKnownHosts(dataDir),
		dataDir:    dataDir,
	}
}

// keyFor returns the pool map key for a host:port pair.
func keyFor(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}

// Connected reports whether there is a live connection to the given host:port.
func (p *Pool) Connected(host string, port int) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	mc, ok := p.conns[keyFor(host, port)]
	return ok && mc.conn != nil && mc.conn.IsAlive()
}

// Get returns the active client for the given host:port (if any).
func (p *Pool) Get(host string, port int) (*Client, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	mc, ok := p.conns[keyFor(host, port)]
	if !ok || mc.conn == nil {
		return nil, errors.New("not connected: call Connect() first")
	}
	if !mc.conn.IsAlive() {
		return nil, errors.New("connection lost: please reconnect")
	}
	return mc.conn, nil
}

// Active returns the (first) connected client, since the v0.x UI is single-server.
func (p *Pool) Active() (*Client, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, mc := range p.conns {
		if mc.conn != nil && mc.conn.IsAlive() {
			return mc.conn, nil
		}
	}
	return nil, errors.New("no active SSH connection")
}

// ActiveConfig returns the ConnConfig of the (first) active connection.
// Used by RotateKey and other operations that need connection params
// but don't have host:port in scope.
func (p *Pool) ActiveConfig() (*ConnConfig, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, mc := range p.conns {
		if mc.conn != nil && mc.conn.IsAlive() {
			return mc.cfg, nil
		}
	}
	return nil, errors.New("no active SSH connection")
}

// ConfigOf returns the stored config for a host:port (including sensitive
// credential material) — used by RotateKey and similar privileged operations.
func (p *Pool) ConfigOf(host string, port int) (*ConnConfig, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	mc, ok := p.conns[keyFor(host, port)]
	if !ok {
		return nil, errors.New("not connected")
	}
	return mc.cfg, nil
}

// Forget drops all state for a host:port (used by /disconnect).
func (p *Pool) Forget(host string, port int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	k := keyFor(host, port)
	mc, ok := p.conns[k]
	if ok {
		if mc.conn != nil {
			_ = mc.conn.Close()
		}
		// zero out credential material
		for i := range mc.password {
			mc.password[i] = 0
		}
		for i := range mc.privateKey {
			mc.privateKey[i] = 0
		}
		delete(p.conns, k)
	}
	return nil
}

// ForgetAll drops every connection (used on shutdown).
func (p *Pool) ForgetAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, mc := range p.conns {
		if mc.conn != nil {
			_ = mc.conn.Close()
		}
		delete(p.conns, k)
	}
}
