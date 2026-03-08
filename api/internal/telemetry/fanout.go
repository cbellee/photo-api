// Package telemetry – fanout handler for slog.
package telemetry

import (
	"context"
	"log/slog"
)

// FanoutHandler is an slog.Handler that forwards every log record to multiple
// underlying handlers. This allows, for example, writing structured JSON to
// stdout while simultaneously bridging logs to the OpenTelemetry Log SDK.
type FanoutHandler struct {
	handlers []slog.Handler
}

// NewFanoutHandler creates a handler that fans out to all supplied handlers.
func NewFanoutHandler(handlers ...slog.Handler) *FanoutHandler {
	return &FanoutHandler{handlers: handlers}
}

func (h *FanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *FanoutHandler) Handle(ctx context.Context, record slog.Record) error {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, record.Level) {
			if err := handler.Handle(ctx, record); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *FanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	cloned := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		cloned[i] = handler.WithAttrs(attrs)
	}
	return NewFanoutHandler(cloned...)
}

func (h *FanoutHandler) WithGroup(name string) slog.Handler {
	cloned := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		cloned[i] = handler.WithGroup(name)
	}
	return NewFanoutHandler(cloned...)
}
