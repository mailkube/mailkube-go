package mailkube

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

// envLogLevel names the environment variable that turns logging on, e.g. MAILKUBE_LOG=debug.
const envLogLevel = "MAILKUBE_LOG"

// sensitiveHeaders are masked wherever headers are logged. This is the only place the list lives.
var sensitiveHeaders = map[string]bool{
	"authorization":   true,
	"idempotency-key": true,
}

// redactedValue replaces a secret header's value in log output.
const redactedValue = "***"

// WithLogger sets the logger the client writes request and response lines to.
//
// Logging is off by default. An explicit logger wins over the MAILKUBE_LOG environment
// variable; passing nil is the same as not passing the option at all.
func WithLogger(logger *slog.Logger) Option {
	return func(c *Client) { c.logger = logger }
}

// resolveLogger picks the logger for a client: the explicit one, else MAILKUBE_LOG, else silence.
//
// A standalone function rather than inline in New for two reasons: New is a composition root
// already close to the complexity limit, and this is the one place the "silent by default"
// promise is kept.
func resolveLogger(explicit *slog.Logger) *slog.Logger {
	if explicit != nil {
		return explicit
	}
	raw := strings.TrimSpace(os.Getenv(envLogLevel))
	if raw == "" {
		return slog.New(discardHandler{})
	}

	// An unparseable value still turns logging on, at debug: the caller asked for output, and
	// failing silently would be the one behaviour they cannot debug.
	level := slog.LevelDebug
	_ = level.UnmarshalText([]byte(raw))
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// redactHeaders returns the headers as a flat map with the secret ones masked, safe to log.
func redactHeaders(headers http.Header) map[string]string {
	safe := make(map[string]string, len(headers))
	for name := range headers {
		if sensitiveHeaders[strings.ToLower(name)] {
			safe[name] = redactedValue
			continue
		}
		safe[name] = headers.Get(name)
	}
	return safe
}

// discardHandler is a slog.Handler that drops everything.
//
// The Null Object that makes "silent by default" free: c.logger is never nil, so no call site
// carries a nil check and no logging branch can go untested. slog.DiscardHandler would do the
// same job but only arrives in Go 1.24, and this module supports 1.23.
type discardHandler struct{}

// Enabled reports that no level is enabled, so slog skips building the record entirely.
func (discardHandler) Enabled(context.Context, slog.Level) bool { return false }

// Handle drops the record.
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }

// WithAttrs returns the same handler: there is nothing to attach attributes to.
func (h discardHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

// WithGroup returns the same handler: there is nothing to group.
func (h discardHandler) WithGroup(string) slog.Handler { return h }
