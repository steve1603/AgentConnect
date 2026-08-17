// Command roundtable runs the AI Roundtable server: three models answer,
// review each other, and one chairs the final response.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/steve1603/AgentConnect/internal/config"
	"github.com/steve1603/AgentConnect/internal/server"
	"github.com/steve1603/AgentConnect/internal/store"
	"github.com/steve1603/AgentConnect/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(".env")
	if err != nil {
		return err
	}
	configureLogging(cfg.LogLevel)

	db, err := store.Open(cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer db.Close()

	assets, err := web.Assets()
	if err != nil {
		return fmt.Errorf("load web assets: %w", err)
	}

	if configured := cfg.Configured(); len(configured) > 0 {
		slog.Info("providers configured", "providers", strings.Join(configured, ", "))
	} else {
		slog.Warn("no API keys found in .env - the roundtable cannot run yet")
	}
	if cfg.Host != "127.0.0.1" && cfg.Host != "localhost" && cfg.Host != "::1" {
		slog.Warn("APP_HOST is not loopback: the server will be reachable from other machines",
			"host", cfg.Host)
	}

	httpServer := &http.Server{
		Addr:              cfg.Address(),
		Handler:           server.New(cfg, db, assets),
		ReadHeaderTimeout: 10 * time.Second,
		// No write timeout: the SSE progress stream is long-lived.
		IdleTimeout: 2 * time.Minute,
	}

	listener, err := net.Listen("tcp", cfg.Address())
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Address(), err)
	}

	url := fmt.Sprintf("http://%s", browsableAddress(cfg))
	slog.Info("AI Roundtable is running", "url", url)
	if cfg.OpenBrowser {
		go openBrowser(url)
	}

	errs := make(chan error, 1)
	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errs:
		return err
	case <-signals:
		slog.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}

func configureLogging(level string) {
	parsed := slog.LevelInfo
	switch strings.ToLower(level) {
	case "debug":
		parsed = slog.LevelDebug
	case "warn", "warning":
		parsed = slog.LevelWarn
	case "error":
		parsed = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: parsed})))
}

// browsableAddress turns a wildcard bind address into something a browser can
// actually open.
func browsableAddress(cfg *config.Config) string {
	host := cfg.Host
	if host == "0.0.0.0" || host == "::" || host == "" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("%s:%d", host, cfg.Port)
}

// openBrowser waits for the server to accept connections, then opens the UI.
func openBrowser(url string) {
	address := strings.TrimPrefix(url, "http://")
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", address, 500*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		time.Sleep(300 * time.Millisecond)
	}

	cmd := browserCommand(url)
	if cmd == nil {
		slog.Info("open this address in your browser", "url", url)
		return
	}
	if err := cmd.Start(); err != nil {
		slog.Debug("could not open a browser automatically", "error", err)
		slog.Info("open this address in your browser", "url", url)
	}
}

// browserCommand picks the platform's URL handler, or nil if none is present.
//
// Android/Termux reports GOOS=android and has no xdg-open; its handler is
// termux-open-url, which ships in the termux-tools package.
func browserCommand(url string) *exec.Cmd {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		return exec.Command("open", url)
	}

	for _, opener := range []string{"termux-open-url", "xdg-open"} {
		if path, err := exec.LookPath(opener); err == nil {
			return exec.Command(path, url)
		}
	}
	return nil
}
