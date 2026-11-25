package logging

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"strings"
	"sync"

	"github.com/coreos/go-systemd/v22/journal"
)

// Log sinks accepted by NewHandler; use them as a kong `enum:"auto,stdout,journal"`.
const (
	OutputAuto    = "auto"    // journal when systemd already connected stdout to it, otherwise stdout
	OutputStdout  = "stdout"  // logfmt text lines on w
	OutputJournal = "journal" // native journal entries; stdout with a warning if journald is unavailable
)

// journalSender matches journal.Send; tests substitute a recorder.
type journalSender func(message string, priority journal.Priority, vars map[string]string) error

// journalFieldMaxLen is journald's limit for field names.
const journalFieldMaxLen = 64

// journalReservedFields are set by journald or by the handler itself; an
// attribute that maps onto one stays in MESSAGE but is not sent as a field.
var journalReservedFields = map[string]struct{}{
	"MESSAGE": {}, "MESSAGE_ID": {}, "PRIORITY": {}, "ERRNO": {},
	"CODE_FILE": {}, "CODE_LINE": {}, "CODE_FUNC": {},
	"SYSLOG_IDENTIFIER": {}, "SYSLOG_FACILITY": {}, "SYSLOG_PID": {}, "SYSLOG_TIMESTAMP": {},
}

// NewHandler selects a process log sink for a systemd-managed daemon.
// identifier becomes SYSLOG_IDENTIFIER on native journal entries. OutputAuto
// uses the journal only when systemd already connected stdout to it, so
// interactive and container runs keep plain text on w.
func NewHandler(w io.Writer, level slog.Leveler, output, identifier string) slog.Handler {
	return newHandler(w, level, output, identifier, journal.Enabled, journal.StdoutIsJournalStream)
}

// newHandler takes the journald probes as parameters so tests can drive the
// sink selection without a journal.
func newHandler(w io.Writer, level slog.Leveler, output, identifier string,
	journalEnabled func() bool, stdoutIsJournal func() (bool, error),
) slog.Handler {
	text := slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})
	switch output {
	case OutputJournal:
		if journalEnabled() {
			return NewJournalHandler(identifier, text)
		}
		slog.New(text).Warn("journal log output requested but journald is unavailable; logging to stdout")
	case OutputAuto:
		if ok, err := stdoutIsJournal(); err == nil && ok && journalEnabled() {
			return NewJournalHandler(identifier, text)
		}
	}
	return text
}

// NewJournalHandler returns a handler that writes records to the systemd
// journal as native entries instead of a text stream. PRIORITY carries the
// level, so `journalctl -p` and journal-tailing log shippers see the real
// severity rather than the stdout default of info; every attribute becomes an
// uppercase journal field; and MESSAGE keeps the logfmt body so `journalctl -u`
// reads like the text handler. Records the journal rejects go to fallback,
// which also owns the level filter.
func NewJournalHandler(identifier string, fallback slog.Handler) slog.Handler {
	return newJournalHandler(identifier, journal.Send, fallback)
}

type journalHandler struct {
	send     journalSender
	fallback slog.Handler
	// text renders the logfmt attribute tail of MESSAGE into body under mu;
	// clones share both so concurrent Handle calls cannot interleave.
	text   slog.Handler
	mu     *sync.Mutex
	body   *bytes.Buffer
	fields map[string]string // journal fields fixed for every record of this handler
	prefix string            // open groups joined with "_"; journalFieldName sanitizes prefix+key
}

func newJournalHandler(identifier string, send journalSender, fallback slog.Handler) *journalHandler {
	body := &bytes.Buffer{}
	return &journalHandler{
		send:     send,
		fallback: fallback,
		text: slog.NewTextHandler(body, &slog.HandlerOptions{
			Level:       slog.LevelDebug, // the outer handler already filtered
			ReplaceAttr: dropJournalBuiltins,
		}),
		mu:     &sync.Mutex{},
		body:   body,
		fields: map[string]string{"SYSLOG_IDENTIFIER": identifier},
	}
}

// dropJournalBuiltins removes the time, level, and message keys the text
// handler would print: journald records the time and PRIORITY itself, and the
// message is prepended verbatim so MESSAGE starts with the human-readable text.
func dropJournalBuiltins(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && (a.Key == slog.TimeKey || a.Key == slog.LevelKey || a.Key == slog.MessageKey) {
		return slog.Attr{}
	}
	return a
}

func (h *journalHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.fallback.Enabled(ctx, level)
}

func (h *journalHandler) Handle(ctx context.Context, r slog.Record) error {
	vars := maps.Clone(h.fields)
	r.Attrs(func(a slog.Attr) bool {
		addJournalField(vars, h.prefix, a)
		return true
	})

	h.mu.Lock()
	h.body.Reset()
	_ = h.text.Handle(ctx, r) // writes to a bytes.Buffer; cannot fail
	tail := strings.TrimSpace(h.body.String())
	h.mu.Unlock()

	message := r.Message
	if tail != "" {
		message += " " + tail
	}
	if h.send(message, journalPriority(r.Level), vars) == nil {
		return nil
	}
	if err := h.fallback.Handle(ctx, r); err != nil {
		return fmt.Errorf("fallback log handler: %w", err)
	}
	return nil
}

func (h *journalHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	clone := *h
	clone.text = h.text.WithAttrs(attrs)
	clone.fallback = h.fallback.WithAttrs(attrs)
	clone.fields = maps.Clone(h.fields)
	for _, a := range attrs {
		addJournalField(clone.fields, h.prefix, a)
	}
	return &clone
}

func (h *journalHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.text = h.text.WithGroup(name)
	clone.fallback = h.fallback.WithGroup(name)
	clone.prefix = h.prefix + name + "_"
	return &clone
}

func journalPriority(level slog.Level) journal.Priority {
	switch {
	case level >= slog.LevelError:
		return journal.PriErr
	case level >= slog.LevelWarn:
		return journal.PriWarning
	case level >= slog.LevelInfo:
		return journal.PriInfo
	default:
		return journal.PriDebug
	}
}

// addJournalField flattens one attribute into vars under the group prefix.
// Group values recurse; empty keys and names that are reserved or sanitize to
// nothing are skipped (they still appear in MESSAGE).
func addJournalField(vars map[string]string, prefix string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		nested := prefix
		if a.Key != "" {
			nested += a.Key + "_"
		}
		for _, member := range a.Value.Group() {
			addJournalField(vars, nested, member)
		}
		return
	}
	if a.Key == "" {
		return
	}
	name := journalFieldName(prefix + a.Key)
	if _, reserved := journalReservedFields[name]; name == "" || reserved {
		return
	}
	vars[name] = a.Value.String()
}

// journalFieldName maps a group-qualified slog key onto journald's field
// grammar: uppercase letters, digits, and underscores, not starting with an
// underscore or a digit, at most 64 characters.
func journalFieldName(key string) string {
	name := strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, strings.ToUpper(key))
	name = strings.TrimLeft(name, "_")
	if name != "" && name[0] >= '0' && name[0] <= '9' {
		name = "F_" + name
	}
	if len(name) > journalFieldMaxLen {
		name = name[:journalFieldMaxLen]
	}
	return name
}
