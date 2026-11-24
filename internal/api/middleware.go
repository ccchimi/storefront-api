package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ccchimi/storefront-api/internal/auth"
	"github.com/ccchimi/storefront-api/internal/store"
)

const maxBodyBytes = 1 << 20

type ctxKey int

const userKey ctxKey = iota

// requireUser rejects requests without a valid bearer token and passes the
// authenticated user to the handler.
func (a *App) requireUser(next func(http.ResponseWriter, *http.Request, store.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing or malformed Authorization header")
			return
		}
		user, err := a.store.UserBySession(r.Context(), auth.HashToken(token))
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Session is invalid or expired")
			return
		}
		if err != nil {
			a.log.Error("look up session", "err", err)
			writeInternal(w)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, user)), user)
	}
}

func bearerToken(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

// clientIP uses the TCP peer address. If the API ever sits behind a proxy,
// this is the place to trust X-Forwarded-For from known proxies only.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

func (a *App) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				a.log.Error("panic while serving request", "panic", v, "method", r.Method, "path", r.URL.Path)
				writeInternal(w)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (a *App) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		a.log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(start).Round(time.Microsecond),
		)
	})
}
