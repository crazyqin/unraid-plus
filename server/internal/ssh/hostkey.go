package ssh

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/ssh"
)

// knownHosts implements a trust-on-first-use (TOFU) cache of remote host keys,
// persisted to a JSON file in the pool's data dir. On first connect we accept
// and store the key; on subsequent connects we require an exact match and
// reject otherwise (which is the only safe default for SSH host keys).
type knownHosts struct {
	mu      sync.Mutex
	path    string
	entries map[string]string // "host:port" -> sha256 fingerprint
}

func newKnownHosts(dataDir string) *knownHosts {
	kh := &knownHosts{
		path:    filepath.Join(dataDir, "known_hosts.json"),
		entries: make(map[string]string),
	}
	_ = kh.load()
	return kh
}

func (k *knownHosts) load() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	f, err := os.Open(k.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(&k.entries)
}

func (k *knownHosts) save() error {
	if err := os.MkdirAll(filepath.Dir(k.path), 0o700); err != nil {
		return err
	}
	tmp := k.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(f).Encode(k.entries); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, k.path)
}

// callback returns an ssh.HostKeyCallback. On first contact we accept any key
// and persist it; on later contacts we require an exact match — mismatch is
// treated as a MITM risk. After a successful connect, callers can retrieve
// the persisted fingerprint via FingerprintOf().
func (k *knownHosts) callback(host string, port int) (ssh.HostKeyCallback, error) {
	key := fmt.Sprintf("%s:%d", host, port)
	return func(_ string, _ net.Addr, remote ssh.PublicKey) error {
		k.mu.Lock()
		defer k.mu.Unlock()
		fp := ssh.FingerprintSHA256(remote)
		existing, ok := k.entries[key]
		if !ok {
			k.entries[key] = fp
			_ = k.save()
			return nil
		}
		if existing != fp {
			return fmt.Errorf("host key mismatch for %s — possible MITM. Stored: %s, Got: %s", key, existing, fp)
		}
		return nil
	}, nil
}

// FingerprintOf returns the cached fingerprint for a host:port (or empty).
func (k *knownHosts) FingerprintOf(host string, port int) string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.entries[fmt.Sprintf("%s:%d", host, port)]
}

// TrustedList returns the "host:port -> fingerprint" map for diagnostics.
func (k *knownHosts) TrustedList() map[string]string {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := make(map[string]string, len(k.entries))
	for k, v := range k.entries {
		out[k] = v
	}
	return out
}
