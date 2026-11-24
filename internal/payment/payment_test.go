package payment

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSimulatorChargesAndDeclines(t *testing.T) {
	sim := Simulator{DeclineAbove: 100_000}
	ctx := context.Background()

	id, err := sim.Charge(ctx, Charge{Amount: 5_000})
	if err != nil || !strings.HasPrefix(id, "ch_") {
		t.Fatalf("Charge = %q, %v; want a charge ID", id, err)
	}
	if _, err := sim.Charge(ctx, Charge{Amount: 100_001}); !errors.Is(err, ErrDeclined) {
		t.Fatalf("charge over the limit: err = %v, want ErrDeclined", err)
	}
	if err := sim.Refund(ctx, id); err != nil {
		t.Fatalf("Refund: %v", err)
	}
}

func TestSimulatorRespectsCancellation(t *testing.T) {
	sim := Simulator{Latency: time.Minute}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sim.Charge(ctx, Charge{Amount: 100}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if err := sim.Refund(ctx, "ch_x"); !errors.Is(err, context.Canceled) {
		t.Fatalf("refund err = %v, want context.Canceled", err)
	}
}

func TestSimulatorWaitsForLatency(t *testing.T) {
	sim := Simulator{Latency: 20 * time.Millisecond}
	start := time.Now()
	if _, err := sim.Charge(context.Background(), Charge{Amount: 100}); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 20*time.Millisecond {
		t.Fatal("simulator answered before its latency")
	}
}
