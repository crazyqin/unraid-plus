// Package main is the unraid-plus server entry point.
//
// It resolves configuration from env vars, wires the SSH pool, the Unraid API
// client, the WebSocket terminal hub, the HTTP router, and serves everything
// (including the embedded frontend) on a single port. Designed to run behind
// `docker compose up` with zero flags.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/crazyqin/unraid-plus/server/internal/api"
	"github.com/crazyqin/unraid-plus/server/internal/api/handler"
	"github.com/crazyqin/unraid-plus/server/internal/config"
	"github.com/crazyqin/unraid-plus/server/internal/ssh"
	"github.com/crazyqin/unraid-plus/server/internal/unraid"
	"github.com/crazyqin/unraid-plus/server/internal/web"
	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

// Version metadata is injected by the linker (see Dockerfile -ldflags).
var (
	Version   = "dev"
	Commit    = "none"
	BuildTime = "unknown"
)

func main() {
	startTime := time.Now()
	logLevel := flag.String("log-level", "", "override UNRAIDPP_LOG_LEVEL (debug|info|warn|error)")
	flag.Parse()

	cfg, err := config.FromEnv()
	if err != nil {
		logger.Fatal("config: %v", err)
	}
	if *logLevel != "" {
		cfg.LogLevel = *logLevel
	}
	logger.SetLevel(cfg.LogLevel)

	// Replace the placeholder session key with a properly random one if env
	// didn't supply one. Sessions drop on restart — that's intended.
	if os.Getenv("UNRAIDPP_SESSION_KEY") == "" {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err == nil {
			cfg.SessionKey = buf
		}
	}

	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		logger.Fatal("mkdir data dir: %v", err)
	}

	logger.Infof("unraid-plus %s (commit=%s built=%s)", Version, Commit, BuildTime)
	logger.Infof("data dir: %s", cfg.DataDir)
	logger.Infof("listening on %s", cfg.Listen)

	// Share version + start time with the API package so the /health
	// endpoint can report them to the frontend.
	api.Version = Version
	api.StartTime = startTime

	// Compose services
	pool := ssh.NewPool(cfg.DataDir)
	ur := unraid.NewClient(cfg)
	hub := ssh.NewTerminalHub(pool)

	// Wire the handler so we can access the server manager for auto-reconnect.
	h := handler.New(pool, ur, hub, cfg)

	// If env provided default credentials, connect eagerly so the onboarding
	// wizard is skipped (useful for unattended deployments).
	if cfg.DefaultHost != "" && cfg.DefaultPasswd != "" {
		go func() {
			logger.Infof("auto-connecting to %s:%d as %s", cfg.DefaultHost, cfg.DefaultPort, cfg.DefaultUser)
			_, err := pool.Connect(&ssh.ConnConfig{
				Host:     cfg.DefaultHost,
				Port:     cfg.DefaultPort,
				User:     cfg.DefaultUser,
				AuthMode: ssh.AuthPassword,
				Password: cfg.DefaultPasswd,
				APIBase:  cfg.DefaultAPI,
			})
			if err != nil {
				logger.Warnf("auto-connect failed: %v", err)
			}
		}()
	} else {
		// v0.8+: Auto-reconnect all saved servers that have stored credentials.
		// This replaces the old LoadPersistedConn (single-server) approach.
		go func() {
			time.Sleep(500 * time.Millisecond) // small delay for network readiness
			for _, entry := range h.ServerManager().List() {
				cfg, err := h.ServerManager().ConnConfigFor(entry.ID)
				if err != nil {
					logger.Warnf("auto-reconnect: skip %s: %v", entry.ID, err)
					continue
				}
				_, err = pool.Connect(cfg)
				if err != nil {
					logger.Warnf("auto-reconnect %s: failed: %v", entry.ID, err)
				} else {
					logger.Infof("auto-reconnect %s: success", entry.ID)
					ur.SetBase(cfg.APIBase)
				}
			}
		}()
	}

	handler := api.BuildWithHandler(cfg, pool, ur, hub, h)

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Embed-self-check: log whether the frontend is bundled or not.
	if dist, err := web.Dist(); err == nil {
		if _, err := fs.Stat(dist, "index.html"); err == nil {
			logger.Infof("frontend bundled and served at /")
		} else {
			logger.Warnf("frontend not bundled — run `pnpm build` in web/ before building server")
		}
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("server: %v", err)
		}
	}()

	// Graceful shutdown
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	logger.Infof("shutting down…")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool.ForgetAll()
	_ = srv.Shutdown(ctx)
	logger.Infof("bye")
}
