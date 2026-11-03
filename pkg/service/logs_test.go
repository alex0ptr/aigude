package service_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// logCapture collects what the service logs while a test runs.
type logCapture struct {
	mu      sync.Mutex
	records []string
}

// Enabled accepts every level.
func (c *logCapture) Enabled(context.Context, slog.Level) bool { return true }

// Handle keeps the record as one line of text.
func (c *logCapture) Handle(_ context.Context, r slog.Record) error {
	var line bytes.Buffer
	line.WriteString(r.Level.String() + " " + r.Message)
	r.Attrs(func(a slog.Attr) bool {
		line.WriteString(" " + a.Key + "=" + a.Value.String())
		return true
	})
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, line.String())
	return nil
}

// WithAttrs returns the same capture, attributes are not needed.
func (c *logCapture) WithAttrs([]slog.Attr) slog.Handler { return c }

// WithGroup returns the same capture, groups are not needed.
func (c *logCapture) WithGroup(string) slog.Handler { return c }

// Lines returns the captured records, one line each: level, message and attributes.
func (c *logCapture) Lines() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.records...)
}

// Contains tells whether a captured record contains all the parts.
func (c *logCapture) Contains(parts ...string) bool {
	for _, line := range c.Lines() {
		found := true
		for _, part := range parts {
			found = found && strings.Contains(line, part)
		}
		if found {
			return true
		}
	}
	return false
}

// captureLogs sends the logs of the whole process to the returned capture until the test ends. Tests that use it
// do not run in parallel.
func captureLogs(t *testing.T) *logCapture {
	t.Helper()
	capture := &logCapture{}
	previous := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return capture
}
