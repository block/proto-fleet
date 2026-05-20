package metrics

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Sample is one observation about to be persisted.
type Sample struct {
	Time   time.Time
	Metric string
	Labels Labels
	Value  float64
}

// Labels carries the contract label set.
type Labels struct {
	OrganizationID string
	DeviceID       string
	DeviceGroup    string
	Driver         string
	SensorKind     string
	Kind           string
	Result         string
}

// Store persists batches of samples.
type Store interface {
	InsertSamples(ctx context.Context, samples []Sample) error
	Close() error
}

type sqlStore struct {
	db *sql.DB
}

func NewSQLStore(db *sql.DB) Store {
	return &sqlStore{db: db}
}

var insertColumns = []string{
	"time",
	"metric",
	"organization_id",
	"device_id",
	"device_group",
	"driver",
	"sensor_kind",
	"kind",
	"result",
	"value",
}

const columnsPerSample = 10

func (s *sqlStore) InsertSamples(ctx context.Context, samples []Sample) error {
	if len(samples) == 0 {
		return nil
	}

	// Building one parameterised statement per flush is fine at the
	// expected sample rate (a few hundred per 15s window for an
	// average fleet). If profiling later shows this as a bottleneck,
	// COPY FROM via pgx would be the next step.
	var b strings.Builder
	b.WriteString("INSERT INTO notification_metric_sample (")
	b.WriteString(strings.Join(insertColumns, ", "))
	b.WriteString(") VALUES ")

	args := make([]any, 0, len(samples)*columnsPerSample)
	for i, sample := range samples {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("(")
		for j := range columnsPerSample {
			if j > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "$%d", len(args)+j+1)
		}
		b.WriteString(")")
		args = append(args,
			sample.Time,
			sample.Metric,
			sample.Labels.OrganizationID,
			sample.Labels.DeviceID,
			sample.Labels.DeviceGroup,
			sample.Labels.Driver,
			sample.Labels.SensorKind,
			sample.Labels.Kind,
			sample.Labels.Result,
			sample.Value,
		)
	}

	if _, err := s.db.ExecContext(ctx, b.String(), args...); err != nil {
		return fmt.Errorf("insert %d metric samples: %w", len(samples), err)
	}
	return nil
}

// Close is a no-op — the *sql.DB is owned by the caller.
func (s *sqlStore) Close() error { return nil }

// inMemoryStore is the test double exposed to provider_test.go. It
// records every sample handed to InsertSamples so tests can assert on
// metric names and label sets without a real TimescaleDB.
type inMemoryStore struct {
	mu      sync.Mutex
	samples []Sample
	err     error
}

// NewInMemoryStore returns a Store that captures samples in a
// goroutine-safe slice. Reserved for tests.
func NewInMemoryStore() *inMemoryStore { //nolint:revive // intentional return of internal type
	return &inMemoryStore{}
}

func (s *inMemoryStore) InsertSamples(_ context.Context, samples []Sample) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	// Copy the slice so the caller can re-use its backing array safely.
	copied := make([]Sample, len(samples))
	copy(copied, samples)
	s.samples = append(s.samples, copied...)
	return nil
}

func (s *inMemoryStore) Close() error { return nil }

// SetError causes subsequent InsertSamples calls to return err. Used
// by tests that exercise the flusher's error-handling path.
func (s *inMemoryStore) SetError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

// Snapshot returns a copy of every sample seen so far.
func (s *inMemoryStore) Snapshot() []Sample {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Sample, len(s.samples))
	copy(out, s.samples)
	return out
}
