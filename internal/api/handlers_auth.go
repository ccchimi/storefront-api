package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ccchimi/storefront-api/internal/auth"
	"github.com/ccchimi/storefront-api/internal/store"
)

type userJSON struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

func toUserJSON(u store.User) userJSON { return userJSON{ID: u.ID, Email: u.Email, Name: u.Name} }

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeObject(r)
	if !ok {
		writeValidation(w, invalidBody())
		return
	}
	fields := map[string]string{}
	email, ok := stringField(body, "email")
	if !ok || !validEmail(email) {
		fields["email"] = "must be a valid email address"
	}
	password, ok := stringField(body, "password")
	if !ok || password == "" {
		fields["password"] = "must be a non-empty string"
	}
	if len(fields) > 0 {
		writeValidation(w, fields)
		return
	}

	// Throttling is per email, so guessing one account's password can't lock
	// out everybody behind the same IP. Once the limit is hit, even the right
	// password is refused until the window passes.
	limitKey := strings.ToLower(email)
	if blocked, retry := a.loginLimit.Exceeded(limitKey); blocked {
		writeRateLimited(w, retry)
		return
	}

	user, err := a.store.UserByEmail(r.Context(), email)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		a.log.Error("look up user", "err", err)
		writeInternal(w)
		return
	}
	if err != nil || !auth.Verify(user.PasswordHash, password) {
		a.loginLimit.Record(limitKey)
		// Same answer for unknown email and wrong password.
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Email or password is incorrect")
		return
	}
	a.loginLimit.Reset(limitKey)

	token, hash := auth.NewToken()
	expires, err := a.store.CreateSession(r.Context(), user.ID, hash, a.cfg.SessionTTL)
	if err != nil {
		a.log.Error("create session", "err", err)
		writeInternal(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":      token,
		"expires_at": expires.UTC().Format(time.RFC3339),
		"user":       toUserJSON(user),
	})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request, _ store.User) {
	token, _ := bearerToken(r)
	if err := a.store.DeleteSession(r.Context(), auth.HashToken(token)); err != nil {
		a.log.Error("delete session", "err", err)
		writeInternal(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleMe(w http.ResponseWriter, _ *http.Request, user store.User) {
	writeJSON(w, http.StatusOK, toUserJSON(user))
}
