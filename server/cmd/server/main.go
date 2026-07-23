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
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"strings"
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
	ur := unraid.NewClient()
	hub := ssh.NewTerminalHub(pool)

	// Wire the handler so we can access the server manager for auto-reconnect.
	h := handler.New(pool, ur, hub, cfg)

	// If env provided default credentials, connect eagerly so the onboarding
	// wizard is skipped (useful for unattended deployments).
	if cfg.DefaultHost != "" && cfg.DefaultPasswd != "" {
		go func() {
			logger.Infof("auto-connecting to %s:%d as %s", cfg.DefaultHost, cfg.DefaultPort, cfg.DefaultUser)
			connCfg := &ssh.ConnConfig{
				Host:     cfg.DefaultHost,
				Port:     cfg.DefaultPort,
				User:     cfg.DefaultUser,
				AuthMode: ssh.AuthPassword,
				Password: cfg.DefaultPasswd,
				APIBase:  cfg.DefaultAPI,
			}
			_, err := pool.Connect(connCfg)
			if err != nil {
				logger.Warnf("auto-connect failed: %v", err)
				return
			}
			// WebGUI login for the default server
			sid := connCfg.Host + ":" + fmt.Sprintf("%d", connCfg.Port)
			if err := ur.Login(sid, connCfg.APIBase, connCfg.User, connCfg.Password); err != nil {
				logger.Warnf("auto-connect WebGUI login failed (non-fatal): %v", err)
				// Retry with HTTP if HTTPS failed due to protocol mismatch
				if strings.HasPrefix(connCfg.APIBase, "https://") && strings.Contains(err.Error(), "HTTP response to HTTPS") {
					httpBase := "http://" + strings.TrimPrefix(connCfg.APIBase, "https://")
					logger.Infof("auto-connect: retrying WebGUI login with %s", httpBase)
					if err2 := ur.Login(sid, httpBase, connCfg.User, connCfg.Password); err2 != nil {
						logger.Warnf("auto-connect WebGUI login (http) also failed: %v", err2)
					} else {
						connCfg.APIBase = httpBase
					}
				}
			}
		}()
	} else {
		// v0.8+: Auto-reconnect all saved servers that have stored credentials.
		// This replaces the old LoadPersistedConn (single-server) approach.
		go func() {
			time.Sleep(500 * time.Millisecond) // small delay for network readiness
			for _, entry := range h.ServerManager().List() {
				rc, err := h.ServerManager().ConnConfigFor(entry.ID)
				if err != nil {
					logger.Warnf("auto-reconnect: skip %s: %v", entry.ID, err)
					continue
				}
				_, err = pool.Connect(rc)
				if err != nil {
					logger.Warnf("auto-reconnect %s: failed: %v", entry.ID, err)
					continue
				}
				logger.Infof("auto-reconnect %s: success", entry.ID)
				ur.SetBase(rc.APIBase)
				// WebGUI login for this server
				if rc.Password != "" {
					go func(sid, apiBase, user, pw string) {
						if err := ur.Login(sid, apiBase, user, pw); err != nil {
							logger.Warnf("auto-reconnect WebGUI login %s failed (non-fatal): %v", sid, err)
							// Retry with HTTP if HTTPS protocol mismatch
							if strings.HasPrefix(apiBase, "https://") && strings.Contains(err.Error(), "HTTP response to HTTPS") {
								httpBase := "http://" + strings.TrimPrefix(apiBase, "https://")
								logger.Infof("auto-reconnect: retrying WebGUI login with %s", httpBase)
								if err2 := ur.Login(sid, httpBase, user, pw); err2 != nil {
									logger.Warnf("auto-reconnect WebGUI login (http) %s also failed: %v", sid, err2)
								} else {
									ur.SetBase(httpBase)
								}
							}
						}
					}(entry.ID, rc.APIBase, rc.User, rc.Password)
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
