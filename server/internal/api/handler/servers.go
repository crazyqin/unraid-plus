package handler

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/crazyqin/unraid-plus/server/internal/ssh"
	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

// This file implements multi-server persistence. Server connection configs
// (including encrypted passwords) are saved to <dataDir>/servers.json so they
// survive restarts and page refreshes. The frontend queries GET /api/servers
// on boot to restore the connection state without going through onboarding.

// serverEntry is one saved server in servers.json.
type serverEntry struct {
	ID         string `json:"id"`         // unique ID: host:port
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	AuthMode   string `json:"authMode"`  // "key" or "password"
	APIBase    string `json:"apiBase"`
	Label      string `json:"label"`
	LastSeen   string `json:"lastSeen"`  // RFC3339 timestamp of last successful connect
	// Encrypted password (AES-GCM, key derived from dataDir secret).
	// Empty for key-auth servers.
	EncPassword []byte `json:"encPassword,omitempty"`
	// For key-auth, we don't store the key inline — the key file lives at
	// <dataDir>/keys/<id>.pub  /  <dataDir>/keys/<id>
}

// serversFile is the on-disk format for servers.json.
type serversFile struct {
	Servers []serverEntry `json:"servers"`
}

// serverManager handles persistence of server configs.
type serverManager struct {
	mu      sync.RWMutex
	dir     string
	entries map[string]*serverEntry // keyed by ID
	gcm     cipher.AEAD            // for encrypting/decrypting passwords

	// Reconnect rate-limiting: per-server last attempt time.
	reconnectMu       sync.Mutex
	reconnectAttempts map[string]time.Time // sid -> last attempt timestamp
}

// newServerManager loads servers.json from dataDir (or creates empty).
func newServerManager(dataDir string) *serverManager {
	sm := &serverManager{
		dir:               dataDir,
		entries:           make(map[string]*serverEntry),
		reconnectAttempts: make(map[string]time.Time),
	}
	// Derive an encryption key for password storage. We use the session
	// key file at <dataDir>/.enc_key — if it doesn't exist, create one.
	// This is NOT military-grade security — it's to avoid storing
	// passwords in plaintext in servers.json. The key file has mode 0600.
	gcm, err := sm.initCipher()
	if err != nil {
		// Log the error but continue — Upsert will gracefully handle nil gcm
		// by not encrypting passwords (they simply won't be persisted).
		logger.Errorf("initCipher failed: %v — password encryption unavailable", err)
	}
	sm.gcm = gcm
	sm.load()
	return sm
}

