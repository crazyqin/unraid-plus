package ssh

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

// Client wraps a single authenticated ssh.Client plus its underlying config
// (so we can re-establish or open SFTP sessions on demand).
type Client struct {
	cfg     *ssh.ClientConfig
	addr    string
	mu      sync.Mutex
	conn    *ssh.Client
	closed  bool
}

// IsAlive reports whether the underlying connection is still usable.
func (c *Client) IsAlive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil && !c.closed
}

// Close terminates the connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// Run executes a single command and returns combined stdout + stderr.
func (c *Client) Run(cmd string) (string, error) {
	sess, err := c.newSession()
	if err != nil {
		logger.Debugf("[ssh.Run] NewSession error for %q: %v", cmd, err)
		return "", err
	}
	defer sess.Close()

	out, err := sess.CombinedOutput(cmd)
	if err != nil {
		logger.Debugf("[ssh.Run] error for %q: %v (output len=%d)", cmd, err, len(out))
		return string(out), err
	}
	logger.Debugf("[ssh.Run] OK %q -> %d bytes", cmd, len(out))
	return string(out), nil
}

// RunStream executes a command and lines its stdout/stderr into the writer.
// Used by Docker log streaming.
func (c *Client) RunStream(cmd string, w io.Writer) error {
	sess, err := c.newSession()
	if err != nil {
		return err
	}
	defer sess.Close()
	sess.Stdout = w
	sess.Stderr = w
	return sess.Run(cmd)
}

// NewSession returns a fresh ssh.Session on the underlying connection. The
// caller is responsible for closing it.
func (c *Client) NewSession() (*ssh.Session, error) {
	return c.newSession()
}

func (c *Client) newSession() (*ssh.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil || c.closed {
		return nil, errors.New("connection closed")
	}
	return c.conn.NewSession()
}

// NewInteractiveSession allocates a PTY and an interactive shell, returning
// the session plus pipes for stdin / stdout / stderr. Used by the WebSocket
// terminal hub.
func (c *Client) NewInteractiveSession(cols, rows int) (
	sess *ssh.Session,
	stdin io.WriteCloser,
	stdout io.Reader,
	stderr io.Reader,
	err error,
) {
	sess, err = c.newSession()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	term := "xterm-256color"
	if err = sess.RequestPty(term, rows, cols, modes); err != nil {
		_ = sess.Close()
		return nil, nil, nil, nil, fmt.Errorf("request pty: %w", err)
	}
	stdin, err = sess.StdinPipe()
	if err != nil {
		_ = sess.Close()
		return nil, nil, nil, nil, err
	}
	stdout, err = sess.StdoutPipe()
	if err != nil {
		_ = sess.Close()
		return nil, nil, nil, nil, err
	}
	stderr, err = sess.StderrPipe()
	if err != nil {
		_ = sess.Close()
		return nil, nil, nil, nil, err
	}
	if err = sess.Shell(); err != nil {
		_ = sess.Close()
		return nil, nil, nil, nil, fmt.Errorf("start shell: %w", err)
	}
	return sess, stdin, stdout, stderr, nil
}

// SFTP opens a new SFTP session. The returned io.Closer must be closed by the
// caller. (We deliberately keep the sftp import in sftp.go to keep this file
// focused on the ssh.Client surface.)
func (c *Client) SFTP() (*SFTPClient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil || c.closed {
		return nil, errors.New("connection closed")
	}
	sc, err := c.conn.NewSession()
	if err != nil {
		return nil, err
	}
	if err := sc.RequestSubsystem("sftp"); err != nil {
		_ = sc.Close()
		return nil, fmt.Errorf("sftp subsystem: %w", err)
	}
	pw, err := sc.StdinPipe()
	if err != nil {
		_ = sc.Close()
		return nil, err
	}
	pr, err := sc.StdoutPipe()
	if err != nil {
		_ = sc.Close()
		return nil, err
	}
	return newSFTPClient(pr, pw, sc)
}

// Connect dials the SSH server using the provided config, performing
// trust-on-first-use host key verification via the pool's knownHosts cache.
func (p *Pool) Connect(cfg *ConnConfig) (*ConnectResult, error) {
	authMethods, err := authMethodsFor(cfg)
	if err != nil {
		return nil, err
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	hostkeyCb, err := p.knownHosts.callback(cfg.Host, cfg.Port)
	if err != nil {
		return nil, err
	}

	sshCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            authMethods,
		HostKeyCallback: hostkeyCb,
		Timeout:         10 * time.Second,
	}

	logger.Infof("dialing ssh %s as %s (mode=%s)", addr, cfg.User, authModeName(cfg.AuthMode))

	// ssh.Dial performs both the TCP dial and the SSH handshake, returning
	// a fully-authenticated *ssh.Client. Using it (instead of
	// net.DialTimeout + ssh.NewClientConn) keeps the connection lifecycle in
	// one place and gives us the *ssh.Client type the rest of this file
	// expects.
	c, err := ssh.Dial("tcp", addr, sshCfg)
	if err != nil {
		return nil, fmt.Errorf("ssh dial: %w", err)
	}

	client := &Client{
		cfg:  sshCfg,
		addr: addr,
		conn: c,
	}

	p.mu.Lock()
	p.conns[keyFor(cfg.Host, cfg.Port)] = &managedConn{
		cfg:        cfg,
		conn:       client,
		password:   []byte(cfg.Password),
		privateKey: cloneBytes(cfg.PrivateKey),
	}
	p.mu.Unlock()

	// Probe a trivial command so we can verify the server is actually
	// usable and (optionally) capture the Unraid version.
	var version string
	if out, err := client.Run("cat /etc/unraid-version 2>/dev/null || true"); err == nil {
		version = strings.TrimSpace(out)
	}

	return &ConnectResult{
		HostFingerprint: p.knownHosts.FingerprintOf(cfg.Host, cfg.Port),
		ServerVersion:   version,
	}, nil
}

// ConnectResult is returned after a successful Connect.
type ConnectResult struct {
	HostFingerprint string
	ServerVersion   string
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

func authModeName(m AuthMode) string {
	if m == AuthKey {
		return "key"
	}
	return "password"
}

// authMethodsFor converts the ConnConfig into a slice of ssh.AuthMethod,
// ordered so that public-key is tried before password (so a server that
// accepts both always prefers the key).
func authMethodsFor(cfg *ConnConfig) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	if len(cfg.PrivateKey) > 0 {
		var signer ssh.Signer
		var err error
		if cfg.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(cfg.PrivateKey, []byte(cfg.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(cfg.PrivateKey)
		}
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}

	if cfg.Password != "" {
		methods = append(methods, ssh.Password(cfg.Password))
	}

	if len(methods) == 0 {
		return nil, errors.New("no authentication method provided (need password or private key)")
	}
	return methods, nil
}
