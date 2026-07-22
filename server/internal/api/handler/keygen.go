package handler

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// genED25519 produces an OpenSSH-formatted ED25519 key pair. The public key
// is returned as a single-line string suitable for authorized_keys; the
// private key is returned as PEM bytes.
func genED25519() (pub []byte, priv []byte, err error) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	sshPub, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return nil, nil, err
	}
	pub = ssh.MarshalAuthorizedKey(sshPub)

	block, err := ssh.MarshalPrivateKey(privKey, "unraidpp@server")
	if err != nil {
		return nil, nil, err
	}
	priv = pem.EncodeToMemory(block)
	return pub, priv, nil
}

// saveKey persists the private key to <dataDir>/ssh_ed25519 with mode 0600.
// On boot, the SSH pool reads this file to reconnect without a password.
func saveKey(dataDir string, priv []byte) error {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dataDir, "ssh_ed25519")
	if err := os.WriteFile(path, priv, 0o600); err != nil {
		return fmt.Errorf("write key: %w", err)
	}
	return nil
}

// PersistedConn holds everything needed to auto-reconnect on server restart.
type PersistedConn struct {
	Host       string
	Port       int
	User       string
	APIBase    string
	Label      string
	PrivateKey []byte
}

// LoadPersistedConn reads the saved SSH key + connection metadata from the
// data directory. Returns (nil, nil) if no persisted connection exists
// (e.g. the user hasn't gone through RotateKey yet).
func LoadPersistedConn(dataDir string) (*PersistedConn, error) {
	meta, err := loadConnMeta(dataDir)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, nil
	}
	key, err := loadKey(dataDir)
	if err != nil {
		return nil, err
	}
	if key == nil {
		// Key file missing — can't auto-reconnect with key auth.
		return nil, nil
	}
	return &PersistedConn{
		Host:       meta.Host,
		Port:       meta.Port,
		User:       meta.User,
		APIBase:    meta.APIBase,
		Label:      meta.Label,
		PrivateKey: key,
	}, nil
}
// Returns nil if the file doesn't exist (e.g. password-mode connections
// that never went through RotateKey).
func loadKey(dataDir string) ([]byte, error) {
	path := filepath.Join(dataDir, "ssh_ed25519")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

// saveConnMeta persists the SSH connection parameters (host, port, user,
// apiBase, label) alongside the key so the server can auto-reconnect after
// a restart. The file is JSON, written to <dataDir>/conn_meta.json.
type connMeta struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	APIBase string `json:"apiBase"`
	Label   string `json:"label"`
}

func saveConnMeta(dataDir string, meta connMeta) error {
	path := filepath.Join(dataDir, "conn_meta.json")
	data, err := jsonMarshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func loadConnMeta(dataDir string) (*connMeta, error) {
	path := filepath.Join(dataDir, "conn_meta.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var meta connMeta
	if err := jsonUnmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// jsonMarshal / jsonUnmarshal wrap encoding/json so keygen.go keeps a
// single import point for JSON (de)serialization of conn meta.
func jsonMarshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}
func jsonUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
