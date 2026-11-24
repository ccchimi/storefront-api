// Command storefront runs the Storefront API.
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
	"syscall"
	"time"

	"github.com/ccchimi/storefront-api/internal/api"
	"github.com/ccchimi/storefront-api/internal/email"
	"github.com/ccchimi/storefront-api/internal/payment"
)

// version is set at build time with -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	addr := flag.String("addr", envOr("ADDR", ":8000"), "address to listen on")
	dbPath := flag.String("db", envOr("DB_PATH", "storefront.db"), "path to the SQLite database file")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("storefront", version)
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil)).With("version", version)
	if err := run(*addr, *dbPath, logger); err != nil {
		logger.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func run(addr, dbPath string, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app, err := api.New(ctx, api.Config{
		DBPath: dbPath,
		// Simulated providers with realistic delays. Anything over $1,000
		// gets declined, which makes the 402 path easy to try out.
		Payments: payment.Simulator{Latency: 300 * time.Millisecond, DeclineAbove: 100_000},
		Email:    email.LogSender{Logger: logger, Latency: 2 * time.Second},
		Logger:   logger,
	})
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", addr, "db", dbPath)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			app.Close()
			return err
		}
	case <-ctx.Done():
		logger.Info("shutting down")
	}

	// Finish in-flight requests first, then let the App flush pending emails
	// and close the database.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown", "err", err)
	}
	return app.Close()
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
