// Package api wires the HTTP API: routing, handlers and the checkout flow.
package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/ccchimi/storefront-api/internal/auth"
	"github.com/ccchimi/storefront-api/internal/cart"
	"github.com/ccchimi/storefront-api/internal/catalog"
	"github.com/ccchimi/storefront-api/internal/email"
	"github.com/ccchimi/storefront-api/internal/payment"
	"github.com/ccchimi/storefront-api/internal/ratelimit"
	"github.com/ccchimi/storefront-api/internal/store"
)

// Config holds the App's dependencies and tunables. Only DBPath, Payments
// and Email are required.
type Config struct {
	DBPath   string
	Payments payment.Gateway
	Email    email.Sender

	Logger *slog.Logger
	// Now is the clock used for sessions, rate limits, caching and
	// timestamps. Tests replace it to move time forward.
	Now func() time.Time

	SessionTTL         time.Duration // default 24h
	ProductCacheTTL    time.Duration // default 30s
	EmailTimeout       time.Duration // default 30s
	PasswordIterations int           // default auth.DefaultIterations
}

// App is the API. Everything with state (carts, caches, limiters) lives
// here rather than in package globals, so two Apps never share anything.
type App struct {
	cfg      Config
	log      *slog.Logger
	store    *store.Store
	hasher   auth.Hasher
	payments payment.Gateway
	mailer   email.Sender

	carts         *cart.Store
	products      *catalog.Cache
	productLocks  keyedMutex
	loginLimit    *ratelimit.Limiter
	checkoutLimit *ratelimit.Limiter
	productsLimit *ratelimit.Limiter

	background sync.WaitGroup // confirmation emails still being sent
	closeOnce  sync.Once
	closeErr   error
}

// New opens the database, seeds demo data and returns a ready App.
func New(ctx context.Context, cfg Config) (*App, error) {
	if cfg.DBPath == "" {
		return nil, errors.New("api: DBPath is required")
	}
	if cfg.Payments == nil || cfg.Email == nil {
		return nil, errors.New("api: Payments and Email are required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 24 * time.Hour
	}
	if cfg.ProductCacheTTL <= 0 {
		cfg.ProductCacheTTL = 30 * time.Second
	}
	if cfg.EmailTimeout <= 0 {
		cfg.EmailTimeout = 30 * time.Second
	}

	st, err := store.Open(ctx, cfg.DBPath, cfg.Now)
	if err != nil {
		return nil, err
	}
	hasher := auth.Hasher{Iterations: cfg.PasswordIterations}
	if err := st.Seed(ctx, hasher.Hash); err != nil {
		st.Close()
		return nil, err
	}
	// Nothing can be in flight yet, so any key still marked as processing
	// belongs to a request that died with the previous process.
	if err := st.ReleaseInterruptedIdempotencyKeys(ctx); err != nil {
		st.Close()
		return nil, err
	}

	return &App{
		cfg:           cfg,
		log:           cfg.Logger,
		store:         st,
		hasher:        hasher,
		payments:      cfg.Payments,
		mailer:        cfg.Email,
		carts:         cart.New(),
		products:      catalog.NewCache(cfg.ProductCacheTTL, cfg.Now),
		loginLimit:    ratelimit.New(5, 15*time.Minute, cfg.Now),
		checkoutLimit: ratelimit.New(10, time.Minute, cfg.Now),
		productsLimit: ratelimit.New(60, time.Minute, cfg.Now),
	}, nil
}

// Handler returns the HTTP handler for the whole API.
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", a.handleHealth)

	mux.HandleFunc("POST /api/auth/login", a.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", a.requireUser(a.handleLogout))
	mux.HandleFunc("GET /api/me", a.requireUser(a.handleMe))

	mux.HandleFunc("GET /api/products", a.handleListProducts)

	mux.HandleFunc("GET /api/cart", a.requireUser(a.handleGetCart))
	mux.HandleFunc("POST /api/cart/items", a.requireUser(a.handleAddToCart))
	mux.HandleFunc("DELETE /api/cart/items/{product_id}", a.requireUser(a.handleRemoveFromCart))

	mux.HandleFunc("POST /api/checkout", a.requireUser(a.handleCheckout))

	mux.HandleFunc("GET /api/orders", a.requireUser(a.handleListOrders))
	mux.HandleFunc("GET /api/orders/{order_id}", a.requireUser(a.handleGetOrder))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "No such endpoint")
	})

	return a.recoverPanics(a.logRequests(limitBody(mux)))
}

// Close waits for confirmation emails that are still being delivered and
// then closes the database. Stop the HTTP server before calling it.
func (a *App) Close() error {
	a.closeOnce.Do(func() {
		a.background.Wait()
		a.closeErr = a.store.Close()
	})
	return a.closeErr
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := a.store.DB().PingContext(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "Database is not reachable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
