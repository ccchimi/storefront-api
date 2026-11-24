// Package email defines the boundary with the email vendor.
package email

import (
	"context"
	"log/slog"
	"time"
)

// Message is a plain-text email.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Sender delivers email. Vendors are slow, so callers should not block a
// request on it.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// LogSender pretends to deliver mail by writing it to the log after Latency.
type LogSender struct {
	Logger  *slog.Logger
	Latency time.Duration
}

func (s LogSender) Send(ctx context.Context, m Message) error {
	if s.Latency > 0 {
		t := time.NewTimer(s.Latency)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	logger := s.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("email sent", "to", m.To, "subject", m.Subject)
	return nil
}
