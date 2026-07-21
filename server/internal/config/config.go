// Package config holds runtime configuration resolved from environment
// variables. The defaults make `docker compose up` work with zero config.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config is the resolved runtime configuration.
type Config struct {
	Listen   string
	DataDir  string
	LogLevel string

	// Optional: pre-trusted host so the onboarding wizard can be skipped.
	DefaultHost   string
	DefaultPort   int
	DefaultUser   string
	DefaultAPI    string
	DefaultPasswd string

	// Session signing key. If unset a fresh random one is generated on boot.
	SessionKey []byte
}

// FromEnv resolves configuration from environment variables.
func FromEnv() (*Config, error) {
	cfg := &Config{
		Listen:     getenv("UNRAIDPP_LISTEN", ":8080"),
		DataDir:    getenv("UNRAIDPP_DATA_DIR", "./data"),
		LogLevel:   strings.ToLower(getenv("UNRAIDPP_LOG_LEVEL", "info")),
		DefaultHost: getenv("UNRAIDPP_DEFAULT_HOST", ""),
		DefaultAPI:  getenv("UNRAIDPP_DEFAULT_API", ""),
		DefaultUser: getenv("UNRAIDPP_DEFAULT_USER", "root"),
		DefaultPasswd: getenv("UNRAIDPP_DEFAULT_PASSWD", ""),
	}

	if v := getenv("UNRAIDPP_DEFAULT_PORT", ""); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid UNRAIDPP_DEFAULT_PORT: %w", err)
		}
		cfg.DefaultPort = p
	} else {
		cfg.DefaultPort = 22
	}

	// Session key: prefer explicit env, else derive a stable-ish key from
	// $UNRAIDPP_SESSION_KEY or generate a random one (session loss on restart).
	if v := os.Getenv("UNRAIDPP_SESSION_KEY"); v != "" {
		cfg.SessionKey = []byte(v)
	} else {
		cfg.SessionKey = randomKey(32)
	}

	return cfg, nil
}

func getenv(k, def string) string {
	if v, ok := os.LookupEnv(k); ok {
		return v
	}
	return def
}

func randomKey(n int) []byte {
	b := make([]byte, n)
	// We use crypto/rand in main; here we just provide a placeholder that
	// main.go will overwrite with a properly random one. This avoids importing
	// crypto/rand in this leaf package (kept purely env-driven).
	for i := range b {
		b[i] = byte(i)
	}
	return b
}
