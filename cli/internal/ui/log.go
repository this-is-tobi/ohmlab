package ui

import (
	"context"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
)

// NewLogHandler is the handler of the process's logs: slog's JSON for a
// program, unchanged, and for a person the message and its attributes, with
// no time and warnings and errors marked.
func NewLogHandler(w io.Writer, mode Mode, color bool, level slog.Leveler) slog.Handler {
	if mode == JSON {
		return slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	}
	if level == nil {
		level = slog.LevelInfo
	}
	return &humanHandler{mu: &sync.Mutex{}, w: w, color: color, level: level}
}

type humanHandler struct {
	mu    *sync.Mutex
	w     io.Writer
	color bool
	level slog.Leveler
	attrs string // rendered by WithAttrs, each with a leading space
	group string // the WithGroup prefix, "a.b."
}

func (h *humanHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level.Level()
}

func (h *humanHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	switch {
	case r.Level >= slog.LevelError:
		b.WriteString(paint(h.color, red, "error: "))
	case r.Level >= slog.LevelWarn:
		b.WriteString(paint(h.color, yellow, "warning: "))
	}
	b.WriteString(r.Message)
	b.WriteString(h.attrs)
	r.Attrs(func(a slog.Attr) bool {
		writeAttr(&b, h.group, a)
		return true
	})
	b.WriteByte('\n')
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, b.String())
	return err
}

func (h *humanHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	var b strings.Builder
	for _, a := range attrs {
		writeAttr(&b, h.group, a)
	}
	c := *h
	c.attrs += b.String()
	return &c
}

func (h *humanHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	c := *h
	c.group += name + "."
	return &c
}

// writeAttr writes " key=value", groups flattened as "group.key", values
// quoted when they hold spaces, quotes or '='.
func writeAttr(b *strings.Builder, prefix string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return
	}
	if a.Value.Kind() == slog.KindGroup {
		if a.Key != "" {
			prefix += a.Key + "."
		}
		for _, g := range a.Value.Group() {
			writeAttr(b, prefix, g)
		}
		return
	}
	v := a.Value.String()
	if v == "" || strings.ContainsAny(v, " \t\n\"=") {
		v = strconv.Quote(v)
	}
	b.WriteString(" " + prefix + a.Key + "=" + v)
}
