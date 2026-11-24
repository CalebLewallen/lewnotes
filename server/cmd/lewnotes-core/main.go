// Command lewnotes-core is the lewnotes server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/caleblewallen/lewnotes/server/internal/api"
	"github.com/caleblewallen/lewnotes/server/internal/auth"
	"github.com/caleblewallen/lewnotes/server/internal/config"
	"github.com/caleblewallen/lewnotes/server/internal/index"
	"github.com/caleblewallen/lewnotes/server/internal/vault"
	"github.com/caleblewallen/lewnotes/server/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "lewnotes-core:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		configPath  = flag.String("config", "", "path to lewnotes.toml (default: ./lewnotes.toml if present)")
		listen      = flag.String("listen", "", "override the listen address")
		vaultPath   = flag.String("vault", "", "override the vault path")
		dataDir     = flag.String("data-dir", "", "override the data directory")
		showVersion = flag.Bool("version", false, "print version and exit")
		healthcheck = flag.Bool("healthcheck", false, "probe the local health endpoint and exit")
		rebuild     = flag.Bool("rebuild-index", false, "rescan every note on startup")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println(version.Version)
		return nil
	}
	if *healthcheck {
		addr := *listen
		if addr == "" {
			addr = envOr("LEWNOTES_LISTEN", ":8080")
		}
		return probeHealth(addr)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if *listen != "" {
		cfg.Server.Listen = *listen
	}
	if *vaultPath != "" {
		cfg.Vault.Path = *vaultPath
	}
	if *dataDir != "" {
		cfg.Server.DataDir = *dataDir
	}
	if err := cfg.Normalize(); err != nil {
		return err
	}

	logger, err := newLogger(cfg.Server.LogLevel)
	if err != nil {
		return err
	}
	slog.SetDefault(logger)

	v, err := vault.Open(cfg.Vault.Path, cfg.Vault.Ignore)
	if err != nil {
		return err
	}
	for _, dir := range []string{cfg.Vault.Inbox, cfg.Vault.Attachments} {
		if dir == "" {
			continue
		}
		if err := v.EnsureDir(dir); err != nil {
			return err
		}
	}

	dbPath := filepath.Join(cfg.Server.DataDir, "lewnotes.db")
	ix, err := index.Open(dbPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := ix.Close(); err != nil {
			logger.Error("close index", "error", err)
		}
	}()

	authService, err := auth.NewService(auth.ServiceOptions{
		Store:                 auth.NewStore(ix.DB()),
		SessionTTL:            cfg.Auth.SessionTTL.Std(),
		TokenLastUsedInterval: cfg.Auth.TokenLastUsedInterval.Std(),
		CookieSecure:          cfg.Auth.CookieSecure,
	})
	if err != nil {
		return err
	}

	scanner := index.NewScanner(v, ix, logger)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if _, err := scanner.Run(ctx, *rebuild); err != nil {
			logger.Error("initial index scan failed", "error", err)
		}
	}()

	logger.Info("starting",
		"version", version.Version,
		"vault", v.Root(),
		"database", dbPath,
		"listen", cfg.Server.Listen,
		"config", cfg.Source,
	)

	srv := &http.Server{
		Addr: cfg.Server.Listen,
		Handler: api.NewRouter(api.Options{
			Version:    version.Version,
			Started:    time.Now(),
			Logger:     logger,
			Index:      ix,
			Scanner:    scanner,
			Auth:       authService,
			TrustProxy: cfg.Server.TrustProxy,
			PublicURL:  cfg.Server.PublicURL,
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errs := make(chan error, 1)
	go func() {
		errs <- srv.ListenAndServe()
	}()

	select {
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func newLogger(level string) (*slog.Logger, error) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("log level %q: %w", level, err)
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l})), nil
}

func probeHealth(listen string) error {
	addr := listen
	switch {
	case strings.HasPrefix(addr, ":"):
		addr = "127.0.0.1" + addr
	case strings.HasPrefix(addr, "0.0.0.0:"):
		addr = "127.0.0.1:" + strings.TrimPrefix(addr, "0.0.0.0:")
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + addr + "/api/v1/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned %s", resp.Status)
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
