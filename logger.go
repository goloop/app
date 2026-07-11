package app

// Logger is the minimal logging surface the App uses for lifecycle events. It
// is intentionally a subset of *slog.Logger, so a *slog.Logger satisfies it
// directly; any other logger can be adapted with a few lines.
type Logger interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
}

// nopLogger is the default Logger: it discards everything.
type nopLogger struct{}

// Info discards the info-level record.
func (nopLogger) Info(string, ...any) {}

// Error discards the error-level record.
func (nopLogger) Error(string, ...any) {}
