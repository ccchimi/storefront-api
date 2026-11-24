package store

import (
	"context"
	"database/sql"
	"errors"
)

// IdempotencyRecord is what we know about a previously seen key.
type IdempotencyRecord struct {
	Completed   bool
	RequestHash string
	Status      int
	Body        []byte
}

// ReserveIdempotencyKey claims (user, key) for a new request. When the key
// is new it returns reserved=true. Otherwise it returns the existing record
// so the caller can replay it, reject a mismatch, or report that the first
// request is still running.
func (s *Store) ReserveIdempotencyKey(ctx context.Context, userID int64, key, requestHash string) (reserved bool, rec IdempotencyRecord, err error) {
	// A failed request releases its key, and that can happen between our
	// INSERT and SELECT. Retrying a couple of times covers that window.
	for range 3 {
		res, err := s.db.ExecContext(ctx, `
			INSERT INTO idempotency_keys (user_id, key, request_hash, state, created_at)
			VALUES (?, ?, ?, 'processing', ?)
			ON CONFLICT (user_id, key) DO NOTHING`,
			userID, key, requestHash, formatTime(s.now()))
		if err != nil {
			return false, rec, err
		}
		if n, err := res.RowsAffected(); err != nil {
			return false, rec, err
		} else if n == 1 {
			return true, rec, nil
		}

		var state string
		var status sql.NullInt64
		err = s.db.QueryRowContext(ctx, `
			SELECT state, request_hash, response_status, response_body
			FROM idempotency_keys WHERE user_id = ? AND key = ?`, userID, key).
			Scan(&state, &rec.RequestHash, &status, &rec.Body)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return false, rec, err
		}
		rec.Completed = state == "completed"
		rec.Status = int(status.Int64)
		return false, rec, nil
	}
	return false, rec, errors.New("idempotency key kept changing while being reserved")
}

// CompleteIdempotencyKey stores the final response so retries can replay it.
func (s *Store) CompleteIdempotencyKey(ctx context.Context, userID int64, key string, status int, body []byte) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE idempotency_keys SET state = 'completed', response_status = ?, response_body = ?
		WHERE user_id = ? AND key = ?`, status, body, userID, key)
	return err
}

// ReleaseIdempotencyKey forgets a key whose request failed, so the client
// can retry with the same key.
func (s *Store) ReleaseIdempotencyKey(ctx context.Context, userID int64, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM idempotency_keys WHERE user_id = ? AND key = ?`, userID, key)
	return err
}

// ReleaseInterruptedIdempotencyKeys clears keys left in 'processing' by a
// crash. Called on startup, when no request can be in flight.
func (s *Store) ReleaseInterruptedIdempotencyKeys(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM idempotency_keys WHERE state = 'processing'`)
	return err
}
