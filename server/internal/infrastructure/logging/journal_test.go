package logging

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/coreos/go-systemd/v22/journal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type journalEntry struct {
	message  string
	priority journal.Priority
	vars     map[string]string
}

type journalRecorder struct {
	entries []journalEntry
	err     error
}

func (r *journalRecorder) send(message string, priority journal.Priority, vars map[string]string) error {
	if r.err != nil {
		return r.err
	}
	r.entries = append(r.entries, journalEntry{message: message, priority: priority, vars: vars})
	return nil
}

func newTestJournalLogger(level slog.Level, rec *journalRecorder) (*slog.Logger, *bytes.Buffer) {
	fallback := &bytes.Buffer{}
	handler := newJournalHandler("fleetnode", rec.send, slog.NewTextHandler(fallback, &slog.HandlerOptions{Level: level}))
	return slog.New(handler), fallback
}

func TestJournalHandler_SendsPriorityFieldsAndLogfmtMessage(t *testing.T) {
	t.Parallel()

	rec := &journalRecorder{}
	logger, fallback := newTestJournalLogger(slog.LevelInfo, rec)

	logger.With("fleet_node_id", int64(44)).WithGroup("command").Warn("heartbeat sent",
		slog.String("kind", "telemetry"),
		slog.Any("err", errors.New("boom")),
		slog.Group("ack", slog.String("code", "OK")),
	)

	require.Len(t, rec.entries, 1)
	entry := rec.entries[0]
	assert.Equal(t, journal.PriWarning, entry.priority)
	assert.Equal(t, `heartbeat sent fleet_node_id=44 command.kind=telemetry command.err=boom command.ack.code=OK`, entry.message)
	assert.Equal(t, map[string]string{
		"SYSLOG_IDENTIFIER": "fleetnode",
		"FLEET_NODE_ID":     "44",
		"COMMAND_KIND":      "telemetry",
		"COMMAND_ERR":       "boom",
		"COMMAND_ACK_CODE":  "OK",
	}, entry.vars)
	assert.Empty(t, fallback.String(), "a delivered record must not also reach the fallback")
}

func TestJournalHandler_MapsLevelsToSyslogPriorities(t *testing.T) {
	t.Parallel()

	rec := &journalRecorder{}
	logger, _ := newTestJournalLogger(slog.LevelDebug, rec)
	logger.Debug("d")
	logger.Info("i")
	logger.Warn("w")
	logger.Error("e")

	require.Len(t, rec.entries, 4)
	var priorities []journal.Priority
	for _, e := range rec.entries {
		priorities = append(priorities, e.priority)
	}
	assert.Equal(t, []journal.Priority{journal.PriDebug, journal.PriInfo, journal.PriWarning, journal.PriErr}, priorities)
	assert.Equal(t, "d", rec.entries[0].message, "a record without attributes is sent verbatim")
}

func TestJournalHandler_RespectsLevel(t *testing.T) {
	t.Parallel()

	rec := &journalRecorder{}
	logger, _ := newTestJournalLogger(slog.LevelWarn, rec)
	logger.Info("filtered")
	logger.Warn("kept")

	require.Len(t, rec.entries, 1)
	assert.Equal(t, "kept", rec.entries[0].message)
}

func TestJournalHandler_SanitizesAndSkipsReservedFieldNames(t *testing.T) {
	t.Parallel()

	rec := &journalRecorder{}
	logger, _ := newTestJournalLogger(slog.LevelInfo, rec)
	logger.Info("names",
		"fleet-node id", 1,
		"_private", 2,
		"message", "shadow",
		"priority", 0,
		"___", 3,
	)

	require.Len(t, rec.entries, 1)
	entry := rec.entries[0]
	assert.Equal(t, map[string]string{
		"SYSLOG_IDENTIFIER": "fleetnode",
		"FLEET_NODE_ID":     "1",
		"PRIVATE":           "2",
	}, entry.vars)
	assert.Equal(t, `names "fleet-node id"=1 _private=2 message=shadow priority=0 ___=3`, entry.message,
		"skipped fields must still be readable in MESSAGE")
}

func TestJournalHandler_FallsBackWhenJournalRejects(t *testing.T) {
	t.Parallel()

	rec := &journalRecorder{err: errors.New("socket gone")}
	logger, fallback := newTestJournalLogger(slog.LevelInfo, rec)
	logger.With("fleet_node_id", int64(7)).Error("control stream closed", "reason", "eof")

	assert.Empty(t, rec.entries)
	assert.Contains(t, fallback.String(), `level=ERROR msg="control stream closed" fleet_node_id=7 reason=eof`)
}

