package store

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// Handler writes every log record to the console and to app_log, so the same
// line is both tailable with journalctl and queryable with sqlite.
type Handler struct {
	store   *LogStore
	console slog.Handler
	attrs   []slog.Attr
	group   string
}

func NewHandler(store *LogStore, console slog.Handler) *Handler {
	return &Handler{store: store, console: console}
}

func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.console.Enabled(ctx, level)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{
		store:   h.store,
		console: h.console.WithAttrs(attrs),
		attrs:   append(append([]slog.Attr(nil), h.attrs...), attrs...),
		group:   h.group,
	}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{store: h.store, console: h.console.WithGroup(name), attrs: h.attrs, group: name}
}

func (h *Handler) Handle(ctx context.Context, record slog.Record) error {
	var message strings.Builder
	message.WriteString(record.Message)
	for _, attr := range h.attrs {
		fmt.Fprintf(&message, " %s=%v", attr.Key, attr.Value)
	}
	record.Attrs(func(attr slog.Attr) bool {
		fmt.Fprintf(&message, " %s=%v", attr.Key, attr.Value)
		return true
	})
	logger := h.group
	if logger == "" {
		logger = "rating_bot"
	}
	if err := h.store.RecordLog(record.Time, record.Level.String(), logger, message.String()); err != nil {
		// A broken log sink must not take the bot down, and must not recurse
		// back into this handler either.
		fmt.Printf("could not write log to sqlite: %v\n", err)
	}
	return h.console.Handle(ctx, record)
}
