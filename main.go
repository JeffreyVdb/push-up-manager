// Command pushup-counter serves a small mobile-first push-up tracking PWA
// backed by SQLite. Everything (assets included) ships in one static binary.
//
// It is designed to run as a socket-activated systemd user service: systemd
// owns the listening socket, hands it over on the first connection, and the
// process reports readiness with sd_notify. It also runs standalone by
// binding LISTEN_ADDR itself.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/coreos/go-systemd/v22/activation"
	"github.com/coreos/go-systemd/v22/daemon"
)

const shutdownGrace = 25 * time.Second

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	dbPath := databasePath()
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		slog.Error("create database directory", "path", filepath.Dir(dbPath), "err", err)
		os.Exit(1)
	}

	// Acquire the listener before touching the database: a socket-activated
	// service should fail fast and loudly if the handover went wrong.
	ln, activated, err := listener()
	if err != nil {
		slog.Error("listen", "err", err)
		os.Exit(1)
	}

	store, err := openStore(dbPath)
	if err != nil {
		slog.Error("open database", "path", dbPath, "err", err)
		os.Exit(1)
	}
	defer store.Close()

	app := newServer(store)
	srv := &http.Server{
		Handler:           app.handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// Deliberately no WriteTimeout: it is a deadline on the whole response,
		// so it would cut every event stream on a fixed interval. handleEvents
		// sets a short deadline per frame instead.
	}
	// Shutdown waits for active handlers, and an event stream is active until
	// the page closes. Releasing them first is what keeps the grace period a
	// bound on real work rather than a full 25 seconds every restart.
	srv.RegisterOnShutdown(app.events.Close)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	slog.Info("ready", "addr", ln.Addr().String(), "db", dbPath, "socket_activated", activated)
	if _, err := daemon.SdNotify(false, daemon.SdNotifyReady); err != nil {
		slog.Warn("sd_notify ready", "err", err)
	}
	go watchdog(ctx)

	select {
	case err := <-errCh:
		slog.Error("http server", "err", err)
		os.Exit(1)
	case <-ctx.Done():
		slog.Info("shutting down")
	}

	if _, err := daemon.SdNotify(false, daemon.SdNotifyStopping); err != nil {
		slog.Warn("sd_notify stopping", "err", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown", "err", err)
	}
	slog.Info("stopped")
}

// listener returns the socket passed by systemd when the unit is socket
// activated, and otherwise binds LISTEN_ADDR directly.
func listener() (net.Listener, bool, error) {
	listeners, err := activation.Listeners()
	if err != nil {
		return nil, false, err
	}
	switch {
	case len(listeners) == 1 && listeners[0] != nil:
		return listeners[0], true, nil
	case len(listeners) > 1:
		// More than one socket means the unit file and this binary disagree.
		for _, l := range listeners[1:] {
			if l != nil {
				l.Close()
			}
		}
		return listeners[0], true, nil
	}

	ln, err := net.Listen("tcp", envOr("LISTEN_ADDR", ":5554"))
	return ln, false, err
}

// watchdog answers systemd's WatchdogSec pings when the unit enables them.
func watchdog(ctx context.Context) {
	interval, err := daemon.SdWatchdogEnabled(false)
	if err != nil || interval == 0 {
		return
	}
	ticker := time.NewTicker(interval / 2)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := daemon.SdNotify(false, daemon.SdNotifyWatchdog); err != nil {
				slog.Warn("sd_notify watchdog", "err", err)
			}
		}
	}
}

// databasePath prefers an explicit DB_PATH, then systemd's StateDirectory,
// then the XDG state directory.
func databasePath() string {
	if p := os.Getenv("DB_PATH"); p != "" {
		return p
	}
	// systemd sets STATE_DIRECTORY when StateDirectory= is used; it may hold a
	// colon-separated list, and the first entry is ours.
	if dirs := os.Getenv("STATE_DIRECTORY"); dirs != "" {
		first, _, _ := strings.Cut(dirs, ":")
		return filepath.Join(first, "pushups.db")
	}
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "pushup-counter", "pushups.db")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "pushups.db"
	}
	return filepath.Join(home, ".local", "state", "pushup-counter", "pushups.db")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
