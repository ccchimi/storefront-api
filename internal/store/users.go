package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// User is an account.
type User struct {
	ID           int64
	Email        string
	Name         string
	PasswordHash string
}

// UserByEmail looks a user up by email, ignoring case.
func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email, name, password_hash FROM users WHERE email = ?`, email).
		Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

// CreateSession stores a session for the given token hash.
func (s *Store) CreateSession(ctx context.Context, userID int64, tokenHash string, ttl time.Duration) (time.Time, error) {
	now := s.now()
	expires := now.Add(ttl)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		tokenHash, userID, formatTime(now), formatTime(expires))
	return expires, err
}

// UserBySession resolves a live session to its user.
func (s *Store) UserBySession(ctx context.Context, tokenHash string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.email, u.name, u.password_hash
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`,
		tokenHash, formatTime(s.now())).
		Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

// DeleteSession logs a session out.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}