// initCipher reads or creates the encryption key file and returns an AEAD.
func (sm *serverManager) initCipher() (cipher.AEAD, error) {
	keyPath := filepath.Join(sm.dir, ".enc_key")
	data, err := os.ReadFile(keyPath)
	if err != nil || len(data) < 32 {
		// Generate a new 32-byte key
		data = make([]byte, 32)
		if _, err := rand.Read(data); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(sm.dir, 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(keyPath, data, 0o600); err != nil {
			return nil, err
		}
	}
	block, err := aes.NewCipher(data[:32])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (sm *serverManager) load() {
	path := filepath.Join(sm.dir, "servers.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var sf serversFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for i := range sf.Servers {
		sm.entries[sf.Servers[i].ID] = &sf.Servers[i]
	}
}

func (sm *serverManager) save() error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.saveLocked()
}

// saveLocked writes entries to disk. Caller must hold at least RLock.
func (sm *serverManager) saveLocked() error {
	servers := make([]serverEntry, 0, len(sm.entries))
	for _, e := range sm.entries {
		servers = append(servers, *e)
	}

	sf := serversFile{Servers: servers}
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(sm.dir, "servers.json")
	return os.WriteFile(path, data, 0o600)
}

// encryptPassword encrypts a password using AES-GCM.
func (sm *serverManager) encryptPassword(password string) ([]byte, error) {
	if sm.gcm == nil || password == "" {
		return nil, nil
	}
	nonce := make([]byte, sm.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return sm.gcm.Seal(nonce, nonce, []byte(password), nil), nil
}

// decryptPassword decrypts an AES-GCM encrypted password.
func (sm *serverManager) decryptPassword(enc []byte) (string, error) {
	if sm.gcm == nil || len(enc) == 0 {
		return "", nil
	}
	nonceSize := sm.gcm.NonceSize()
	if len(enc) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := enc[:nonceSize], enc[nonceSize:]
	plain, err := sm.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// Upsert adds or updates a server entry and saves to disk.
func (sm *serverManager) Upsert(cfg *ssh.ConnConfig, password string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	id := serverID(cfg.Host, cfg.Port)
	entry := &serverEntry{
		ID:       id,
		Host:     cfg.Host,
		Port:     cfg.Port,
		User:     cfg.User,
		AuthMode: authModeStr(cfg.AuthMode),
		APIBase:  cfg.APIBase,
		Label:    cfg.Label,
		LastSeen: time.Now().Format(time.RFC3339),
	}

	// Encrypt password if present
	if password != "" && cfg.AuthMode == ssh.AuthPassword {
		enc, err := sm.encryptPassword(password)
		if err != nil {
			// Log warning but don't fail — password will just not be persisted
			enc = nil
		}
		entry.EncPassword = enc
	}

	sm.entries[id] = entry
	return sm.saveLocked()
}

// Get returns a server entry by ID.
func (sm *serverManager) Get(id string) *serverEntry {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.entries[id]
}

// List returns all saved server entries.
func (sm *serverManager) List() []serverEntry {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	out := make([]serverEntry, 0, len(sm.entries))
	for _, e := range sm.entries {
		out = append(out, *e)
	}
	return out
}

// Delete removes a server entry and its key file.
func (sm *serverManager) Delete(id string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.entries, id)

	// Clean up key files
	keyPath := filepath.Join(sm.dir, "keys", id)
	os.Remove(keyPath)
	pubPath := filepath.Join(sm.dir, "keys", id+".pub")
	os.Remove(pubPath)

	return sm.saveLocked()
}

// UpdateEntry updates fields of an existing server. If host/port change the
// entry ID changes and key files are renamed. password empty keeps existing.
// Returns the (possibly new) entry ID.
func (sm *serverManager) UpdateEntry(id string, host string, port int, user, apiBase, label, password string) (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	old, ok := sm.entries[id]
	if !ok {
		return "", fmt.Errorf("server %s not found", id)
	}

	if host == "" {
		host = old.Host
	}
	if port <= 0 {
		port = old.Port
	}
	if user == "" {
		user = old.User
	}
	if apiBase == "" {
		apiBase = old.APIBase
	}
	// label may intentionally be cleared — only keep old if not provided via pointer;
	// here we accept empty label as clear/replace when the caller always sends it.

	newID := serverID(host, port)
	entry := &serverEntry{
		ID:          newID,
		Host:        host,
		Port:        port,
		User:        user,
		AuthMode:    old.AuthMode,
		APIBase:     apiBase,
		Label:       label,
		LastSeen:    time.Now().Format(time.RFC3339),
		EncPassword: old.EncPassword,
	}

	// Update password if a new one was supplied
	if password != "" {
		entry.AuthMode = "password"
		enc, err := sm.encryptPassword(password)
		if err != nil {
			logger.Warnf("encrypt password for %s failed: %v", newID, err)
		} else {
			entry.EncPassword = enc
		}
	}

	// Migrate key files when ID changes
	if newID != id {
		oldKey := filepath.Join(sm.dir, "keys", id)
		newKey := filepath.Join(sm.dir, "keys", newID)
		if data, err := os.ReadFile(oldKey); err == nil {
			_ = os.MkdirAll(filepath.Join(sm.dir, "keys"), 0o700)
			_ = os.WriteFile(newKey, data, 0o600)
			_ = os.Remove(oldKey)
			_ = os.Remove(oldKey + ".pub")
		}
		delete(sm.entries, id)
	}

	sm.entries[newID] = entry
	if err := sm.saveLocked(); err != nil {
		return "", err
	}
	return newID, nil
}

// ConnConfigFor returns an ssh.ConnConfig that can be used to reconnect to a
// saved server. Returns nil if the server is not found or credentials are
// unavailable.
func (sm *serverManager) ConnConfigFor(id string) (*ssh.ConnConfig, error) {
	sm.mu.RLock()
	entry, ok := sm.entries[id]
	sm.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("server %s not found", id)
	}

	cfg := &ssh.ConnConfig{
		Host:    entry.Host,
		Port:    entry.Port,
		User:    entry.User,
		AuthMode: authMode(entry.AuthMode),
		APIBase: entry.APIBase,
		Label:   entry.Label,
	}

	switch entry.AuthMode {
	case "key":
		// Load key file from <dataDir>/keys/<id>
		keyPath := filepath.Join(sm.dir, "keys", id)
		keyData, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("key file not found for %s: %w", id, err)
		}
		cfg.PrivateKey = keyData
		cfg.AuthMode = ssh.AuthKey
	case "password":
		if len(entry.EncPassword) > 0 {
			pw, err := sm.decryptPassword(entry.EncPassword)
			if err != nil {
				return nil, fmt.Errorf("decrypt password for %s: %w", id, err)
			}
			cfg.Password = pw
			cfg.AuthMode = ssh.AuthPassword
		} else {
			return nil, fmt.Errorf("no stored credentials for %s", id)
		}
	}

	return cfg, nil
}

// SaveServerKey writes a private key for the given server ID.
func (sm *serverManager) SaveServerKey(id string, keyData []byte) error {
	dir := filepath.Join(sm.dir, "keys")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, id), keyData, 0o600)
}

// LoadServerKey reads the private key for a server ID.
func (sm *serverManager) LoadServerKey(id string) ([]byte, error) {
	return os.ReadFile(filepath.Join(sm.dir, "keys", id))
}

func serverID(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}

func authModeStr(m ssh.AuthMode) string {
	if m == ssh.AuthKey {
		return "key"
	}
	return "password"
}

func authMode(s string) ssh.AuthMode {
	if s == "key" {
		return ssh.AuthKey
	}
	return ssh.AuthPassword
}

// reconnectCooldown is the minimum interval between auto-reconnect attempts
// for the same server. Prevents hammering the Unraid WebGUI when it's down.
const reconnectCooldown = 30 * time.Second

// shouldTryReconnect returns true if enough time has elapsed since the last
// auto-reconnect attempt for this server. It records the attempt timestamp.
func (sm *serverManager) shouldTryReconnect(sid string) bool {
	sm.reconnectMu.Lock()
	defer sm.reconnectMu.Unlock()

	now := time.Now()
	last, exists := sm.reconnectAttempts[sid]
	if exists && now.Sub(last) < reconnectCooldown {
		logger.Debugf("reconnect rate-limited for %s (last attempt %.0fs ago, cooldown %s)",
			sid, now.Sub(last).Seconds(), reconnectCooldown)
		return false
	}
	sm.reconnectAttempts[sid] = now
	return true
}

// markReconnectSuccess clears the rate-limit entry for a server after a
// successful reconnect, so the next disconnect triggers immediate retry.
func (sm *serverManager) markReconnectSuccess(sid string) {
	sm.reconnectMu.Lock()
	defer sm.reconnectMu.Unlock()
	delete(sm.reconnectAttempts, sid)
}
