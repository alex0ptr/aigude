// Package logbuffer keeps the newest log lines of the service in memory so that the web interface can show them.
package logbuffer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Handler is a slog.Handler that hands every record to the normal handler and keeps the newest ones as text lines.
// The normal handler must not be the default handler of slog: that one writes through the log package, which leads
// back to the default logger and so to this Handler again.
type Handler struct {
	next  slog.Handler
	lines *ring
	// attrs are the attributes of the logger this handler was derived for, already rendered with their group prefix.
	attrs []string
	// group is the prefix of the keys of the following attributes.
	group string
}

var _ slog.Handler = (*Handler)(nil)

// ring holds the newest lines, shared by a Handler and the handlers derived from it.
type ring struct {
	mu       sync.Mutex
	capacity int
	lines    []string
	// epoch tells this ring from the ring of an earlier run of the service, so that a cursor of that run is not trusted.
	epoch int64
	// total is how many lines were ever added, the ones that were dropped included.
	total uint64
}

// New creates a Handler that passes records on to next and keeps the newest capacity lines of those that next accepts.
func New(next slog.Handler, capacity int) *Handler {
	return &Handler{next: next, lines: &ring{capacity: capacity, epoch: time.Now().UnixNano()}}
}

// Lines returns the kept lines, oldest first: time, level, message and attributes.
func (h *Handler) Lines() []string {
	h.lines.mu.Lock()
	defer h.lines.mu.Unlock()
	return append([]string(nil), h.lines.lines...)
}

// Since returns the lines added after the position the cursor stands for, oldest first, and the cursor for the next
// call. An empty cursor, one that cannot be read and one of another run of the service stand for no position: then all
// kept lines are returned and reset is true, which tells the reader to forget what it showed before. Lines that were
// dropped between two calls are missing without notice.
func (h *Handler) Since(cursor string) (lines []string, next string, reset bool) {
	r := h.lines
	r.mu.Lock()
	defer r.mu.Unlock()
	var epoch int64
	var after uint64
	if _, err := fmt.Sscanf(cursor, "%d.%d", &epoch, &after); err != nil || epoch != r.epoch || after > r.total {
		after, reset = 0, true
	}
	count := min(int(r.total-after), len(r.lines))
	lines = append([]string(nil), r.lines[len(r.lines)-count:]...)
	return lines, fmt.Sprintf("%d.%d", r.epoch, r.total), reset
}

// Enabled asks the normal handler, so that a record it would drop is not kept either.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle keeps the record as a line and passes it on.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	var line strings.Builder
	line.WriteString(r.Time.Format(time.DateTime) + " " + r.Level.String() + " " + r.Message)
	for _, attr := range h.attrs {
		line.WriteString(" " + attr)
	}
	r.Attrs(func(a slog.Attr) bool {
		for _, text := range renderAll(h.group, []slog.Attr{a}) {
			line.WriteString(" " + text)
		}
		return true
	})
	h.lines.add(line.String())
	return h.next.Handle(ctx, r)
}

// WithAttrs returns a Handler whose lines carry the attributes.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	derived := *h
	derived.next = h.next.WithAttrs(attrs)
	derived.attrs = append(append([]string(nil), h.attrs...), renderAll(h.group, attrs)...)
	return &derived
}

// WithGroup returns a Handler whose following attributes have the group name as a prefix of their keys.
func (h *Handler) WithGroup(name string) slog.Handler {
	derived := *h
	derived.next = h.next.WithGroup(name)
	derived.group = h.group + name + "."
	return &derived
}

// add keeps a line and drops the oldest one when the capacity is exceeded.
func (r *ring) add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
	r.total++
	if len(r.lines) > r.capacity {
		r.lines = r.lines[len(r.lines)-r.capacity:]
	}
}

// renderAll renders attributes as key=value text with the group prefix. A value that resolves to a group is shown as
// one key=value for each of its attributes, with the key of the group as prefix.
func renderAll(group string, attrs []slog.Attr) []string {
	rendered := make([]string, 0, len(attrs))
	for _, a := range attrs {
		value := a.Value.Resolve()
		if value.Kind() == slog.KindGroup {
			rendered = append(rendered, renderAll(group+a.Key+".", value.Group())...)
			continue
		}
		rendered = append(rendered, group+a.Key+"="+value.String())
	}
	return rendered
}
