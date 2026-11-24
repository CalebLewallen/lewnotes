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
	"strings"
	"syscall"
	"time"

	"github.com/caleblewallen/lewnotes/server/internal/api"
	"github.com/caleblewallen/lewnotes/server/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "lewnotes-core:", err)
		os.Exit(1)
	}
}

func run() error {
	listen := flag.String("listen", envOr("LEWNOTES_LISTEN", ":8080"), "address to listen on")
	showVersion := flag.Bool("version", false, "print version and exit")
	healthcheck := flag.Bool("healthcheck", false, "probe the local health endpoint and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.Version)
		return nil
	}
	if *healthcheck {
		return probeHealth(*listen)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	srv := &http.Server{
		Addr:              *listen,
		Handler:           api.NewRouter(api.Options{Version: version.Version, Started: time.Now(), Logger: logger}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errs := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", *listen, "version", version.Version)
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
