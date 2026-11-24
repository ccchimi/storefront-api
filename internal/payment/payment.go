// Package payment defines the payment provider boundary.
//
// The real provider is an external service: it can be slow and it can say no.
// The rest of the code only sees the Gateway interface, so tests and local
// runs can swap in the simulator.
package payment

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/ccchimi/storefront-api/internal/money"
)

// ErrDeclined means the provider refused the charge. The customer was not charged.
var ErrDeclined = errors.New("payment declined")

// Charge describes a payment request.
type Charge struct {
	UserID int64
	Amount money.Cents
	// Reference is our own identifier for the attempt, useful when reconciling
	// with the provider.
	Reference string
}

// Gateway is a payment provider.
type Gateway interface {
	// Charge takes the money and returns the provider's charge ID, or
	// ErrDeclined (possibly wrapped) when the payment is refused.
	Charge(ctx context.Context, c Charge) (chargeID string, err error)
	// Refund gives back a previous charge in full.
	Refund(ctx context.Context, chargeID string) error
}

// Simulator is a fake provider for local development. It waits Latency
// before answering and declines anything above DeclineAbove, which gives an
// easy way to see a 402 from curl.
type Simulator struct {
	Latency      time.Duration
	DeclineAbove money.Cents
}

func (s Simulator) Charge(ctx context.Context, c Charge) (string, error) {
	if err := sleep(ctx, s.Latency); err != nil {
		return "", err
	}
	if s.DeclineAbove > 0 && c.Amount > s.DeclineAbove {
		return "", fmt.Errorf("%w: amount %s over the simulated card limit", ErrDeclined, c.Amount)
	}
	return "ch_" + rand.Text()[:16], nil
}

func (s Simulator) Refund(ctx context.Context, _ string) error {
	return sleep(ctx, s.Latency)
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
