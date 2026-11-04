package ui

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestLogHandlerJSONIsSlogJSON(t *testing.T) {
	r := slog.NewRecord(time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC), slog.LevelInfo, "x", 0)
	r.AddAttrs(slog.String("k", "v"), slog.Int("n", 3))
	var got, want bytes.Buffer
	if err := NewLogHandler(&got, JSON, false, slog.LevelInfo).Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if err := slog.NewJSONHandler(&want, &slog.HandlerOptions{Level: slog.LevelInfo}).Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if got.String() != want.String() {
		t.Errorf("got %q, want %q", got.String(), want.String())
	}
}

func TestLogHandlerHuman(t *testing.T) {
	var b bytes.Buffer
	l := slog.New(NewLogHandler(&b, Human, false, slog.LevelInfo))
	l.Debug("hidden")
	l.Info("msg", "k", "v")
	l.Warn("careful")
	l.Error("failed", "err", "boom")
	l.With("app", "gitea").WithGroup("vault").Info("synced", "path", "a b")
	want := "msg k=v\nwarning: careful\nerror: failed err=boom\nsynced app=gitea vault.path=\"a b\"\n"
	if b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}
