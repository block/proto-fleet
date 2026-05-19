package metrics

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeDB captures every ExecContext call so the test can assert what the
// writer pushed into TimescaleDB. The earlier provider test used a fake OTLP
// receiver here; the TimescaleDB shim does not run a network endpoint, so we
// substitute the *sql.DB interface directly.
type fakeDB struct {
	mu       sync.Mutex
	queries  []string
	argsByQ  [][]any
	received chan struct{}
}

func newFakeDB() *fakeDB {
	return &fakeDB{received: make(chan struct{}, 1)}
}

func (f *fakeDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	f.mu.Lock()
	f.queries = append(f.queries, query)
	f.argsByQ = append(f.argsByQ, args)
	f.mu.Unlock()
	select {
	case f.received <- struct{}{}:
	default:
	}
	return nopResult{}, nil
}

func (f *fakeDB) tables() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.queries))
	for _, q := range f.queries {
		// queries are of the form "INSERT INTO <table> ..."
		const prefix = "INSERT INTO "
		if !strings.HasPrefix(q, prefix) {
			continue
		}
		rest := q[len(prefix):]
		if idx := strings.IndexByte(rest, ' '); idx > 0 {
			out = append(out, rest[:idx])
		}
	}
	return out
}

type nopResult struct{}

func (nopResult) LastInsertId() (int64, error) { return 0, nil }
func (nopResult) RowsAffected() (int64, error) { return 0, nil }

func TestSetupWritesContractRowsToTimescaleDB(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fake := newFakeDB()
	cfg := Config{
		Enabled:       true,
		FlushInterval: 100 * time.Millisecond,
		BufferSize:    64,
		BatchSize:     32,
	}
	provider, err := Setup(ctx, "test", cfg, fake)
	require.NoError(t, err)
	require.True(t, provider.Enabled())

	labels := DeviceLabels{
		OrganizationID: "1",
		DeviceID:       "42",
		DeviceGroup:    "group-a",
		Driver:         "virtual",
	}

	provider.EmitDeviceOnline(ctx, labels, true)
	provider.EmitDeviceHashrate(ctx, labels, 110.5, 115.0)
	provider.EmitDeviceTemperature(ctx, labels, SensorKindBoard, 75.0, 70.0)
	provider.EmitDevicePoolConnected(ctx, labels, true)
	provider.EmitCommand(ctx, CommandLabels{
		OrganizationID: labels.OrganizationID,
		Kind:           "reboot",
		Result:         ResultSuccess,
	})
	provider.EmitTelemetryPoll(ctx, TelemetryPollLabels{
		OrganizationID: labels.OrganizationID,
		DeviceID:       labels.DeviceID,
		Result:         ResultSuccess,
	})

	// Wait for the writer to flush at least once.
	select {
	case <-fake.received:
	case <-time.After(5 * time.Second):
		t.Fatal("writer never executed a query")
	}

	require.NoError(t, provider.Shutdown(ctx))

	tables := fake.tables()
	for _, want := range []string{
		"notification_device_metrics",
		"notification_device_temperature",
		"notification_command_events",
		"notification_telemetry_poll_events",
	} {
		require.Contains(t, tables, want, "expected an INSERT into %q", want)
	}
}

// disabling the metrics package leaves the rest of the system functional
func TestSetupDisabledIsNoOp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	provider, err := Setup(ctx, "test", Config{Enabled: false}, nil)
	require.NoError(t, err)
	require.False(t, provider.Enabled())

	// These must not panic and must not block.
	labels := DeviceLabels{OrganizationID: "1", DeviceID: "42"}
	provider.EmitDeviceOnline(ctx, labels, false)
	provider.EmitDeviceHashrate(ctx, labels, 0, 0)
	provider.EmitDeviceTemperature(ctx, labels, SensorKindBoard, 0, 0)
	provider.EmitDevicePoolConnected(ctx, labels, false)
	provider.EmitCommand(ctx, CommandLabels{Kind: "reboot", Result: ResultSuccess})
	provider.EmitTelemetryPoll(ctx, TelemetryPollLabels{Result: ResultSuccess})

	require.NoError(t, provider.Shutdown(ctx))
}

// guards against a nil DB when metrics are enabled — the writer cannot run
// without a real connection, so the constructor must refuse rather than
// silently dropping rows at flush time.
func TestSetupEnabledRequiresDB(t *testing.T) {
	_, err := Setup(context.Background(), "test", Config{Enabled: true}, nil)
	require.Error(t, err)
}
