// Package logging provides an injectable logging contract and a standard-library
// implementation. Applications select implementations through their DI bindings.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
)

// Logger writes structured events. Fields are alternating string keys and values,
// or slog.Attr values when using SlogLogger. Implementations must be safe for
// concurrent use. With returns a logger with additional fields without changing
// the receiver. Logging does not exit the process or panic.
type Logger interface {
	Debug(context.Context, string, ...any)
	Info(context.Context, string, ...any)
	Warn(context.Context, string, ...any)
	Error(context.Context, string, ...any)
	With(...any) Logger
}

// Options configures the default implementation. The zero value writes text
// events at info level or above to stderr. Output must support concurrent use
// when shared by independently constructed loggers.
type Options struct {
	Output io.Writer
	Level  slog.Level
	JSON   bool
}

type SlogLogger struct{ logger *slog.Logger }

var _ Logger = (*SlogLogger)(nil)

// New creates an independent logger; it never modifies slog's global default.
func New(options Options) *SlogLogger {
	output := options.Output
	if output == nil {
		output = os.Stderr
	}
	handlerOptions := &slog.HandlerOptions{Level: options.Level}
	var handler slog.Handler = slog.NewTextHandler(output, handlerOptions)
	if options.JSON {
		handler = slog.NewJSONHandler(output, handlerOptions)
	}
	return &SlogLogger{logger: slog.New(handler)}
}

func (l *SlogLogger) Debug(ctx context.Context, message string, fields ...any) {
	l.logger.DebugContext(ctx, message, fields...)
}
func (l *SlogLogger) Info(ctx context.Context, message string, fields ...any) {
	l.logger.InfoContext(ctx, message, fields...)
}
func (l *SlogLogger) Warn(ctx context.Context, message string, fields ...any) {
	l.logger.WarnContext(ctx, message, fields...)
}
func (l *SlogLogger) Error(ctx context.Context, message string, fields ...any) {
	l.logger.ErrorContext(ctx, message, fields...)
}
func (l *SlogLogger) With(fields ...any) Logger {
	return &SlogLogger{logger: l.logger.With(fields...)}
}
