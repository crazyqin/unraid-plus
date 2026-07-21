package handler

import (
	"crypto/ed25519"
	"crypto/rand"
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
// On boot, the SSH pool could (in a future revision) read this file and use
// it to reconnect without a password — but for v0.x we just store it so an
// admin can copy it to other clients if they want.
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