func TestJournalHandler_GroupThenWithPrefixesFields(t *testing.T) {
	t.Parallel()

	rec := &journalRecorder{}
	logger, _ := newTestJournalLogger(slog.LevelInfo, rec)
	logger.WithGroup("g").With("k", "v").Info("grouped", "n", 1)

	require.Len(t, rec.entries, 1)
	assert.Equal(t, "grouped g.k=v g.n=1", rec.entries[0].message)
	assert.Equal(t, map[string]string{"SYSLOG_IDENTIFIER": "fleetnode", "G_K": "v", "G_N": "1"}, rec.entries[0].vars)
}

func TestJournalHandler_ClonesDoNotInterleaveUnderConcurrency(t *testing.T) {
	t.Parallel()

	const workers, perWorker = 8, 50
	rec := &lockedJournalRecorder{}
	logger := slog.New(newJournalHandler("fleetnode", rec.send, slog.NewTextHandler(&bytes.Buffer{}, nil)))

	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			l := logger.With("worker", w).WithGroup("req")
			for i := range perWorker {
				l.Info("tick", "i", i)
			}
		})
	}
	wg.Wait()

	require.Len(t, rec.entries, workers*perWorker)
	for _, e := range rec.entries {
		assert.Equal(t, fmt.Sprintf("tick worker=%s req.i=%s", e.vars["WORKER"], e.vars["REQ_I"]), e.message,
			"MESSAGE must hold only the attrs of its own record")
	}
}

type lockedJournalRecorder struct {
	mu sync.Mutex
	journalRecorder
}

func (r *lockedJournalRecorder) send(message string, priority journal.Priority, vars map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.journalRecorder.send(message, priority, vars)
}

func TestJournalFieldName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "FLEET_NODE_ID", journalFieldName("fleet_node_id"))
	assert.Equal(t, "CMD_ACK_CODE", journalFieldName("cmd_ack_code"))
	assert.Equal(t, "PLUGINS_DIR", journalFieldName("plugins.dir"))
	assert.Equal(t, "F_5M_RATE", journalFieldName("5m_rate"), "journald rejects digit-leading names")
	assert.Equal(t, "", journalFieldName("___"))
	assert.Len(t, journalFieldName(string(bytes.Repeat([]byte("a"), 100))), journalFieldMaxLen)
}

func TestNewHandler_SelectsSink(t *testing.T) {
	t.Parallel()

	enabled := func() bool { return true }
	disabled := func() bool { return false }
	stream := func() (bool, error) { return true, nil }
	noStream := func() (bool, error) { return false, nil }
	streamErr := func() (bool, error) { return false, errors.New("fstat") }

	tests := []struct {
		name            string
		output          string
		journalEnabled  func() bool
		stdoutIsJournal func() (bool, error)
		wantJournal     bool
		wantWarning     bool
	}{
		{name: "stdout ignores probes", output: OutputStdout, journalEnabled: enabled, stdoutIsJournal: stream},
		{name: "auto under systemd", output: OutputAuto, journalEnabled: enabled, stdoutIsJournal: stream, wantJournal: true},
		{name: "auto interactive", output: OutputAuto, journalEnabled: enabled, stdoutIsJournal: noStream},
		{name: "auto stream probe error", output: OutputAuto, journalEnabled: enabled, stdoutIsJournal: streamErr},
		{name: "auto stream without socket", output: OutputAuto, journalEnabled: disabled, stdoutIsJournal: stream},
		{name: "journal available", output: OutputJournal, journalEnabled: enabled, stdoutIsJournal: noStream, wantJournal: true},
		{name: "journal unavailable", output: OutputJournal, journalEnabled: disabled, stdoutIsJournal: noStream, wantWarning: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			h := newHandler(&out, slog.LevelInfo, tt.output, "fleetnode", tt.journalEnabled, tt.stdoutIsJournal)

			_, isJournal := h.(*journalHandler)
			assert.Equal(t, tt.wantJournal, isJournal)
			if tt.wantWarning {
				assert.Contains(t, out.String(), "journald is unavailable")
			} else {
				assert.Empty(t, out.String())
			}
		})
	}
}
