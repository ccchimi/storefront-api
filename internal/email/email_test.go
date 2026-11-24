package email

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestLogSenderLogsTheMessage(t *testing.T) {
	var buf bytes.Buffer
	s := LogSender{Logger: slog.New(slog.NewTextHandler(&buf, nil)), Latency: time.Millisecond}

	if err := s.Send(context.Background(), Message{To: "a@b.co", Subject: "Hi"}); err != nil {
		t.Fatal(err)
	}
	if out := buf.String(); !strings.Contains(out, "a@b.co") || !strings.Contains(out, "Hi") {
		t.Fatalf("log output = %q", out)
	}
}

func TestLogSenderGivesUpWhenCancelled(t *testing.T) {
	s := LogSender{Latency: time.Minute}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Send(ctx, Message{To: "a@b.co"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestLogSenderFallsBackToDefaultLogger(t *testing.T) {
	if err := (LogSender{}).Send(context.Background(), Message{To: "a@b.co"}); err != nil {
		t.Fatal(err)
	}
}
